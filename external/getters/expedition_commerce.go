package getters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"github.com/jackc/pgx/v5"
)

// The period describes the expedition, never a subscription/renewal term.
func expeditionPeriod() (time.Time, time.Time) {
	return time.Date(2028, 1, 24, 0, 0, 0, 0, time.UTC), time.Date(2028, 2, 6, 0, 0, 0, 0, time.UTC)
}

// CreateExpeditionPurchase uses persisted party pricing, shared tax evidence and
// shared legal acceptance. Tax evidence is either the explicit conference-ticket
// policy or a preserved provider quote; no customer address is invented.
func CreateExpeditionPurchase(app *config.AppContext, buyer, reservation string, number int, tax TaxQuoteInput, acceptedVersion string) (*ServicePurchase, error) {
	// Return the exact original order on retries even after it has been fulfilled.
	key := fmt.Sprintf("expedition:%s:%d", reservation, number)
	var snapshot []byte
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT quote_snapshot FROM service_purchase_requests WHERE person_id=$1 AND request_key=$2`, buyer, key).Scan(&snapshot)
	if err == nil {
		var original ServiceQuote
		if err = json.Unmarshal(snapshot, &original); err != nil {
			return nil, err
		}
		if original.ProductCode != expedition.ProductCode || original.ResourceID != reservation {
			return nil, ErrServiceQuote
		}
		original.LegalAcceptance = nil
		return CreateTaxedServicePurchaseWithPolicy(app, buyer, key, original, tax, acceptedVersion)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	c, err := (&expedition.Store{DB: app.DB}).Charge(app.DatabaseContext(), buyer, reservation, number)
	if err != nil {
		return nil, err
	}
	start, end := expeditionPeriod()
	expires := c.ExpiresAt
	if tax.ExpiresAt != nil && tax.ExpiresAt.Before(expires) {
		expires = *tax.ExpiresAt
	}
	q := ServiceQuote{ProductCode: c.ProductCode, ResourceID: c.ResourceID, Name: c.Name, Provider: c.Provider, Quantity: 1, UnitCents: c.TotalCents, TotalCents: c.TotalCents, TaxCents: int64(tax.SalesTaxAmountCents), PeriodStart: &start, PeriodEnd: &end, ExpiresAt: expires, Fulfillment: c.Fulfillment}
	return CreateTaxedServicePurchaseWithPolicy(app, buyer, c.Key, q, tax, acceptedVersion)
}

func validateExpeditionQuoteTx(ctx context.Context, tx pgx.Tx, app *config.AppContext, buyer, key string, q ServiceQuote) error {
	var f expedition.Fulfillment
	if err := json.Unmarshal(q.Fulfillment, &f); err != nil || f.Version != 1 || f.ReservationID != q.ResourceID {
		return ErrServiceQuote
	}
	c, err := (&expedition.Store{DB: app.DB}).ChargeTx(ctx, tx, buyer, f.ReservationID, f.Installment)
	if err != nil {
		return err
	}
	if f.Installment > 1 {
		var conflict bool
		// The reservation lock held by ChargeTx serializes creation and forfeiture.
		// Do not open a second order while another checkout is live or ambiguous.
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id
   WHERE p.person_id=$1 AND p.quote_snapshot->>'product_code'=$2 AND p.quote_snapshot->>'resource_id'=$3
   AND p.quote_snapshot->'fulfillment'->>'installment'=$4 AND p.request_key<>$5
   AND (o.status='paid' OR (o.status='pending' AND o.checkout_expires_at>now())
     OR EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=p.id AND a.settled_at IS NULL AND NOT EXISTS(SELECT 1 FROM expedition_payment_checks pc WHERE pc.attempt_id=a.id AND pc.status='expired' AND pc.checked_at>now()-interval '5 minutes'))))`, buyer, expedition.ProductCode, f.ReservationID, fmt.Sprint(f.Installment), key).Scan(&conflict)
		if err != nil {
			return err
		}
		if conflict {
			return ErrServicePurchaseConflict
		}
		if err = expeditionLifecyclePayableTx(ctx, tx, f.ReservationID); err != nil {
			return err
		}
	}
	start, end := expeditionPeriod()
	if !expedition.ValidChargeKey(key, c.Key, f.Installment) || q.ProductCode != c.ProductCode || q.Provider != c.Provider || q.Name != c.Name || q.Quantity != 1 || q.UnitCents != c.TotalCents || q.TotalCents != c.TotalCents || q.ExpiresAt.After(c.ExpiresAt) || q.PeriodStart == nil || q.PeriodEnd == nil || !q.PeriodStart.Equal(start) || !q.PeriodEnd.Equal(end) || q.RecurringInvoice || q.BillingMonths != 0 || q.BaseOrderID != "" || q.AccountRevision != "" {
		return ErrServiceQuote
	}
	return nil
}

// FulfillExpeditionPurchase revalidates settlement and locks order/items before
// projecting fulfillment. The financial and berth state commit together.
func FulfillExpeditionPurchase(app *config.AppContext, orderID string) error {
	ctx := app.DatabaseContext()
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	var buyer, key string
	err = tx.QueryRow(ctx, `SELECT r.quote_snapshot,r.person_id::text,r.request_key FROM service_purchase_requests r JOIN shop_orders o ON o.id=r.order_id WHERE o.id=$1 AND o.status='paid' AND r.person_id IS NOT NULL FOR UPDATE OF o`, orderID).Scan(&raw, &buyer, &key)
	if err != nil {
		return err
	}
	var q ServiceQuote
	if err = json.Unmarshal(raw, &q); err != nil {
		return err
	}
	if q.ProductCode != expedition.ProductCode {
		return ErrServiceFulfillment
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM shop_order_items WHERE order_id=$1 FOR UPDATE`, orderID); err != nil {
		return err
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_payment_attempts a WHERE a.purchase_id=r.id AND a.settled_at IS NOT NULL AND a.provider=o.payment_provider AND a.provider_reference=o.payment_provider_id AND a.provider=r.quote_snapshot->>'provider')
	 AND o.currency='USD' AND o.total_cents=$2 AND o.sales_tax_amount_cents=$3 AND o.subtotal_cents=$2-$3
	 AND o.shipping_amount_cents=0 AND o.discount_amount_cents=0 AND o.import_duty_amount_cents=0 AND o.import_tax_amount_cents=0
	 AND NOT EXISTS(SELECT 1 FROM shop_order_items i WHERE i.order_id=o.id AND (i.status NOT IN ('pending','fulfilled') OR i.refunded_quantity<>0 OR (i.status='pending' AND i.fulfilled_quantity<>0) OR (i.status='fulfilled' AND i.fulfilled_quantity<>i.quantity)))
	 AND `+serviceItemsMatchQuoteSQL+` FROM shop_orders o JOIN service_purchase_requests r ON r.order_id=o.id WHERE o.id=$1`, orderID, q.TotalCents, q.TaxCents).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrServiceFulfillment
	}
	var f expedition.Fulfillment
	if err = json.Unmarshal(q.Fulfillment, &f); err != nil {
		return err
	}
	// The immutable quote may have a shorter tax expiry than the berth contract.
	// The product's fulfillment deadline remains the hold (or voyage) boundary.
	var expires time.Time
	if f.Installment == 1 {
		err = tx.QueryRow(ctx, `SELECT hold_expires_at FROM expedition_reservations WHERE id=$1`, f.ReservationID).Scan(&expires)
	} else {
		expires, _ = expeditionPeriod()
	}
	if err != nil {
		return err
	}
	c := expedition.Charge{Key: key, ProductCode: q.ProductCode, ResourceID: q.ResourceID, BuyerID: buyer, Provider: q.Provider, Currency: "USD", Name: q.Name, TotalCents: q.TotalCents, ExpiresAt: expires, Fulfillment: q.Fulfillment}
	fulfillErr := (&expedition.Store{DB: app.DB}).FulfillPaidTx(ctx, tx, c, orderID)
	if fulfillErr != nil && fulfillErr != expedition.ErrReview {
		return fulfillErr
	}
	// A settlement observed after the contractual cutoff needs manual review even
	// if the periodic abandonment worker has not run yet. Use recorded payment
	// time, never fulfillment processing time, to protect delayed callbacks.
	if fulfillErr == nil && f.Installment > 1 {
		var due, paidAt time.Time
		var zone string
		err = tx.QueryRow(ctx, `SELECT i.due_at,o.paid_at,l.time_zone
          FROM expedition_installments i
          JOIN service_purchase_requests p ON p.request_key='expedition:'||i.reservation_id::text||':1'
          JOIN expedition_lifecycle_policies l ON l.version=p.quote_snapshot->'legal_acceptance'->>'version'
          JOIN shop_orders o ON o.id=$3
          WHERE i.reservation_id=$1 AND i.number=$2`, f.ReservationID, f.Installment, orderID).Scan(&due, &paidAt, &zone)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			deadline, deadlineErr := expedition.InstallmentDeadline(due, zone)
			if deadlineErr != nil {
				return deadlineErr
			}
			if !paidAt.Before(deadline.AddDate(0, 0, 30)) {
				if _, err = tx.Exec(ctx, `UPDATE expedition_reservations SET state='review' WHERE id=$1 AND state='confirmed'`, f.ReservationID); err != nil {
					return err
				}
				fulfillErr = expedition.ErrReview
			}
		}
	}
	if fulfillErr == nil {
		if _, err = tx.Exec(ctx, `UPDATE shop_order_items SET fulfilled_quantity=quantity,status='fulfilled' WHERE order_id=$1 AND status='pending'`, orderID); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return fulfillErr
}

// New immutable quote/order for a later installment. The UUID is a browser retry
// key; expired quotes with no provider attempt can be replaced with a fresh key.
func CreateExpeditionInstallmentPurchase(app *config.AppContext, buyer, reservation string, number int, key string, tax TaxQuoteInput, version string) (*ServicePurchase, error) {
	if number < 2 {
		return nil, ErrServiceQuote
	}
	c, err := (&expedition.Store{DB: app.DB}).Charge(app.DatabaseContext(), buyer, reservation, number)
	if err != nil {
		return nil, err
	}
	if !expedition.ValidChargeKey(key, c.Key, number) {
		return nil, ErrServiceQuote
	}
	start, end := expeditionPeriod()
	expiry := c.ExpiresAt
	if tax.ExpiresAt != nil && tax.ExpiresAt.Before(expiry) {
		expiry = *tax.ExpiresAt
	}
	q := ServiceQuote{ProductCode: c.ProductCode, ResourceID: c.ResourceID, Name: c.Name, Provider: c.Provider, Quantity: 1, UnitCents: c.TotalCents, TotalCents: c.TotalCents, TaxCents: int64(tax.SalesTaxAmountCents), PeriodStart: &start, PeriodEnd: &end, ExpiresAt: expiry, Fulfillment: c.Fulfillment}
	return CreateTaxedServicePurchaseWithPolicy(app, buyer, key, q, tax, version)
}

func expeditionLifecyclePayableTx(ctx context.Context, tx pgx.Tx, reservation string) error {
	var due time.Time
	var zone string
	err := tx.QueryRow(ctx, `SELECT i.due_at,l.time_zone FROM expedition_reservations r
 JOIN service_purchase_requests p ON p.request_key='expedition:'||r.id::text||':1' AND p.person_id::text=r.buyer_id
 JOIN expedition_lifecycle_policies l ON l.version=p.quote_snapshot->'legal_acceptance'->>'version'
 JOIN LATERAL(SELECT due_at FROM expedition_installments WHERE reservation_id=r.id AND paid_order_id IS NULL ORDER BY number LIMIT 1)i ON true
 WHERE r.id=$1`, reservation).Scan(&due, &zone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	deadline, err := expedition.InstallmentDeadline(due, zone)
	if err != nil {
		return err
	}
	if !time.Now().Before(deadline.AddDate(0, 0, 30)) {
		return expedition.ErrExpired
	}
	return nil
}
