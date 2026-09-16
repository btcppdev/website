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

// PrepareAlertsStripeDecrease performs read-only provider work and saves a
// review. It never confirms, attaches a schedule, or changes an allowance.
// Zero paid slots belongs to the existing end-of-period cancellation flow.
func PrepareAlertsStripeDecrease(ctx context.Context, app *config.AppContext, person, checkout string, slots int64, client alertsstripe.Client) (*AlertsStripeDecreaseQuote, error) {
	if _, err := AlertsStripeCheckoutForPerson(app, person, checkout); err != nil {
		return nil, err
	}
	if slots < 1 {
		return nil, alertsstripe.ErrSubscription
	}
	var pending string
	err := app.DB.QueryRow(ctx, `SELECT quote_id::text FROM alerts_stripe_decreases WHERE checkout_id=$1 AND state IN ('pending','scheduled','review')`, checkout).Scan(&pending)
	if err == nil {
		return AlertsStripeDecreaseQuoteForPerson(ctx, app, person, pending)
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
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != checkout || slots >= b.PaidSlots {
		return nil, alertsstripe.ErrSubscription
	}
	var existing string
	err = app.DB.QueryRow(ctx, `SELECT q.id::text FROM alerts_stripe_decrease_quotes q WHERE q.checkout_id=$1 AND q.revision=$2 AND (q.preview->>'PaidSlots')::bigint=$3 AND q.expires_at>now() AND NOT EXISTS(SELECT 1 FROM alerts_stripe_decreases d WHERE d.quote_id=q.id) ORDER BY q.created_at DESC,q.id DESC LIMIT 1`, checkout, revision, slots).Scan(&existing)
	if err == nil {
		return AlertsStripeDecreaseQuoteForPerson(ctx, app, person, existing)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	preview, err := client.PreviewDecrease(ctx, b, slots, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return SaveAlertsStripeDecreaseQuote(ctx, app, person, checkout, revision, *preview)
}
