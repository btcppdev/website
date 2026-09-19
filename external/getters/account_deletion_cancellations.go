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
	var id, person, product, subscription, reference string
	var attempt int64
	var raw, intent []byte
	err := app.DB.QueryRow(ctx, `WITH due AS (
	 SELECT j.checkout_id FROM account_deletion_cancellations j
	 WHERE j.state='pending' AND j.next_attempt_at<=$2 AND j.lease_until<=$2 AND ($1='' OR j.checkout_id::text=$1)
	 ORDER BY j.next_attempt_at,j.checkout_id FOR UPDATE SKIP LOCKED LIMIT 1
	), claimed AS (
	 UPDATE account_deletion_cancellations j SET attempts=attempts+1,lease_until=$3 FROM due
	 WHERE j.checkout_id=due.checkout_id RETURNING j.*
	) SELECT j.checkout_id::text,j.person_id::text,c.product_code,j.attempts,coalesce(s.subscription_id,''),s.binding,c.intent,coalesce(c.stripe_session_id,'')
	 FROM claimed j JOIN service_stripe_checkouts c ON c.id=j.checkout_id
	 LEFT JOIN alerts_stripe_current_subscriptions s ON s.checkout_id=j.checkout_id
	 WHERE c.person_id IS NULL AND NOT EXISTS(SELECT 1 FROM people WHERE id=j.person_id)
	 AND EXISTS(SELECT 1 FROM account_deletion_deliveries d WHERE d.person_id=j.person_id)`, checkoutID, now, now.Add(2*time.Minute)).Scan(&id, &person, &product, &attempt, &subscription, &raw, &intent, &reference)
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
		if reference == "" {
			return finish("review", errors.New("deleted account checkout has no saved provider reference"))
		}
		if product == "alerts.repository-slots" {
			var in alertsstripe.Intent
			if json.Unmarshal(intent, &in) != nil || in.ID != id || in.PersonID != person {
				return finish("review", servicestripe.ErrCheckout)
			}
			var recovered *alertsstripe.Binding
			recovered, err = client.ResolveDeletedCheckout(ctx, in, reference)
			if err == nil && recovered != nil {
				raw, _ = json.Marshal(recovered)
				subscription = recovered.SubscriptionID
			}
		} else {
			var in servicestripe.CheckoutIntent
			if json.Unmarshal(intent, &in) != nil || in.ID != id || in.PersonID != person || in.ProductCode != product {
				return finish("review", servicestripe.ErrCheckout)
			}
			var recovered *servicestripe.Binding
			recovered, err = (servicestripe.CheckoutClient{Retrieve: client.Retrieve, Expire: client.Expire, Live: client.Live}).ResolveDeletedCheckout(ctx, in, reference)
			if err == nil && recovered != nil {
				raw, _ = json.Marshal(recovered)
				subscription = recovered.SubscriptionID
			}
		}
		if errors.Is(err, servicestripe.ErrCheckout) {
			return finish("review", err)
		}
		if err != nil {
			return finish("pending", err)
		}
		if subscription == "" {
			return finish("confirmed", nil)
		} // authenticated expired checkout
		// Preserve the authenticated binding for subsequent retries and audit even
		// though the account no longer exists. Never overwrite existing evidence.
		if _, err = app.DB.Exec(ctx, `INSERT INTO service_stripe_subscriptions(checkout_id,subscription_id,binding) VALUES($1,$2,$3) ON CONFLICT(checkout_id) DO NOTHING`, id, subscription, raw); err != nil {
			return finish("pending", err)
		}
		var same bool
		if err = app.DB.QueryRow(ctx, `SELECT subscription_id=$2 AND binding=$3::jsonb FROM service_stripe_subscriptions WHERE checkout_id=$1`, id, subscription, raw).Scan(&same); err != nil {
			return finish("pending", err)
		}
		if !same {
			return finish("review", servicestripe.ErrSubscription)
		}
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
