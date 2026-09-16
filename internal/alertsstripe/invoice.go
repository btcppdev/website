package alertsstripe

import (
	"context"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/servicebilling"
	"github.com/stripe/stripe-go/v86"
)

var ErrInvoice = errors.New("Stripe invoice does not match the Alerts subscription")

// Binding is loaded from a server-owned subscription record. Never construct it
// from webhook metadata alone: metadata is evidence to compare, not ownership.
type Binding struct {
	IntentID, PersonID, SubscriptionID, CustomerID, PriceID, ProductID string
	PaidSlots                                                          int64
	Live                                                               bool
}

type PaidPeriod struct {
	InvoiceID, SubscriptionID      string
	Quantity, TotalCents, TaxCents int64
	Start, End, PaidAt             time.Time
}

// VerifyMonthlyInvoice verifies a complete, authenticated Stripe invoice and an
// authenticated Price lookup. Prorations require their own quantity-change
// transaction; treating a proration as a full month's access would overgrant.
func VerifyMonthlyInvoice(in *stripe.Invoice, price *stripe.Price, b Binding) (*PaidPeriod, error) {
	period, err := verifyMonthlyInvoiceTerms(in, price, b)
	if err != nil {
		return nil, err
	}
	if in.Status != "paid" || in.AmountPaid != period.TotalCents || in.AmountRemaining != 0 || in.StatusTransitions == nil || in.StatusTransitions.PaidAt <= 0 {
		return nil, ErrInvoice
	}
	period.PaidAt = time.Unix(in.StatusTransitions.PaidAt, 0).UTC()
	return period, nil
}

func verifyMonthlyInvoiceTerms(in *stripe.Invoice, price *stripe.Price, b Binding) (*PaidPeriod, error) {
	if in == nil || price == nil || b.IntentID == "" || b.PersonID == "" || !strings.HasPrefix(b.SubscriptionID, "sub_") || !strings.HasPrefix(b.CustomerID, "cus_") || !strings.HasPrefix(b.PriceID, "price_") || !strings.HasPrefix(b.ProductID, "prod_") || b.PaidSlots < 1 || b.PaidSlots > 2147483647/servicebilling.AlertsMonthlySlotCents {
		return nil, ErrInvoice
	}
	total := b.PaidSlots * servicebilling.AlertsMonthlySlotCents
	if !strings.HasPrefix(in.ID, "in_") || in.Livemode != b.Live || in.Currency != "usd" || in.Customer == nil || in.Customer.ID != b.CustomerID || in.Parent == nil || in.Parent.Type != "subscription_details" || in.Parent.SubscriptionDetails == nil || in.Parent.SubscriptionDetails.Subscription == nil || in.Parent.SubscriptionDetails.Subscription.ID != b.SubscriptionID {
		return nil, ErrInvoice
	}
	meta := in.Parent.SubscriptionDetails.Metadata
	if meta["product"] != "alerts.repository-slots" || meta["account_id"] != b.PersonID || meta["checkout_intent"] != b.IntentID {
		return nil, ErrInvoice
	}
	if in.BillingReason != "subscription_create" && in.BillingReason != "subscription_cycle" {
		return nil, ErrInvoice
	}
	if in.Total != total || in.AmountPaidOffStripe != 0 || in.PostPaymentCreditNotesAmount != 0 || in.PrePaymentCreditNotesAmount != 0 || len(in.Discounts) != 0 || in.AutomaticTax == nil || !in.AutomaticTax.Enabled || in.AutomaticTax.Status != "complete" {
		return nil, ErrInvoice
	}
	if !validMonthlyPrice(price, b) {
		return nil, ErrInvoice
	}
	if in.Lines == nil || in.Lines.HasMore || len(in.Lines.Data) != 1 {
		return nil, ErrInvoice
	}
	line := in.Lines.Data[0]
	if line == nil || line.Currency != "usd" || line.Quantity != b.PaidSlots || line.Amount != total || line.Period == nil || line.Parent == nil || line.Parent.Type != "subscription_item_details" || line.Parent.SubscriptionItemDetails == nil || line.Parent.SubscriptionItemDetails.Subscription != b.SubscriptionID || line.Parent.SubscriptionItemDetails.Proration || line.Pricing == nil || line.Pricing.PriceDetails == nil || line.Pricing.PriceDetails.Price == nil || line.Pricing.PriceDetails.Price.ID != b.PriceID || line.Pricing.PriceDetails.Product != b.ProductID || len(line.Discounts) != 0 {
		return nil, ErrInvoice
	}
	start, end := time.Unix(line.Period.Start, 0).UTC(), time.Unix(line.Period.End, 0).UTC()
	if line.Period.Start <= 0 || !end.After(start) || end.Sub(start) < 27*24*time.Hour || end.Sub(start) > 32*24*time.Hour {
		return nil, ErrInvoice
	}
	var tax int64
	for _, t := range in.TotalTaxes {
		if t == nil || t.TaxBehavior != "inclusive" || t.Amount < 0 || t.Amount > total-tax {
			return nil, ErrInvoice
		}
		tax += t.Amount
	}
	return &PaidPeriod{InvoiceID: in.ID, SubscriptionID: b.SubscriptionID, Quantity: b.PaidSlots, TotalCents: total, TaxCents: tax, Start: start, End: end}, nil
}

// PaidInvoice re-reads Stripe rather than trusting a webhook body or return URL.
// It returns verified billing evidence only; the caller records/fulfills it in
// one durable transaction keyed by invoice ID before reporting paid access.
func (c Client) PaidInvoice(ctx context.Context, id string, b Binding) (*PaidPeriod, error) {
	if !strings.HasPrefix(id, "in_") || len(id) > 255 || c.Invoice == nil || c.Price == nil || b.Live != c.Live {
		return nil, ErrInvoice
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	params := &stripe.InvoiceParams{}
	params.Context = ctx
	inv, err := c.Invoice(id, params)
	if err != nil {
		return nil, err
	}
	if inv == nil || inv.ID != id {
		return nil, ErrInvoice
	}
	priceParams := &stripe.PriceParams{}
	priceParams.Context = ctx
	priceParams.AddExpand("product")
	price, err := c.Price(b.PriceID, priceParams)
	if err != nil {
		return nil, err
	}
	return VerifyMonthlyInvoice(inv, price, b)
}

func validMonthlyPrice(price *stripe.Price, b Binding) bool {
	return price != nil && !(price.ID != b.PriceID || price.Livemode != b.Live || price.Currency != "usd" || price.UnitAmount != servicebilling.AlertsMonthlySlotCents || price.TaxBehavior != "inclusive" || price.Type != "recurring" || price.Recurring == nil || price.Recurring.Interval != "month" || price.Recurring.IntervalCount != 1 || price.Product == nil || price.Product.ID != b.ProductID || price.Product.TaxCode == nil || price.Product.TaxCode.ID != servicebilling.AlertsStripeTaxCode)
}
