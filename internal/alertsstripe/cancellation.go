package alertsstripe

import (
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"time"
)

// CancellationIntent must be stored by the account-owned coordinator before
// provider I/O. Reuse the same intent after ambiguous failures; never derive it
// from a webhook. PeriodEnd binds confirmation to the reviewed billing period.
type CancellationIntent struct {
	ID        string
	PeriodEnd int64
	CreatedAt time.Time
}

// ScheduleCancellation stops future renewals without changing quantities,
// billing dates, paid grants, or issuing a refund. The caller must serialize
// account billing changes and reconcile the resulting provider state.
func (c Client) ScheduleCancellation(ctx context.Context, b Binding, in CancellationIntent, now time.Time) (SubscriptionState, error) {
	var empty SubscriptionState
	if _, err := uuid.Parse(in.ID); err != nil {
		return empty, ErrSubscription
	}
	if c.Subscription == nil || c.UpdateSubscription == nil || c.Live != b.Live || in.CreatedAt.IsZero() || in.CreatedAt.After(now.Add(time.Minute)) || !now.Before(in.CreatedAt.Add(23*time.Hour)) || in.PeriodEnd <= now.Unix() {
		return empty, ErrSubscription
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	get := &stripe.SubscriptionParams{}
	get.Context = ctx
	get.AddExpand("items.data.price.product")
	current, err := c.Subscription(b.SubscriptionID, get)
	if err != nil {
		return empty, err
	}
	state, err := verifySubscription(current, b)
	if err != nil {
		return empty, err
	}
	if state.PeriodEnd != in.PeriodEnd || current.Schedule != nil || current.PendingUpdate != nil {
		return empty, ErrSubscription
	}
	switch state.ProviderStatus {
	case "active", "trialing", "past_due", "unpaid", "paused":
	default:
		return empty, ErrSubscription
	}
	// An already verified cancellation needs no second mutation. Do not replace
	// a custom cancellation date with a different end-of-period instruction.
	if state.CancelAtPeriodEnd && (state.CancelAt == 0 || state.CancelAt == in.PeriodEnd) {
		return state, nil
	}
	if state.CancelAt != 0 {
		return empty, ErrSubscription
	}
	p := &stripe.SubscriptionParams{CancelAtPeriodEnd: stripe.Bool(true)}
	p.Context = ctx
	p.SetIdempotencyKey("alerts-cancel:" + in.ID)
	p.AddExpand("items.data.price.product")
	updated, err := c.UpdateSubscription(b.SubscriptionID, p)
	if err != nil {
		return empty, err
	}
	result, err := verifySubscription(updated, b)
	if err != nil || !result.CancelAtPeriodEnd || result.PeriodEnd != in.PeriodEnd || result.PeriodStart != state.PeriodStart || (result.CancelAt != 0 && result.CancelAt != in.PeriodEnd) {
		return empty, ErrSubscription
	}
	return result, nil
}
