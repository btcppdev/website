package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

// PrepareAlertsStripeIncrease performs read-only provider work and saves review
// evidence. It neither confirms the change nor attempts payment.
func PrepareAlertsStripeIncrease(ctx context.Context, app *config.AppContext, person, checkout string, slots int64, client alertsstripe.Client) (*AlertsStripeIncreaseQuote, error) {
	if _, err := AlertsStripeCheckoutForPerson(app, person, checkout); err != nil {
		return nil, err
	}
	var pending string
	err := app.DB.QueryRow(ctx, `SELECT quote_id::text FROM alerts_stripe_increases WHERE checkout_id=$1 AND state='pending'`, checkout).Scan(&pending)
	if err == nil {
		return AlertsStripeIncreaseQuoteForPerson(ctx, app, person, pending)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err = RefreshAlertsStripeSubscription(ctx, app, person, checkout, client); err != nil {
		return nil, err
	}
	var raw []byte
	var revision int64
	err = app.DB.QueryRow(ctx, `SELECT b.binding,s.revision FROM alerts_stripe_current_subscriptions b JOIN alerts_stripe_subscription_state s USING(checkout_id) JOIN alerts_stripe_checkouts c ON c.id=b.checkout_id WHERE c.id=$1 AND c.person_id=$2 AND c.closed_at IS NULL`, checkout, person).Scan(&raw, &revision)
	if err != nil {
		return nil, err
	}
	var b alertsstripe.Binding
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != checkout || slots <= b.PaidSlots {
		return nil, alertsstripe.ErrSubscription
	}
	// Reuse an unchanged, unexpired review when a browser reloads the handoff.
	var existing string
	err = app.DB.QueryRow(ctx, `SELECT q.id::text FROM alerts_stripe_increase_quotes q WHERE q.checkout_id=$1 AND q.revision=$2 AND (q.preview->>'PaidSlots')::bigint=$3 AND q.expires_at>now() AND NOT EXISTS(SELECT 1 FROM alerts_stripe_increases i WHERE i.quote_id=q.id) ORDER BY q.created_at DESC LIMIT 1`, checkout, revision, slots).Scan(&existing)
	if err == nil {
		return AlertsStripeIncreaseQuoteForPerson(ctx, app, person, existing)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	preview, err := client.PreviewIncrease(ctx, b, slots, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return SaveAlertsStripeIncreaseQuote(ctx, app, person, checkout, revision, *preview)
}
