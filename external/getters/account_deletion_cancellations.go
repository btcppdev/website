package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReconcileDeletedAccountCancellation operates only on the durable authorization
// captured by DeletePerson. No browser or webhook can supply a subscription ID.
func ReconcileDeletedAccountCancellation(ctx context.Context, app *config.AppContext, checkoutID string, client alertsstripe.Client, now time.Time) (int, error) {
	var id, person, product, subscription string
	var attempt int64
	var raw []byte
	err := app.DB.QueryRow(ctx, `WITH due AS (
	 SELECT j.checkout_id FROM account_deletion_cancellations j
	 WHERE j.state='pending' AND j.next_attempt_at<=$2 AND j.lease_until<=$2 AND ($1='' OR j.checkout_id::text=$1)
	 ORDER BY j.next_attempt_at,j.checkout_id FOR UPDATE SKIP LOCKED LIMIT 1
	), claimed AS (
	 UPDATE account_deletion_cancellations j SET attempts=attempts+1,lease_until=$3 FROM due
	 WHERE j.checkout_id=due.checkout_id RETURNING j.*
	) SELECT j.checkout_id::text,j.person_id::text,c.product_code,j.attempts,coalesce(s.subscription_id,''),s.binding
	 FROM claimed j JOIN service_stripe_checkouts c ON c.id=j.checkout_id
	 LEFT JOIN alerts_stripe_current_subscriptions s ON s.checkout_id=j.checkout_id
	 WHERE c.person_id IS NULL AND NOT EXISTS(SELECT 1 FROM people WHERE id=j.person_id)
	 AND EXISTS(SELECT 1 FROM account_deletion_deliveries d WHERE d.person_id=j.person_id)`, checkoutID, now, now.Add(2*time.Minute)).Scan(&id, &person, &product, &attempt, &subscription, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	finish := func(state string, cause error) (int, error) {
		delay := time.Minute
		for i := int64(1); i < attempt && delay < time.Hour; i++ {
			delay *= 2
		}
		if delay > time.Hour {
			delay = time.Hour
		}
		result, err := app.DB.Exec(ctx, `UPDATE account_deletion_cancellations SET state=$3,next_attempt_at=$4,lease_until=$5 WHERE checkout_id=$1 AND attempts=$2 AND state='pending'`, id, attempt, state, now.Add(delay), now)
		if err != nil {
			return 1, err
		}
		if result.RowsAffected() != 1 {
			return 1, errors.New("deleted account cancellation lease superseded")
		}
		return 1, cause
	}
	if len(raw) == 0 || subscription == "" {
		return finish("review", errors.New("deleted account checkout requires provider binding reconciliation"))
	}
	var state servicestripe.SubscriptionState
	if product == "alerts.repository-slots" {
		var binding alertsstripe.Binding
		if json.Unmarshal(raw, &binding) != nil || binding.PersonID != person || binding.IntentID != id || binding.SubscriptionID != subscription {
			return finish("review", servicestripe.ErrSubscription)
		}
		state, err = client.ScheduleDeletedAccountCancellation(ctx, binding, now)
	} else {
		var binding servicestripe.Binding
		if json.Unmarshal(raw, &binding) != nil || binding.PersonID != person || binding.IntentID != id || binding.SubscriptionID != subscription || binding.ProductCode != product {
			return finish("review", servicestripe.ErrSubscription)
		}
		shared := servicestripe.SubscriptionClient{Subscription: client.Subscription, UpdateSubscription: client.UpdateSubscription, Live: client.Live}
		state, err = shared.ScheduleDeletedAccountCancellation(ctx, binding, now)
	}
	if errors.Is(err, servicestripe.ErrSubscription) {
		return finish("review", err)
	}
	if err != nil {
		return finish("pending", err)
	}
	if !state.CancelAtPeriodEnd && state.ProviderStatus != "canceled" && state.ProviderStatus != "incomplete_expired" {
		return finish("review", errors.New("provider cancellation not confirmed"))
	}
	return finish("confirmed", nil)
}
