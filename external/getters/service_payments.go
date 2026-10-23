package getters

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"github.com/jackc/pgx/v5"
)

var ErrServicePayment = errors.New("service payment does not match its purchase")

type ServicePaymentAttempt struct {
	BuyerEmail    string
	OrderPublicID string
	ID            string
	Provider      string
	Reference     string
	TotalCents    int64
	// False means lookup/reconciliation only; do not create a provider charge.
	CanCreateCharge bool
}

// ServicePaymentContext is the immutable product and buyer data associated
// with a provider attempt. It is loaded only after the provider adapter has
// authenticated or fetched settlement evidence.
type ServicePaymentContext struct {
	AttemptID  string
	PurchaseID string
	OrderID    string
	PublicID   string
	PersonID   string
	BuyerEmail string
	PaidAt     time.Time
	Quote      ServiceQuote
}

func GetServicePaymentContext(app *config.AppContext, attemptID string) (*ServicePaymentContext, error) {
	var result ServicePaymentContext
	var raw []byte
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT a.id::text,r.id::text,o.id::text,o.public_id,
		r.person_id::text,o.buyer_email::text,r.quote_snapshot
		FROM service_payment_attempts a
		JOIN service_purchase_requests r ON r.id=a.purchase_id
		JOIN shop_orders o ON o.id=r.order_id
		WHERE a.id=$1`, strings.TrimSpace(attemptID)).Scan(&result.AttemptID, &result.PurchaseID,
		&result.OrderID, &result.PublicID, &result.PersonID, &result.BuyerEmail, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrServicePayment
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &result.Quote); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetFulfillableServicePaymentContext returns the immutable purchase only while
// its verified payment and mutable order are still safe to send to an external
// product owner. Recording payment evidence is intentionally a separate step:
// canceled or altered orders must remain visible to reconciliation without
// becoming Insider access.
func GetFulfillableServicePaymentContext(app *config.AppContext, attemptID string) (*ServicePaymentContext, error) {
	var result ServicePaymentContext
	var raw []byte
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT a.id::text,r.id::text,o.id::text,o.public_id,
		r.person_id::text,o.buyer_email::text,a.settled_at,r.quote_snapshot
		FROM service_payment_attempts a
		JOIN service_purchase_requests r ON r.id=a.purchase_id
		JOIN shop_orders o ON o.id=r.order_id
		WHERE a.id=$1 AND r.person_id IS NOT NULL AND a.settled_at IS NOT NULL AND o.status='paid'
		AND o.currency='USD' AND o.payment_provider=a.provider
		AND o.payment_provider_id=a.provider_reference
		AND a.provider=r.quote_snapshot->>'provider'
		AND o.total_cents=(r.quote_snapshot->>'total_cents')::bigint
		AND o.sales_tax_amount_cents=(r.quote_snapshot->>'tax_cents')::bigint
		AND o.subtotal_cents=(r.quote_snapshot->>'total_cents')::bigint-(r.quote_snapshot->>'tax_cents')::bigint
		AND o.shipping_amount_cents=0 AND o.discount_amount_cents=0
		AND o.import_duty_amount_cents=0 AND o.import_tax_amount_cents=0
		AND NOT EXISTS(SELECT 1 FROM shop_order_items i WHERE i.order_id=o.id
		  AND (i.status NOT IN ('pending','fulfilled') OR i.refunded_quantity<>0
		  OR (i.status='pending' AND i.fulfilled_quantity<>0)
		  OR (i.status='fulfilled' AND i.fulfilled_quantity<>i.quantity)))
		AND `+serviceItemsMatchQuoteSQL,
		strings.TrimSpace(attemptID)).Scan(&result.AttemptID, &result.PurchaseID,
		&result.OrderID, &result.PublicID, &result.PersonID, &result.BuyerEmail, &result.PaidAt, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrServicePayment
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &result.Quote); err != nil {
		return nil, err
	}
	return &result, nil
}

// BeginServicePayment allocates a durable provider idempotency key before any
// network call. Stripe may retry using this attempt's idempotency key. OpenNode
// creation is claimed once; ambiguous failures require lookup/reconciliation.
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
	if err = requireServiceSales(ctx, tx, quote.ProductCode); err != nil {
		return nil, err
	}
	var attempt ServicePaymentAttempt
	if status == "pending" && quote.ExpiresAt.After(time.Now()) {
		if quote.ProductCode == expedition.ProductCode {
			var f expedition.Fulfillment
			if err = json.Unmarshal(quote.Fulfillment, &f); err != nil {
				return nil, ErrServiceQuote
			}
			key := fmt.Sprintf("expedition:%s:%d", f.ReservationID, f.Installment)
			if err = validateExpeditionQuoteTx(ctx, tx, app, personID, key, quote); err != nil {
				return nil, err
			}
		}
		// Freeze line edits while checking the order against its immutable quote.
		// This runs under the same order lock as charge-attempt allocation.
		if _, err = tx.Exec(ctx, `SELECT id FROM shop_order_items WHERE order_id=$1 FOR UPDATE`, orderID); err != nil {
			return nil, err
		}
		var valid bool
		err = tx.QueryRow(ctx, `SELECT o.currency='USD' AND o.payment_provider=$2
			AND o.total_cents=$3 AND o.sales_tax_amount_cents=$4 AND o.subtotal_cents=$3-$4
			AND o.shipping_amount_cents=0 AND o.discount_amount_cents=0 AND o.import_duty_amount_cents=0 AND o.import_tax_amount_cents=0
			AND NOT EXISTS(SELECT 1 FROM shop_order_items i WHERE i.order_id=o.id AND (i.status<>'pending' OR i.fulfilled_quantity<>0 OR i.refunded_quantity<>0))
			AND `+serviceItemsMatchQuoteSQL+`,o.buyer_email,o.public_id
			FROM shop_orders o JOIN service_purchase_requests r ON r.order_id=o.id WHERE o.id=$1`, orderID, quote.Provider, quote.TotalCents, quote.TaxCents).Scan(&valid, &attempt.BuyerEmail, &attempt.OrderPublicID)
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, ErrServicePayment
		}
	}
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
	if attempt.CanCreateCharge && attempt.Provider == "opennode" {
		result, err := tx.Exec(ctx, `INSERT INTO service_charge_creation_claims(attempt_id) VALUES($1) ON CONFLICT DO NOTHING`, attempt.ID)
		if err != nil {
			return nil, err
		}
		attempt.CanCreateCharge = result.RowsAffected() == 1
	}
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
	// SettledSats is populated by Lightning adapters for branded receipts; it
	// is not used to establish the frozen USD order total.
	SettledSats     int64
	OpenNodeBitcoin *OpenNodeBitcoinEvidence `json:",omitempty"`
}

// RecordServicePayment persists settlement and advances an unchanged pending
// order to paid. It grants no access. A canceled or edited order still retains
// settlement evidence but requires reconciliation before product fulfillment.
func RecordServicePayment(app *config.AppContext, payment VerifiedServicePayment) error {
	return recordServicePayment(app, payment, false)
}

func recordServicePayment(app *config.AppContext, payment VerifiedServicePayment, observedPaid bool) error {
	if strings.TrimSpace(payment.EventID) == "" || len(payment.EventID) > 255 || (!observedPaid && payment.PaidAt.IsZero()) || payment.Currency != "USD" || (observedPaid && payment.Provider != "opennode") {
		return ErrServicePayment
	}
	var bitcoinEvidence []byte
	if observedPaid || payment.OpenNodeBitcoin != nil {
		if payment.Provider != "opennode" || !payment.OpenNodeBitcoin.valid() {
			return ErrServicePayment
		}
		if payment.SettledSats != 0 && payment.SettledSats != payment.OpenNodeBitcoin.PriceSatoshis {
			return ErrServicePayment
		}
		payment.SettledSats = payment.OpenNodeBitcoin.PriceSatoshis
		var err error
		bitcoinEvidence, err = json.Marshal(payment.OpenNodeBitcoin)
		if err != nil {
			return err
		}
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
	if observedPaid {
		// The charge-info API does not promise a payment timestamp. Persist our
		// first verified observation, explicitly labeled as such. The order lock
		// serializes concurrent observations; retries reuse the original instant.
		err = tx.QueryRow(ctx, `SELECT paid_at FROM service_payment_events WHERE provider=$1 AND event_id=$2`, provider, payment.EventID).Scan(&payment.PaidAt)
		if errors.Is(err, pgx.ErrNoRows) {
			payment.PaidAt = time.Now().UTC().Truncate(time.Microsecond)
		} else if err != nil {
			return err
		}
	}
	timeSource := "provider"
	if observedPaid {
		timeSource = "first_verified_observation"
	}
	result, err := tx.Exec(ctx, `INSERT INTO service_payment_events(provider,event_id,attempt_id,total_cents,currency,paid_at,time_source,opennode_bitcoin)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(provider,event_id) DO NOTHING`, provider, payment.EventID, payment.AttemptID, payment.TotalCents, payment.Currency, payment.PaidAt, timeSource, bitcoinEvidence)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var same bool
		err = tx.QueryRow(ctx, `SELECT attempt_id=$3 AND total_cents=$4 AND currency=$5 AND paid_at=$6 AND time_source=$7 AND opennode_bitcoin IS NOT DISTINCT FROM $8::jsonb FROM service_payment_events WHERE provider=$1 AND event_id=$2`, provider, payment.EventID, payment.AttemptID, payment.TotalCents, payment.Currency, payment.PaidAt, timeSource, bitcoinEvidence).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return ErrServicePayment
		}
		if strings.HasPrefix(quote.ProductCode, "insider.") || quote.ProductCode == expedition.ProductCode {
			evidence, marshalErr := json.Marshal(payment)
			if marshalErr != nil {
				return marshalErr
			}
			if _, err := tx.Exec(ctx, `INSERT INTO service_fulfillment_jobs(attempt_id,payment) VALUES($1,$2) ON CONFLICT(attempt_id) DO NOTHING`, payment.AttemptID, evidence); err != nil {
				return err
			}
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
	if strings.HasPrefix(quote.ProductCode, "insider.") || quote.ProductCode == expedition.ProductCode {
		evidence, marshalErr := json.Marshal(payment)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err := tx.Exec(ctx, `INSERT INTO service_fulfillment_jobs(attempt_id,payment) VALUES($1,$2) ON CONFLICT(attempt_id) DO NOTHING`, payment.AttemptID, evidence); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
