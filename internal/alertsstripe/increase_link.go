package alertsstripe

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v86"
)

// IncreasePaymentLink returns Stripe's existing hosted invoice page only after
// comparing authenticated invoice terms with the saved upgrade confirmation.
// It never creates an invoice, attempts payment, or grants access.
func (c Client) IncreasePaymentLink(ctx context.Context, id string, b Binding, intent IncreaseIntent) (string, error) {
	if !strings.HasPrefix(id, "in_") || len(id) > 255 || c.Invoice == nil || c.Price == nil || c.Live != b.Live {
		return "", ErrInvoice
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ip := &stripe.InvoiceParams{}
	ip.Context = ctx
	in, err := c.Invoice(id, ip)
	if err != nil {
		return "", err
	}
	if in == nil || in.ID != id {
		return "", ErrInvoice
	}
	pp := &stripe.PriceParams{}
	pp.Context = ctx
	pp.AddExpand("product")
	price, err := c.Price(b.PriceID, pp)
	if err != nil {
		return "", err
	}
	if err = verifyReviewedIncreaseInvoice(in, price, b, intent); err != nil {
		return "", err
	}
	if in.Status != "open" || in.AmountPaid != 0 || in.AmountRemaining != in.Total || in.Total <= 0 || (in.StatusTransitions != nil && in.StatusTransitions.PaidAt != 0) {
		return "", ErrInvoice
	}
	u, err := url.Parse(in.HostedInvoiceURL)
	if err != nil || u.Scheme != "https" || u.Host != "invoice.stripe.com" || u.User != nil || u.Fragment != "" || !strings.HasPrefix(u.Path, "/i/") || len(u.Path) <= 3 {
		return "", ErrInvoice
	}
	return u.String(), nil
}
