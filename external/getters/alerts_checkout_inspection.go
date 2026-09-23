package getters

import (
	"btcpp-web/internal/config"
	"context"
	"github.com/google/uuid"
	"strings"
	"time"
)

// AlertsCheckoutInspection contains recovery facts, not frozen buyer details,
// credentials or a provider payment URL. The command using it needs operator DB
// access; it is not an account-facing API or authorization boundary.
type AlertsCheckoutInspection struct {
	CheckoutID       string     `json:"checkout_id"`
	SessionReference string     `json:"session_reference,omitempty"`
	State            string     `json:"state"`
	CreatedAt        time.Time  `json:"created_at"`
	NextReconcileAt  time.Time  `json:"next_reconcile_at"`
	ClosedAt         *time.Time `json:"closed_at,omitempty"`
	AccessConflict   bool       `json:"access_conflict"`
}

// ListAlertsCheckouts shows up to 100 oldest unresolved records, or one explicit
// checkout (including bound/closed). No provider or mutation calls are made.
func ListAlertsCheckouts(ctx context.Context, app *config.AppContext, checkout string) ([]AlertsCheckoutInspection, error) {
	checkout = strings.TrimSpace(checkout)
	if checkout != "" {
		if _, err := uuid.Parse(checkout); err != nil {
			return nil, err
		}
	}
	conflict := strings.ReplaceAll(alertsStripePurchaseConflictSQL, "$1", "c.person_id")
	rows, err := app.DB.Query(ctx, `SELECT c.id::text,coalesce(c.stripe_session_id,''),
 CASE WHEN c.closed_at IS NOT NULL THEN 'closed' WHEN b.checkout_id IS NOT NULL THEN 'bound' WHEN c.stripe_session_id IS NULL THEN 'missing_reference' ELSE 'referenced' END,
 c.created_at,c.next_reconcile_at,c.closed_at,
 (c.closed_at IS NULL AND b.checkout_id IS NULL AND (`+conflict+`))
 FROM alerts_stripe_checkouts c LEFT JOIN alerts_stripe_subscriptions b ON b.checkout_id=c.id
 WHERE c.person_id IS NOT NULL AND (($1='' AND c.closed_at IS NULL AND b.checkout_id IS NULL) OR c.id::text=$1)
 ORDER BY c.created_at,c.id LIMIT 100`, checkout)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertsCheckoutInspection{}
	for rows.Next() {
		var item AlertsCheckoutInspection
		if err = rows.Scan(&item.CheckoutID, &item.SessionReference, &item.State, &item.CreatedAt, &item.NextReconcileAt, &item.ClosedAt, &item.AccessConflict); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
