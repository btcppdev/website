package alertsstripe

import (
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// WithdrawDecreaseSchedule requires a saved customer withdrawal instruction.
// Releasing stops future phases and leaves the current subscription in place.
// Never use Stripe schedule cancellation here: that would end the subscription.
func (c Client) WithdrawDecreaseSchedule(ctx context.Context, b Binding, quoteID, withdrawalID, scheduleID string, q DecreasePreview, requestedAt, now time.Time) (SubscriptionState, error) {
	var empty SubscriptionState
	if _, err := uuid.Parse(withdrawalID); err != nil {
		return empty, ErrSubscription
	}
	if _, err := uuid.Parse(quoteID); err != nil {
		return empty, ErrSubscription
	}
	if c.Schedule == nil || c.Subscription == nil || c.ReleaseSchedule == nil || c.Live != b.Live || !strings.HasPrefix(scheduleID, "sub_sched_") || requestedAt.IsZero() || requestedAt.After(now) || requestedAt.Unix() >= q.EffectiveAt || q.PreviousSlots != b.PaidSlots {
		return empty, ErrSubscription
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	get := &stripe.SubscriptionScheduleParams{}
	get.Context = ctx
	s, err := c.Schedule(scheduleID, get)
	if err != nil {
		return empty, err
	}
	if s == nil || s.ID != scheduleID || s.Livemode != b.Live || s.Customer == nil || s.Customer.ID != b.CustomerID || s.EndBehavior != "release" || !verifiedDecreasePhases(s, b, quoteID, q) {
		return empty, ErrSubscription
	}
	readSubscription := func() (SubscriptionState, *stripe.Subscription, error) {
		p := &stripe.SubscriptionParams{}
		p.Context = ctx
		p.AddExpand("items.data.price.product")
		sub, e := c.Subscription(b.SubscriptionID, p)
		if e != nil {
			return empty, nil, e
		}
		state, e := verifySubscription(sub, b)
		if e != nil || state.State != "active" || state.ProviderStatus != "active" || state.PeriodStart != q.PeriodStart || state.PeriodEnd != q.EffectiveAt || sub.PendingUpdate != nil {
			return empty, nil, ErrSubscription
		}
		return state, sub, nil
	}
	if s.Status == "active" {
		if !decreaseScheduleIdentity(s, b, scheduleID, q) || !now.Before(requestedAt.Add(23*time.Hour)) || now.Add(30*time.Second).Unix() >= q.EffectiveAt {
			return empty, ErrReconcile
		}
		_, sub, e := readSubscription()
		if e != nil {
			return empty, e
		}
		if sub.Schedule == nil || sub.Schedule.ID != scheduleID {
			return empty, ErrSubscription
		}
		p := &stripe.SubscriptionScheduleReleaseParams{PreserveCancelDate: stripe.Bool(true)}
		p.Context = ctx
		p.SetIdempotencyKey("alerts-decrease-withdraw:" + withdrawalID)
		s, err = c.ReleaseSchedule(scheduleID, p)
		if err != nil {
			return empty, err
		}
	}
	if s == nil || s.ID != scheduleID || s.Livemode != b.Live || s.Status != "released" || s.Customer == nil || s.Customer.ID != b.CustomerID || s.ReleasedSubscription == nil || s.ReleasedSubscription.ID != b.SubscriptionID || s.ReleasedAt < q.PeriodStart || s.ReleasedAt >= q.EffectiveAt || s.ReleasedAt > now.Add(30*time.Second).Unix() || !verifiedDecreasePhases(s, b, quoteID, q) {
		return empty, ErrSubscription
	}
	state, sub, err := readSubscription()
	if err != nil {
		return empty, err
	}
	if sub.Schedule != nil {
		return empty, ErrSubscription
	}
	return state, nil
}
