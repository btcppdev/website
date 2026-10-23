package getters

import (
	"context"
	"encoding/json"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/expedition"
	"btcpp-web/internal/servicebilling"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ExpeditionBooking struct {
	ID, State, Plan, Provider string
	Quantity                  int
	CreatedAt, HoldExpiresAt  time.Time
	Schedule                  expedition.Schedule
	Payments                  []ExpeditionBookingPayment
}

type ExpeditionBookingPayment struct {
	Number                     int
	DueAt                      time.Time
	Cents                      int64
	Applied                    bool
	OrderPublicID, OrderStatus string
	RefundedCents              int64
	Legal                      *servicebilling.LegalAcceptance
}

// ExpeditionBookingForPerson never authorizes by email or by knowledge of a URL.
// Read the reservation and shared order evidence in one consistent snapshot.
func ExpeditionBookingForPerson(ctx context.Context, app *config.AppContext, personID, id string) (*ExpeditionBooking, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, pgx.ErrNoRows
	}
	tx, err := app.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	b := &ExpeditionBooking{}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT id::text,state,plan,provider,quantity,created_at,hold_expires_at,schedule FROM expedition_reservations WHERE id=$1 AND buyer_id=$2`, id, personID).Scan(&b.ID, &b.State, &b.Plan, &b.Provider, &b.Quantity, &b.CreatedAt, &b.HoldExpiresAt, &raw)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &b.Schedule); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT i.number,i.due_at,i.cents,i.paid_order_id IS NOT NULL,
 COALESCE(o.public_id,''),COALESCE(o.status,''),
 COALESCE((SELECT sum(f.amount_cents) FROM service_payment_refunds f JOIN service_payment_attempts a ON a.id=f.attempt_id WHERE a.purchase_id=p.id),0),
 p.quote_snapshot
 FROM expedition_installments i
 LEFT JOIN LATERAL(SELECT p.* FROM service_purchase_requests p JOIN shop_orders so ON so.id=p.order_id
 WHERE p.person_id::text=$2 AND p.quote_snapshot->>'product_code'='expedition.antarctica2028.passage'
 AND p.quote_snapshot->>'resource_id'=i.reservation_id::text AND p.quote_snapshot->'fulfillment'->>'installment'=i.number::text
 ORDER BY (so.id::text=i.paid_order_id) DESC NULLS LAST,so.created_at DESC LIMIT 1)p ON true
 LEFT JOIN shop_orders o ON o.id=p.order_id
 WHERE i.reservation_id=$1 ORDER BY i.number`, id, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var payment ExpeditionBookingPayment
		var snapshot []byte
		if err = rows.Scan(&payment.Number, &payment.DueAt, &payment.Cents, &payment.Applied, &payment.OrderPublicID, &payment.OrderStatus, &payment.RefundedCents, &snapshot); err != nil {
			return nil, err
		}
		if len(snapshot) > 0 {
			var quote ServiceQuote
			if err = json.Unmarshal(snapshot, &quote); err != nil {
				return nil, err
			}
			payment.Legal = quote.LegalAcceptance
		}
		b.Payments = append(b.Payments, payment)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return b, nil
}

// AccountExpeditionBookings lists paid bookings independently of payment-history
// pagination. Unpaid checkout holds stay in order history, not the trip summary.
func AccountExpeditionBookings(ctx context.Context, app *config.AppContext, personID string) ([]*ExpeditionBooking, error) {
	rows, err := app.DB.Query(ctx, `SELECT r.id::text FROM expedition_reservations r
 WHERE r.buyer_id=$1 AND EXISTS(SELECT 1 FROM people WHERE id::text=$1 AND NOT is_deleted_account)
 AND (r.confirmed_at IS NOT NULL OR EXISTS(
 SELECT 1 FROM service_purchase_requests p JOIN shop_orders o ON o.id=p.order_id
 WHERE p.person_id::text=$1 AND p.quote_snapshot->>'product_code'='expedition.antarctica2028.passage'
 AND p.quote_snapshot->>'resource_id'=r.id::text AND o.status IN ('paid','refunded','partially_refunded')))
 ORDER BY r.created_at DESC,r.id`, personID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	bookings := make([]*ExpeditionBooking, 0, len(ids))
	for _, id := range ids {
		b, err := ExpeditionBookingForPerson(ctx, app, personID, id)
		if err != nil {
			return nil, err
		}
		bookings = append(bookings, b)
	}
	return bookings, nil
}
