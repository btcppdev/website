// Package alertsstripe implements the Stripe subscription boundary for Alerts.
// Account ownership and checkout idempotency must be persisted by the caller
// before invoking this package. Browser redirects never establish paid access.
package alertsstripe

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"btcpp-web/internal/servicebilling"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	checkout "github.com/stripe/stripe-go/v86/checkout/session"
	stripeInvoice "github.com/stripe/stripe-go/v86/invoice"
	stripePrice "github.com/stripe/stripe-go/v86/price"
	stripeSubscription "github.com/stripe/stripe-go/v86/subscription"
)

var ErrCheckout = errors.New("invalid Alerts Stripe checkout")
var ErrReconcile = errors.New("existing Stripe checkout must be reconciled")

// Intent is a frozen, server-owned checkout request, not an HTTP request DTO.
// Reuse every field (including CreatedAt) when retrying an ambiguous creation.
type Intent struct {
	ID            string
	PersonID      string
	Email         string
	PaidSlots     int64
	CreatedAt     time.Time
	ReturnBaseURL string
}

type Client struct {
	Subscription func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error)
	ListInvoices func(*stripe.InvoiceListParams) (*stripe.InvoiceList, error)
	Invoice      func(string, *stripe.InvoiceParams) (*stripe.Invoice, error)
	Price        func(string, *stripe.PriceParams) (*stripe.Price, error)
	Create       func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	Retrieve     func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	Live         bool
}

func NewClient(live bool) Client {
	return Client{Create: checkout.New, Retrieve: checkout.Get, Invoice: stripeInvoice.Get, Price: stripePrice.Get, Subscription: stripeSubscription.Get, Live: live, ListInvoices: func(params *stripe.InvoiceListParams) (*stripe.InvoiceList, error) {
		params.Single = true
		iter := stripeInvoice.List(params)
		for iter.Next() {
		}
		if err := iter.Err(); err != nil {
			return nil, err
		}
		return iter.InvoiceList(), nil
	}}
}

func checkoutParams(ctx context.Context, in Intent, now time.Time) (*stripe.CheckoutSessionParams, error) {
	if _, err := uuid.Parse(in.ID); err != nil {
		return nil, ErrCheckout
	}
	if _, err := uuid.Parse(in.PersonID); err != nil {
		return nil, ErrCheckout
	}
	address, err := mail.ParseAddress(in.Email)
	if err != nil || address.Address != in.Email || len(in.Email) > 254 {
		return nil, ErrCheckout
	}
	if _, err = servicebilling.AlertsTerm("stripe", in.PaidSlots, 1, in.CreatedAt); err != nil {
		return nil, ErrCheckout
	}
	// Stripe permits sessions lasting 30 minutes to 24 hours. A fixed two-hour
	// expiry permits retry for 90 minutes without changing idempotent parameters.
	if in.CreatedAt.After(now.Add(time.Minute)) || !now.Before(in.CreatedAt.Add(90*time.Minute)) {
		return nil, ErrReconcile
	}
	base, err := url.Parse(in.ReturnBaseURL)
	if err != nil || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Path != "" || base.Opaque != "" || base.Host == "" || (base.Scheme != "https" && !(base.Scheme == "http" && (base.Hostname() == "localhost" || base.Hostname() == "127.0.0.1"))) {
		return nil, ErrCheckout
	}
	metadata := map[string]string{"product": "alerts.repository-slots", "checkout_intent": in.ID, "account_id": in.PersonID}
	p := &stripe.CheckoutSessionParams{
		Mode:                     stripe.String("subscription"),
		ClientReferenceID:        stripe.String(in.ID),
		CustomerEmail:            stripe.String(in.Email),
		SuccessURL:               stripe.String(in.ReturnBaseURL + "/services/alerts/checkout/" + in.ID + "?result=returned"),
		CancelURL:                stripe.String(in.ReturnBaseURL + "/services/alerts/checkout/" + in.ID + "?result=cancelled"),
		ExpiresAt:                stripe.Int64(in.CreatedAt.Add(2 * time.Hour).Unix()),
		BillingAddressCollection: stripe.String("required"),
		AutomaticTax:             &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(true)},
		AllowPromotionCodes:      stripe.Bool(false),
		PaymentMethodTypes:       stripe.StringSlice([]string{"card"}),
		Metadata:                 metadata,
		SubscriptionData:         &stripe.CheckoutSessionSubscriptionDataParams{Metadata: map[string]string{"product": metadata["product"], "checkout_intent": in.ID, "account_id": in.PersonID}},
		LineItems: []*stripe.CheckoutSessionLineItemParams{{Quantity: stripe.Int64(in.PaidSlots), PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
			Currency: stripe.String("usd"), UnitAmount: stripe.Int64(servicebilling.AlertsMonthlySlotCents), TaxBehavior: stripe.String("inclusive"),
			Recurring:   &stripe.CheckoutSessionLineItemPriceDataRecurringParams{Interval: stripe.String("month"), IntervalCount: stripe.Int64(1)},
			ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{Name: stripe.String("Bitcoin++ Alerts additional repositories"), TaxCode: stripe.String(servicebilling.AlertsStripeTaxCode)},
		}}},
	}
	p.Context = ctx
	p.SetIdempotencyKey("alerts-checkout:" + in.ID)
	return p, nil
}

// Open creates or retrieves a hosted session. A saved reference is lookup-only,
// including expired/completed sessions; it never silently starts a replacement.
func (c Client) Open(ctx context.Context, in Intent, reference string, now time.Time) (string, string, error) {
	var session *stripe.CheckoutSession
	var err error
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if reference != "" {
		if !strings.HasPrefix(reference, "cs_") || len(reference) > 255 || c.Retrieve == nil {
			return "", "", ErrCheckout
		}
		params := &stripe.CheckoutSessionParams{}
		params.Context = ctx
		session, err = c.Retrieve(reference, params)
	} else {
		params, e := checkoutParams(ctx, in, now)
		if e != nil {
			return "", "", e
		}
		if c.Create == nil {
			return "", "", ErrCheckout
		}
		session, err = c.Create(params)
	}
	if err != nil {
		return "", "", err
	}
	if session == nil || !strings.HasPrefix(session.ID, "cs_") || (reference != "" && session.ID != reference) || session.Mode != "subscription" || session.Livemode != c.Live || session.ClientReferenceID != in.ID || session.Metadata["account_id"] != in.PersonID || session.Metadata["checkout_intent"] != in.ID || session.Metadata["product"] != "alerts.repository-slots" {
		return "", "", ErrCheckout
	}
	// Preserve the authenticated session ID even when no longer payable, allowing
	// the caller to reconcile its invoice instead of making a second subscription.
	if session.Status != "open" || session.ExpiresAt <= now.Unix() {
		return session.ID, "", ErrReconcile
	}
	target, e := url.Parse(session.URL)
	if e != nil || target.Scheme != "https" || target.Host != "checkout.stripe.com" || target.User != nil || target.Path != "/c/pay/"+session.ID {
		return session.ID, "", ErrCheckout
	}
	return session.ID, session.URL, nil
}

// ValidateIntent validates a frozen checkout before it is reserved in storage.
func ValidateIntent(in Intent, now time.Time) error {
	_, err := checkoutParams(context.Background(), in, now)
	return err
}
