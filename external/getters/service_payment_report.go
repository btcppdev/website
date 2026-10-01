package getters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
)

type UnsettledServicePayment struct {
	AttemptID     string    `json:"attempt_id"`
	OrderPublicID string    `json:"order_public_id"`
	ProductCode   string    `json:"product_code"`
	Provider      string    `json:"provider"`
	Reference     string    `json:"provider_reference,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ReviewReason  string    `json:"review_reason"`
}

type UnsettledServicePayments struct {
	Service string                    `json:"service"`
	Items   []UnsettledServicePayment `json:"items"`
	Next    string                    `json:"next,omitempty"`
}

type unsettledPaymentCursor struct {
	Service string `json:"service"`
	Attempt string `json:"attempt"`
}

// PageUnsettledServicePayments reads local one-off/prepaid attempts needing
// reconciliation. It does not inspect providers, cover subscription checkouts or
// prove closure complete. Restart pagination after concurrent settlement activity.
func PageUnsettledServicePayments(ctx context.Context, app *config.AppContext, service, after string) (*UnsettledServicePayments, error) {
	return pageUnsettledServicePayments(ctx, app, service, after, 100)
}

func pageUnsettledServicePayments(ctx context.Context, app *config.AppContext, service, after string, limit int) (*UnsettledServicePayments, error) {
	if (service != "alerts" && service != "insider") || limit < 1 || limit > 100 {
		return nil, ErrServiceQuote
	}
	cursor := unsettledPaymentCursor{}
	if after != "" {
		if len(after) > 1024 {
			return nil, ErrServiceRefundCursor
		}
		raw, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Service != service {
			return nil, ErrServiceRefundCursor
		}
		if id, err := uuid.Parse(cursor.Attempt); err != nil || id.String() != cursor.Attempt {
			return nil, ErrServiceRefundCursor
		}
	}
	rows, err := app.DB.Query(ctx, `SELECT a.id::text,o.public_id,r.quote_snapshot->>'product_code',a.provider,coalesce(a.provider_reference,''),a.created_at,
 CASE WHEN a.provider_reference IS NULL THEN 'provider_reference_missing'
 WHEN a.provider='opennode' THEN 'opennode_charge_requires_reconciliation'
 ELSE 'stripe_payment_requires_reconciliation' END
 FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id
 WHERE a.settled_at IS NULL AND split_part(r.quote_snapshot->>'product_code','.',1)=$1
 AND a.id::text>$2 AND NOT EXISTS(SELECT 1 FROM service_payment_checkout_expirations e WHERE e.attempt_id=a.id)
 ORDER BY a.id::text LIMIT $3`, service, cursor.Attempt, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &UnsettledServicePayments{Service: service, Items: []UnsettledServicePayment{}}
	for rows.Next() {
		var item UnsettledServicePayment
		if err := rows.Scan(&item.AttemptID, &item.OrderPublicID, &item.ProductCode, &item.Provider, &item.Reference, &item.CreatedAt, &item.ReviewReason); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		raw, err := json.Marshal(unsettledPaymentCursor{Service: service, Attempt: out.Items[limit-1].AttemptID})
		if err != nil {
			return nil, err
		}
		out.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
