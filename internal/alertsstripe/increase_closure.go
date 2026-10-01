package alertsstripe

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

// VoidPendingIncrease cancels only the reviewed, unpaid pending upgrade. The
// coordinator must hold the local submission fence and verify permanent closure.
// Callers still need VoidedIncrease before releasing the local instruction.
func (c Client) VoidPendingIncrease(ctx context.Context, id string, b Binding, intent IncreaseIntent) error {
	if parsed, err := uuid.Parse(intent.ID); err != nil || parsed.String() != intent.ID {
		return ErrInvoice
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	observed, err := c.InspectIncreaseInvoice(ctx, id, b, intent)
	if err != nil {
		return err
	}
	if observed.Status == "void" {
		return nil
	}
	if observed.Status != "open" || c.Subscription == nil || c.VoidInvoice == nil {
		return ErrReconcile
	}
	p := &stripe.SubscriptionParams{}
	p.Context = ctx
	p.AddExpand("items.data.price.product")
	sub, err := c.Subscription(b.SubscriptionID, p)
	if err != nil {
		return err
	}
	state, err := verifySubscription(sub, b)
	if err != nil {
		return err
	}
	q := intent.Preview
	if state.LatestInvoice != id || state.State != "active" || state.ProviderStatus != "active" || state.PeriodStart != q.PeriodStart || state.PeriodEnd != q.PeriodEnd || sub.Schedule != nil || sub.CollectionMethod != "charge_automatically" || sub.Items.Data[0].ID != q.SubscriptionItemID {
		return ErrReconcile
	}
	pending := sub.PendingUpdate
	if pending == nil || pending.BillingCycleAnchor != 0 || pending.TrialEnd != 0 || pending.TrialFromPlan || pending.Discount != nil || len(pending.Discounts) != 0 || len(pending.SubscriptionItems) != 1 {
		return ErrReconcile
	}
	item := pending.SubscriptionItems[0]
	if item == nil || item.ID != q.SubscriptionItemID || item.Quantity != q.PaidSlots || item.Price == nil || item.Price.ID != b.PriceID {
		return ErrReconcile
	}
	params := &stripe.InvoiceVoidInvoiceParams{}
	params.Context = ctx
	params.SetIdempotencyKey("alerts-closure-void-increase:" + intent.ID)
	_, err = c.VoidInvoice(id, params)
	if err != nil {
		return err
	}
	// Never trust an action acknowledgement alone, including an ambiguous race
	// with payment. Read the provider's invoice and subscription again.
	_, err = c.VoidedIncrease(ctx, id, b, intent)
	return err
}
