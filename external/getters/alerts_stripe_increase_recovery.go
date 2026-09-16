package getters

import (
	"context"
	"encoding/json"
	"errors"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
)

// ReconcileAlertsStripeIncreaseInvoice verifies a provider candidate against a
// saved confirmation. Neither a callback body nor a return URL supplies terms.
func ReconcileAlertsStripeIncreaseInvoice(ctx context.Context, app *config.AppContext, person, quoteID, invoiceID string, client alertsstripe.Client) (*ServicePurchase, error) {
	quote, err := AlertsStripeIncreaseQuoteForPerson(ctx, app, person, quoteID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	var state string
	err = app.DB.QueryRow(ctx, `SELECT s.binding,i.state FROM alerts_stripe_subscriptions s JOIN alerts_stripe_increases i ON i.checkout_id=s.checkout_id WHERE i.quote_id=$1 AND i.checkout_id=$2`, quoteID, quote.CheckoutID).Scan(&raw, &state)
	if err != nil {
		return nil, err
	}
	if state != "pending" && state != "applied" {
		return nil, alertsstripe.ErrInvoice
	}
	var b alertsstripe.Binding
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != quote.CheckoutID {
		return nil, alertsstripe.ErrInvoice
	}
	// Identity comes from the original subscription; the immutable reviewed
	// quote supplies the pre-upgrade quantity even on a later callback replay.
	b.PaidSlots = quote.Preview.PreviousSlots
	paid, err := client.PaidIncreaseInvoice(ctx, invoiceID, b, alertsstripe.IncreaseIntent{ID: quoteID, Preview: quote.Preview})
	if err != nil {
		return nil, err
	}
	return recordAlertsStripeIncreasePayment(ctx, app, person, *paid)
}

// RecoverAlertsStripeIncrease discovers a lost invoice reference without another
// charge attempt. Listing only produces candidates; every candidate is re-read
// and verified before the transaction can grant access.
func RecoverAlertsStripeIncrease(ctx context.Context, app *config.AppContext, person, quoteID string, client alertsstripe.Client) (bool, error) {
	quote, err := AlertsStripeIncreaseQuoteForPerson(ctx, app, person, quoteID)
	if err != nil {
		return false, err
	}
	var raw []byte
	if err = app.DB.QueryRow(ctx, `SELECT s.binding FROM alerts_stripe_subscriptions s JOIN alerts_stripe_increases i ON i.checkout_id=s.checkout_id WHERE i.quote_id=$1 AND i.state='pending'`, quoteID).Scan(&raw); err != nil {
		return false, err
	}
	var b alertsstripe.Binding
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != quote.CheckoutID {
		return false, alertsstripe.ErrInvoice
	}
	var attempted bool
	if err = app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_increase_submissions WHERE quote_id=$1)`, quoteID).Scan(&attempted); err != nil {
		return false, err
	}
	if attempted {
		observed, e := RecoverAlertsStripeIncreaseReference(ctx, app, person, quoteID, client)
		if e == nil {
			if observed.Status == "void" {
				return false, ResolveVoidedAlertsStripeIncrease(ctx, app, person, quoteID, client)
			}
			if observed.Status != "paid" {
				return false, nil
			}
			_, e = ReconcileAlertsStripeIncreaseInvoice(ctx, app, person, quoteID, observed.InvoiceID, client)
			return e == nil, e
		}
		if !errors.Is(e, alertsstripe.ErrInvoice) {
			return false, e
		}
	}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		ids, next, err := client.PaidInvoiceIDs(ctx, b, cursor)
		if err != nil {
			return false, err
		}
		for _, id := range ids {
			if seen[id] {
				return false, alertsstripe.ErrInvoice
			}
			seen[id] = true
			if _, err = ReconcileAlertsStripeIncreaseInvoice(ctx, app, person, quoteID, id, client); err == nil {
				return true, nil
			}
			if !errors.Is(err, alertsstripe.ErrInvoice) {
				return false, err
			}
		}
		if next == "" {
			return false, nil
		}
		cursor = next
	}
	return false, errors.New("Stripe upgrade recovery page limit exceeded")
}
