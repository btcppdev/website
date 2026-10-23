package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"context"
	"fmt"
	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/checkout/session"
	"net/url"
	"strings"
	"time"
)

// Use the shared durable attempt and webhook settlement; redirects never confirm
// payment. Idempotency and the original expiry are stable across retries.
func expeditionStripeCheckout(ctx context.Context, app *config.AppContext, buyer string, order *getters.ServiceOrderView) (string, error) {
	if !antarcticaPaymentProviderAllowed(app, expedition.Stripe) {
		return "", fmt.Errorf("expedition Stripe checkout is disabled")
	}
	if order.Quote.ProductCode != expedition.ProductCode || order.Quote.Provider != expedition.Stripe {
		return "", getters.ErrServicePayment
	}
	attempt, err := getters.BeginServicePayment(app, buyer, order.ID)
	if err != nil {
		return "", err
	}
	client := session.Client{B: stripe.GetBackend(stripe.APIBackend), Key: app.Env.StripeKey}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var checkout *stripe.CheckoutSession
	if attempt.Reference != "" {
		params := &stripe.CheckoutSessionParams{}
		params.Context = requestCtx
		checkout, err = client.Get(attempt.Reference, params)
	} else {
		if !attempt.CanCreateCharge || time.Until(order.Quote.ExpiresAt) < 31*time.Minute {
			return "", fmt.Errorf("payment window expired")
		}
		returnURL := strings.TrimRight(app.Env.GetURI(), "/") + "/services/orders/" + url.PathEscape(order.PublicID)
		params := &stripe.CheckoutSessionParams{
			CustomerEmail: stripe.String(attempt.BuyerEmail), Mode: stripe.String("payment"),
			PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
			LineItems:          []*stripe.CheckoutSessionLineItemParams{{Quantity: stripe.Int64(1), PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{Currency: stripe.String("usd"), UnitAmount: stripe.Int64(order.Quote.TotalCents), ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{Name: stripe.String(order.Quote.Name)}}}},
			Metadata:           map[string]string{"checkout-kind": "service", "service-attempt-id": attempt.ID, "service-order-id": order.ID},
			SuccessURL:         stripe.String(returnURL), CancelURL: stripe.String(returnURL),
			AutomaticTax: &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(false)}, ExpiresAt: stripe.Int64(order.Quote.ExpiresAt.Unix()),
		}
		params.Context = requestCtx
		params.SetIdempotencyKey(attempt.ID)
		checkout, err = client.New(params)
	}
	if err != nil {
		return "", err
	}
	if err = validateExpeditionStripeSession(checkout, order, attempt.ID, app.Env.Prod); err != nil {
		return "", err
	}
	if err = getters.BindServicePayment(app, attempt.ID, "stripe", checkout.ID); err != nil {
		return "", err
	}
	return checkout.URL, nil
}
func validateExpeditionStripeSession(s *stripe.CheckoutSession, order *getters.ServiceOrderView, attempt string, live bool) error {
	if s == nil || !strings.HasPrefix(s.ID, "cs_") || s.Mode != "payment" || s.Currency != "usd" || s.AmountTotal != order.Quote.TotalCents || s.Livemode != live || s.Metadata["service-attempt-id"] != attempt || s.Metadata["service-order-id"] != order.ID || s.Metadata["checkout-kind"] != "service" || s.ExpiresAt != order.Quote.ExpiresAt.Unix() || s.Status != "open" {
		return getters.ErrServicePayment
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Scheme != "https" || u.Host != "checkout.stripe.com" || u.User != nil {
		return getters.ErrServicePayment
	}
	return nil
}
