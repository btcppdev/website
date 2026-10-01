package getters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type BitcoinClosureReport struct {
	Service   string                `json:"service"`
	ClosureAt time.Time             `json:"closure_at"`
	Items     []*BitcoinClosurePlan `json:"items"`
	Next      string                `json:"next,omitempty"`
}

type closureReportCursor struct {
	Service   string    `json:"service"`
	ClosureAt time.Time `json:"closure_at"`
	Attempt   string    `json:"attempt"`
}

// PageBitcoinClosureReport inventories settled Bitcoin payments, including expired,
// refunded and review-required payments. It is not a payout instruction or a
// reservation. Each page is consistent; concurrent settlements/refunds require a
// fresh inventory after checkout and settlement activity have been quiesced.
func PageBitcoinClosureReport(ctx context.Context, app *config.AppContext, service string, closure time.Time, after string) (*BitcoinClosureReport, error) {
	return pageBitcoinClosureReport(ctx, app, service, closure, after, 100)
}

func pageBitcoinClosureReport(ctx context.Context, app *config.AppContext, service string, closure time.Time, after string, limit int) (*BitcoinClosureReport, error) {
	if (service != "alerts" && service != "insider") || closure.IsZero() || closure.Year() < 1 || closure.Year() > 9999 || limit < 1 || limit > 100 {
		return nil, ErrServiceRefund
	}
	cursor := closureReportCursor{}
	if after != "" {
		if len(after) > 2048 {
			return nil, ErrServiceRefundCursor
		}
		raw, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Service != service || !cursor.ClosureAt.Equal(closure) {
			return nil, ErrServiceRefundCursor
		}
		if id, err := uuid.Parse(cursor.Attempt); err != nil || id.String() != cursor.Attempt {
			return nil, ErrServiceRefundCursor
		}
	}
	tx, err := app.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT a.id::text,o.public_id,r.quote_snapshot->>'product_code'
 FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id
 WHERE a.provider='opennode' AND a.settled_at IS NOT NULL
 AND split_part(r.quote_snapshot->>'product_code','.',1)=$1 AND a.id::text>$2
 ORDER BY a.id::text LIMIT $3`, service, cursor.Attempt, limit+1)
	if err != nil {
		return nil, err
	}
	type entry struct{ attempt, order, product string }
	entries := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.attempt, &e.order, &e.product); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := &BitcoinClosureReport{Service: service, ClosureAt: closure.UTC(), Items: []*BitcoinClosurePlan{}}
	if len(entries) > limit {
		entries = entries[:limit]
		raw, err := json.Marshal(closureReportCursor{Service: service, ClosureAt: closure.UTC(), Attempt: entries[limit-1].attempt})
		if err != nil {
			return nil, err
		}
		out.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	for _, e := range entries {
		plan, err := planBitcoinClosureRefund(ctx, tx, e.attempt, closure)
		if errors.Is(err, pgx.ErrNoRows) {
			// A settled attempt with inconsistent order evidence must remain visible.
			plan = &BitcoinClosurePlan{AttemptID: e.attempt, OrderPublicID: e.order, ProductCode: e.product, ClosureAt: closure.UTC(), ReviewReasons: []string{"settlement_order_requires_review"}}
		} else if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, plan)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
