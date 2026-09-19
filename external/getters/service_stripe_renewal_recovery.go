package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

// RecoverServiceStripePayments discovers missed initial and renewal payments.
// Existing financial evidence, including refunded orders, is never overwritten.
func RecoverServiceStripePayments(ctx context.Context, app *config.AppContext, person, id, product string, checkout servicestripe.CheckoutClient, invoices servicestripe.InvoiceClient) error {
	if _, err := ServiceStripeCheckoutForPerson(app, person, id, product); err != nil {
		return err
	}
	var raw []byte
	err := app.DB.QueryRow(ctx, `SELECT binding FROM service_stripe_subscriptions WHERE checkout_id=$1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return RecoverServiceStripeCheckout(ctx, app, person, id, product, checkout, invoices)
	}
	if err != nil {
		return err
	}
	var binding servicestripe.Binding
	if err = json.Unmarshal(raw, &binding); err != nil {
		return err
	}
	if binding.IntentID != id || binding.PersonID != person || binding.ProductCode != product {
		return servicestripe.ErrInvoice
	}
	cursor := ""
	seen := map[string]bool{}
	var firstErr error
	for page := 0; page < 100; page++ {
		ids, next, err := invoices.PaidInvoiceIDs(ctx, binding, cursor)
		if err != nil {
			return err
		}
		for _, invoice := range ids {
			if seen[invoice] {
				return servicestripe.ErrInvoice
			}
			seen[invoice] = true
			var recorded bool
			if err = app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_stripe_invoices WHERE invoice_id=$1 AND checkout_id=$2)`, invoice, id).Scan(&recorded); err != nil {
				return err
			}
			if recorded {
				continue
			}
			if _, err = ReconcileServiceStripeInvoice(ctx, app, person, id, product, invoice, invoices); err != nil && firstErr == nil {
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
