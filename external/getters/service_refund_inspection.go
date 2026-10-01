package getters

import (
	"btcpp-web/internal/config"
	"context"
	"strings"
	"time"
)

// ServiceRefundInspection exposes operational facts, never the delivery payload,
// recipient address, provider credentials, or an ability to issue a refund.
type ServiceRefundInspection struct {
	TaxState                string     `json:"tax_state"`
	TaxReviewReason         string     `json:"tax_review_reason,omitempty"`
	TaxReversalReference    string     `json:"tax_reversal_reference,omitempty"`
	AttemptID               string     `json:"attempt_id"`
	OrderPublicID           string     `json:"order_public_id"`
	ProductCode             string     `json:"product_code"`
	Provider                string     `json:"provider"`
	RefundReference         string     `json:"refund_reference"`
	AmountCents             int64      `json:"amount_cents"`
	AmountSatoshis          *int64     `json:"amount_satoshis"`
	Currency                string     `json:"currency"`
	RecordedAt              time.Time  `json:"recorded_at"`
	AccessAction            string     `json:"access_action,omitempty"`
	DecisionActor           string     `json:"decision_actor,omitempty"`
	DecisionAt              *time.Time `json:"decision_at,omitempty"`
	State                   string     `json:"state"`
	Revision                int64      `json:"revision"`
	Attempts                int64      `json:"attempts"`
	NextAttemptAt           time.Time  `json:"next_attempt_at"`
	LeaseUntil              time.Time  `json:"lease_until"`
	OriginalSettlementState string     `json:"original_settlement_state"`
	EarlierRefundPending    bool       `json:"earlier_refund_pending"`
}

// ListServiceRefunds is a read-only operator query. Scope by attempt to inspect
// its complete sequence (up to 100 rows); otherwise show the oldest unresolved
// jobs first. Ordering is stable, and no provider or delivery calls are made.
func ListServiceRefunds(ctx context.Context, app *config.AppContext, attemptID string) ([]ServiceRefundInspection, error) {
	rows, err := app.DB.Query(ctx, `SELECT f.attempt_id::text,o.public_id,r.quote_snapshot->>'product_code',
	 f.provider,f.refund_reference,f.amount_cents,f.amount_satoshis,f.currency,f.recorded_at,
	 coalesce(d.action,''),coalesce(d.actor_id::text,''),d.decided_at,
	 j.state,j.revision,j.attempts,j.next_attempt_at,j.lease_until,
	 coalesce(original.state,'not_queued'),
 coalesce(taxjob.state,'not_queued'),coalesce(taxjob.review_reason,''),coalesce(taxrecord.reversal_transaction,''),
	 EXISTS(SELECT 1 FROM service_refund_jobs prior WHERE prior.attempt_id=j.attempt_id AND prior.revision<j.revision AND prior.state<>'accepted')
	 FROM service_payment_refunds f JOIN service_refund_jobs j USING(provider,refund_reference)
	 JOIN service_payment_attempts a ON a.id=f.attempt_id
	 JOIN service_purchase_requests r ON r.id=a.purchase_id
	 JOIN shop_orders o ON o.id=r.order_id
	 LEFT JOIN service_refund_access_decisions d ON d.provider=f.provider AND d.refund_reference=f.refund_reference
	 LEFT JOIN service_fulfillment_jobs original ON original.attempt_id=f.attempt_id
	 LEFT JOIN service_refund_tax_jobs taxjob ON taxjob.provider=f.provider AND taxjob.refund_reference=f.refund_reference
 LEFT JOIN service_refund_tax_records taxrecord ON taxrecord.provider=f.provider AND taxrecord.refund_reference=f.refund_reference
 WHERE ($1='' AND (j.state<>'accepted' OR taxjob.state<>'accepted')) OR f.attempt_id::text=$1
	 ORDER BY f.recorded_at,f.attempt_id,j.revision LIMIT 100`, strings.TrimSpace(attemptID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ServiceRefundInspection, 0)
	for rows.Next() {
		var item ServiceRefundInspection
		if err := rows.Scan(&item.AttemptID, &item.OrderPublicID, &item.ProductCode, &item.Provider, &item.RefundReference, &item.AmountCents, &item.AmountSatoshis, &item.Currency, &item.RecordedAt, &item.AccessAction, &item.DecisionActor, &item.DecisionAt, &item.State, &item.Revision, &item.Attempts, &item.NextAttemptAt, &item.LeaseUntil, &item.OriginalSettlementState, &item.TaxState, &item.TaxReviewReason, &item.TaxReversalReference, &item.EarlierRefundPending); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
