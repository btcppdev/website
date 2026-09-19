package servicestripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// InvoiceClient retrieves current provider evidence; webhook payloads alone
// cannot establish a paid period.
type InvoiceClient struct {
	ListInvoices func(*stripe.InvoiceListParams) (*stripe.InvoiceList, error)
	Invoice      func(string, *stripe.InvoiceParams) (*stripe.Invoice, error)
	Price        func(string, *stripe.PriceParams) (*stripe.Price, error)
	Live         bool
}

func (c InvoiceClient) PaidInvoice(ctx context.Context, id string, b Binding) (*PaidPeriod, error) {
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
	return VerifyPaidInvoice(inv, price, b)
}
