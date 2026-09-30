package servicebilling

import (
	"errors"
	"math/big"
)

var ErrBitcoinRefundAccounting = errors.New("Bitcoin refund USD allocation requires review")

// BitcoinRefundUSDAllocation allocates the original tax-inclusive USD book value.
// It is not a new exchange quote or evidence of a provider tax reversal.
type BitcoinRefundUSDAllocation struct {
	Policy                     string `json:"policy"`
	OriginalSatoshis           int64  `json:"original_satoshis"`
	OriginalTotalCents         int64  `json:"original_total_cents"`
	PreviouslyRefundedSatoshis int64  `json:"previously_refunded_satoshis"`
	PreviouslyRefundedCents    int64  `json:"previously_refunded_cents"`
	RefundSatoshis             int64  `json:"refund_satoshis"`
	AmountCents                int64  `json:"amount_cents"`
	CumulativeRefundedCents    int64  `json:"cumulative_refunded_cents"`
}

// AllocateBitcoinRefundUSD floors the cumulative refunded share of the original
// gross USD amount, then subtracts previously booked cents. Cumulative rounding
// avoids losing pennies when a refund is split into multiple payments. A final
// full-asset refund allocates exactly the original USD total. Existing inconsistent
// allocations require reconciliation, never an automatic compensating entry.
// Tax must be reversed against the provider's original tax transaction separately.
func AllocateBitcoinRefundUSD(originalSats, totalCents, previousSats, previousCents, refundSats int64) (*BitcoinRefundUSDAllocation, error) {
	if originalSats <= 0 || originalSats > 2_100_000_000_000_000 || totalCents <= 0 || previousSats < 0 || previousSats > originalSats || previousCents < 0 || previousCents > totalCents || refundSats < 0 || refundSats > originalSats-previousSats {
		return nil, ErrBitcoinRefundAccounting
	}
	allocated := func(sats int64) int64 {
		product := new(big.Int).Mul(big.NewInt(totalCents), big.NewInt(sats))
		return product.Quo(product, big.NewInt(originalSats)).Int64()
	}
	if allocated(previousSats) != previousCents {
		return nil, ErrBitcoinRefundAccounting
	}
	cumulative := allocated(previousSats + refundSats)
	return &BitcoinRefundUSDAllocation{Policy: "original_usd_cumulative_floor_v1", OriginalSatoshis: originalSats, OriginalTotalCents: totalCents, PreviouslyRefundedSatoshis: previousSats, PreviouslyRefundedCents: previousCents, RefundSatoshis: refundSats, AmountCents: cumulative - previousCents, CumulativeRefundedCents: cumulative}, nil
}
