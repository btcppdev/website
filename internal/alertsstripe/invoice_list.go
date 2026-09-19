package alertsstripe

import (
	"btcpp-web/internal/servicestripe"
	"context"
)

func (c Client) PaidInvoiceIDs(ctx context.Context, b Binding, cursor string) ([]string, string, error) {
	return (servicestripe.InvoiceClient{ListInvoices: c.ListInvoices, Live: c.Live}).PaidInvoiceIDs(ctx, serviceBinding(b), cursor)
}
