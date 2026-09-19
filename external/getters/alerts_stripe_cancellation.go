package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"time"
)

type AlertsStripeCancellation = ServiceStripeCancellation

func ReserveAlertsStripeCancellation(ctx context.Context, app *config.AppContext, person, checkout, id string, revision, periodEnd int64) (*AlertsStripeCancellation, error) {
	return ReserveServiceStripeCancellation(ctx, app, person, checkout, "alerts.repository-slots", id, revision, periodEnd)
}

// ProcessAlertsStripeCancellation may be retried after a crash. A committed
// reservation is required. Ambiguous provider errors leave that record pending.
func ProcessAlertsStripeCancellation(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) (string, error) {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return "", err
	}
	var in alertsstripe.CancellationIntent
	var raw []byte
	var status, checkout string
	err = tx.QueryRow(ctx, `SELECT x.id::text,x.period_end,x.created_at,x.state,x.checkout_id::text,s.binding FROM alerts_stripe_cancellations x JOIN alerts_stripe_checkouts c ON c.id=x.checkout_id JOIN alerts_stripe_current_subscriptions s ON s.checkout_id=c.id WHERE x.id=$1 AND c.person_id=$2 FOR UPDATE OF x,c`, id, person).Scan(&in.ID, &in.PeriodEnd, &in.CreatedAt, &status, &checkout, &raw)
	if err != nil {
		return "", err
	}
	if status != "pending" {
		return status, tx.Commit(ctx)
	}
	var b alertsstripe.Binding
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != checkout {
		return "", alertsstripe.ErrSubscription
	}
	state, err := client.CurrentSubscription(ctx, b)
	if err != nil {
		return "", err
	}
	confirmed := state.PeriodEnd == in.PeriodEnd && ((state.CancelAtPeriodEnd && (state.CancelAt == 0 || state.CancelAt == in.PeriodEnd)) || (state.State == "cancelled" && state.CancellationReason == "cancellation_requested"))
	now := time.Now().UTC()
	if !confirmed {
		if state.PeriodEnd != in.PeriodEnd || now.Unix() >= in.PeriodEnd || !now.Before(in.CreatedAt.Add(23*time.Hour)) {
			status = "review"
		} else {
			state, err = client.ScheduleCancellation(ctx, b, in, now)
			if err != nil {
				return "", err
			}
			confirmed = true
		}
	}
	if confirmed {
		status = "confirmed"
	}
	if err = saveAlertsStripeSubscriptionState(ctx, tx, b, state, client, now); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_cancellations SET state=$2,updated_at=now() WHERE id=$1`, id, status); err != nil {
		return "", err
	}
	return status, tx.Commit(ctx)
}
