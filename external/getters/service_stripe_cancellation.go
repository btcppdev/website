package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type ServiceStripeCancellation struct {
	Intent     servicestripe.CancellationIntent
	CheckoutID string
	State      string
}

// ReserveServiceStripeCancellation persists consent before provider I/O. Revision
// and period must come from the account's reviewed, verified subscription state.
func ReserveServiceStripeCancellation(ctx context.Context, app *config.AppContext, person, checkout, product, id string, revision, periodEnd int64) (*ServiceStripeCancellation, error) {
	if _, err := servicebilling.LookupProductPlan(product); err != nil {
		return nil, servicestripe.ErrSubscription
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, servicestripe.ErrSubscription
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
	if err = tx.QueryRow(ctx, `SELECT s.snapshot,s.revision FROM service_stripe_subscription_state s JOIN service_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2 AND c.product_code=$3 FOR UPDATE OF c`, checkout, person, product).Scan(&raw, &currentRevision); err != nil {
		return nil, err
	}
	out := &ServiceStripeCancellation{CheckoutID: checkout}
	// A repeated POST recovers its original result even after state refresh.
	err = tx.QueryRow(ctx, `SELECT id::text,period_end,created_at,state FROM service_stripe_cancellations WHERE id=$1 AND checkout_id=$2`, id, checkout).Scan(&out.Intent.ID, &out.Intent.PeriodEnd, &out.Intent.CreatedAt, &out.State)
	if err == nil {
		if out.Intent.PeriodEnd != periodEnd {
			return nil, servicestripe.ErrSubscription
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var state servicestripe.SubscriptionState
	if json.Unmarshal(raw, &state) != nil || currentRevision != revision || state.PeriodEnd != periodEnd || periodEnd <= time.Now().Unix() {
		return nil, servicestripe.ErrSubscription
	}
	switch state.ProviderStatus {
	case "active", "trialing", "past_due", "unpaid", "paused":
	default:
		return nil, servicestripe.ErrSubscription
	}
	// A second browser tab reuses the unresolved instruction, never replaces it.
	err = tx.QueryRow(ctx, `SELECT id::text,period_end,created_at,state FROM service_stripe_cancellations WHERE checkout_id=$1 AND state='pending'`, checkout).Scan(&out.Intent.ID, &out.Intent.PeriodEnd, &out.Intent.CreatedAt, &out.State)
	if err == nil {
		if out.Intent.PeriodEnd != periodEnd {
			return nil, servicestripe.ErrSubscription
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var increasing bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_increases WHERE checkout_id=$1 AND state='pending') OR EXISTS(SELECT 1 FROM alerts_stripe_decreases WHERE checkout_id=$1 AND state IN ('pending','scheduled','review'))`, checkout).Scan(&increasing); err != nil {
		return nil, err
	}
	if increasing {
		return nil, servicestripe.ErrSubscription
	}
	err = tx.QueryRow(ctx, `INSERT INTO service_stripe_cancellations(id,checkout_id,period_end) VALUES($1,$2,$3) RETURNING id::text,period_end,created_at,state`, id, checkout, periodEnd).Scan(&out.Intent.ID, &out.Intent.PeriodEnd, &out.Intent.CreatedAt, &out.State)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE service_stripe_checkouts SET next_reconcile_at=now() WHERE id=$1`, checkout); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// ProcessServiceStripeCancellation may be retried after a crash. A committed
// reservation is required. Ambiguous provider errors leave that record pending.
func ProcessServiceStripeCancellation(ctx context.Context, app *config.AppContext, person, id, product string, client servicestripe.SubscriptionClient) (string, error) {
	plan, planErr := servicebilling.LookupProductPlan(product)
	if planErr != nil || plan.Family != "insider" {
		return "", servicestripe.ErrSubscription
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return "", err
	}
	var in servicestripe.CancellationIntent
	var raw []byte
	var status, checkout string
	err = tx.QueryRow(ctx, `SELECT x.id::text,x.period_end,x.created_at,x.state,x.checkout_id::text,s.binding FROM service_stripe_cancellations x JOIN service_stripe_checkouts c ON c.id=x.checkout_id JOIN service_stripe_subscriptions s ON s.checkout_id=c.id WHERE x.id=$1 AND c.person_id=$2 AND c.product_code=$3 FOR UPDATE OF x,c`, id, person, product).Scan(&in.ID, &in.PeriodEnd, &in.CreatedAt, &status, &checkout, &raw)
	if err != nil {
		return "", err
	}
	if status != "pending" {
		return status, tx.Commit(ctx)
	}
	var b servicestripe.Binding
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != checkout || b.ProductCode != product {
		return "", servicestripe.ErrSubscription
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
	if err = saveServiceStripeSubscriptionState(ctx, tx, checkout, state); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE service_stripe_cancellations SET state=$2,updated_at=now() WHERE id=$1`, id, status); err != nil {
		return "", err
	}
	return status, tx.Commit(ctx)
}
