package alertsstripe

import (
	"context"
	"errors"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

var ErrSubscription = errors.New("Stripe subscription does not match the saved Alerts plan")

// SubscriptionState does not grant access or establish renewal failure. An
// unpaid renewal still needs its invoice and previous paid period verified.
type SubscriptionState struct {
	State                  string `json:"state"`
	ProviderStatus         string `json:"providerStatus"`
	CancellationReason     string `json:"cancellationReason"`
	CancelAtPeriodEnd      bool   `json:"cancelAtPeriodEnd"`
	CollectionPaused       bool   `json:"collectionPaused"`
	CancelAt               int64  `json:"cancelAt"`
	PeriodStart, PeriodEnd int64
	LatestInvoice          string `json:"latestInvoice"`
}

func (c Client) CurrentSubscription(ctx context.Context, b Binding) (SubscriptionState, error) {
	var out SubscriptionState
	if c.Subscription == nil || b.Live != c.Live || !strings.HasPrefix(b.SubscriptionID, "sub_") {
		return out, ErrSubscription
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p := &stripe.SubscriptionParams{}
	p.Context = ctx
	p.AddExpand("items.data.price.product")
	s, err := c.Subscription(b.SubscriptionID, p)
	if err != nil {
		return out, err
	}
	if s == nil || s.ID != b.SubscriptionID || s.Livemode != b.Live || s.Customer == nil || s.Customer.ID != b.CustomerID || s.Metadata["account_id"] != b.PersonID || s.Metadata["checkout_intent"] != b.IntentID || s.Metadata["product"] != "alerts.repository-slots" || s.Items == nil || s.Items.HasMore || len(s.Items.Data) != 1 {
		return out, ErrSubscription
	}
	item := s.Items.Data[0]
	if item == nil || item.Quantity != b.PaidSlots || !validMonthlyPrice(item.Price, b) || item.CurrentPeriodStart <= 0 || item.CurrentPeriodEnd <= item.CurrentPeriodStart {
		return out, ErrSubscription
	}
	out.ProviderStatus = string(s.Status)
	out.CancelAtPeriodEnd = s.CancelAtPeriodEnd
	out.CancelAt = s.CancelAt
	out.CollectionPaused = s.PauseCollection != nil
	out.PeriodStart = item.CurrentPeriodStart
	out.PeriodEnd = item.CurrentPeriodEnd
	if s.CancellationDetails != nil {
		out.CancellationReason = string(s.CancellationDetails.Reason)
	}
	if s.LatestInvoice != nil {
		if !strings.HasPrefix(s.LatestInvoice.ID, "in_") {
			return SubscriptionState{}, ErrSubscription
		}
		out.LatestInvoice = s.LatestInvoice.ID
	}
	switch s.Status {
	case "active", "trialing", "past_due", "unpaid", "paused", "canceled", "incomplete", "incomplete_expired":
	default:
		return SubscriptionState{}, ErrSubscription
	}
	switch {
	case s.Status == "canceled" && out.CancellationReason == "payment_failed":
		out.State = "renewal_unpaid"
	case s.Status == "canceled" && out.CancellationReason == "payment_disputed":
		out.State = "disputed"
	case s.Status == "canceled":
		out.State = "cancelled"
	case out.CollectionPaused || s.Status == "paused":
		out.State = "paused"
	case s.CancelAtPeriodEnd || s.CancelAt > 0:
		out.State = "cancelling"
	case s.Status == "past_due" || s.Status == "unpaid":
		out.State = "renewal_unpaid"
	case s.Status == "incomplete":
		out.State = "initial_unpaid"
	case s.Status == "incomplete_expired":
		out.State = "expired"
	default:
		out.State = "active"
	}
	return out, nil
}
