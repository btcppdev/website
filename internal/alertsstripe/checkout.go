// Package alertsstripe implements the Stripe subscription boundary for Alerts.
// Account ownership and checkout idempotency must be persisted by the caller
// before invoking this package. Browser redirects never establish paid access.
package alertsstripe

import (
	"context"
	"time"

	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/servicestripe"
	"github.com/stripe/stripe-go/v86"
	checkout "github.com/stripe/stripe-go/v86/checkout/session"
	stripeInvoice "github.com/stripe/stripe-go/v86/invoice"
	stripePrice "github.com/stripe/stripe-go/v86/price"
	stripeSubscription "github.com/stripe/stripe-go/v86/subscription"
	stripeSchedule "github.com/stripe/stripe-go/v86/subscriptionschedule"
)

var ErrCheckout = servicestripe.ErrCheckout
var ErrReconcile = servicestripe.ErrReconcile

// Intent is a frozen, server-owned checkout request, not an HTTP request DTO.
// Reuse every field (including CreatedAt) when retrying an ambiguous creation.
type Intent struct {
	ID            string
	PersonID      string
	Email         string
	PaidSlots     int64
	CreatedAt     time.Time
	ReturnBaseURL string
}

type Client struct {
	ListCheckouts      func(*stripe.CheckoutSessionListParams) (*stripe.CheckoutSessionList, error)
	Expire             func(string, *stripe.CheckoutSessionExpireParams) (*stripe.CheckoutSession, error)
	ReleaseSchedule    func(string, *stripe.SubscriptionScheduleReleaseParams) (*stripe.SubscriptionSchedule, error)
	Schedule           func(string, *stripe.SubscriptionScheduleParams) (*stripe.SubscriptionSchedule, error)
	UpdateSchedule     func(string, *stripe.SubscriptionScheduleParams) (*stripe.SubscriptionSchedule, error)
	CreateSchedule     func(*stripe.SubscriptionScheduleParams) (*stripe.SubscriptionSchedule, error)
	PreviewInvoice     func(*stripe.InvoiceCreatePreviewParams) (*stripe.Invoice, error)
	UpdateSubscription func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error)
	Subscription       func(string, *stripe.SubscriptionParams) (*stripe.Subscription, error)
	ListInvoices       func(*stripe.InvoiceListParams) (*stripe.InvoiceList, error)
	Invoice            func(string, *stripe.InvoiceParams) (*stripe.Invoice, error)
	Price              func(string, *stripe.PriceParams) (*stripe.Price, error)
	Create             func(*stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	Retrieve           func(string, *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	Live               bool
}

func NewClient(live bool) Client {
	return Client{ListCheckouts: listCheckoutPage, Expire: checkout.Expire, ReleaseSchedule: stripeSchedule.Release, Schedule: stripeSchedule.Get, UpdateSchedule: stripeSchedule.Update, CreateSchedule: stripeSchedule.New, PreviewInvoice: stripeInvoice.CreatePreview, UpdateSubscription: stripeSubscription.Update, Create: checkout.New, Retrieve: checkout.Get, Invoice: stripeInvoice.Get, Price: stripePrice.Get, Subscription: stripeSubscription.Get, Live: live, ListInvoices: func(params *stripe.InvoiceListParams) (*stripe.InvoiceList, error) {
		params.Single = true
		iter := stripeInvoice.List(params)
		for iter.Next() {
		}
		if err := iter.Err(); err != nil {
			return nil, err
		}
		return iter.InvoiceList(), nil
	}}
}

func serviceCheckoutIntent(in Intent) servicestripe.CheckoutIntent {
	return servicestripe.CheckoutIntent{ID: in.ID, PersonID: in.PersonID, Email: in.Email, ProductCode: servicebilling.AlertsProduct, TaxCode: servicebilling.AlertsStripeTaxCode, Quantity: in.PaidSlots, CreatedAt: in.CreatedAt, ReturnOrigin: in.ReturnBaseURL, SuccessURL: in.ReturnBaseURL + "/services/alerts/checkout/" + in.ID + "?result=returned", CancelURL: in.ReturnBaseURL + "/services/alerts/checkout/" + in.ID + "?result=cancelled"}
}

func (c Client) ResolveDeletedCheckout(ctx context.Context, in Intent, reference string) (*Binding, error) {
	b, err := (servicestripe.CheckoutClient{Retrieve: c.Retrieve, Expire: c.Expire, Live: c.Live}).ResolveDeletedCheckout(ctx, serviceCheckoutIntent(in), reference)
	if err != nil || b == nil {
		return nil, err
	}
	return &Binding{IntentID: b.IntentID, PersonID: b.PersonID, SubscriptionID: b.SubscriptionID, CustomerID: b.CustomerID, PriceID: b.PriceID, ProductID: b.ProductID, PaidSlots: b.Quantity, Live: b.Live}, nil
}
func checkoutParams(ctx context.Context, in Intent, now time.Time) (*stripe.CheckoutSessionParams, error) {
	return servicestripe.CheckoutParams(ctx, serviceCheckoutIntent(in), now)
}
func (c Client) Open(ctx context.Context, in Intent, reference string, now time.Time) (string, string, error) {
	shared := servicestripe.CheckoutClient{Create: c.Create, Retrieve: c.Retrieve, Live: c.Live}
	return shared.Open(ctx, serviceCheckoutIntent(in), reference, now)
}

// ValidateIntent validates a frozen checkout before it is reserved in storage.
func ValidateIntent(in Intent, now time.Time) error {
	_, err := checkoutParams(context.Background(), in, now)
	return err
}

func listCheckoutPage(p *stripe.CheckoutSessionListParams) (*stripe.CheckoutSessionList, error) {
	p.Single = true
	iter := checkout.List(p)
	for iter.Next() {
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return iter.CheckoutSessionList(), nil
}
func (c Client) FindExistingCheckout(ctx context.Context, in Intent) (string, error) {
	return (servicestripe.CheckoutClient{List: c.ListCheckouts, Live: c.Live}).FindExistingCheckout(ctx, serviceCheckoutIntent(in))
}
