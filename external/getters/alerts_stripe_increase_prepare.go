package getters

import (
	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

// PrepareAlertsStripeIncrease performs read-only provider work and saves review
// evidence. It neither confirms the change nor attempts payment.
func PrepareAlertsStripeIncrease(ctx context.Context, app *config.AppContext, person, checkout string, slots int64, client alertsstripe.Client) (*AlertsStripeIncreaseQuote, error) {
	if _, err := AlertsStripeCheckoutForPerson(app, person, checkout); err != nil {
		return nil, err
	}
	var pending string
	err := app.DB.QueryRow(ctx, `SELECT quote_id::text FROM alerts_stripe_increases WHERE checkout_id=$1 AND state='pending'`, checkout).Scan(&pending)
	if err == nil {
		return AlertsStripeIncreaseQuoteForPerson(ctx, app, person, pending)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err = RefreshAlertsStripeSubscription(ctx, app, person, checkout, client); err != nil {
		return nil, err
	}
	var raw, stateRaw []byte
	var revision int64
	err = app.DB.QueryRow(ctx, `SELECT b.binding,s.revision,s.snapshot FROM alerts_stripe_current_subscriptions b JOIN alerts_stripe_subscription_state s USING(checkout_id) JOIN alerts_stripe_checkouts c ON c.id=b.checkout_id WHERE c.id=$1 AND c.person_id=$2 AND c.closed_at IS NULL`, checkout, person).Scan(&raw, &revision, &stateRaw)
	if err != nil {
		return nil, err
	}
	var b alertsstripe.Binding
	if json.Unmarshal(raw, &b) != nil || b.PersonID != person || b.IntentID != checkout || slots <= b.PaidSlots {
		return nil, alertsstripe.ErrSubscription
	}
	// Reuse an unchanged, unexpired review when a browser reloads the handoff.
	var existing string
	err = app.DB.QueryRow(ctx, `SELECT q.id::text FROM alerts_stripe_increase_quotes q WHERE q.checkout_id=$1 AND q.revision=$2 AND (q.preview->>'PaidSlots')::bigint=$3 AND q.expires_at>now() AND NOT EXISTS(SELECT 1 FROM alerts_stripe_increases i WHERE i.quote_id=q.id) ORDER BY q.created_at DESC LIMIT 1`, checkout, revision, slots).Scan(&existing)
	if err == nil {
		return AlertsStripeIncreaseQuoteForPerson(ctx, app, person, existing)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var state alertsstripe.SubscriptionState
	if json.Unmarshal(stateRaw, &state) != nil {
		return nil, alertsstripe.ErrSubscription
	}
	funding := &alertsstripe.IncreaseFunding{}
	var priorID string
	var priorRaw []byte
	err = app.DB.QueryRow(ctx, `SELECT p.invoice_id,q.id::text,q.preview FROM alerts_stripe_increase_payments p JOIN alerts_stripe_increase_quotes q ON q.id=p.quote_id JOIN shop_orders o ON o.id=p.order_id JOIN service_purchase_requests r ON r.order_id=o.id JOIN service_entitlements e ON e.purchase_id=r.id WHERE p.checkout_id=$1 AND e.ends_at=to_timestamp($2) AND e.starts_at>=to_timestamp($3) AND o.status='paid' AND e.revoked_at IS NULL ORDER BY p.sequence DESC LIMIT 1`, checkout, state.PeriodEnd, state.PeriodStart).Scan(&funding.InvoiceID, &priorID, &priorRaw)
	if err == nil {
		var prior alertsstripe.IncreasePreview
		if json.Unmarshal(priorRaw, &prior) != nil {
			return nil, alertsstripe.ErrInvoice
		}
		prior.Funding = nil // one prior paid invoice is sufficient; do not nest history.
		funding.Prior = &alertsstripe.IncreaseIntent{ID: priorID, Preview: prior}
	} else if errors.Is(err, pgx.ErrNoRows) {
		err = app.DB.QueryRow(ctx, `SELECT i.invoice_id FROM alerts_stripe_invoices i JOIN shop_orders o ON o.id=i.order_id JOIN service_purchase_requests r ON r.order_id=o.id JOIN service_entitlements e ON e.purchase_id=r.id WHERE i.checkout_id=$1 AND i.starts_at=to_timestamp($2) AND i.ends_at=to_timestamp($3) AND o.status='paid' AND e.revoked_at IS NULL`, checkout, state.PeriodStart, state.PeriodEnd).Scan(&funding.InvoiceID)
		if errors.Is(err, pgx.ErrNoRows) {
			funding = nil
		} else if err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	preview, err := client.PreviewIncrease(ctx, b, slots, time.Now().UTC(), funding)
	if err != nil {
		return nil, err
	}
	return SaveAlertsStripeIncreaseQuote(ctx, app, person, checkout, revision, *preview)
}
