package getters

import (
	"context"
	"encoding/json"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/servicebilling"
	"github.com/google/uuid"
)

// BitcoinClosurePlan is a read-only snapshot, never authorization to pay. An
// executor must reserve the amount and revalidate under the same order lock used
// by settlement/refunds before contacting a provider.
type BitcoinClosurePlan struct {
	AttemptID                  string                                 `json:"attempt_id"`
	OrderPublicID              string                                 `json:"order_public_id"`
	ProductCode                string                                 `json:"product_code"`
	ClosureAt                  time.Time                              `json:"closure_at"`
	Estimate                   *servicebilling.BitcoinClosureEstimate `json:"estimate"`
	PreviouslyRefundedSatoshis *int64                                 `json:"previously_refunded_satoshis"`
	RemainingRefundSatoshis    *int64                                 `json:"remaining_refund_satoshis"`
	ReviewReasons              []string                               `json:"review_reasons"`
}

// PlanBitcoinClosureRefund reads the immutable purchased period and original
// payment, then applies the confirmed floor rounding rule. Prior closure refunds
// reduce the remaining obligation. Other manual refunds require allocation review
// rather than assuming that a goodwill refund paid for unused service time.
func PlanBitcoinClosureRefund(ctx context.Context, app *config.AppContext, attemptID string, closure time.Time) (*BitcoinClosurePlan, error) {
	if id, err := uuid.Parse(attemptID); err != nil || id.String() != attemptID || closure.IsZero() || closure.Year() < 1 || closure.Year() > 9999 {
		return nil, ErrServiceRefund
	}
	plan := &BitcoinClosurePlan{AttemptID: attemptID, ClosureAt: closure.UTC(), ReviewReasons: []string{}}
	var quoteBytes, paymentBytes, refundBytes []byte
	// A single statement sees one consistent PostgreSQL snapshot, including any
	// concurrent refund. It does not reserve funds or prevent future changes.
	err := app.DB.QueryRow(ctx, `SELECT o.public_id,r.quote_snapshot,
 coalesce((SELECT jsonb_agg(e.opennode_bitcoin) FROM service_payment_events e WHERE e.attempt_id=a.id),'[]'::jsonb),
 coalesce((SELECT jsonb_agg(jsonb_build_object('satoshis',f.amount_satoshis,'reason',f.reason)) FROM service_payment_refunds f WHERE f.attempt_id=a.id),'[]'::jsonb)
 FROM service_payment_attempts a JOIN service_purchase_requests r ON r.id=a.purchase_id JOIN shop_orders o ON o.id=r.order_id
 WHERE a.id=$1 AND a.provider='opennode' AND a.settled_at IS NOT NULL
 AND o.payment_provider=a.provider AND o.payment_provider_id=a.provider_reference
 AND o.status IN ('paid','partially_refunded','refunded')`, attemptID).Scan(&plan.OrderPublicID, &quoteBytes, &paymentBytes, &refundBytes)
	if err != nil {
		return nil, err
	}
	var quote ServiceQuote
	var payments []*OpenNodeBitcoinEvidence
	var refunds []struct {
		Satoshis *int64 `json:"satoshis"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(quoteBytes, &quote); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(paymentBytes, &payments); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(refundBytes, &refunds); err != nil {
		return nil, err
	}
	plan.ProductCode = quote.ProductCode
	if quote.Provider != "opennode" || quote.PeriodStart == nil || quote.PeriodEnd == nil {
		plan.ReviewReasons = append(plan.ReviewReasons, "no_prepaid_service_period")
		return plan, nil
	}
	var original int64
	if len(payments) == 0 {
		plan.ReviewReasons = append(plan.ReviewReasons, "missing_bitcoin_payment_evidence")
	}
	for _, payment := range payments {
		if !payment.valid() {
			plan.ReviewReasons = append(plan.ReviewReasons, "missing_or_invalid_bitcoin_payment_evidence")
			return plan, nil
		}
		if original != 0 && original != payment.PriceSatoshis {
			plan.ReviewReasons = append(plan.ReviewReasons, "conflicting_bitcoin_payment_evidence")
			return plan, nil
		}
		original = payment.PriceSatoshis
		if payment.OverpaidSatoshis == nil || *payment.OverpaidSatoshis != 0 {
			plan.ReviewReasons = append(plan.ReviewReasons, "overpayment_requires_review")
		}
	}
	if original == 0 {
		return plan, nil
	}
	estimate, err := servicebilling.EstimateBitcoinClosureRefund(original, *quote.PeriodStart, *quote.PeriodEnd, closure)
	if err != nil {
		return nil, err
	}
	plan.Estimate = &estimate
	var refunded int64
	for _, refund := range refunds {
		if refund.Satoshis == nil {
			plan.ReviewReasons = append(plan.ReviewReasons, "prior_refund_bitcoin_amount_unknown")
			return plan, nil
		}
		if *refund.Satoshis <= 0 || *refund.Satoshis > original-refunded {
			plan.ReviewReasons = append(plan.ReviewReasons, "prior_refunds_exceed_original_payment")
			return plan, nil
		}
		refunded += *refund.Satoshis
		if refund.Reason != "permanent_closure" {
			plan.ReviewReasons = append(plan.ReviewReasons, "prior_refund_allocation_requires_review")
		}
	}
	plan.PreviouslyRefundedSatoshis = &refunded
	if len(plan.ReviewReasons) == 0 {
		remaining := max(int64(0), estimate.FloorSatoshis-refunded)
		plan.RemainingRefundSatoshis = &remaining
	}
	return plan, nil
}
