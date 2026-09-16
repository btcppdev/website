package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

// RecoverAlertsStripePayments repairs initial checkout and later paid invoices.
// Existing evidence (including refunded orders) is never overwritten by polling.
func RecoverAlertsStripePayments(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) error {
	// Recover paid upgrades before reading the effective binding or refreshing
	// Stripe state, which may already contain the new quantity.
	var increase string
	err := app.DB.QueryRow(ctx, `SELECT i.quote_id::text FROM alerts_stripe_increases i JOIN alerts_stripe_checkouts c ON c.id=i.checkout_id WHERE c.id=$1 AND c.person_id=$2 AND i.state='pending'`, id, person).Scan(&increase)
	if err == nil {
		if err = ProcessAlertsStripeIncrease(ctx, app, person, increase, client); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var decrease string
	err = app.DB.QueryRow(ctx, `SELECT d.quote_id::text FROM alerts_stripe_decreases d JOIN alerts_stripe_checkouts c ON c.id=d.checkout_id WHERE c.id=$1 AND c.person_id=$2 AND d.state IN ('pending','scheduled')`, id, person).Scan(&decrease)
	if err == nil {
		if err = ProcessAlertsStripeDecrease(ctx, app, person, decrease, client); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var raw []byte
	err = app.DB.QueryRow(ctx, `SELECT s.binding FROM alerts_stripe_current_subscriptions s JOIN alerts_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2`, id, person).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return RecoverAlertsStripeCheckout(ctx, app, person, id, client)
	}
	if err != nil {
		return err
	}
	var binding alertsstripe.Binding
	if err = json.Unmarshal(raw, &binding); err != nil {
		return err
	}
	if binding.IntentID != id || binding.PersonID != person {
		return alertsstripe.ErrInvoice
	}
	var cancellation string
	err = app.DB.QueryRow(ctx, `SELECT id::text FROM alerts_stripe_cancellations WHERE checkout_id=$1 AND state='pending'`, id).Scan(&cancellation)
	if err == nil {
		if _, err = ProcessAlertsStripeCancellation(ctx, app, person, cancellation, client); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err = RefreshAlertsStripeSubscription(ctx, app, person, id, client); err != nil {
		return err
	}
	cursor := ""
	seen := map[string]bool{}
	var firstErr error
	for page := 0; page < 100; page++ {
		ids, next, err := client.PaidInvoiceIDs(ctx, binding, cursor)
		if err != nil {
			return err
		}
		for _, invoice := range ids {
			if seen[invoice] {
				return alertsstripe.ErrInvoice
			}
			seen[invoice] = true
			var recorded bool
			if err = app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_invoices WHERE invoice_id=$1 AND checkout_id=$2 UNION ALL SELECT 1 FROM alerts_stripe_increase_payments WHERE invoice_id=$1 AND checkout_id=$2)`, invoice, id).Scan(&recorded); err != nil {
				return err
			}
			if recorded {
				continue
			}
			if _, err = ReconcileAlertsStripeInvoice(ctx, app, person, id, invoice, client); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if next == "" {
			return firstErr
		}
		cursor = next
	}
	return errors.New("Stripe invoice recovery page limit exceeded")
}
