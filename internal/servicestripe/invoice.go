// Package servicestripe verifies provider billing evidence against server-owned product plans.
package servicestripe

import (
	"btcpp-web/internal/servicebilling"
	"errors"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

var ErrInvoice = errors.New("Stripe invoice does not match the service subscription")

// Binding must come from trusted subscription storage, never webhook metadata.
type Binding struct {
	IntentID, PersonID, SubscriptionID, CustomerID, PriceID, ProductID string
	ProductCode, TaxCode                                               string
	Quantity                                                           int64
	Live                                                               bool
}
type PaidPeriod struct {
	InvoiceID, SubscriptionID      string
	Quantity, TotalCents, TaxCents int64
	Start, End, PaidAt             time.Time
}

// VerifyPaidInvoice verifies a complete, authenticated Stripe invoice and an
// authenticated Price lookup. Prorations require their own quantity-change
// transaction; treating a proration as a full month's access would overgrant.
func VerifyPaidInvoice(in *stripe.Invoice, price *stripe.Price, b Binding) (*PaidPeriod, error) {
	period, err := VerifyInvoiceTerms(in, price, b)
	if err != nil {
		return nil, err
	}
	if in.Status != "paid" || in.AmountPaid != period.TotalCents || in.AmountRemaining != 0 || in.StatusTransitions == nil || in.StatusTransitions.PaidAt <= 0 {
		return nil, ErrInvoice
	}
	period.PaidAt = time.Unix(in.StatusTransitions.PaidAt, 0).UTC()
	return period, nil
}

func VerifyInvoiceTerms(in *stripe.Invoice, price *stripe.Price, b Binding) (*PaidPeriod, error) {
	plan, planErr := servicebilling.LookupProductPlan(b.ProductCode)
	if planErr != nil {
		return nil, ErrInvoice
	}
	if in == nil || price == nil || b.IntentID == "" || b.PersonID == "" || !strings.HasPrefix(b.SubscriptionID, "sub_") || !strings.HasPrefix(b.CustomerID, "cus_") || !strings.HasPrefix(b.PriceID, "price_") || !strings.HasPrefix(b.ProductID, "prod_") || b.Quantity < 1 || b.Quantity > plan.MaxQuantity {
		return nil, ErrInvoice
	}
	total := b.Quantity * plan.UnitCents
	if !strings.HasPrefix(in.ID, "in_") || in.Livemode != b.Live || in.Currency != "usd" || in.Customer == nil || in.Customer.ID != b.CustomerID || in.Parent == nil || in.Parent.Type != "subscription_details" || in.Parent.SubscriptionDetails == nil || in.Parent.SubscriptionDetails.Subscription == nil || in.Parent.SubscriptionDetails.Subscription.ID != b.SubscriptionID {
		return nil, ErrInvoice
	}
	meta := in.Parent.SubscriptionDetails.Metadata
	if meta["product"] != plan.Code || meta["account_id"] != b.PersonID || meta["checkout_intent"] != b.IntentID {
		return nil, ErrInvoice
	}
	if in.BillingReason != "subscription_create" && in.BillingReason != "subscription_cycle" {
		return nil, ErrInvoice
	}
	if in.AmountPaidOffStripe != 0 || in.PostPaymentCreditNotesAmount != 0 || in.PrePaymentCreditNotesAmount != 0 || len(in.Discounts) != 0 || in.AutomaticTax == nil || !in.AutomaticTax.Enabled || in.AutomaticTax.Status != "complete" {
		return nil, ErrInvoice
	}
	if !ValidPrice(price, b) {
		return nil, ErrInvoice
	}
	if in.Lines == nil || in.Lines.HasMore || len(in.Lines.Data) != 1 {
		return nil, ErrInvoice
	}
	line := in.Lines.Data[0]
	if line == nil || line.Currency != "usd" || line.Quantity != b.Quantity || line.Amount != total || line.Period == nil || line.Parent == nil || line.Parent.Type != "subscription_item_details" || line.Parent.SubscriptionItemDetails == nil || line.Parent.SubscriptionItemDetails.Subscription != b.SubscriptionID || line.Parent.SubscriptionItemDetails.Proration || line.Pricing == nil || line.Pricing.PriceDetails == nil || line.Pricing.PriceDetails.Price == nil || line.Pricing.PriceDetails.Price.ID != b.PriceID || line.Pricing.PriceDetails.Product != b.ProductID || len(line.Discounts) != 0 {
		return nil, ErrInvoice
	}
	start, end := time.Unix(line.Period.Start, 0).UTC(), time.Unix(line.Period.End, 0).UTC()
	if line.Period.Start <= 0 || !end.After(start) || !validPeriodLength(start, end, plan.Months) {
		return nil, ErrInvoice
	}
	var tax int64
	for _, t := range in.TotalTaxes {
		if t == nil || string(t.TaxBehavior) != plan.TaxBehavior || t.Amount < 0 || t.Amount > 2147483647-tax {
			return nil, ErrInvoice
		}
		tax += t.Amount
	}
	if plan.TaxBehavior == "exclusive" {
		if tax > 2147483647-total {
			return nil, ErrInvoice
		}
		total += tax
	} else if tax > total {
		return nil, ErrInvoice
	}
	if in.Total != total {
		return nil, ErrInvoice
	}
	return &PaidPeriod{InvoiceID: in.ID, SubscriptionID: b.SubscriptionID, Quantity: b.Quantity, TotalCents: total, TaxCents: tax, Start: start, End: end}, nil
}

func validPeriodLength(start, end time.Time, months int) bool {
	if months == 12 {
		return end.Sub(start) >= 364*24*time.Hour && end.Sub(start) <= 367*24*time.Hour
	}
	return months == 1 && end.Sub(start) >= 27*24*time.Hour && end.Sub(start) <= 32*24*time.Hour
}
func ValidPrice(price *stripe.Price, b Binding) bool {
	plan, err := servicebilling.LookupProductPlan(b.ProductCode)
	if err != nil || b.TaxCode == "" || (plan.StripeTaxCode != "" && b.TaxCode != plan.StripeTaxCode) {
		return false
	}
	interval := "month"
	if plan.Months == 12 {
		interval = "year"
	}
	return price != nil && price.ID == b.PriceID && price.Livemode == b.Live && string(price.Currency) == plan.Currency && price.UnitAmount == plan.UnitCents && string(price.TaxBehavior) == plan.TaxBehavior && price.Type == "recurring" && price.Recurring != nil && string(price.Recurring.Interval) == interval && price.Recurring.IntervalCount == 1 && price.Product != nil && price.Product.ID == b.ProductID && productTaxCodeMatches(price.Product, b.TaxCode)
}

// Stripe's newer API returns tax_details.tax_code rather than tax_code.
// Require authenticated product evidence in either shape; conflicting values
// must never pass by falling back to the expected plan or account default.
func productTaxCodeMatches(product *stripe.Product, expected string) bool {
	if product == nil || expected == "" {
		return false
	}
	code := ""
	if product.TaxCode != nil {
		code = product.TaxCode.ID
	}
	if product.TaxDetails != nil && product.TaxDetails.TaxCode != "" {
		if code != "" && code != product.TaxDetails.TaxCode {
			return false
		}
		code = product.TaxDetails.TaxCode
	}
	return code == expected
}
