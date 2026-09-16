package alertsstripe

import (
	"btcpp-web/internal/servicebilling"
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// DecreaseTransition changes the expected recurring quantity, not entitlement.
// Access still requires a verified paid invoice or eligible renewal grace.
type DecreaseTransition struct {
	Binding     Binding
	State       SubscriptionState
	ScheduleID  string
	EffectiveAt int64
}

// ObserveDecreaseTransition verifies that a saved schedule has reached its lower
// quantity phase (or released afterward). A clock boundary alone is insufficient.
func (c Client) ObserveDecreaseTransition(ctx context.Context, b Binding, id, scheduleID string, q DecreasePreview, now time.Time) (*DecreaseTransition, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrSubscription
	}
	if c.Schedule == nil || c.Subscription == nil || c.Live != b.Live || !strings.HasPrefix(scheduleID, "sub_sched_") || q.PreviousSlots != b.PaidSlots || q.PaidSlots < 1 || q.PaidSlots >= q.PreviousSlots || q.MonthlyCents != q.PaidSlots*servicebilling.AlertsMonthlySlotCents || q.PeriodStart <= 0 || q.EffectiveAt <= q.PeriodStart || now.Unix() < q.EffectiveAt {
		return nil, ErrSubscription
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p := &stripe.SubscriptionScheduleParams{}
	p.Context = ctx
	s, err := c.Schedule(scheduleID, p)
	if err != nil {
		return nil, err
	}
	if s == nil || s.ID != scheduleID || s.Livemode != b.Live || s.Customer == nil || s.Customer.ID != b.CustomerID || s.EndBehavior != "release" || s.Prebilling != nil || !verifiedDecreasePhases(s, b, id, q) {
		return nil, ErrSubscription
	}
	switch s.Status {
	case "active":
		if s.Subscription == nil || s.Subscription.ID != b.SubscriptionID || s.CurrentPhase == nil || s.CurrentPhase.StartDate != q.EffectiveAt || s.CurrentPhase.EndDate != s.Phases[1].EndDate {
			return nil, ErrSubscription
		}
	case "released":
		if s.ReleasedSubscription == nil || s.ReleasedSubscription.ID != b.SubscriptionID || s.ReleasedAt < q.EffectiveAt || s.ReleasedAt > now.Unix() {
			return nil, ErrSubscription
		}
	case "completed":
		if s.CompletedAt < s.Phases[1].EndDate || s.CompletedAt > now.Unix() || (s.Subscription != nil && s.Subscription.ID != b.SubscriptionID) || (s.ReleasedSubscription != nil && s.ReleasedSubscription.ID != b.SubscriptionID) {
			return nil, ErrSubscription
		}
	default:
		return nil, ErrSubscription
	}
	next := b
	next.PaidSlots = q.PaidSlots
	subp := &stripe.SubscriptionParams{}
	subp.Context = ctx
	subp.AddExpand("items.data.price.product")
	sub, err := c.Subscription(b.SubscriptionID, subp)
	if err != nil {
		return nil, err
	}
	state, err := verifySubscription(sub, next)
	if err != nil || state.PeriodStart < q.EffectiveAt || state.PeriodStart > now.Unix() || sub.PendingUpdate != nil || len(sub.Discounts) != 0 || len(sub.Items.Data[0].Discounts) != 0 || sub.CollectionMethod != "charge_automatically" {
		return nil, ErrSubscription
	}
	if s.Status == "active" && (sub.Schedule == nil || sub.Schedule.ID != scheduleID || state.PeriodStart != q.EffectiveAt) {
		return nil, ErrSubscription
	}
	if (s.Status == "released" || s.Status == "completed") && sub.Schedule != nil {
		return nil, ErrSubscription
	}
	return &DecreaseTransition{Binding: next, State: state, ScheduleID: scheduleID, EffectiveAt: q.EffectiveAt}, nil
}
