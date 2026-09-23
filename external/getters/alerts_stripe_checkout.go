package getters

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"btcpp-web/internal/alertsstripe"
	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrAlertsStripeCheckoutConflict = errors.New("an Alerts checkout or subscription already needs attention")

// Shared by initial reservation and saved checkout handoff.
const alertsStripePurchaseConflictSQL = `SELECT EXISTS(SELECT 1 FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id WHERE r.person_id=$1 AND r.quote_snapshot->>'product_code'='alerts.repository-slots' AND ((o.status='pending' AND (o.checkout_expires_at>now() OR EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=r.id))) OR ((o.status='paid' OR (` + servicePurchaseAccessSQL + ` AND EXISTS(SELECT 1 FROM service_entitlements e WHERE e.purchase_id=r.id AND e.revoked_at IS NULL))) AND (r.quote_snapshot->>'period_end')::timestamptz>now())))`

type AlertsStripeCheckout struct {
	Intent    alertsstripe.Intent
	Reference string
	Closed    bool
}

// ReserveAlertsStripeCheckout serializes all account purchase coordinators and
// freezes the email, quantity, callback origin and timestamp before provider I/O.
// It starts NEW subscriptions only; paid accounts use the quantity-change flow.
func ReserveAlertsStripeCheckout(app *config.AppContext, person, key string, slots int64, returnBase string) (*AlertsStripeCheckout, error) {
	if _, err := uuid.Parse(person); err != nil || len(key) < 16 || len(key) > 128 || slots < 1 || slots > 2147483647/servicebilling.AlertsMonthlySlotCents {
		return nil, ErrServiceQuote
	}
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "service-purchase:"+person); err != nil {
		return nil, err
	}
	var occupied bool
	err = tx.QueryRow(ctx, alertsStripePurchaseConflictSQL, person).Scan(&occupied)
	if err != nil {
		return nil, err
	}
	if occupied {
		return nil, ErrAlertsStripeCheckoutConflict
	}
	var raw []byte
	out := &AlertsStripeCheckout{}
	err = tx.QueryRow(ctx, `SELECT intent,coalesce(stripe_session_id,''),closed_at IS NOT NULL FROM alerts_stripe_checkouts WHERE person_id=$1 AND (request_key=$2 OR closed_at IS NULL) ORDER BY (request_key=$2) DESC LIMIT 1`, person, key).Scan(&raw, &out.Reference, &out.Closed)
	if err == nil {
		if err = json.Unmarshal(raw, &out.Intent); err != nil {
			return nil, err
		}
		if out.Intent.PaidSlots != slots || out.Intent.ReturnBaseURL != returnBase || out.Closed {
			return nil, ErrAlertsStripeCheckoutConflict
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var email string
	err = tx.QueryRow(ctx, `SELECT email::text FROM person_emails WHERE person_id=$1 AND verified_at IS NOT NULL ORDER BY is_primary DESC,email LIMIT 1 FOR SHARE`, person).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrServiceBuyer
	}
	if err != nil {
		return nil, err
	}
	out.Intent = alertsstripe.Intent{ID: uuid.NewString(), PersonID: person, Email: email, PaidSlots: slots, ReturnBaseURL: returnBase, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	if err = alertsstripe.ValidateIntent(out.Intent, time.Now().UTC()); err != nil {
		return nil, err
	}
	raw, err = json.Marshal(out.Intent)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO alerts_stripe_checkouts(id,person_id,request_key,intent) VALUES($1,$2,$3,$4)`, out.Intent.ID, person, key, raw)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func AlertsStripeCheckoutForPerson(app *config.AppContext, person, id string) (*AlertsStripeCheckout, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, pgx.ErrNoRows
	}
	var raw []byte
	out := &AlertsStripeCheckout{}
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT intent,coalesce(stripe_session_id,''),closed_at IS NOT NULL FROM alerts_stripe_checkouts WHERE person_id=$1 AND id=$2`, person, id).Scan(&raw, &out.Reference, &out.Closed)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.Intent); err != nil {
		return nil, err
	}
	return out, nil
}

func OpenAlertsStripeCheckout(ctx context.Context, app *config.AppContext, person, id string, client alertsstripe.Client) (string, error) {
	stored, err := AlertsStripeCheckoutForPerson(app, person, id)
	if err != nil {
		return "", err
	}
	if stored.Closed {
		return "", ErrAlertsStripeCheckoutConflict
	}
	var occupied bool
	if err = app.DB.QueryRow(ctx, alertsStripePurchaseConflictSQL, person).Scan(&occupied); err != nil {
		return "", err
	}
	if occupied {
		return "", ErrAlertsStripeCheckoutConflict
	}
	reference, destination, providerErr := client.Open(ctx, stored.Intent, stored.Reference, time.Now().UTC())
	if reference != "" {
		result, err := app.DB.Exec(app.DatabaseContext(), `UPDATE alerts_stripe_checkouts SET stripe_session_id=$3 WHERE person_id=$1 AND id=$2 AND (stripe_session_id IS NULL OR stripe_session_id=$3)`, person, id, reference)
		if err != nil {
			return "", err
		}
		if result.RowsAffected() != 1 {
			return "", ErrAlertsStripeCheckoutConflict
		}
	}
	if providerErr != nil {
		return "", providerErr
	}
	// A provider request runs outside database locks. Recheck after saving its
	// reference so a concurrent access adjustment cannot yield a payment link,
	// and recovery can still inspect any session the provider created.
	if err = app.DB.QueryRow(ctx, alertsStripePurchaseConflictSQL, person).Scan(&occupied); err != nil {
		return "", err
	}
	if occupied {
		return "", ErrAlertsStripeCheckoutConflict
	}
	return destination, nil
}
