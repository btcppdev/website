package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ResolveVoidedAlertsStripeIncrease releases a confirmation only after fresh
// provider reads. A live submission lease prevents racing a charge attempt.
func ResolveVoidedAlertsStripeIncrease(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) error {
	return resolveAlertsStripeIncrease(ctx, app, person, id, client, false)
}

func resolveAlertsStripeIncrease(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client, closing bool) error {
	quote, err := AlertsStripeIncreaseQuoteForPerson(ctx, app, person, id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return err
	}
	var raw []byte
	var state, reference string
	var lease time.Time
	err = tx.QueryRow(ctx, `SELECT s.binding,i.state,COALESCE(a.invoice_id,''),a.lease_until FROM alerts_stripe_increases i JOIN alerts_stripe_increase_submissions a ON a.quote_id=i.quote_id JOIN alerts_stripe_subscriptions s ON s.checkout_id=i.checkout_id WHERE i.quote_id=$1 FOR UPDATE OF i,a`, id).Scan(&raw, &state, &reference, &lease)
	if err != nil {
		return err
	}
	if state == "expired" {
		return tx.Commit(ctx)
	}
	if state != "pending" || reference == "" || lease.After(time.Now()) {
		return alertsstripe.ErrReconcile
	}
	var b alertsstripe.Binding
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != quote.CheckoutID {
		return alertsstripe.ErrSubscription
	}
	if closing {
		if err := checkServiceSales(ctx, tx, "alerts"); !errors.Is(err, ErrServiceSalesClosed) {
			if err != nil {
				return err
			}
			return ErrServiceQuote
		}
	}
	b.PaidSlots = quote.Preview.PreviousSlots
	if closing {
		if err := client.VoidPendingIncrease(ctx, reference, b, alertsstripe.IncreaseIntent{ID: id, Preview: quote.Preview}); err != nil {
			return err
		}
	}
	observed, err := client.VoidedIncrease(ctx, reference, b, alertsstripe.IncreaseIntent{ID: id, Preview: quote.Preview})
	if err != nil {
		return err
	}
	evidence, err := json.Marshal(observed)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO alerts_stripe_increase_expirations(quote_id,invoice_id,subscription_state) VALUES($1,$2,$3)`, id, reference, evidence); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_increases SET state='expired',updated_at=now() WHERE quote_id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=revision+1,observed_at=now() WHERE checkout_id=$1`, quote.CheckoutID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
