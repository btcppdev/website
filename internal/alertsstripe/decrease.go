package alertsstripe

import (
	"btcpp-web/internal/servicebilling"
	"context"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
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
