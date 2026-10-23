package alertsstripe

import (
	"context"
	"github.com/stripe/stripe-go/v86"
	"net/url"
	"strings"
	"time"
)

// RenewalLink reads an existing invoice. Opening this link never creates a
// subscription, attempts a charge, or grants access.
func (c Client) RenewalLink(ctx context.Context, id string, b Binding) (string, error) {
	if !strings.HasPrefix(id, "in_") || len(id) > 255 || c.Invoice == nil || c.Price == nil || c.Live != b.Live {
		return "", ErrInvoice
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ip := &stripe.InvoiceParams{}
	ip.Context = ctx
	inv, err := c.Invoice(id, ip)
	if err != nil {
		return "", err
	}
	if inv == nil || inv.ID != id {
		return "", ErrInvoice
	}
	pp := &stripe.PriceParams{}
	pp.Context = ctx
	pp.AddExpand("product")
	price, err := c.Price(b.PriceID, pp)
	if err != nil {
		return "", err
	}
	if _, err = verifyAnnualInvoiceTerms(inv, price, b); err != nil {
		return "", err
	}
	if inv.BillingReason != "subscription_cycle" || (inv.Status != "open" && inv.Status != "paid") {
		return "", ErrInvoice
	}
	u, err := url.Parse(inv.HostedInvoiceURL)
	if err != nil || u.Scheme != "https" || u.Host != "invoice.stripe.com" || u.User != nil || u.Fragment != "" || !strings.HasPrefix(u.Path, "/i/") || len(u.Path) <= 3 {
		return "", ErrInvoice
	}
	return u.String(), nil
}
