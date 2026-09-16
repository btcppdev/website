package alertsstripe

import (
	"context"
	"strings"
	"time"

	"btcpp-web/internal/servicebilling"
	"github.com/stripe/stripe-go/v86"
)

// IncreasePreview is review evidence, never a paid grant or permission to charge.
// The coordinator must persist it with the account's billing revision before a
// separate confirmation can use this exact item and proration timestamp.
type IncreasePreview struct {
	SubscriptionItemID                    string
	PreviousSlots, PaidSlots              int64
	PeriodStart, PeriodEnd, ProrationDate int64
	TotalCents, TaxCents                  int64
	ExpiresAt                             time.Time
	Funding                               *IncreaseFunding `json:",omitempty"`
}

// Funding comes from recorded payments, never a browser. A prior proration's
// frozen review lets another upgrade verify the currently paid quantity.
type IncreaseFunding struct {
	InvoiceID string
	Prior     *IncreaseIntent `json:",omitempty"`
}

func (c Client) PreviewIncrease(ctx context.Context, b Binding, slots int64, now time.Time, funding ...*IncreaseFunding) (*IncreasePreview, error) {
	// Stripe's proration timestamp has second precision. Keep the saved expiry
	// reproducible when confirmation rechecks that exact timestamp.
	now = now.UTC().Truncate(time.Second)
	if len(funding) > 1 {
		return nil, ErrInvoice
	}
	var proof *IncreaseFunding
	if len(funding) == 1 {
		proof = funding[0]
	}
	if c.PreviewInvoice == nil || c.Subscription == nil || c.Live != b.Live || slots <= b.PaidSlots {
		return nil, ErrSubscription
	}
	if _, err := servicebilling.AlertsTerm("stripe", slots, 1, now); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p := &stripe.SubscriptionParams{}
	p.Context = ctx
	p.AddExpand("items.data.price.product")
	sub, err := c.Subscription(b.SubscriptionID, p)
	if err != nil {
		return nil, err
	}
	state, err := verifySubscription(sub, b)
	if err != nil || state.State != "active" || state.ProviderStatus != "active" || sub.Schedule != nil || sub.PendingUpdate != nil || sub.CollectionMethod != "charge_automatically" || state.PeriodStart > now.Unix() || state.PeriodEnd <= now.Unix() || len(sub.Discounts) != 0 {
		return nil, ErrSubscription
	}
	item := sub.Items.Data[0]
	if !strings.HasPrefix(item.ID, "si_") || len(item.Discounts) != 0 {
		return nil, ErrSubscription
	}
	if err = c.verifyIncreaseFunding(ctx, b, state, proof, now); err != nil {
		return nil, err
	}
	request := &stripe.InvoiceCreatePreviewParams{Customer: stripe.String(b.CustomerID), Subscription: stripe.String(b.SubscriptionID), PreviewMode: stripe.String("next"), SubscriptionDetails: &stripe.InvoiceCreatePreviewSubscriptionDetailsParams{ProrationBehavior: stripe.String("always_invoice"), ProrationDate: stripe.Int64(now.Unix()), Items: []*stripe.InvoiceCreatePreviewSubscriptionDetailsItemParams{{ID: stripe.String(item.ID), Quantity: stripe.Int64(slots)}}}}
	request.Context = ctx
	preview, err := c.PreviewInvoice(request)
	if err != nil {
		return nil, err
	}
	out := &IncreasePreview{SubscriptionItemID: item.ID, PreviousSlots: b.PaidSlots, PaidSlots: slots, PeriodStart: state.PeriodStart, PeriodEnd: state.PeriodEnd, ProrationDate: now.Unix(), ExpiresAt: now.Add(10 * time.Minute)}
	out.Funding = proof
	if end := time.Unix(state.PeriodEnd, 0); out.ExpiresAt.After(end) {
		out.ExpiresAt = end
	}
	if err = verifyIncreasePreview(preview, b, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c Client) verifyIncreaseFunding(ctx context.Context, b Binding, state SubscriptionState, proof *IncreaseFunding, now time.Time) error {
	id := state.LatestInvoice
	if proof != nil {
		id = proof.InvoiceID
	}
	if proof != nil && proof.Prior != nil {
		prior := proof.Prior
		if prior.Preview.PaidSlots != b.PaidSlots || prior.Preview.PeriodStart != state.PeriodStart || prior.Preview.PeriodEnd != state.PeriodEnd || prior.Preview.Funding != nil {
			return ErrInvoice
		}
		previous := b
		previous.PaidSlots = prior.Preview.PreviousSlots
		paid, err := c.PaidIncreaseInvoice(ctx, id, previous, *prior)
		if err != nil {
			return err
		}
		if paid.PaidAt.After(now) || paid.Start.After(now) {
			return ErrInvoice
		}
		return nil
	}
	paid, err := c.PaidInvoice(ctx, id, b)
	if err != nil {
		return err
	}
	if paid.Start.Unix() != state.PeriodStart || paid.End.Unix() != state.PeriodEnd || paid.PaidAt.After(now) {
		return ErrInvoice
	}
	return nil
}

func verifyIncreasePreview(in *stripe.Invoice, b Binding, out *IncreasePreview) error {
	if in == nil || !strings.HasPrefix(in.ID, "upcoming_in_") || in.AmountPaid != 0 {
		return ErrInvoice
	}
	return verifyIncreaseTerms(in, b, out)
}

func verifyIncreaseTerms(in *stripe.Invoice, b Binding, out *IncreasePreview) error {
	if in == nil || in.Livemode != b.Live || in.Currency != "usd" || in.Customer == nil || in.Customer.ID != b.CustomerID || in.Parent == nil || in.Parent.Type != "subscription_details" || in.Parent.SubscriptionDetails == nil || in.Parent.SubscriptionDetails.Subscription == nil || in.Parent.SubscriptionDetails.Subscription.ID != b.SubscriptionID || in.BillingReason != "subscription_update" {
		return ErrInvoice
	}
	if in.AutomaticTax == nil || !in.AutomaticTax.Enabled || in.AutomaticTax.Status != "complete" || len(in.Discounts) != 0 || in.StartingBalance != 0 || in.EndingBalance != 0 || in.AmountPaidOffStripe != 0 || in.PrePaymentCreditNotesAmount != 0 || in.PostPaymentCreditNotesAmount != 0 || in.AmountDue != in.Total || in.Total < 0 || in.Total > (out.PaidSlots-out.PreviousSlots)*servicebilling.AlertsMonthlySlotCents {
		return ErrInvoice
	}
	// This entry point accepts a single licensed monthly item with a credit for
	// the old quantity and debit for the replacement. Additional invoice items,
	// discounts, future renewals or incomplete pages need explicit reconciliation.
	if in.Lines == nil || in.Lines.HasMore || len(in.Lines.Data) != 2 {
		return ErrInvoice
	}
	var sum int64
	credit, debit := false, false
	for _, line := range in.Lines.Data {
		if line == nil || line.Currency != "usd" || line.Period == nil || line.Period.Start != out.ProrationDate || line.Period.End != out.PeriodEnd || line.Parent == nil || line.Parent.SubscriptionItemDetails == nil || !line.Parent.SubscriptionItemDetails.Proration || line.Parent.SubscriptionItemDetails.Subscription != b.SubscriptionID || line.Parent.SubscriptionItemDetails.SubscriptionItem != out.SubscriptionItemID || line.Pricing == nil || line.Pricing.PriceDetails == nil || line.Pricing.PriceDetails.Price == nil || line.Pricing.PriceDetails.Price.ID != b.PriceID || line.Pricing.PriceDetails.Product != b.ProductID || len(line.Discounts) != 0 {
			return ErrInvoice
		}
		switch {
		case line.Quantity == out.PreviousSlots && line.Amount <= 0 && !credit && line.Amount >= -out.PreviousSlots*servicebilling.AlertsMonthlySlotCents:
			credit = true
		case line.Quantity == out.PaidSlots && line.Amount >= 0 && !debit && line.Amount <= out.PaidSlots*servicebilling.AlertsMonthlySlotCents:
			debit = true
		default:
			return ErrInvoice
		}
		sum += line.Amount
	}
	if !credit || !debit || sum != in.Total {
		return ErrInvoice
	}
	var tax int64
	for _, part := range in.TotalTaxes {
		if part == nil || part.TaxBehavior != "inclusive" || part.Amount < 0 || part.Amount > in.Total-tax {
			return ErrInvoice
		}
		tax += part.Amount
	}
	out.TotalCents, out.TaxCents = in.Total, tax
	return nil
}
