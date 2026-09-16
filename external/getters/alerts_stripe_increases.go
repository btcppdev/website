package getters

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AlertsStripeIncreaseQuote struct {
	ID, CheckoutID string
	Revision       int64
	Preview        alertsstripe.IncreasePreview
}

// SaveAlertsStripeIncreaseQuote accepts trusted PreviewIncrease output, never a
// browser DTO. Recheck the account's revision after provider I/O before showing
// the customer a frozen quote. Saving does not authorize or submit an update.
func SaveAlertsStripeIncreaseQuote(ctx context.Context, app *config.AppContext, person, checkout string, revision int64, preview alertsstripe.IncreasePreview) (*AlertsStripeIncreaseQuote, error) {
	now := time.Now().UTC()
	if !strings.HasPrefix(preview.SubscriptionItemID, "si_") || preview.PreviousSlots < 1 || preview.PaidSlots <= preview.PreviousSlots || preview.PeriodStart <= 0 || preview.ProrationDate < preview.PeriodStart || preview.ProrationDate >= preview.PeriodEnd || preview.ProrationDate > now.Add(time.Minute).Unix() || now.Unix()-preview.ProrationDate > 600 || !preview.ExpiresAt.After(now) || preview.ExpiresAt.Unix() > preview.ProrationDate+600 || preview.ExpiresAt.Unix() > preview.PeriodEnd || preview.TotalCents < 0 || preview.TaxCents < 0 || preview.TaxCents > preview.TotalCents {
		return nil, alertsstripe.ErrSubscription
	}
	if _, err := servicebilling.AlertsTerm("stripe", preview.PaidSlots, 1, now); err != nil {
		return nil, err
	}
	if preview.TotalCents > (preview.PaidSlots-preview.PreviousSlots)*servicebilling.AlertsMonthlySlotCents {
		return nil, alertsstripe.ErrSubscription
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockAlertsIncrease(ctx, tx, person, checkout, revision, preview); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(preview)
	if err != nil {
		return nil, err
	}
	out := &AlertsStripeIncreaseQuote{CheckoutID: checkout, Revision: revision, Preview: preview}
	if err = tx.QueryRow(ctx, `INSERT INTO alerts_stripe_increase_quotes(checkout_id,revision,preview,expires_at) VALUES($1,$2,$3,$4) RETURNING id::text`, checkout, revision, raw, preview.ExpiresAt).Scan(&out.ID); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func lockAlertsIncrease(ctx context.Context, tx pgx.Tx, person, checkout string, revision int64, preview alertsstripe.IncreasePreview) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return err
	}
	var raw, stateRaw []byte
	var current int64
	if err := tx.QueryRow(ctx, `SELECT b.binding,s.snapshot,s.revision FROM alerts_stripe_checkouts c JOIN alerts_stripe_current_subscriptions b ON b.checkout_id=c.id JOIN alerts_stripe_subscription_state s ON s.checkout_id=c.id WHERE c.id=$1 AND c.person_id=$2 AND c.closed_at IS NULL FOR UPDATE OF c`, checkout, person).Scan(&raw, &stateRaw, &current); err != nil {
		return err
	}
	var b alertsstripe.Binding
	var state alertsstripe.SubscriptionState
	if json.Unmarshal(raw, &b) != nil || json.Unmarshal(stateRaw, &state) != nil || b.PersonID != person || b.IntentID != checkout || current != revision || b.PaidSlots != preview.PreviousSlots || state.State != "active" || state.ProviderStatus != "active" || state.CancelAtPeriodEnd || state.CollectionPaused || state.CancelAt != 0 || state.PeriodStart != preview.PeriodStart || state.PeriodEnd != preview.PeriodEnd {
		return alertsstripe.ErrSubscription
	}
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_cancellations WHERE checkout_id=$1 AND state='pending') OR EXISTS(SELECT 1 FROM alerts_stripe_decreases WHERE checkout_id=$1 AND state IN ('pending','scheduled','review'))`, checkout).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return alertsstripe.ErrSubscription
	}
	return nil
}

func AlertsStripeIncreaseQuoteForPerson(ctx context.Context, app *config.AppContext, person, id string) (*AlertsStripeIncreaseQuote, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, pgx.ErrNoRows
	}
	out := &AlertsStripeIncreaseQuote{ID: id}
	var raw []byte
	err := app.DB.QueryRow(ctx, `SELECT q.checkout_id::text,q.revision,q.preview FROM alerts_stripe_increase_quotes q JOIN alerts_stripe_checkouts c ON c.id=q.checkout_id WHERE q.id=$1 AND c.person_id=$2`, id, person).Scan(&out.CheckoutID, &out.Revision, &raw)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.Preview); err != nil {
		return nil, err
	}
	return out, nil
}

// ReserveAlertsStripeIncrease freezes the customer's confirmation before any
// provider mutation. Retries recover the original record even after quote expiry.
// A distinct reviewed quote cannot replace an unresolved instruction.
func ReserveAlertsStripeIncrease(ctx context.Context, app *config.AppContext, person, id string) (string, error) {
	quote, err := AlertsStripeIncreaseQuoteForPerson(ctx, app, person, id)
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
	var status string
	err = tx.QueryRow(ctx, `SELECT state FROM alerts_stripe_increases WHERE quote_id=$1`, id).Scan(&status)
	if err == nil {
		return status, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if !time.Now().Before(quote.Preview.ExpiresAt) {
		return "", alertsstripe.ErrSubscription
	}
	if err = lockAlertsIncrease(ctx, tx, person, quote.CheckoutID, quote.Revision, quote.Preview); err != nil {
		return "", err
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alerts_stripe_increases WHERE checkout_id=$1 AND state='pending')`, quote.CheckoutID).Scan(&pending); err != nil {
		return "", err
	}
	if pending {
		return "", alertsstripe.ErrSubscription
	}
	if _, err = tx.Exec(ctx, `INSERT INTO alerts_stripe_increases(quote_id,checkout_id) VALUES($1,$2)`, id, quote.CheckoutID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_checkouts SET next_reconcile_at=now() WHERE id=$1`, quote.CheckoutID); err != nil {
		return "", err
	}
	return "pending", tx.Commit(ctx)
}
