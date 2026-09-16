package getters

import (
	"btcpp-web/internal/alertsstripe"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

// Caller holds the account billing lock, including the subscription lookup.
// Grace facts are separate from paid access and cannot alter invoice evidence.
func projectAlertsStripeRenewal(ctx context.Context, tx pgx.Tx, b alertsstripe.Binding, state alertsstripe.SubscriptionState, client alertsstripe.Client, now time.Time) error {
	terminal := ""
	switch state.State {
	case "cancelled", "cancelling":
		terminal = "cancelled"
	case "paused":
		terminal = "paused"
	case "disputed":
		terminal = "disputed"
	case "expired":
		terminal = "expired"
	}
	if terminal != "" {
		_, err := tx.Exec(ctx, `UPDATE alerts_stripe_renewals SET state=$2,revision=revision+1,observed_at=now() WHERE checkout_id=$1 AND state='failed'`, b.IntentID, terminal)
		return err
	}
	if state.State != "renewal_unpaid" || state.LatestInvoice == "" {
		return nil
	}
	var recorded bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_invoices WHERE invoice_id=$1) OR EXISTS(SELECT 1 FROM alerts_stripe_renewals WHERE invoice_id=$1 AND state<>'failed')`, state.LatestInvoice).Scan(&recorded); err != nil {
		return err
	}
	if recorded {
		return nil
	}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT i.paid_period FROM alerts_stripe_invoices i JOIN shop_orders o ON o.id=i.order_id
 JOIN service_purchase_requests p ON p.order_id=o.id JOIN service_entitlements e ON e.purchase_id=p.id
 WHERE i.checkout_id=$1 AND i.ends_at=$2 AND o.status='paid' AND e.revoked_at IS NULL`, b.IntentID, time.Unix(state.PeriodStart, 0)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `UPDATE alerts_stripe_renewals SET state='expired',revision=revision+1,observed_at=now() WHERE invoice_id=$1 AND checkout_id=$2 AND state='failed'`, state.LatestInvoice, b.IntentID)
		return err
	}
	if err != nil {
		return err
	}
	var previous alertsstripe.PaidPeriod
	if err = json.Unmarshal(raw, &previous); err != nil {
		return err
	}
	// The preceding paid period may consist of a base invoice plus upgrades.
	// Only paid, unrevoked grants ending at this renewal support grace.
	var added int64
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(e.quantity),0) FROM alerts_stripe_increase_payments i JOIN shop_orders o ON o.id=i.order_id JOIN service_purchase_requests p ON p.order_id=o.id JOIN service_entitlements e ON e.purchase_id=p.id WHERE i.checkout_id=$1 AND e.starts_at>=$2 AND e.ends_at=$3 AND o.status='paid' AND o.paid_at<=$4 AND e.revoked_at IS NULL`, b.IntentID, previous.Start, previous.End, now).Scan(&added)
	if err != nil {
		return err
	}
	previous.Quantity += added
	failure, err := client.FailedRenewalInvoice(ctx, state.LatestInvoice, b, previous, now)
	if errors.Is(err, alertsstripe.ErrInvoice) {
		return nil
	} // Verified but ineligible; no grace.
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO alerts_stripe_renewals(invoice_id,checkout_id,state,due_at,period_end,paid_slots) VALUES($1,$2,'failed',$3,$4,$5)
 ON CONFLICT(invoice_id) DO NOTHING`, failure.InvoiceID, b.IntentID, failure.DueAt, failure.PeriodEnd, failure.Quantity)
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT checkout_id=$2 AND due_at=$3 AND period_end=$4 AND paid_slots=$5 FROM alerts_stripe_renewals WHERE invoice_id=$1`, failure.InvoiceID, b.IntentID, failure.DueAt, failure.PeriodEnd, failure.Quantity).Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return alertsstripe.ErrInvoice
	}
	return nil
}
