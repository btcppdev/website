package servicebilling

import (
	"errors"
	"math/big"
	"time"
)

var ErrClosureRefund = errors.New("invalid prepaid Bitcoin refund basis")

// BitcoinClosureEstimate calculates a refund range from the original Bitcoin
// paid for one service period. It does not authorize a refund or select a rounding
// policy. A caller must supply verified original payment evidence, account for
// any earlier refunds, and persist an idempotent execution decision separately.
type BitcoinClosureEstimate struct {
	OriginalSatoshis int64     `json:"original_satoshis"`
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
	ClosureAt        time.Time `json:"closure_at"`
	FloorSatoshis    int64     `json:"floor_satoshis"`
	CeilingSatoshis  int64     `json:"ceiling_satoshis"`
}

// EstimateBitcoinClosureRefund uses elapsed time in the actual purchased period,
// not nominal 30-day months or a contemporary USD/BTC exchange rate. For a paid
// upgrade, pass its own funded service window and original payment, not the base
// subscription's start. Before the period starts the whole payment is unused;
// at or after its end none is unused. Subsecond inputs retain their exact precision.
func EstimateBitcoinClosureRefund(originalSatoshis int64, start, end, closure time.Time) (BitcoinClosureEstimate, error) {
	const maxBitcoinSatoshis int64 = 21_000_000 * 100_000_000
	valid := func(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
	if originalSatoshis <= 0 || originalSatoshis > maxBitcoinSatoshis || !valid(start) || !valid(end) || !valid(closure) || !end.After(start) {
		return BitcoinClosureEstimate{}, ErrClosureRefund
	}
	out := BitcoinClosureEstimate{OriginalSatoshis: originalSatoshis, PeriodStart: start.UTC(), PeriodEnd: end.UTC(), ClosureAt: closure.UTC()}
	if !closure.Before(end) {
		return out, nil
	}
	if !closure.After(start) {
		out.FloorSatoshis = originalSatoshis
		out.CeilingSatoshis = originalSatoshis
		return out, nil
	}
	// UnixNano and time.Duration overflow for large timestamps/windows. Use
	// arbitrary precision for both elapsed time and multiplication by satoshis.
	nanos := func(t time.Time) *big.Int {
		n := new(big.Int).Mul(big.NewInt(t.Unix()), big.NewInt(1_000_000_000))
		return n.Add(n, big.NewInt(int64(t.Nanosecond())))
	}
	total := new(big.Int).Sub(nanos(end), nanos(start))
	unused := new(big.Int).Sub(nanos(end), nanos(closure))
	numerator := new(big.Int).Mul(big.NewInt(originalSatoshis), unused)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, total, remainder)
	out.FloorSatoshis = quotient.Int64()
	out.CeilingSatoshis = out.FloorSatoshis
	if remainder.Sign() != 0 {
		out.CeilingSatoshis++
	}
	return out, nil
}
