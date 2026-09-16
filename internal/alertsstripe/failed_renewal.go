package alertsstripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

type FailedRenewal struct {
	InvoiceID, SubscriptionID string
	Quantity                  int64
	DueAt, PeriodEnd          time.Time
}

// Previous must come from the account's settled, non-refunded invoice ledger.
// The current subscription must separately be verified as renewal_unpaid before
// this evidence can grant grace. Retry count never shifts the original due time.
func VerifyFailedRenewal(in *stripe.Invoice, price *stripe.Price, b Binding, previous PaidPeriod, now time.Time) (*FailedRenewal, error) {
	period, err := verifyMonthlyInvoiceTerms(in, price, b)
	if err != nil {
		return nil, err
	}
	if now.IsZero() || period.Start.After(now) || in.Status != "open" || in.BillingReason != "subscription_cycle" || !in.Attempted || in.AttemptCount < 1 || in.CollectionMethod != "charge_automatically" || in.AmountPaid < 0 || in.AmountPaid >= period.TotalCents || in.AmountRemaining != period.TotalCents-in.AmountPaid || (in.StatusTransitions != nil && in.StatusTransitions.PaidAt != 0) {
		return nil, ErrInvoice
	}
	if previous.SubscriptionID != b.SubscriptionID || previous.Quantity != b.PaidSlots || previous.InvoiceID == in.ID || previous.InvoiceID == "" || previous.PaidAt.IsZero() || previous.PaidAt.After(now) || !previous.End.Equal(period.Start) || !previous.Start.Before(previous.End) {
		return nil, ErrInvoice
	}
	return &FailedRenewal{InvoiceID: in.ID, SubscriptionID: b.SubscriptionID, Quantity: b.PaidSlots, DueAt: period.Start, PeriodEnd: period.End}, nil
}

func (c Client) FailedRenewalInvoice(ctx context.Context, id string, b Binding, previous PaidPeriod, now time.Time) (*FailedRenewal, error) {
	if !strings.HasPrefix(id, "in_") || len(id) > 255 || c.Invoice == nil || c.Price == nil || b.Live != c.Live {
		return nil, ErrInvoice
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ip := &stripe.InvoiceParams{}
	ip.Context = ctx
	inv, err := c.Invoice(id, ip)
	if err != nil {
		return nil, err
	}
	if inv == nil || inv.ID != id {
		return nil, ErrInvoice
	}
	pp := &stripe.PriceParams{}
	pp.Context = ctx
	pp.AddExpand("product")
	price, err := c.Price(b.PriceID, pp)
	if err != nil {
		return nil, err
	}
	return VerifyFailedRenewal(inv, price, b, previous, now)
}
