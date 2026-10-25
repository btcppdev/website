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

// ConfirmServiceCheckoutQuoteSwitchingToBitcoin resolves a pending Alerts card
// checkout or retires a canceled, fully refunded subscription only after the
// user confirms a valid Bitcoin quote. Provider and financial evidence is retained.
func ConfirmServiceCheckoutQuoteSwitchingToBitcoin(ctx context.Context, app *config.AppContext, person, quoteID, version string, client alertsstripe.Client) (*ServicePurchase, error) {
	purchase, err := ConfirmServiceCheckoutQuoteWithPolicy(app, person, quoteID, version)
	if !errors.Is(err, ErrAlertsStripeCheckoutConflict) {
		return purchase, err
	}
	stored, err := ServiceCheckoutQuoteForPerson(app, person, quoteID)
	if err != nil {
		return nil, err
	}
	if stored.Quote.ProductCode != "alerts.repository-slots" || stored.Quote.Provider != "opennode" || !stored.Quote.ExpiresAt.After(time.Now()) {
		return nil, ErrServiceQuote
	}
	policy, err := CurrentServiceLegalPolicy(app, stored.Quote.ProductCode)
	if err != nil {
		return nil, err
	}
	if (policy == nil && version != "") || (policy != nil && (version != policy.Version || policy.EffectiveAt.After(time.Now()))) {
		return nil, ErrServiceLegalReview
	}
	var existing string
	err = app.DB.QueryRow(ctx, `SELECT id::text FROM alerts_stripe_checkouts WHERE person_id=$1 AND closed_at IS NULL ORDER BY created_at DESC LIMIT 1`, person).Scan(&existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		closed, closeErr := closeRefundedAlertsSubscription(ctx, app, person, existing, client)
		if closeErr != nil {
			return nil, errors.Join(ErrAlertsStripeCheckoutConflict, closeErr)
		}
		if !closed {
			if err = CancelAlertsStripeCheckout(ctx, app, person, existing, client); err != nil {
				return nil, errors.Join(ErrAlertsStripeCheckoutConflict, err)
			}
		}
	}
	// Recheck quote expiry, account revision, policy and competing checkouts under
	// the purchase transaction. Never loop over and cancel a newly raced checkout.
	return ConfirmServiceCheckoutQuoteWithPolicy(app, person, quoteID, version)
}

// Retire only an already-canceled subscription whose recorded purchases are
// fully refunded without retained access. Never cancel a subscription at Stripe.
// Keep the account lock across provider verification and local retirement.
func closeRefundedAlertsSubscription(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) (bool, error) {
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return false, err
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT s.binding FROM alerts_stripe_current_subscriptions s JOIN alerts_stripe_checkouts c ON c.id=s.checkout_id WHERE c.id=$1 AND c.person_id=$2 AND c.closed_at IS NULL FOR UPDATE OF c`, id, person).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var binding alertsstripe.Binding
	if err = json.Unmarshal(raw, &binding); err != nil {
		return false, err
	}
	if binding.PersonID != person || binding.IntentID != id {
		return false, alertsstripe.ErrSubscription
	}
	state, err := client.CurrentSubscription(ctx, binding)
	if err != nil {
		return false, err
	}
	if state.ProviderStatus != "canceled" || state.State != "cancelled" || state.LatestInvoice == "" {
		return false, ErrAlertsStripeCheckoutConflict
	}
	// Lock order evidence against concurrent settlement/refund changes.
	if _, err = alertsBillingRevision(ctx, tx, person); err != nil {
		return false, err
	}
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM alerts_stripe_invoices WHERE checkout_id=$1 AND invoice_id=$2)
 AND NOT EXISTS (
 SELECT 1 FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id
 WHERE o.id IN (SELECT order_id FROM alerts_stripe_invoices WHERE checkout_id=$1 UNION SELECT order_id FROM alerts_stripe_increase_payments WHERE checkout_id=$1)
 AND (r.person_id<>$3 OR o.status<>'refunded' OR `+servicePurchaseAccessSQL+`))`, id, state.LatestInvoice, person).Scan(&eligible)
	if err != nil {
		return false, err
	}
	if !eligible {
		return false, ErrAlertsStripeCheckoutConflict
	}
	if err = saveAlertsStripeSubscriptionState(ctx, tx, binding, state, client, time.Now().UTC()); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE service_stripe_checkouts SET closed_at=now() WHERE id=$1 AND person_id=$2 AND closed_at IS NULL`, id, person); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
