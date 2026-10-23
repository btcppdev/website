package alertsstripe

import (
	"btcpp-web/internal/servicestripe"
	"context"

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
func VerifyAnnualInvoice(in *stripe.Invoice, price *stripe.Price, b Binding) (*PaidPeriod, error) {
	return servicestripe.VerifyPaidInvoice(in, price, serviceBinding(b))
}
func verifyAnnualInvoiceTerms(in *stripe.Invoice, price *stripe.Price, b Binding) (*PaidPeriod, error) {
	return servicestripe.VerifyInvoiceTerms(in, price, serviceBinding(b))
}
func validAnnualPrice(price *stripe.Price, b Binding) bool {
	return servicestripe.ValidPrice(price, serviceBinding(b))
}

// PaidInvoice re-reads Stripe rather than trusting a webhook body or return URL.
// It returns verified billing evidence only; the caller records/fulfills it in
// one durable transaction keyed by invoice ID before reporting paid access.
func (c Client) PaidInvoice(ctx context.Context, id string, b Binding) (*PaidPeriod, error) {
	return (servicestripe.InvoiceClient{Invoice: c.Invoice, Price: c.Price, Live: c.Live}).PaidInvoice(ctx, id, serviceBinding(b))
}
