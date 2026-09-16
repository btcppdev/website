package alertsstripe

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

// IncreaseIntent must come from a committed, account-owned confirmation record.
// Its ID and preview must remain identical across retries.
type IncreaseIntent struct {
	ID      string
	Preview IncreasePreview
}

// IncreaseSubmission is a reconciliation reference, never proof of payment.
type IncreaseSubmission struct {
	InvoiceID string
	Pending   bool
}

// SubmitIncrease submits a reviewed change using Stripe's payment-gated update.
// An ambiguous response must be reconciled before another attempt. In particular,
// a pending update is never replaced with a newly computed quote.
func (c Client) SubmitIncrease(ctx context.Context, b Binding, in IncreaseIntent, now time.Time) (*IncreaseSubmission, error) {
	q := in.Preview
	if _, err := uuid.Parse(in.ID); err != nil {
		return nil, ErrSubscription
	}
	if c.UpdateSubscription == nil || q.PreviousSlots != b.PaidSlots || !now.Before(q.ExpiresAt) || q.ProrationDate > now.Unix() || q.ExpiresAt.Unix() > q.ProrationDate+600 {
		return nil, ErrSubscription
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Recheck ownership, the paid base period, item and exact reviewed totals.
	// Reusing the original timestamp keeps the proration unchanged during review.
	fresh, err := c.PreviewIncrease(ctx, b, q.PaidSlots, time.Unix(q.ProrationDate, 0))
	if err != nil {
		return nil, err
	}
	if fresh.SubscriptionItemID != q.SubscriptionItemID || fresh.PeriodStart != q.PeriodStart || fresh.PeriodEnd != q.PeriodEnd || fresh.TotalCents != q.TotalCents || fresh.TaxCents != q.TaxCents || !fresh.ExpiresAt.Equal(q.ExpiresAt) {
		return nil, ErrSubscription
	}
	p := &stripe.SubscriptionParams{
		PaymentBehavior:   stripe.String("pending_if_incomplete"),
		ProrationBehavior: stripe.String("always_invoice"),
		ProrationDate:     stripe.Int64(q.ProrationDate),
		Items:             []*stripe.SubscriptionItemsParams{{ID: stripe.String(q.SubscriptionItemID), Quantity: stripe.Int64(q.PaidSlots)}},
	}
	p.Context = ctx
	p.SetIdempotencyKey("alerts-increase:" + in.ID)
	p.AddExpand("items.data.price.product")
	updated, err := c.UpdateSubscription(b.SubscriptionID, p)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrSubscription
	}
	expected := b
	if updated.PendingUpdate == nil {
		expected.PaidSlots = q.PaidSlots
	}
	state, err := verifySubscription(updated, expected)
	if err != nil || state.State != "active" || state.ProviderStatus != "active" || state.PeriodStart != q.PeriodStart || state.PeriodEnd != q.PeriodEnd || updated.Schedule != nil || updated.CollectionMethod != "charge_automatically" || len(updated.Discounts) != 0 || updated.Items.Data[0].ID != q.SubscriptionItemID || len(updated.Items.Data[0].Discounts) != 0 || !strings.HasPrefix(state.LatestInvoice, "in_") {
		return nil, ErrSubscription
	}
	if pending := updated.PendingUpdate; pending != nil {
		if pending.ExpiresAt <= now.Unix() || pending.BillingCycleAnchor != 0 || pending.TrialEnd != 0 || pending.TrialFromPlan || pending.Discount != nil || len(pending.Discounts) != 0 || len(pending.SubscriptionItems) != 1 {
			return nil, ErrSubscription
		}
		item := pending.SubscriptionItems[0]
		if item == nil || item.ID != q.SubscriptionItemID || item.Quantity != q.PaidSlots || item.Price == nil || item.Price.ID != b.PriceID {
			return nil, ErrSubscription
		}
	}
	return &IncreaseSubmission{InvoiceID: state.LatestInvoice, Pending: updated.PendingUpdate != nil}, nil
}
