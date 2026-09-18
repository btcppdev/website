package getters

import (
	"time"

	"btcpp-web/internal/config"
)

type ServicePaymentReview struct {
	AttemptID         string    `json:"attemptId"`
	OrderID           string    `json:"orderId"`
	Provider          string    `json:"provider"`
	ProviderReference string    `json:"providerReference"`
	Reason            string    `json:"reason"`
	ExpectedCents     int64     `json:"expectedCents"`
	OrderCents        int64     `json:"orderCents"`
	CreatedAt         time.Time `json:"createdAt"`
}

type ServicePaymentReviewPage struct {
	Items []ServicePaymentReview `json:"items"`
	Next  string                 `json:"next"`
}

// ReviewServicePayments is an internal operator/worker query, not an account API.
// It exposes no email, buyer name, raw provider payload or credentials. Reasons
// request investigation; they do not authorize retries, refunds or access grants.
// Scan all pages and start a fresh scan to discover newly changed earlier rows.
func ReviewServicePayments(app *config.AppContext, after string, asOf time.Time) (*ServicePaymentReviewPage, error) {
	rows, err := app.DB.Query(app.DatabaseContext(), `WITH review AS (
		SELECT a.id,a.provider,coalesce(a.provider_reference,'') AS reference,a.created_at,
			o.id AS order_id,o.total_cents,(r.quote_snapshot->>'total_cents')::bigint AS expected,
			CASE
			WHEN a.settled_at IS NOT NULL AND o.status='cancelled' THEN 'payment_after_cancellation'
			WHEN a.settled_at IS NOT NULL AND o.status NOT IN ('refunded','partially_refunded') AND (
				o.currency<>'USD' OR o.payment_provider<>a.provider OR o.total_cents<>(r.quote_snapshot->>'total_cents')::bigint
				OR o.sales_tax_amount_cents<>(r.quote_snapshot->>'tax_cents')::bigint
				OR o.subtotal_cents<>(r.quote_snapshot->>'total_cents')::bigint-(r.quote_snapshot->>'tax_cents')::bigint
				OR o.shipping_amount_cents<>0 OR o.discount_amount_cents<>0 OR o.import_duty_amount_cents<>0 OR o.import_tax_amount_cents<>0
			) THEN 'order_amount_mismatch'
			WHEN a.settled_at IS NOT NULL AND o.status NOT IN ('refunded','partially_refunded') AND NOT `+serviceItemsMatchQuoteSQL+` THEN 'order_item_mismatch'
			WHEN a.settled_at IS NOT NULL AND o.status='pending' THEN 'settlement_not_applied'
			WHEN a.settled_at IS NOT NULL AND o.status='paid' AND r.person_id IS NULL THEN 'paid_account_removed'
			WHEN a.settled_at IS NOT NULL AND o.status='paid'
			AND EXISTS(SELECT 1 FROM tax_quotes t WHERE t.order_id=o.id AND t.sales_tax_provider='stripe')
			AND NOT EXISTS(SELECT 1 FROM tax_transactions t WHERE t.order_id=o.id AND t.provider='stripe' AND t.status='recorded')
			THEN 'tax_transaction_missing'
			WHEN a.settled_at IS NOT NULL AND o.status='paid' AND (
				((r.quote_snapshot->>'product_code') NOT LIKE 'insider.%'
				  AND NOT EXISTS(SELECT 1 FROM service_entitlements e WHERE e.purchase_id=r.id))
				OR
				NOT EXISTS(SELECT 1 FROM shop_order_items i WHERE i.order_id=o.id)
				OR EXISTS(SELECT 1 FROM shop_order_items i WHERE i.order_id=o.id AND i.fulfilled_quantity<i.quantity-i.refunded_quantity)
			) THEN 'paid_order_unfulfilled'
			WHEN a.settled_at IS NULL AND a.created_at<=$2::timestamptz-interval '15 minutes' THEN 'payment_unresolved'
			ELSE NULL END AS reason
		FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id
		JOIN shop_orders o ON o.id=r.order_id
		WHERE ($1='' OR a.id>NULLIF($1,'')::uuid)
	) SELECT id::text,order_id::text,provider,reference,reason,expected,total_cents,created_at
	FROM review WHERE reason IS NOT NULL ORDER BY id LIMIT 101`, after, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &ServicePaymentReviewPage{Items: []ServicePaymentReview{}}
	for rows.Next() {
		var item ServicePaymentReview
		if err = rows.Scan(&item.AttemptID, &item.OrderID, &item.Provider, &item.ProviderReference, &item.Reason, &item.ExpectedCents, &item.OrderCents, &item.CreatedAt); err != nil {
			return nil, err
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Items) > 100 {
		page.Items = page.Items[:100]
		page.Next = page.Items[99].AttemptID
	}
	return page, nil
}
