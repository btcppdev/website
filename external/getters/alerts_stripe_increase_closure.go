package getters

import (
	"context"
	"errors"

	"btcpp-web/internal/config"
)

// A committed submission row means a provider request may have happened, even
// without an invoice reference. Only reservations with no submission can be
// released locally. The account lock serializes this decision with claiming.
func expireUnsubmittedStoppedAlertsIncrease(ctx context.Context, app *config.AppContext, person, id string) (bool, error) {
	if err := checkServiceSales(ctx, app.DB, "alerts"); !errors.Is(err, ErrServiceSalesClosed) {
		return false, err
	}
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return false, err
	}
	var state, checkout string
	var submitted bool
	err = tx.QueryRow(ctx, `SELECT i.state,i.checkout_id::text,EXISTS(SELECT 1 FROM alerts_stripe_increase_submissions s WHERE s.quote_id=i.quote_id)
 FROM alerts_stripe_increases i JOIN alerts_stripe_checkouts c ON c.id=i.checkout_id
 WHERE i.quote_id=$1 AND c.person_id=$2 FOR UPDATE OF i`, id, person).Scan(&state, &checkout, &submitted)
	if err != nil {
		return false, err
	}
	if state != "pending" {
		return true, tx.Commit(ctx)
	}
	if submitted {
		return false, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_increases SET state='expired',updated_at=now() WHERE quote_id=$1`, id); err != nil {
		return false, err
	}
	// Invalidate stale account actions without claiming a fresh provider read.
	if _, err = tx.Exec(ctx, `UPDATE alerts_stripe_subscription_state SET revision=revision+1 WHERE checkout_id=$1`, checkout); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
