package alertsstripe

import (
	"btcpp-web/internal/servicebilling"
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// DecreasePreview is a review of next-renewal terms, never a mutation or grant.
// Zero paid slots uses end-of-period cancellation instead of a zero-item plan.
type DecreasePreview struct {
	PreviousSlots, PaidSlots int64
	PeriodStart, EffectiveAt int64
	MonthlyCents             int64
	ExpiresAt                time.Time
}

// AttachDecreaseSchedule migrates a subscription without changing its terms.
// The caller must durably reserve id before calling and persist the returned
// reference before changing phases. Retry an ambiguous response with the same
// id: never adopt the subscription's existing schedule as proof of ownership.
func (c Client) AttachDecreaseSchedule(ctx context.Context, b Binding, id string, q DecreasePreview, now time.Time) (string, error) {
	return c.attachDecreaseSchedule(ctx, b, id, q, now, now)
}

// ResumeDecreaseSchedule uses a database-recorded first attempt, never a browser
// timestamp. Consent must have been valid then. Stop before Stripe can prune the
// idempotency key (24h), and before the reviewed renewal boundary.
func (c Client) ResumeDecreaseSchedule(ctx context.Context, b Binding, id string, q DecreasePreview, firstAttempt, now time.Time) (string, error) {
	if firstAttempt.IsZero() || firstAttempt.After(now) || !now.Before(firstAttempt.Add(23*time.Hour)) || now.Unix() >= q.EffectiveAt {
		return "", ErrReconcile
	}
	return c.attachDecreaseSchedule(ctx, b, id, q, firstAttempt, now)
}

func (c Client) attachDecreaseSchedule(ctx context.Context, b Binding, id string, q DecreasePreview, validatedAt, now time.Time) (string, error) {
	if c.CreateSchedule == nil || c.Subscription == nil || c.Live != b.Live {
		return "", ErrSubscription
	}
	if _, err := decreaseScheduleParams(ctx, b, id, q, validatedAt); err != nil {
		return "", err
	}
	if now.Unix() >= q.EffectiveAt {
		return "", ErrReconcile
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p := &stripe.SubscriptionParams{}
	p.Context = ctx
	p.AddExpand("items.data.price.product")
	sub, err := c.Subscription(b.SubscriptionID, p)
	if err != nil {
		return "", err
	}
	state, err := verifySubscription(sub, b)
	if err != nil || state.State != "active" || state.ProviderStatus != "active" || state.PeriodStart != q.PeriodStart || state.PeriodEnd != q.EffectiveAt || sub.PendingUpdate != nil || sub.CollectionMethod != "charge_automatically" || len(sub.Discounts) != 0 || len(sub.Items.Data[0].Discounts) != 0 {
		return "", ErrSubscription
	}
	// Stripe's from_subscription call cannot include phase changes or metadata.
	// An existing schedule is allowed only so this exact creation can be replayed
	// after a lost response; Stripe rejects a new migration of a scheduled sub.
	create := &stripe.SubscriptionScheduleParams{FromSubscription: stripe.String(b.SubscriptionID)}
	create.Context = ctx
	create.SetIdempotencyKey("alerts-decrease-create:" + id)
	s, err := c.CreateSchedule(create)
	if err != nil {
		return "", err
	}
	if s == nil || !strings.HasPrefix(s.ID, "sub_sched_") || s.Livemode != b.Live || s.Status != "active" || s.EndBehavior != "release" || s.Subscription == nil || s.Subscription.ID != b.SubscriptionID || s.Customer == nil || s.Customer.ID != b.CustomerID || s.CurrentPhase == nil || s.CurrentPhase.StartDate != q.PeriodStart || s.CurrentPhase.EndDate != q.EffectiveAt || len(s.Phases) != 1 {
		return "", ErrSubscription
	}
	if sub.Schedule != nil && sub.Schedule.ID != s.ID {
		return "", ErrSubscription
	}
	phase := s.Phases[0]
	if phase == nil || phase.StartDate != q.PeriodStart || phase.EndDate != q.EffectiveAt || len(phase.Items) != 1 || phase.Items[0] == nil || phase.Items[0].Price == nil || phase.Items[0].Price.ID != b.PriceID || phase.Items[0].Quantity != b.PaidSlots {
		return "", ErrSubscription
	}
	return s.ID, nil
}

func (c Client) PreviewDecrease(ctx context.Context, b Binding, slots int64, now time.Time) (*DecreasePreview, error) {
	if c.Subscription == nil || c.Live != b.Live || slots < 0 || slots >= b.PaidSlots {
		return nil, ErrSubscription
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p := &stripe.SubscriptionParams{}
	p.Context = ctx
	p.AddExpand("items.data.price.product")
	sub, err := c.Subscription(b.SubscriptionID, p)
	if err != nil {
		return nil, err
	}
	state, err := verifySubscription(sub, b)
	if err != nil || state.State != "active" || state.ProviderStatus != "active" || state.PeriodStart > now.Unix() || state.PeriodEnd <= now.Unix() || sub.Schedule != nil || sub.PendingUpdate != nil || sub.CollectionMethod != "charge_automatically" || len(sub.Discounts) != 0 || len(sub.Items.Data[0].Discounts) != 0 {
		return nil, ErrSubscription
	}
	now = now.UTC().Truncate(time.Second)
	out := &DecreasePreview{PreviousSlots: b.PaidSlots, PaidSlots: slots, PeriodStart: state.PeriodStart, EffectiveAt: state.PeriodEnd, MonthlyCents: slots * servicebilling.AlertsMonthlySlotCents, ExpiresAt: now.Add(10 * time.Minute)}
	if end := time.Unix(state.PeriodEnd, 0).UTC(); out.ExpiresAt.After(end) {
		out.ExpiresAt = end
	}
	return out, nil
}

// decreaseScheduleParams is for an already attached and verified schedule. The
// coordinator must first persist consent and its schedule reference. A schedule
// update never changes today's quantity or creates an immediate credit/charge.
func decreaseScheduleParams(ctx context.Context, b Binding, id string, q DecreasePreview, now time.Time) (*stripe.SubscriptionScheduleParams, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrSubscription
	}
	if q.PreviousSlots != b.PaidSlots || q.PaidSlots < 1 || q.PaidSlots >= b.PaidSlots || q.MonthlyCents != q.PaidSlots*servicebilling.AlertsMonthlySlotCents || q.PeriodStart <= 0 || q.PeriodStart > now.Unix() || q.EffectiveAt <= now.Unix() || !now.Before(q.ExpiresAt) || q.ExpiresAt.Unix() > q.EffectiveAt {
		return nil, ErrSubscription
	}
	phase := func(slots int64) *stripe.SubscriptionSchedulePhaseParams {
		return &stripe.SubscriptionSchedulePhaseParams{
			Items:    []*stripe.SubscriptionSchedulePhaseItemParams{{Price: stripe.String(b.PriceID), Quantity: stripe.Int64(slots)}},
			Currency: stripe.String("usd"), CollectionMethod: stripe.String("charge_automatically"),
			AutomaticTax:      &stripe.SubscriptionSchedulePhaseAutomaticTaxParams{Enabled: stripe.Bool(true)},
			ProrationBehavior: stripe.String("none"),
			Metadata:          map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "product": "alerts.repository-slots"},
		}
	}
	current, next := phase(q.PreviousSlots), phase(q.PaidSlots)
	current.StartDate = stripe.Int64(q.PeriodStart)
	current.EndDate = stripe.Int64(q.EffectiveAt)
	next.StartDate = stripe.Int64(q.EffectiveAt)
	next.Duration = &stripe.SubscriptionSchedulePhaseDurationParams{Interval: stripe.String("month"), IntervalCount: stripe.Int64(1)}
	p := &stripe.SubscriptionScheduleParams{EndBehavior: stripe.String("release"), ProrationBehavior: stripe.String("none"), Phases: []*stripe.SubscriptionSchedulePhaseParams{current, next}, Metadata: map[string]string{"account_id": b.PersonID, "checkout_intent": b.IntentID, "alerts_decrease": id}}
	p.Context = ctx
	p.SetIdempotencyKey("alerts-decrease-update:" + id)
	return p, nil
}
