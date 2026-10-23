package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type AlertsStripeDecreaseQuote struct {
	ID, CheckoutID string
	Revision       int64
	Preview        alertsstripe.DecreasePreview
}

// Only trusted adapter output may be saved. Zero paid slots goes through the
// existing end-of-period cancellation flow instead of creating a schedule.
func SaveAlertsStripeDecreaseQuote(ctx context.Context, app *config.AppContext, person, checkout string, revision int64, p alertsstripe.DecreasePreview) (*AlertsStripeDecreaseQuote, error) {
	now := time.Now().UTC()
	if p.PaidSlots < 1 || p.PaidSlots >= p.PreviousSlots || p.PreviousSlots > 2147483647/servicebilling.AlertsAnnualSlotCents || p.AnnualCents != p.PaidSlots*servicebilling.AlertsAnnualSlotCents || p.PeriodStart <= 0 || p.PeriodStart > now.Unix() || p.EffectiveAt <= now.Unix() || !p.ExpiresAt.After(now) || p.ExpiresAt.After(now.Add(10*time.Minute)) || p.ExpiresAt.Unix() > p.EffectiveAt {
		return nil, alertsstripe.ErrSubscription
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockAlertsDecrease(ctx, tx, person, checkout, revision, p); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	out := &AlertsStripeDecreaseQuote{CheckoutID: checkout, Revision: revision, Preview: p}
	if err = tx.QueryRow(ctx, `INSERT INTO alerts_stripe_decrease_quotes(checkout_id,revision,preview,expires_at) VALUES($1,$2,$3,$4) RETURNING id::text`, checkout, revision, raw, p.ExpiresAt).Scan(&out.ID); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func lockAlertsDecrease(ctx context.Context, tx pgx.Tx, person, checkout string, revision int64, p alertsstripe.DecreasePreview) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return err
	}
	var binding, state []byte
	var current int64
	if err := tx.QueryRow(ctx, `SELECT b.binding,s.snapshot,s.revision FROM alerts_stripe_checkouts c JOIN alerts_stripe_current_subscriptions b ON b.checkout_id=c.id JOIN alerts_stripe_subscription_state s ON s.checkout_id=c.id WHERE c.id=$1 AND c.person_id=$2 AND c.closed_at IS NULL FOR UPDATE OF c`, checkout, person).Scan(&binding, &state, &current); err != nil {
		return err
	}
	var b alertsstripe.Binding
	var s alertsstripe.SubscriptionState
	if json.Unmarshal(binding, &b) != nil || json.Unmarshal(state, &s) != nil || b.PersonID != person || b.IntentID != checkout || b.PaidSlots != p.PreviousSlots || current != revision || s.State != "active" || s.ProviderStatus != "active" || s.CancelAtPeriodEnd || s.CancelAt != 0 || s.CollectionPaused || s.PeriodStart != p.PeriodStart || s.PeriodEnd != p.EffectiveAt {
		return alertsstripe.ErrSubscription
	}
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_increases WHERE checkout_id=$1 AND state='pending') OR EXISTS(SELECT 1 FROM alerts_stripe_cancellations WHERE checkout_id=$1 AND state='pending') OR EXISTS(SELECT 1 FROM alerts_stripe_decreases WHERE checkout_id=$1 AND state IN ('pending','scheduled','review'))`, checkout).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return alertsstripe.ErrSubscription
	}
	return nil
}

func AlertsStripeDecreaseQuoteForPerson(ctx context.Context, app *config.AppContext, person, id string) (*AlertsStripeDecreaseQuote, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, pgx.ErrNoRows
	}
	out := &AlertsStripeDecreaseQuote{ID: id}
	var raw []byte
	err := app.DB.QueryRow(ctx, `SELECT q.checkout_id::text,q.revision,q.preview FROM alerts_stripe_decrease_quotes q JOIN alerts_stripe_checkouts c ON c.id=q.checkout_id WHERE q.id=$1 AND c.person_id=$2`, id, person).Scan(&out.CheckoutID, &out.Revision, &raw)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.Preview); err != nil {
		return nil, err
	}
	return out, nil
}

// Confirmation persists consent without changing quantities or paid access.
func ReserveAlertsStripeDecrease(ctx context.Context, app *config.AppContext, person, id string) (string, error) {
	q, err := AlertsStripeDecreaseQuoteForPerson(ctx, app, person, id)
	if err != nil {
		return "", err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return "", err
	}
	var state string
	err = tx.QueryRow(ctx, `SELECT state FROM alerts_stripe_decreases WHERE quote_id=$1`, id).Scan(&state)
	if err == nil {
		return state, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if !time.Now().Before(q.Preview.ExpiresAt) {
		return "", alertsstripe.ErrSubscription
	}
	if err = lockAlertsDecrease(ctx, tx, person, q.CheckoutID, q.Revision, q.Preview); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO alerts_stripe_decreases(quote_id,checkout_id) VALUES($1,$2)`, id, q.CheckoutID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_checkouts SET next_reconcile_at=now() WHERE id=$1`, q.CheckoutID); err != nil {
		return "", err
	}
	return "pending", tx.Commit(ctx)
}
