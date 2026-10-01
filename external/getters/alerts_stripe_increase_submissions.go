package getters

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

type alertsIncreaseSubmissionClaim struct {
	Binding   alertsstripe.Binding
	Intent    alertsstripe.IncreaseIntent
	Attempt   int64
	InvoiceID string
}

// Claim commits before provider I/O. The lease exceeds the adapter's deadline;
// attempts fence late responses, while the quote ID is the stable Stripe key.
func claimAlertsStripeIncreaseSubmission(ctx context.Context, app *config.AppContext, person, id string) (*alertsIncreaseSubmissionClaim, error) {
	quote, err := AlertsStripeIncreaseQuoteForPerson(ctx, app, person, id)
	if err != nil {
		return nil, err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = requireServiceSales(ctx, tx, "alerts"); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return nil, err
	}
	var raw []byte
	var state string
	err = tx.QueryRow(ctx, `SELECT s.binding,i.state FROM alerts_stripe_current_subscriptions s JOIN alerts_stripe_increases i ON i.checkout_id=s.checkout_id WHERE i.quote_id=$1 FOR UPDATE OF i`, id).Scan(&raw, &state)
	if err != nil {
		return nil, err
	}
	if state != "pending" {
		return nil, alertsstripe.ErrReconcile
	}
	out := &alertsIncreaseSubmissionClaim{Intent: alertsstripe.IncreaseIntent{ID: id, Preview: quote.Preview}}
	if json.Unmarshal(raw, &out.Binding) != nil || out.Binding.PersonID != person || out.Binding.IntentID != quote.CheckoutID {
		return nil, alertsstripe.ErrSubscription
	}
	var lease time.Time
	err = tx.QueryRow(ctx, `SELECT attempts,lease_until,COALESCE(invoice_id,'') FROM alerts_stripe_increase_submissions WHERE quote_id=$1`, id).Scan(&out.Attempt, &lease, &out.InvoiceID)
	if err == nil && out.InvoiceID != "" {
		return out, tx.Commit(ctx)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	now := time.Now().UTC()
	if !now.Before(quote.Preview.ExpiresAt) {
		// No attempt means no provider operation could be in flight. Otherwise
		// preserve the pending instruction until provider recovery resolves it.
		if out.Attempt == 0 {
			if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_increases SET state='expired',updated_at=now() WHERE quote_id=$1`, id); err != nil {
				return nil, err
			}
			if err = tx.Commit(ctx); err != nil {
				return nil, err
			}
		}
		return nil, alertsstripe.ErrReconcile
	}
	if lease.After(now) {
		return nil, alertsstripe.ErrReconcile
	}
	if err = lockAlertsIncrease(ctx, tx, person, quote.CheckoutID, quote.Revision, quote.Preview); err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO alerts_stripe_increase_submissions(quote_id,lease_until) VALUES($1,now()+interval '2 minutes') ON CONFLICT(quote_id) DO UPDATE SET attempts=alerts_stripe_increase_submissions.attempts+1,lease_until=excluded.lease_until RETURNING attempts`, id).Scan(&out.Attempt)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// SubmitAlertsStripeIncrease requires durable confirmation and persists the
// provider invoice reference. It never grants access. Ambiguous failures leave
// the claim intact for read-only recovery or a retry using the same Stripe key.
func SubmitAlertsStripeIncrease(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) (string, error) {
	claim, err := claimAlertsStripeIncreaseSubmission(ctx, app, person, id)
	if err != nil {
		return "", err
	}
	if claim.InvoiceID != "" {
		return claim.InvoiceID, nil
	}
	result, err := client.SubmitIncrease(ctx, claim.Binding, claim.Intent, time.Now().UTC())
	if err != nil {
		return "", err
	}
	updated, err := app.DB.Exec(ctx, `UPDATE alerts_stripe_increase_submissions SET invoice_id=$3 WHERE quote_id=$1 AND attempts=$2 AND (invoice_id IS NULL OR invoice_id=$3)`, id, claim.Attempt, result.InvoiceID)
	if err != nil {
		return "", err
	}
	if updated.RowsAffected() != 1 {
		return "", alertsstripe.ErrReconcile
	}
	return result.InvoiceID, nil
}
