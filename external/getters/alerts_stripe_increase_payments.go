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

// recordAlertsStripeIncreasePayment accepts verified provider evidence only.
// It atomically records the receipt, added-slot grant, quantity revision and
// resolved confirmation. No HTTP handler may supply paid evidence directly.
func recordAlertsStripeIncreasePayment(ctx context.Context, app *config.AppContext, person string, paid alertsstripe.PaidIncrease) (*ServicePurchase, error) {
	quote, err := AlertsStripeIncreaseQuoteForPerson(ctx, app, person, paid.QuoteID)
	if err != nil {
		return nil, err
	}
	q := quote.Preview
	if paid.PreviousSlots != q.PreviousSlots || paid.PaidSlots != q.PaidSlots || paid.AdditionalSlots != q.PaidSlots-q.PreviousSlots || paid.AdditionalSlots < 1 || paid.TotalCents != q.TotalCents || paid.TaxCents != q.TaxCents || paid.Start.Unix() != q.ProrationDate || paid.End.Unix() != q.PeriodEnd || paid.PaidAt.Before(paid.Start) {
		return nil, alertsstripe.ErrInvoice
	}
	rawPaid, err := json.Marshal(paid)
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
	out := &ServicePurchase{}
	var same bool
	err = tx.QueryRow(ctx, `SELECT o.id::text,o.public_id,p.paid_increase=$2::jsonb FROM alerts_stripe_increase_payments p JOIN shop_orders o ON o.id=p.order_id WHERE p.quote_id=$1`, paid.QuoteID, rawPaid).Scan(&out.OrderID, &out.PublicID, &same)
	if err == nil {
		if !same {
			return nil, alertsstripe.ErrInvoice
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var raw, acceptanceRaw []byte
	var email, status string
	err = tx.QueryRow(ctx, `SELECT s.binding,c.intent->>'Email',i.state,i.legal_acceptance FROM alerts_stripe_checkouts c JOIN alerts_stripe_current_subscriptions s ON s.checkout_id=c.id JOIN alerts_stripe_increases i ON i.checkout_id=c.id WHERE c.id=$1 AND c.person_id=$2 AND i.quote_id=$3 FOR UPDATE OF c,i`, quote.CheckoutID, person, paid.QuoteID).Scan(&raw, &email, &status, &acceptanceRaw)
	if err != nil {
		return nil, err
	}
	var acceptance *servicebilling.LegalAcceptance
	if len(acceptanceRaw) > 0 {
		if err = json.Unmarshal(acceptanceRaw, &acceptance); err != nil {
			return nil, err
		}
	}
	var b alertsstripe.Binding
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != quote.CheckoutID || b.SubscriptionID != paid.SubscriptionID || b.PaidSlots != paid.PreviousSlots || status != "pending" {
		return nil, alertsstripe.ErrInvoice
	}
	// A proration only extends a verified, paid base period. It cannot establish
	// the subscription's base allowance or silently replace an unpaid renewal.
	var base bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_invoices i JOIN shop_orders o ON o.id=i.order_id JOIN service_purchase_requests r ON r.order_id=o.id JOIN service_entitlements e ON e.purchase_id=r.id WHERE i.checkout_id=$1 AND i.starts_at=to_timestamp($2) AND i.ends_at=$3 AND o.status='paid' AND e.revoked_at IS NULL AND e.person_id=$4 AND e.product_code='alerts.repository-slots')`, quote.CheckoutID, q.PeriodStart, paid.End, person).Scan(&base)
	if err != nil {
		return nil, err
	}
	if !base {
		return nil, alertsstripe.ErrInvoice
	}
	period := alertsstripe.PaidPeriod{InvoiceID: paid.InvoiceID, SubscriptionID: paid.SubscriptionID, Quantity: paid.AdditionalSlots, TotalCents: paid.TotalCents, TaxCents: paid.TaxCents, Start: paid.Start, End: paid.End, PaidAt: paid.PaidAt}
	// Stripe prorates the old and replacement quantities independently. The net
	// total need not divide evenly by the added slot count; retain the exact total.
	serviceQuote := ServiceQuote{LegalAcceptance: acceptance, ProductCode: "alerts.repository-slots", Name: "Bitcoin++ Alerts prorated additional repositories", Provider: "stripe", Quantity: paid.AdditionalSlots, UnitCents: paid.TotalCents / paid.AdditionalSlots, TotalCents: paid.TotalCents, TaxCents: paid.TaxCents, PeriodStart: &paid.Start, PeriodEnd: &paid.End, ExpiresAt: paid.End, BillingMonths: 12}
	if err = recordStripeServicePayment(ctx, tx, person, email, period, serviceQuote, out); err != nil {
		return nil, err
	}
	b.PaidSlots = paid.PaidSlots
	next, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO alerts_stripe_increase_payments(quote_id,checkout_id,invoice_id,order_id,paid_increase,binding) VALUES($1,$2,$3,$4,$5,$6)`, paid.QuoteID, quote.CheckoutID, paid.InvoiceID, out.OrderID, rawPaid, next); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_increases SET state='applied',updated_at=now() WHERE quote_id=$1`, paid.QuoteID); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=revision+1,observed_at=now() WHERE checkout_id=$1`, quote.CheckoutID); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
