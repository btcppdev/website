package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

// RefreshAlertsStripeSubscription serializes the provider lookup with account
// billing operations so a slower older response cannot replace a newer state.
// Subscription status alone never rewrites paid-through dates or grants grace.
func RefreshAlertsStripeSubscription(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) error {
	return refreshAlertsStripeSubscriptionAt(ctx, app, person, id, client, time.Now().UTC())
}

func refreshAlertsStripeSubscriptionAt(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client, now time.Time) error {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT s.binding FROM alerts_stripe_subscriptions s JOIN alerts_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2 FOR UPDATE OF c`, id, person).Scan(&raw); err != nil {
		return err
	}
	var binding alertsstripe.Binding
	if err = json.Unmarshal(raw, &binding); err != nil {
		return err
	}
	if binding.PersonID != person || binding.IntentID != id {
		return alertsstripe.ErrSubscription
	}
	state, err := client.CurrentSubscription(ctx, binding)
	if err != nil {
		return err
	}
	if err = saveAlertsStripeSubscriptionState(ctx, tx, binding, state, client, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func saveAlertsStripeSubscriptionState(ctx context.Context, tx pgx.Tx, binding alertsstripe.Binding, state alertsstripe.SubscriptionState, client alertsstripe.Client, now time.Time) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO alerts_stripe_subscription_state(checkout_id,revision,snapshot) VALUES($1,1,$2)
 ON CONFLICT(checkout_id) DO UPDATE SET revision=alerts_stripe_subscription_state.revision+CASE WHEN alerts_stripe_subscription_state.snapshot<>excluded.snapshot THEN 1 ELSE 0 END,snapshot=excluded.snapshot,observed_at=now()`, binding.IntentID, raw)
	if err != nil {
		return err
	}
	if err = projectAlertsStripeRenewal(ctx, tx, binding, state, client, now); err != nil {
		return err
	}
	return nil
}
