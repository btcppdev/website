package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
)

func alertsIncreaseReference(ctx context.Context, app *config.AppContext, person, id string) (alertsstripe.Binding, alertsstripe.IncreaseIntent, string, error) {
	var b alertsstripe.Binding
	var intent alertsstripe.IncreaseIntent
	quote, err := AlertsStripeIncreaseQuoteForPerson(ctx, app, person, id)
	if err != nil {
		return b, intent, "", err
	}
	var raw []byte
	var reference string
	err = app.DB.QueryRow(ctx, `SELECT s.binding,COALESCE(a.invoice_id,'') FROM alerts_stripe_increase_submissions a JOIN alerts_stripe_increases i ON i.quote_id=a.quote_id JOIN alerts_stripe_subscriptions s ON s.checkout_id=i.checkout_id WHERE a.quote_id=$1 AND i.state='pending'`, id).Scan(&raw, &reference)
	if err != nil {
		return b, intent, "", err
	}
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != quote.CheckoutID {
		return b, intent, "", alertsstripe.ErrSubscription
	}
	b.PaidSlots = quote.Preview.PreviousSlots
	intent = alertsstripe.IncreaseIntent{ID: id, Preview: quote.Preview}
	return b, intent, reference, nil
}

// Recover a committed attempt's invoice without submitting another update.
func RecoverAlertsStripeIncreaseReference(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) (*alertsstripe.IncreaseInvoiceObservation, error) {
	b, intent, reference, err := alertsIncreaseReference(ctx, app, person, id)
	if err != nil {
		return nil, err
	}
	var observed *alertsstripe.IncreaseInvoiceObservation
	if reference == "" {
		observed, err = client.LatestIncreaseInvoice(ctx, b, intent)
	} else {
		observed, err = client.InspectIncreaseInvoice(ctx, reference, b, intent)
	}
	if err != nil {
		return nil, err
	}
	result, err := app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET invoice_id=$2 WHERE quote_id=$1 AND (invoice_id IS NULL OR invoice_id=$2)`, id, observed.InvoiceID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, alertsstripe.ErrReconcile
	}
	return observed, nil
}

// Account-owned handoff: callers cannot substitute another invoice or terms.
func AlertsStripeIncreasePaymentLink(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) (string, error) {
	b, intent, reference, err := alertsIncreaseReference(ctx, app, person, id)
	if err != nil {
		return "", err
	}
	if reference == "" {
		return "", alertsstripe.ErrReconcile
	}
	return client.IncreasePaymentLink(ctx, reference, b, intent)
}
