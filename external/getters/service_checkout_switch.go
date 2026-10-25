package getters

import (
	"context"
	"errors"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

// ConfirmServiceCheckoutQuoteSwitchingToBitcoin resolves a pending Alerts card
// checkout only after the user confirms a valid Bitcoin quote. Cancellation
// retains the provider evidence and must prove the card checkout cannot pay.
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
		if err = CancelAlertsStripeCheckout(ctx, app, person, existing, client); err != nil {
			return nil, errors.Join(ErrAlertsStripeCheckoutConflict, err)
		}
	}
	// Recheck quote expiry, account revision, policy and competing checkouts under
	// the purchase transaction. Never loop over and cancel a newly raced checkout.
	return ConfirmServiceCheckoutQuoteWithPolicy(app, person, quoteID, version)
}
