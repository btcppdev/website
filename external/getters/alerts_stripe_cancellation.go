package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type AlertsStripeCancellation struct {
	Intent     alertsstripe.CancellationIntent
	CheckoutID string
	State      string
}

// ReserveAlertsStripeCancellation persists consent before provider I/O. Revision
// and period must come from the account's reviewed, verified subscription state.
func ReserveAlertsStripeCancellation(ctx context.Context, app *config.AppContext, person, checkout, id string, revision, periodEnd int64) (*AlertsStripeCancellation, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, alertsstripe.ErrSubscription
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return nil, err
	}
	var raw []byte
	var currentRevision int64
	if err = tx.QueryRow(ctx, `SELECT s.snapshot,s.revision FROM alerts_stripe_subscription_state s JOIN alerts_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2 FOR UPDATE OF c`, checkout, person).Scan(&raw, &currentRevision); err != nil {
		return nil, err
	}
	out := &AlertsStripeCancellation{CheckoutID: checkout}
	// A repeated POST recovers its original result even after state refresh.
	err = tx.QueryRow(ctx, `SELECT id::text,period_end,created_at,state FROM alerts_stripe_cancellations WHERE id=$1 AND checkout_id=$2`, id, checkout).Scan(&out.Intent.ID, &out.Intent.PeriodEnd, &out.Intent.CreatedAt, &out.State)
	if err == nil {
		if out.Intent.PeriodEnd != periodEnd {
			return nil, alertsstripe.ErrSubscription
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var state alertsstripe.SubscriptionState
	if json.Unmarshal(raw, &state) != nil || currentRevision != revision || state.PeriodEnd != periodEnd || periodEnd <= time.Now().Unix() {
		return nil, alertsstripe.ErrSubscription
	}
	switch state.ProviderStatus {
	case "active", "trialing", "past_due", "unpaid", "paused":
	default:
		return nil, alertsstripe.ErrSubscription
	}
	// A second browser tab reuses the unresolved instruction, never replaces it.
	err = tx.QueryRow(ctx, `SELECT id::text,period_end,created_at,state FROM alerts_stripe_cancellations WHERE checkout_id=$1 AND state='pending'`, checkout).Scan(&out.Intent.ID, &out.Intent.PeriodEnd, &out.Intent.CreatedAt, &out.State)
	if err == nil {
		if out.Intent.PeriodEnd != periodEnd {
			return nil, alertsstripe.ErrSubscription
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var increasing bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_increases WHERE checkout_id=$1 AND state='pending')`, checkout).Scan(&increasing); err != nil {
		return nil, err
	}
	if increasing {
		return nil, alertsstripe.ErrSubscription
	}
	err = tx.QueryRow(ctx, `INSERT INTO alerts_stripe_cancellations(id,checkout_id,period_end) VALUES($1,$2,$3) RETURNING id::text,period_end,created_at,state`, id, checkout, periodEnd).Scan(&out.Intent.ID, &out.Intent.PeriodEnd, &out.Intent.CreatedAt, &out.State)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_checkouts SET next_reconcile_at=now() WHERE id=$1`, checkout); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
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
