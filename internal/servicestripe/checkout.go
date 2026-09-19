package servicestripe

import (
	"btcpp-web/internal/servicebilling"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

var ErrCheckout = errors.New("invalid service Stripe checkout")
var ErrReconcile = errors.New("existing Stripe checkout must be reconciled")

// CheckoutIntent is frozen trusted server output. ReturnOrigin must first be
// authorized for the calling product; it is not a browser-supplied allowlist.
type CheckoutIntent struct {
	ID, PersonID, Email, ProductCode, TaxCode string
	Quantity                                  int64
	CreatedAt                                 time.Time
	ReturnOrigin, SuccessURL, CancelURL       string
}
type CheckoutClient struct {
	Create   func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	Retrieve func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	Live     bool
}

func CheckoutParams(ctx context.Context, in CheckoutIntent, now time.Time) (*stripe.CheckoutSessionParams, error) {
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
	plan, _, err := servicebilling.ProductTerm(in.ProductCode, "stripe", in.Quantity, in.CreatedAt)
	if err != nil {
		return nil, ErrCheckout
	}
	// Stripe permits sessions lasting 30 minutes to 24 hours. A fixed two-hour
	// expiry permits retry for 90 minutes without changing idempotent parameters.
	if in.CreatedAt.After(now.Add(time.Minute)) || !now.Before(in.CreatedAt.Add(90*time.Minute)) {
		return nil, ErrReconcile
	}
	base, err := url.Parse(in.ReturnOrigin)
	if err != nil || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Path != "" || base.Opaque != "" || base.Host == "" || (base.Scheme != "https" && !(base.Scheme == "http" && (base.Hostname() == "localhost" || base.Hostname() == "127.0.0.1"))) {
		return nil, ErrCheckout
	}
	if in.TaxCode == "" || (plan.StripeTaxCode != "" && in.TaxCode != plan.StripeTaxCode) {
		return nil, ErrCheckout
	}
	for _, raw := range []string{in.SuccessURL, in.CancelURL} {
		target, e := url.Parse(raw)
		if e != nil || target.User != nil || target.Fragment != "" || target.Opaque != "" || target.Scheme != base.Scheme || target.Host != base.Host || !strings.HasPrefix(target.Path, "/") {
			return nil, ErrCheckout
		}
	}
	interval := "month"
	if plan.Months == 12 {
		interval = "year"
	}
	metadata := map[string]string{"product": plan.Code, "checkout_intent": in.ID, "account_id": in.PersonID}
	p := &stripe.CheckoutSessionParams{
		Mode:                     stripe.String("subscription"),
		ClientReferenceID:        stripe.String(in.ID),
		CustomerEmail:            stripe.String(in.Email),
		SuccessURL:               stripe.String(in.SuccessURL),
		CancelURL:                stripe.String(in.CancelURL),
		ExpiresAt:                stripe.Int64(in.CreatedAt.Add(2 * time.Hour).Unix()),
		BillingAddressCollection: stripe.String("required"),
		AutomaticTax:             &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(true)},
		AllowPromotionCodes:      stripe.Bool(false),
		PaymentMethodTypes:       stripe.StringSlice([]string{"card"}),
		Metadata:                 metadata,
		SubscriptionData:         &stripe.CheckoutSessionSubscriptionDataParams{Metadata: map[string]string{"product": metadata["product"], "checkout_intent": in.ID, "account_id": in.PersonID}},
		LineItems: []*stripe.CheckoutSessionLineItemParams{{Quantity: stripe.Int64(in.Quantity), PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
			Currency: stripe.String(plan.Currency), UnitAmount: stripe.Int64(plan.UnitCents), TaxBehavior: stripe.String(plan.TaxBehavior),
			Recurring:   &stripe.CheckoutSessionLineItemPriceDataRecurringParams{Interval: stripe.String(interval), IntervalCount: stripe.Int64(1)},
			ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{Name: stripe.String(plan.Name), TaxCode: stripe.String(in.TaxCode)},
		}}},
	}
	p.Context = ctx
	prefix := "service-checkout:"
	if plan.Family == "alerts" {
		prefix = "alerts-checkout:"
	}
	p.SetIdempotencyKey(prefix + in.ID)
	return p, nil
}

// Open creates or retrieves a hosted session. A saved reference is lookup-only,
// including expired/completed sessions; it never silently starts a replacement.
func (c CheckoutClient) Open(ctx context.Context, in CheckoutIntent, reference string, now time.Time) (string, string, error) {
	// Validate frozen identity/plan even for a lookup; creation-age limits do
	// not prevent looking up a legitimately expired saved checkout.
	if _, err := CheckoutParams(ctx, in, in.CreatedAt); err != nil {
		return "", "", err
	}
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
		params, e := CheckoutParams(ctx, in, now)
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
	if session == nil || !strings.HasPrefix(session.ID, "cs_") || (reference != "" && session.ID != reference) || session.Mode != "subscription" || session.Livemode != c.Live || session.ClientReferenceID != in.ID || session.Metadata["account_id"] != in.PersonID || session.Metadata["checkout_intent"] != in.ID || session.Metadata["product"] != in.ProductCode {
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
