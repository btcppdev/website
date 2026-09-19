package servicestripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"strings"
	"time"
)

// PaidInvoiceIDs discovers candidates only. Each unrecorded invoice still needs
// its own authenticated lookup and full price/payment/period validation.
func (c InvoiceClient) PaidInvoiceIDs(ctx context.Context, b Binding, cursor string) ([]string, string, error) {
	if c.ListInvoices == nil || b.Live != c.Live || !strings.HasPrefix(b.SubscriptionID, "sub_") || !strings.HasPrefix(b.CustomerID, "cus_") {
		return nil, "", ErrInvoice
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p := &stripe.InvoiceListParams{Customer: stripe.String(b.CustomerID), Subscription: stripe.String(b.SubscriptionID), Status: stripe.String("paid")}
	p.Context = ctx
	p.Limit = stripe.Int64(100)
	p.Single = true
	if cursor != "" {
		if !strings.HasPrefix(cursor, "in_") {
			return nil, "", ErrInvoice
		}
		p.StartingAfter = stripe.String(cursor)
	}
	page, err := c.ListInvoices(p)
	if err != nil {
		return nil, "", err
	}
	if page == nil || len(page.Data) > 100 || (page.HasMore && len(page.Data) == 0) {
		return nil, "", ErrInvoice
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, invoice := range page.Data {
		if invoice == nil || !strings.HasPrefix(invoice.ID, "in_") || invoice.ID == cursor || seen[invoice.ID] || invoice.Status != "paid" || invoice.Livemode != b.Live || invoice.Customer == nil || invoice.Customer.ID != b.CustomerID {
			return nil, "", ErrInvoice
		}
		seen[invoice.ID] = true
		ids = append(ids, invoice.ID)
	}
	next := ""
	if page.HasMore {
		next = ids[len(ids)-1]
	}
	return ids, next, nil
}
