package alertsstripe

import (
	"btcpp-web/internal/servicestripe"
	"context"
	"strings"
	"time"

	"btcpp-web/internal/servicebilling"
	"github.com/stripe/stripe-go/v86"
)

var ErrInvoice = servicestripe.ErrInvoice

// Binding is loaded from a server-owned subscription record. Never construct it
// from webhook metadata alone: metadata is evidence to compare, not ownership.
type Binding struct {
	IntentID, PersonID, SubscriptionID, CustomerID, PriceID, ProductID string
	PaidSlots                                                          int64
	Live                                                               bool
}

type PaidPeriod = servicestripe.PaidPeriod

func serviceBinding(b Binding) servicestripe.Binding {
	return servicestripe.Binding{IntentID: b.IntentID, PersonID: b.PersonID, SubscriptionID: b.SubscriptionID, CustomerID: b.CustomerID, PriceID: b.PriceID, ProductID: b.ProductID, Quantity: b.PaidSlots, Live: b.Live, ProductCode: servicebilling.AlertsProduct, TaxCode: servicebilling.AlertsStripeTaxCode}
}
func VerifyMonthlyInvoice(in *stripe.Invoice, price *stripe.Price, b Binding) (*PaidPeriod, error) {
	return servicestripe.VerifyPaidInvoice(in, price, serviceBinding(b))
}
func verifyMonthlyInvoiceTerms(in *stripe.Invoice, price *stripe.Price, b Binding) (*PaidPeriod, error) {
	return servicestripe.VerifyInvoiceTerms(in, price, serviceBinding(b))
}
func validMonthlyPrice(price *stripe.Price, b Binding) bool {
	return servicestripe.ValidPrice(price, serviceBinding(b))
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
