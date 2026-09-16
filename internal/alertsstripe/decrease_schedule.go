package alertsstripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// VerifyDecreaseSchedule checks a provider observation against frozen consent.
// Callers must obtain the observation through authenticated provider APIs.
func VerifyDecreaseSchedule(s *stripe.SubscriptionSchedule, b Binding, id, scheduleID string, q DecreasePreview) error {
	if !decreaseScheduleIdentity(s, b, scheduleID, q) || !verifiedDecreasePhases(s, b, id, q) {
		return ErrSubscription
	}
	return nil
}

// SetDecreaseSchedule requires a durably saved attachment reference and first
// attempt timestamp. It changes only future quantity, never current access.
// A previously applied update is recovered by verifying its current phases.
func (c Client) SetDecreaseSchedule(ctx context.Context, b Binding, id, scheduleID string, q DecreasePreview, firstAttempt, now time.Time) (*stripe.SubscriptionSchedule, error) {
	if c.Schedule == nil || c.UpdateSchedule == nil || c.Subscription == nil || c.Live != b.Live || !strings.HasPrefix(scheduleID, "sub_sched_") || firstAttempt.IsZero() || firstAttempt.After(now) || now.Unix() >= q.EffectiveAt {
		return nil, ErrSubscription
	}
	p, err := decreaseScheduleParams(ctx, b, id, q, firstAttempt)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	get := &stripe.SubscriptionScheduleParams{}
	get.Context = ctx
	s, err := c.Schedule(scheduleID, get)
	if err != nil {
		return nil, err
	}
	if !decreaseScheduleIdentity(s, b, scheduleID, q) {
		return nil, ErrSubscription
	}
	subp := &stripe.SubscriptionParams{}
	subp.Context = ctx
	subp.AddExpand("items.data.price.product")
	sub, err := c.Subscription(b.SubscriptionID, subp)
	if err != nil {
		return nil, err
	}
	state, err := verifySubscription(sub, b)
	if err != nil || state.State != "active" || state.ProviderStatus != "active" || state.PeriodStart != q.PeriodStart || state.PeriodEnd != q.EffectiveAt || sub.Schedule == nil || sub.Schedule.ID != scheduleID || sub.PendingUpdate != nil || sub.CollectionMethod != "charge_automatically" || len(sub.Discounts) != 0 || len(sub.Items.Data[0].Discounts) != 0 {
		return nil, ErrSubscription
	}
	if verifiedDecreasePhases(s, b, id, q) {
		return s, nil
	}
	// Once the retry horizon has passed, only read-only recovery is permitted.
	if !now.Before(firstAttempt.Add(23 * time.Hour)) {
		return nil, ErrReconcile
	}
	if len(s.Phases) != 1 || !plainDecreasePhase(s.Phases[0], b, b.PaidSlots) || s.Phases[0].StartDate != q.PeriodStart || s.Phases[0].EndDate != q.EffectiveAt || (s.Metadata["alerts_decrease"] != "" && s.Metadata["alerts_decrease"] != id) {
		return nil, ErrSubscription
	}
	current := s.Phases[0]
	// Keep schedule defaults untouched, and carry phase-specific overrides and
	// metadata into both phases instead of silently dropping them on transition.
	for _, phase := range p.Phases {
		for key, value := range current.Metadata {
			if _, owned := phase.Metadata[key]; !owned {
				phase.Metadata[key] = value
			}
		}
		if current.DefaultPaymentMethod != nil {
			phase.DefaultPaymentMethod = stripe.String(current.DefaultPaymentMethod.ID)
		}
		if current.Description != "" {
			phase.Description = stripe.String(current.Description)
		}
		phase.Items[0].Metadata = current.Items[0].Metadata
	}
	p.Context = ctx
	result, err := c.UpdateSchedule(scheduleID, p)
	if err != nil {
		return nil, err
	}
	if !decreaseScheduleIdentity(result, b, scheduleID, q) || !verifiedDecreasePhases(result, b, id, q) {
		return nil, ErrSubscription
	}
	for _, phase := range result.Phases {
		if current.DefaultPaymentMethod != nil && (phase.DefaultPaymentMethod == nil || phase.DefaultPaymentMethod.ID != current.DefaultPaymentMethod.ID) {
			return nil, ErrSubscription
		}
		if phase.Description != current.Description {
			return nil, ErrSubscription
		}
		for key, value := range current.Metadata {
			if key != "account_id" && key != "checkout_intent" && key != "product" && phase.Metadata[key] != value {
				return nil, ErrSubscription
			}
		}
		for key, value := range current.Items[0].Metadata {
			if phase.Items[0].Metadata[key] != value {
				return nil, ErrSubscription
			}
		}
	}
	return result, nil
}

func decreaseScheduleIdentity(s *stripe.SubscriptionSchedule, b Binding, id string, q DecreasePreview) bool {
	return s != nil && s.ID == id && s.Livemode == b.Live && s.Status == "active" && s.EndBehavior == "release" && s.Subscription != nil && s.Subscription.ID == b.SubscriptionID && s.Customer != nil && s.Customer.ID == b.CustomerID && s.CurrentPhase != nil && s.CurrentPhase.StartDate == q.PeriodStart && s.CurrentPhase.EndDate == q.EffectiveAt && s.Prebilling == nil && (s.BillingBehavior == "" || s.BillingBehavior == "prorate_on_next_phase")
}

// Reject unsupported billing overrides rather than erase them during a phase
// replacement. Payment method, description and metadata are preserved above.
func plainDecreasePhase(p *stripe.SubscriptionSchedulePhase, b Binding, slots int64) bool {
	if p == nil || len(p.Items) != 1 || p.Items[0] == nil || p.Items[0].Price == nil || p.Items[0].Price.ID != b.PriceID || p.Items[0].Quantity != slots || len(p.AddInvoiceItems) != 0 || p.ApplicationFeePercent != 0 || p.BillingThresholds != nil || (p.BillingCycleAnchor != "" && p.BillingCycleAnchor != "automatic") || (p.CollectionMethod != nil && *p.CollectionMethod != "charge_automatically") || (p.Currency != "" && p.Currency != "usd") || len(p.DefaultTaxRates) != 0 || len(p.Discounts) != 0 || p.InvoiceSettings != nil || p.OnBehalfOf != nil || p.PauseCollection != nil || p.TransferData != nil || p.Trial || p.TrialEnd != 0 || p.TrialSettings != nil || p.TrialContinuation != "" {
		return false
	}
	item := p.Items[0]
	return item.BillingThresholds == nil && len(item.Discounts) == 0 && len(item.TaxRates) == 0 && item.Trial == nil && item.TrialOffer == ""
}

func verifiedDecreasePhases(s *stripe.SubscriptionSchedule, b Binding, id string, q DecreasePreview) bool {
	if len(s.Phases) != 2 || s.Metadata["alerts_decrease"] != id || s.Metadata["account_id"] != b.PersonID || s.Metadata["checkout_intent"] != b.IntentID {
		return false
	}
	for i, p := range s.Phases {
		slots := q.PreviousSlots
		if i == 1 {
			slots = q.PaidSlots
		}
		if !plainDecreasePhase(p, b, slots) || p.ProrationBehavior != "none" || p.AutomaticTax == nil || !p.AutomaticTax.Enabled || p.Currency != "usd" || p.CollectionMethod == nil || *p.CollectionMethod != "charge_automatically" || p.Metadata["account_id"] != b.PersonID || p.Metadata["checkout_intent"] != b.IntentID || p.Metadata["product"] != "alerts.repository-slots" {
			return false
		}
	}
	return s.Phases[0].StartDate == q.PeriodStart && s.Phases[0].EndDate == q.EffectiveAt && s.Phases[1].StartDate == q.EffectiveAt && s.Phases[1].EndDate > q.EffectiveAt
}
