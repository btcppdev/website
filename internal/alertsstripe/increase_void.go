package alertsstripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"time"
)

// VoidedIncrease verifies the invoice can no longer be paid and that Stripe did
// not apply (or retain) the quantity change. Uncollectible is not equivalent to
// void: it can still be paid and therefore cannot release a pending instruction.
func (c Client) VoidedIncrease(ctx context.Context, id string, b Binding, intent IncreaseIntent) (SubscriptionState, error) {
	var empty SubscriptionState
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	observed, err := c.InspectIncreaseInvoice(ctx, id, b, intent)
	if err != nil {
		return empty, err
	}
	if observed.Status != "void" || c.Subscription == nil {
		return empty, ErrReconcile
	}
	p := &stripe.SubscriptionParams{}
	p.Context = ctx
	p.AddExpand("items.data.price.product")
	sub, err := c.Subscription(b.SubscriptionID, p)
	if err != nil {
		return empty, err
	}
	state, err := verifySubscription(sub, b)
	if err != nil {
		return empty, err
	}
	if sub.PendingUpdate != nil || sub.Schedule != nil {
		return empty, ErrReconcile
	}
	return state, nil
}
