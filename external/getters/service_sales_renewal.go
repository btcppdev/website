package getters

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"btcpp-web/internal/config"
	"btcpp-web/internal/servicestripe"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrServiceClosureCancellationReview = errors.New("service closure cancellation requires operator review")

// Reuse ordinary term-end cancellation and its durable provider retry intent.
// The sales-stop audit authorizes this operation; no paid period is shortened.
func cancelStoppedServiceRenewal(ctx context.Context, app *config.AppContext, person, checkout, product string, refresh func() error, process func(string) (string, error)) error {
	err := checkServiceSales(ctx, app.DB, product)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrServiceSalesClosed) {
		return err
	}
	var bound bool
	if err := app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_stripe_subscriptions s JOIN service_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2 AND c.product_code=$3)`, checkout, person, product).Scan(&bound); err != nil {
		return err
	}
	if !bound {
		return nil
	}
	if err := refresh(); err != nil {
		return err
	}
	// Finish ambiguous previous mutations before deciding a provider flag means
	// success. Review-state instructions are never silently replaced.
	var prior, priorState string
	err = app.DB.QueryRow(ctx, `SELECT id::text,state FROM service_stripe_cancellations WHERE checkout_id=$1 AND state IN ('pending','review') ORDER BY created_at,id LIMIT 1`, checkout).Scan(&prior, &priorState)
	if err == nil {
		if priorState == "review" {
			return ErrServiceClosureCancellationReview
		}
		status, err := process(prior)
		if err != nil {
			return err
		}
		if status != "confirmed" {
			return ErrServiceClosureCancellationReview
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var raw []byte
	var revision int64
	if err := app.DB.QueryRow(ctx, `SELECT snapshot,revision FROM service_stripe_subscription_state WHERE checkout_id=$1`, checkout).Scan(&raw, &revision); err != nil {
		return err
	}
	var state servicestripe.SubscriptionState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	if state.State == "cancelled" || (state.CancelAtPeriodEnd && (state.CancelAt == 0 || state.CancelAt == state.PeriodEnd)) {
		return nil
	}
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("btcpp:closure-cancel:"+product+":"+checkout+":"+strconv.FormatInt(state.PeriodEnd, 10))).String()
	reserved, err := ReserveServiceStripeCancellation(ctx, app, person, checkout, product, id, revision, state.PeriodEnd)
	if err != nil {
		return err
	}
	// A previously confirmed instruction followed by renewed provider billing
	// needs review; never claim success from an obsolete idempotency result.
	if reserved.State != "pending" {
		return ErrServiceClosureCancellationReview
	}
	status, err := process(reserved.Intent.ID)
	if err != nil {
		return err
	}
	if status != "confirmed" {
		return ErrServiceClosureCancellationReview
	}
	return nil
}
