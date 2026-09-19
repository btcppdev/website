package servicestripe

import (
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"strconv"
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

// ScheduleDeletedAccountCancellation is only for a persisted account-deletion
// cancellation job. That durable authorization survives the browser consent
// window: each retry verifies current provider state before any mutation. It
// never issues a refund or changes a purchased quantity/period.
func (c SubscriptionClient) ScheduleDeletedAccountCancellation(ctx context.Context, b Binding, now time.Time) (SubscriptionState, error) {
	checkout, err := uuid.Parse(b.IntentID)
	if err != nil || checkout == uuid.Nil || now.IsZero() {
		return SubscriptionState{}, ErrSubscription
	}
	state, err := c.CurrentSubscription(ctx, b)
	if err != nil {
		return SubscriptionState{}, err
	}
	if state.ProviderStatus == "canceled" || state.ProviderStatus == "incomplete_expired" {
		return state, nil
	}
	// A retry after a prolonged outage may observe a newly started period. Use
	// the verified current period, never a stale saved boundary or a new charge.
	// The deterministic key is reused within that period, including lost replies.
	key := uuid.NewSHA1(checkout, []byte("deleted-account-cancel:"+strconv.FormatInt(state.PeriodEnd, 10))).String()
	return c.ScheduleCancellation(ctx, b, CancellationIntent{ID: key, PeriodEnd: state.PeriodEnd, CreatedAt: now}, now)
}

// ScheduleCancellation stops future renewals without changing quantities,
// billing dates, paid grants, or issuing a refund. The caller must serialize
// account billing changes and reconcile the resulting provider state.
func (c SubscriptionClient) ScheduleCancellation(ctx context.Context, b Binding, in CancellationIntent, now time.Time) (SubscriptionState, error) {
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
	state, err := VerifySubscription(current, b)
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
	prefix := "service-cancel:"
	if b.ProductCode == "alerts.repository-slots" {
		prefix = "alerts-cancel:"
	}
	p.SetIdempotencyKey(prefix + in.ID)
	p.AddExpand("items.data.price.product")
	updated, err := c.UpdateSubscription(b.SubscriptionID, p)
	if err != nil {
		return empty, err
	}
	result, err := VerifySubscription(updated, b)
	if err != nil || !result.CancelAtPeriodEnd || result.PeriodEnd != in.PeriodEnd || result.PeriodStart != state.PeriodStart || (result.CancelAt != 0 && result.CancelAt != in.PeriodEnd) {
		return empty, ErrSubscription
	}
	return result, nil
}
