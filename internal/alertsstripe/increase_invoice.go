package alertsstripe

import (
	"context"
	"strings"
	"time"

	"btcpp-web/internal/servicebilling"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
)

// PaidIncrease covers only the additional slots for the remaining billing period.
// It is not a replacement full-month grant. Fulfillment must atomically record
// this evidence, the invoice's uniqueness, and the resulting quantity revision.
type PaidIncrease struct {
	QuoteID, InvoiceID, SubscriptionID        string
	PreviousSlots, PaidSlots, AdditionalSlots int64
	TotalCents, TaxCents                      int64
	Start, End, PaidAt                        time.Time
}

func VerifyIncreaseInvoice(in *stripe.Invoice, price *stripe.Price, b Binding, intent IncreaseIntent) (*PaidIncrease, error) {
	q := intent.Preview
	if _, err := uuid.Parse(intent.ID); err != nil {
		return nil, ErrInvoice
	}
	if b.IntentID == "" || b.PersonID == "" || !strings.HasPrefix(b.SubscriptionID, "sub_") || !strings.HasPrefix(b.CustomerID, "cus_") || !strings.HasPrefix(q.SubscriptionItemID, "si_") || b.PaidSlots < 1 || q.PreviousSlots != b.PaidSlots || q.PaidSlots <= q.PreviousSlots || q.PeriodStart <= 0 || q.ProrationDate < q.PeriodStart || q.ProrationDate >= q.PeriodEnd || !validMonthlyPrice(price, b) {
		return nil, ErrInvoice
	}
	if _, err := servicebilling.AlertsTerm("stripe", q.PaidSlots, 1, time.Unix(q.ProrationDate, 0)); err != nil {
		return nil, ErrInvoice
	}
	if in == nil || !strings.HasPrefix(in.ID, "in_") || in.Status != "paid" || in.AmountPaid != q.TotalCents || in.AmountRemaining != 0 || in.StatusTransitions == nil || in.StatusTransitions.PaidAt < q.ProrationDate {
		return nil, ErrInvoice
	}
	actual := q
	if err := verifyIncreaseTerms(in, b, &actual); err != nil {
		return nil, err
	}
	meta := in.Parent.SubscriptionDetails.Metadata
	if meta["product"] != "alerts.repository-slots" || meta["account_id"] != b.PersonID || meta["checkout_intent"] != b.IntentID || actual.TotalCents != q.TotalCents || actual.TaxCents != q.TaxCents {
		return nil, ErrInvoice
	}
	return &PaidIncrease{QuoteID: intent.ID, InvoiceID: in.ID, SubscriptionID: b.SubscriptionID, PreviousSlots: q.PreviousSlots, PaidSlots: q.PaidSlots, AdditionalSlots: q.PaidSlots - q.PreviousSlots, TotalCents: q.TotalCents, TaxCents: q.TaxCents, Start: time.Unix(q.ProrationDate, 0).UTC(), End: time.Unix(q.PeriodEnd, 0).UTC(), PaidAt: time.Unix(in.StatusTransitions.PaidAt, 0).UTC()}, nil
}

// PaidIncreaseInvoice obtains authenticated provider evidence; webhook bodies
// and browser redirects are never accepted as payment evidence.
func (c Client) PaidIncreaseInvoice(ctx context.Context, id string, b Binding, in IncreaseIntent) (*PaidIncrease, error) {
	if !strings.HasPrefix(id, "in_") || len(id) > 255 || c.Invoice == nil || c.Price == nil || c.Live != b.Live {
		return nil, ErrInvoice
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p := &stripe.InvoiceParams{}
	p.Context = ctx
	invoice, err := c.Invoice(id, p)
	if err != nil {
		return nil, err
	}
	if invoice == nil || invoice.ID != id {
		return nil, ErrInvoice
	}
	pp := &stripe.PriceParams{}
	pp.Context = ctx
	pp.AddExpand("product")
	price, err := c.Price(b.PriceID, pp)
	if err != nil {
		return nil, err
	}
	return VerifyIncreaseInvoice(invoice, price, b, in)
}
