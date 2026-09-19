package getters

import (
	"context"
	"encoding/json"
	"errors"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"github.com/jackc/pgx/v5"
)

// ResolveAlertsStripeSubscription compares authenticated Stripe evidence with
// the persisted intent; callbacks cannot choose the purchaser or paid quantity.
func ResolveAlertsStripeSubscription(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client, recoveredReference ...string) (*alertsstripe.Binding, error) {
	stored, err := AlertsStripeCheckoutForPerson(app, person, id)
	if err != nil {
		return nil, err
	}
	if len(recoveredReference) > 1 {
		return nil, alertsstripe.ErrCheckout
	}
	if len(recoveredReference) == 1 {
		if stored.Reference != "" && stored.Reference != recoveredReference[0] {
			return nil, alertsstripe.ErrCheckout
		}
		stored.Reference = recoveredReference[0]
	}
	binding, err := client.CompletedBinding(ctx, stored.Intent, stored.Reference)
	if err != nil {
		return nil, err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return nil, err
	}
	result, err := tx.Exec(ctx, `UPDATE alerts_stripe_checkouts SET stripe_session_id=$3 WHERE id=$1 AND person_id=$2 AND closed_at IS NULL AND (stripe_session_id IS NULL OR stripe_session_id=$3)`, id, person, stored.Reference)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, alertsstripe.ErrCheckout
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3) ON CONFLICT(checkout_id) DO NOTHING`, id, binding.SubscriptionID, raw)
	if err != nil {
		return nil, err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT subscription_id=$2 AND binding=$3::jsonb FROM alerts_stripe_subscriptions WHERE checkout_id=$1`, id, binding.SubscriptionID, raw).Scan(&same)
	if err != nil {
		return nil, err
	}
	if !same {
		return nil, alertsstripe.ErrInvoice
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return binding, nil
}

// ReconcileAlertsStripeInvoice never accepts payment evidence from a browser.
// The invoice lookup is authenticated; all financial/access records commit at
// once, with invoice ID as the durable deduplication key.
func ReconcileAlertsStripeInvoice(ctx context.Context, app *config.AppContext, person, checkoutID, invoiceID string, client alertsstripe.Client) (*ServicePurchase, error) {
	var raw []byte
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT s.binding FROM alerts_stripe_current_subscriptions s JOIN alerts_stripe_checkouts c ON c.id=s.checkout_id WHERE c.person_id=$1 AND c.id=$2`, person, checkoutID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var binding alertsstripe.Binding
	if err = json.Unmarshal(raw, &binding); err != nil {
		return nil, err
	}
	if binding.PersonID != person || binding.IntentID != checkoutID {
		return nil, alertsstripe.ErrInvoice
	}
	// A repeated callback for an older paid month must use that month's
	// quantity, rather than the current upgraded allowance.
	var recorded []byte
	err = app.DB.QueryRow(ctx, `SELECT paid_period FROM alerts_stripe_invoices WHERE invoice_id=$1 AND checkout_id=$2`, invoiceID, checkoutID).Scan(&recorded)
	if err == nil {
		var original alertsstripe.PaidPeriod
		if json.Unmarshal(recorded, &original) != nil {
			return nil, alertsstripe.ErrInvoice
		}
		binding.PaidSlots = original.Quantity
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	paid, err := client.PaidInvoice(ctx, invoiceID, binding)
	if err != nil {
		return nil, err
	}
	return recordAlertsStripeInvoice(app, binding, *paid)
}

func recordAlertsStripeInvoice(app *config.AppContext, b alertsstripe.Binding, paid alertsstripe.PaidPeriod) (*ServicePurchase, error) {
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
	err = tx.QueryRow(ctx, `SELECT c.intent->>'Email',s.binding=$3::jsonb FROM alerts_stripe_current_subscriptions s JOIN alerts_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2 FOR UPDATE OF c`, b.IntentID, b.PersonID, rawBinding).Scan(&email, &same)
	if err != nil {
		return nil, err
	}
	currentBinding := same
	out := &ServicePurchase{}
	err = tx.QueryRow(ctx, `SELECT o.id::text,o.public_id,i.checkout_id=$2 AND i.paid_period=$3::jsonb FROM alerts_stripe_invoices i JOIN shop_orders o ON o.id=i.order_id WHERE i.invoice_id=$1`, paid.InvoiceID, b.IntentID, rawPaid).Scan(&out.OrderID, &out.PublicID, &same)
	if err == nil {
		if !same {
			return nil, alertsstripe.ErrInvoice
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if !currentBinding {
		return nil, alertsstripe.ErrInvoice
	}
	// Full-month invoices for this subscription cannot overlap. Upgrade invoices
	// use a separate quantity-change path and cannot pass the monthly verifier.
	var overlaps bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_invoices WHERE checkout_id=$1 AND starts_at<$3 AND ends_at>$2)`, b.IntentID, paid.Start, paid.End).Scan(&overlaps)
	if err != nil {
		return nil, err
	}
	if overlaps {
		return nil, alertsstripe.ErrInvoice
	}
	quote := ServiceQuote{ProductCode: "alerts.repository-slots", Name: "Bitcoin++ Alerts additional repositories", Provider: "stripe", Quantity: paid.Quantity, UnitCents: servicebilling.AlertsMonthlySlotCents, TotalCents: paid.TotalCents, TaxCents: paid.TaxCents, PeriodStart: &paid.Start, PeriodEnd: &paid.End, ExpiresAt: paid.End, BillingMonths: 1}
	if err = recordStripeServicePayment(ctx, tx, b.PersonID, email, paid, quote, out); err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO alerts_stripe_invoices(invoice_id,checkout_id,order_id,paid_period,starts_at,ends_at) VALUES($1,$2,$3,$4,$5,$6)`, paid.InvoiceID, b.IntentID, out.OrderID, rawPaid, paid.Start, paid.End)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_renewals SET state='recovered',revision=revision+1,observed_at=now() WHERE invoice_id=$1 AND checkout_id=$2 AND state<>'recovered'`, paid.InvoiceID, b.IntentID); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// recordStripeServicePayment shares the existing receipt and entitlement ledger
// between monthly renewals and prorations. The caller owns verification,
// serialization, invoice deduplication and the transaction commit.
func recordStripeServicePayment(ctx context.Context, tx pgx.Tx, person, email string, paid alertsstripe.PaidPeriod, quote ServiceQuote, out *ServicePurchase) error {
	snapshot, err := json.Marshal(quote)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `INSERT INTO shop_orders(public_id,buyer_email,buyer_person_id,checkout_kind,payment_provider,payment_provider_id,currency,subtotal_cents,sales_tax_amount_cents,total_cents,status,paid_at) VALUES(gen_random_uuid()::text,$1,$2,'service','stripe',$3,'USD',$4,$5,$6,'paid',$7) RETURNING id::text,public_id`, email, person, paid.InvoiceID, paid.TotalCents-paid.TaxCents, paid.TaxCents, paid.TotalCents, paid.PaidAt).Scan(&out.OrderID, &out.PublicID)
	if err != nil {
		return err
	}
	// A prorated total may not divide evenly by the slot count. Split the
	// rounding cent across two receipt lines rather than misstate a unit price.
	remainder := paid.TotalCents % paid.Quantity
	quantities := []int64{paid.Quantity - remainder, remainder}
	units := []int64{paid.TotalCents / paid.Quantity, paid.TotalCents/paid.Quantity + 1}
	taxLeft := paid.TaxCents
	for i, quantity := range quantities {
		if quantity == 0 {
			continue
		}
		total := quantity * units[i]
		tax := taxLeft
		if i == 0 && remainder != 0 && paid.TotalCents > 0 {
			tax = paid.TaxCents * total / paid.TotalCents
		}
		taxLeft -= tax
		_, err = tx.Exec(ctx, `INSERT INTO shop_order_items(order_id,quantity,unit_price_cents,tax_amount_cents,line_total_cents,product_name_snapshot,fulfillment_method,service_product_code,service_resource_id,service_period_start,service_period_end,fulfilled_quantity,status) VALUES($1,$2,$3,$4,$5,$6,'service','alerts.repository-slots','',$7,$8,$2,'fulfilled')`, out.OrderID, quantity, units[i], tax, total, quote.Name, paid.Start, paid.End)
		if err != nil {
			return err
		}
	}
	var purchaseID, attemptID string
	err = tx.QueryRow(ctx, `INSERT INTO service_purchase_requests(person_id,request_key,order_id,quote_snapshot) VALUES($1,$2,$3,$4) RETURNING id::text`, person, "stripe-invoice:"+paid.InvoiceID, out.OrderID, snapshot).Scan(&purchaseID)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `INSERT INTO service_payment_attempts(purchase_id,provider,provider_reference,settled_at) VALUES($1,'stripe',$2,$3) RETURNING id::text`, purchaseID, paid.InvoiceID, paid.PaidAt).Scan(&attemptID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_payment_events(provider,event_id,attempt_id,total_cents,currency,paid_at) VALUES('stripe',$1,$2,$3,'USD',$4)`, "invoice-paid:"+paid.InvoiceID, attemptID, paid.TotalCents, paid.PaidAt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO service_entitlements(purchase_id,person_id,product_code,quantity,starts_at,ends_at) VALUES($1,$2,'alerts.repository-slots',$3,$4,$5)`, purchaseID, person, paid.Quantity, paid.Start, paid.End)
	if err != nil {
		return err
	}
	return nil
}
