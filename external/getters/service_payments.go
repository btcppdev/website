package getters

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"github.com/jackc/pgx/v5"
)

var ErrServicePayment = errors.New("service payment does not match its purchase")

type ServicePaymentAttempt struct {
	ID         string
	Provider   string
	Reference  string
	TotalCents int64
	// False means lookup/reconciliation only; do not create a provider charge.
	CanCreateCharge bool
}

// BeginServicePayment allocates a durable provider idempotency key before any
// network call. Retry the same attempt after ambiguous provider failures; never
// create another charge merely because an HTTP request timed out.
func BeginServicePayment(app *config.AppContext, personID, orderID string) (*ServicePaymentAttempt, error) {
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var purchaseID, status string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT r.id::text,r.quote_snapshot,o.status FROM service_purchase_requests r
		JOIN shop_orders o ON o.id=r.order_id WHERE r.person_id=$1 AND o.id=$2 FOR UPDATE OF o`, personID, orderID).Scan(&purchaseID, &raw, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrServicePayment
	}
	if err != nil {
		return nil, err
	}
	var quote ServiceQuote
	if err = json.Unmarshal(raw, &quote); err != nil {
		return nil, err
	}
	var attempt ServicePaymentAttempt
	err = tx.QueryRow(ctx, `SELECT id::text,provider,coalesce(provider_reference,'') FROM service_payment_attempts WHERE purchase_id=$1`, purchaseID).Scan(&attempt.ID, &attempt.Provider, &attempt.Reference)
	if errors.Is(err, pgx.ErrNoRows) {
		if status != "pending" || !quote.ExpiresAt.After(time.Now()) {
			return nil, ErrServicePayment
		}
		err = tx.QueryRow(ctx, `INSERT INTO service_payment_attempts(purchase_id,provider) VALUES($1,$2) RETURNING id::text,provider`, purchaseID, quote.Provider).Scan(&attempt.ID, &attempt.Provider)
	}
	if err != nil {
		return nil, err
	}
	attempt.TotalCents = quote.TotalCents
	attempt.CanCreateCharge = status == "pending" && quote.ExpiresAt.After(time.Now()) && attempt.Reference == ""
	return &attempt, tx.Commit(ctx)
}

// BindServicePayment is called only with the provider's authenticated creation
// response or lookup. Reference identifies the charge/payment, not a browser URL.
func BindServicePayment(app *config.AppContext, attemptID, provider, reference string) error {
	if strings.TrimSpace(reference) == "" || len(reference) > 255 {
		return ErrServicePayment
	}
	result, err := app.DB.Exec(app.DatabaseContext(), `UPDATE service_payment_attempts SET provider_reference=$3
		WHERE id=$1 AND provider=$2 AND (provider_reference IS NULL OR provider_reference=$3)`, attemptID, provider, reference)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrServicePayment
	}
	return nil
}

// VerifiedServicePayment is trusted adapter output, not webhook request data.
// Adapters must authenticate the event or query the provider and establish final
// payment success before calling RecordServicePayment. No public handler exists.
type VerifiedServicePayment struct {
	AttemptID  string
	Provider   string
	Reference  string
	EventID    string
	Currency   string
	TotalCents int64
	PaidAt     time.Time
}

// RecordServicePayment persists settlement and advances an unchanged pending
// order to paid. It grants no access. A canceled or edited order still retains
// settlement evidence but requires reconciliation before product fulfillment.
func RecordServicePayment(app *config.AppContext, payment VerifiedServicePayment) error {
	if strings.TrimSpace(payment.EventID) == "" || len(payment.EventID) > 255 || payment.PaidAt.IsZero() || payment.Currency != "USD" {
		return ErrServicePayment
	}
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var orderID, provider, reference string
	var raw []byte
	// Match BeginServicePayment's lock order: order first, then attempt.
	err = tx.QueryRow(ctx, `SELECT o.id::text,r.quote_snapshot FROM service_payment_attempts a
		JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id
		WHERE a.id=$1 FOR UPDATE OF o`, payment.AttemptID).Scan(&orderID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrServicePayment
	}
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT provider,coalesce(provider_reference,'') FROM service_payment_attempts WHERE id=$1 FOR UPDATE`, payment.AttemptID).Scan(&provider, &reference)
	if err != nil {
		return err
	}
	var quote ServiceQuote
	if err = json.Unmarshal(raw, &quote); err != nil {
		return err
	}
	if provider != payment.Provider || reference == "" || reference != payment.Reference || quote.Provider != provider || quote.TotalCents != payment.TotalCents {
		return ErrServicePayment
	}
	result, err := tx.Exec(ctx, `INSERT INTO service_payment_events(provider,event_id,attempt_id,total_cents,currency,paid_at)
		VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(provider,event_id) DO NOTHING`, provider, payment.EventID, payment.AttemptID, payment.TotalCents, payment.Currency, payment.PaidAt)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var same bool
		err = tx.QueryRow(ctx, `SELECT attempt_id=$3 AND total_cents=$4 AND currency=$5 AND paid_at=$6 FROM service_payment_events WHERE provider=$1 AND event_id=$2`, provider, payment.EventID, payment.AttemptID, payment.TotalCents, payment.Currency, payment.PaidAt).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return ErrServicePayment
		}
		return tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE service_payment_attempts SET settled_at=$2 WHERE id=$1 AND settled_at IS NULL`, payment.AttemptID, payment.PaidAt)
	if err != nil {
		return err
	}
	// Prevent a concurrent edit of existing lines between validation and commit.
	// The order lock also blocks new FK references being inserted during settlement.
	if _, err = tx.Exec(ctx, `SELECT id FROM shop_order_items WHERE order_id=$1 FOR UPDATE`, orderID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE shop_orders o SET status='paid',paid_at=$2,payment_provider_id=$3
		FROM service_purchase_requests r
		WHERE o.id=$1 AND r.order_id=o.id AND o.status='pending' AND o.currency='USD' AND o.payment_provider=$4
		AND o.total_cents=$5 AND o.sales_tax_amount_cents=$6 AND o.subtotal_cents=$5-$6
		AND o.shipping_amount_cents=0 AND o.discount_amount_cents=0 AND o.import_duty_amount_cents=0 AND o.import_tax_amount_cents=0
		AND `+serviceItemsMatchQuoteSQL, orderID, payment.PaidAt, reference, provider, quote.TotalCents, quote.TaxCents)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
