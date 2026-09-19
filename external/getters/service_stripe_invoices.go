package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

// ReconcileServiceStripeInvoice retrieves provider evidence against the saved
// product binding. Recording the invoice and queuing fulfillment are atomic.
func ReconcileServiceStripeInvoice(ctx context.Context, app *config.AppContext, person, checkoutID, product, invoiceID string, client servicestripe.InvoiceClient) (*ServicePurchase, error) {
	stored, err := ServiceStripeCheckoutForPerson(app, person, checkoutID, product)
	if err != nil {
		return nil, err
	}
	var raw []byte
	if err = app.DB.QueryRow(ctx, `SELECT binding FROM service_stripe_subscriptions WHERE checkout_id=$1`, stored.Intent.ID).Scan(&raw); err != nil {
		return nil, err
	}
	var b servicestripe.Binding
	if err = json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	if b.PersonID != person || b.IntentID != checkoutID || b.ProductCode != product {
		return nil, servicestripe.ErrInvoice
	}
	paid, err := client.PaidInvoice(ctx, invoiceID, b)
	if err != nil {
		return nil, err
	}
	return recordServiceStripeInvoice(app, b, *paid)
}

func recordServiceStripeInvoice(app *config.AppContext, b servicestripe.Binding, paid servicestripe.PaidPeriod) (*ServicePurchase, error) {
	rawBinding, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	rawPaid, err := json.Marshal(paid)
	if err != nil {
		return nil, err
	}
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+b.PersonID); err != nil {
		return nil, err
	}
	var email string
	var same bool
	err = tx.QueryRow(ctx, `SELECT c.intent->>'Email',s.binding=$3::jsonb FROM service_stripe_subscriptions s JOIN service_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2 FOR UPDATE OF c`, b.IntentID, b.PersonID, rawBinding).Scan(&email, &same)
	if err != nil {
		return nil, err
	}
	currentBinding := same
	out := &ServicePurchase{}
	err = tx.QueryRow(ctx, `SELECT o.id::text,o.public_id,r.id::text,o.buyer_email::text,i.checkout_id=$2 AND i.paid_period=$3::jsonb FROM service_stripe_invoices i JOIN shop_orders o ON o.id=i.order_id JOIN service_purchase_requests r ON r.order_id=o.id WHERE i.invoice_id=$1`, paid.InvoiceID, b.IntentID, rawPaid).Scan(&out.OrderID, &out.PublicID, &out.PurchaseID, &out.BuyerEmail, &same)
	if err == nil {
		if !same {
			return nil, servicestripe.ErrInvoice
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if !currentBinding {
		return nil, servicestripe.ErrInvoice
	}
	// Monthly and annual invoice periods cannot overlap. Prorations cannot
	// pass the full-period verifier.
	var overlaps bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_stripe_invoices WHERE checkout_id=$1 AND starts_at<$3 AND ends_at>$2)`, b.IntentID, paid.Start, paid.End).Scan(&overlaps)
	if err != nil {
		return nil, err
	}
	if overlaps {
		return nil, servicestripe.ErrInvoice
	}
	plan, planErr := servicebilling.LookupProductPlan(b.ProductCode)
	if planErr != nil || plan.Family != "insider" || paid.Quantity != 1 {
		return nil, servicestripe.ErrInvoice
	}
	quote := ServiceQuote{RecurringInvoice: true, ProductCode: b.ProductCode, Name: plan.Name, Provider: "stripe", Quantity: 1, UnitCents: paid.TotalCents, TotalCents: paid.TotalCents, TaxCents: paid.TaxCents, PeriodStart: &paid.Start, PeriodEnd: &paid.End, ExpiresAt: paid.End, BillingMonths: plan.Months, Fulfillment: json.RawMessage(`{}`)}

	if err = recordStripeServicePayment(ctx, tx, b.PersonID, email, paid, quote, out); err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_stripe_invoices(invoice_id,checkout_id,order_id,paid_period,starts_at,ends_at) VALUES($1,$2,$3,$4,$5,$6)`, paid.InvoiceID, b.IntentID, out.OrderID, rawPaid, paid.Start, paid.End)
	if err != nil {
		return nil, err
	}

	return out, tx.Commit(ctx)
}
