package alertsstripe

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v86"
)

func (c Client) StartCardUpdate(ctx context.Context, b Binding, id, base string) (*stripe.CheckoutSession, error) {
	if _, err := c.CurrentSubscription(ctx, b); err != nil {
		return nil, err
	}
	p := &stripe.CheckoutSessionParams{Mode: stripe.String("setup"), Customer: stripe.String(b.CustomerID), PaymentMethodTypes: stripe.StringSlice([]string{"card"}), ClientReferenceID: stripe.String(id), SuccessURL: stripe.String(strings.TrimRight(base, "/") + "/services/alerts/payment-method?change=" + url.QueryEscape(id)), CancelURL: stripe.String(strings.TrimRight(base, "/") + "/services/alerts/manage"), SetupIntentData: &stripe.CheckoutSessionSetupIntentDataParams{Metadata: map[string]string{"alerts_card_update": id, "subscription": b.SubscriptionID}}}
	p.Context = ctx
	p.Metadata = map[string]string{"purpose": "alerts-card-update"}
	p.SetIdempotencyKey("alerts-card:" + id)
	session, err := c.Create(p)
	if err != nil {
		return nil, err
	}
	if session == nil || session.Mode != "setup" || session.Customer == nil || session.Customer.ID != b.CustomerID || session.ClientReferenceID != id || session.Livemode != b.Live || !strings.HasPrefix(session.ID, "cs_") {
		return nil, ErrSubscription
	}
	u, err := url.Parse(session.URL)
	if err != nil || u.Scheme != "https" || u.Host != "checkout.stripe.com" || u.User != nil {
		return nil, ErrSubscription
	}
	return session, nil
}

// Validate the SetupIntent and customer before using a payment method. A return
// URL or browser-supplied payment-method ID never authorizes a card change.
func (c Client) CardUpdateMethod(ctx context.Context, b Binding, id, reference string) (string, error) {
	p := &stripe.CheckoutSessionParams{}
	p.Context = ctx
	p.AddExpand("setup_intent.payment_method")
	session, err := c.Retrieve(reference, p)
	if err != nil {
		return "", err
	}
	if session == nil || session.ID != reference || session.Livemode != b.Live || session.Mode != "setup" || session.Status != "complete" || session.Customer == nil || session.Customer.ID != b.CustomerID || session.ClientReferenceID != id {
		return "", ErrSubscription
	}
	setup := session.SetupIntent
	if setup == nil || setup.Status != "succeeded" || setup.Livemode != b.Live || setup.Customer == nil || setup.Customer.ID != b.CustomerID || setup.Metadata["alerts_card_update"] != id || setup.Metadata["subscription"] != b.SubscriptionID {
		return "", ErrSubscription
	}
	pm := setup.PaymentMethod
	if pm == nil || pm.Type != "card" || pm.Customer == nil || pm.Customer.ID != b.CustomerID || !strings.HasPrefix(pm.ID, "pm_") {
		return "", ErrSubscription
	}
	return pm.ID, nil
}

func (c Client) ApplyCardUpdate(ctx context.Context, b Binding, id, reference string) error {
	pm, err := c.CardUpdateMethod(ctx, b, id, reference)
	if err != nil {
		return err
	}
	before, err := c.CurrentSubscription(ctx, b)
	if err != nil {
		return err
	}
	if before.ProviderStatus == "canceled" || before.ProviderStatus == "incomplete_expired" {
		return ErrSubscription
	}
	p := &stripe.SubscriptionParams{DefaultPaymentMethod: stripe.String(pm)}
	p.Context = ctx
	p.SetIdempotencyKey("alerts-card-apply:" + id)
	p.AddExpand("items.data.price.product")
	sub, err := c.UpdateSubscription(b.SubscriptionID, p)
	if err != nil {
		return err
	}
	after, err := verifySubscription(sub, b)
	if err != nil || sub.DefaultPaymentMethod == nil || sub.DefaultPaymentMethod.ID != pm || after.PeriodStart != before.PeriodStart || after.PeriodEnd != before.PeriodEnd || after.CancelAtPeriodEnd != before.CancelAtPeriodEnd {
		return ErrSubscription
	}
	return nil
}

func (c Client) ResumeAfterBitcoinSwitch(ctx context.Context, b Binding, id string, end int64) (SubscriptionState, error) {
	p := &stripe.SubscriptionParams{}
	p.Context = ctx
	p.AddExpand("items.data.price.product")
	sub, err := c.Subscription(b.SubscriptionID, p)
	if err != nil {
		return SubscriptionState{}, err
	}
	state, err := verifySubscription(sub, b)
	if err != nil || state.ProviderStatus != "active" || state.PeriodEnd != end || end <= time.Now().Unix() || sub.Schedule != nil || sub.PendingUpdate != nil {
		return SubscriptionState{}, ErrSubscription
	}
	if !state.CancelAtPeriodEnd && state.CancelAt == 0 {
		return state, nil
	}
	if !state.CancelAtPeriodEnd || (state.CancelAt != 0 && state.CancelAt != end) {
		return SubscriptionState{}, ErrSubscription
	}
	update := &stripe.SubscriptionParams{CancelAtPeriodEnd: stripe.Bool(false)}
	update.Context = ctx
	update.SetIdempotencyKey("alerts-switch-withdraw:" + id)
	update.AddExpand("items.data.price.product")
	sub, err = c.UpdateSubscription(b.SubscriptionID, update)
	if err != nil {
		return SubscriptionState{}, err
	}
	after, err := verifySubscription(sub, b)
	if err != nil || after.CancelAtPeriodEnd || after.CancelAt != 0 || after.PeriodEnd != end || after.PeriodStart != state.PeriodStart {
		return SubscriptionState{}, ErrSubscription
	}
	return after, nil
}
