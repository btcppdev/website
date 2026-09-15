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
	var raw []byte
	err := app.DB.QueryRow(ctx, `SELECT s.binding FROM alerts_stripe_subscriptions s JOIN alerts_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2`, id, person).Scan(&raw)
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
			if err = app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_invoices WHERE invoice_id=$1 AND checkout_id=$2)`, invoice, id).Scan(&recorded); err != nil {
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
