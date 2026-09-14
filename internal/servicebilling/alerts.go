// Package servicebilling contains server-owned product pricing rules. It never
// calls payment providers or grants access.
package servicebilling

import (
	"errors"
	"math/big"
	"time"
)

const AlertsFreeRepositories int64 = 5
const AlertsMonthlySlotCents int64 = 100

// Hosted SaaS for personal use; confirmed product classification for Alerts.
const AlertsStripeTaxCode = "txcd_10103000"
const maxOrderCents int64 = 2147483647

var ErrAlertsPrice = errors.New("invalid Alerts pricing request")

// AlertsPrice is gross USD pricing: applicable tax must be allocated within
// TotalCents, never added to it. Slots counts only repositories beyond the free
// allowance. Provider adapters must enforce their own minimum charge amounts.
type AlertsPrice struct {
	Slots      int64
	UnitCents  int64
	TotalCents int64
	Start      time.Time
	End        time.Time
}

// AlertsTerm prices a recurring Stripe month or a supported OpenNode prepaid
// term. Account entitlement state determines slots; clients cannot supply prices.
func AlertsTerm(provider string, slots int64, months int, start time.Time) (AlertsPrice, error) {
	if !validTerm(provider, months) || start.IsZero() || start.Year() < 1 || start.Year() > 9800 {
		return AlertsPrice{}, ErrAlertsPrice
	}
	unit := AlertsMonthlySlotCents * int64(months)
	if slots < 1 || slots > maxOrderCents/unit {
		return AlertsPrice{}, ErrAlertsPrice
	}
	start = start.UTC()
	return AlertsPrice{Slots: slots, UnitCents: unit, TotalCents: slots * unit, Start: start, End: monthBoundary(start, months)}, nil
}

// AlertsIncrease prices added slots over the remainder of the original paid
// period, retaining its expiry. The caller must load that period and quantity
// from central billing and bind the quote to its revision before confirmation.
// Round half up to cents per added slot, then multiply by added slots. A result
// of zero is explicit; callers must not turn it into an arbitrary minimum charge.
func AlertsIncrease(provider string, addedSlots int64, months int, periodStart, periodEnd, effective time.Time) (AlertsPrice, error) {
	full, err := AlertsTerm(provider, addedSlots, months, periodStart)
	if err != nil || !full.End.Equal(periodEnd) || effective.Before(full.Start) || !effective.Before(full.End) {
		return AlertsPrice{}, ErrAlertsPrice
	}
	remaining := full.End.Sub(effective)
	duration := full.End.Sub(full.Start)
	// Ten-year nanosecond durations multiplied by cents exceed int64. Use
	// integer arithmetic throughout so previews have no floating point drift.
	numerator := new(big.Int).Mul(big.NewInt(full.UnitCents), big.NewInt(int64(remaining)))
	numerator.Mul(numerator, big.NewInt(2))
	numerator.Add(numerator, big.NewInt(int64(duration)))
	denominator := new(big.Int).Mul(big.NewInt(int64(duration)), big.NewInt(2))
	unit := new(big.Int).Quo(numerator, denominator).Int64()
	return AlertsPrice{Slots: addedSlots, UnitCents: unit, TotalCents: unit * addedSlots, Start: effective.UTC(), End: full.End}, nil
}

func validTerm(provider string, months int) bool {
	return provider == "stripe" && months == 1 || provider == "opennode" && (months == 12 || months == 24 || months == 120)
}

// Derive each boundary from the original anchor. Jan 31 + one month clamps to
// February's last day; Jan 31 + two months remains March 31 rather than March 28.
func monthBoundary(anchor time.Time, months int) time.Time {
	first := time.Date(anchor.Year(), anchor.Month()+time.Month(months), 1, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), time.UTC)
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	day := anchor.Day()
	if day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), time.UTC)
}
