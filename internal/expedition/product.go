// Package expedition owns passage pricing, inventory and fulfillment policy.
// Financial records and provider operations belong to shared commerce.
package expedition

import (
	"errors"
	"time"
)

const ProductCode = "expedition.antarctica2028.passage"
const (
	Installments = "installments"
	Full         = "full"
	Stripe       = "stripe"
	Bitcoin      = "opennode"
)

var (
	ErrInvalid  = errors.New("invalid expedition request")
	ErrClosed   = errors.New("expedition sales are closed")
	ErrSoldOut  = errors.New("no berths available")
	ErrConflict = errors.New("expedition request conflicts with existing record")
	ErrExpired  = errors.New("berth hold expired")
	ErrNotDue   = errors.New("installment is not payable yet")
	ErrReview   = errors.New("payment requires manual review; berth not confirmed")
)

type Fare struct {
	ID, Name, Description string
	Cents                 int64
}

func Fares() []Fare {
	return []Fare{
		{"shared", "Two-person cabin", "Share a twin cabin. Nine cabins, two berths each.", 1670000},
		{"triple", "Three-person cabin", "Share three ways. Save $900 per person.", 1580000},
		{"quad", "Four-person cabin", "A little closer together. Save $1,800 per person.", 1490000},
	}
}

func FullBitcoinCents(cents int64) int64 { return (cents*95 + 50) / 100 }

type Installment struct {
	Number int       `json:"number"`
	DueAt  time.Time `json:"due_at"`
	Cents  int64     `json:"cents"`
}

type Schedule struct {
	BetaTrial     bool             `json:"beta_trial,omitempty"`
	Selections    []CabinSelection `json:"selections,omitempty"`
	Quantity      int              `json:"quantity"`
	Version       int              `json:"version"`
	Berth         string           `json:"berth"`
	Plan          string           `json:"plan"`
	Provider      string           `json:"provider"`
	Currency      string           `json:"currency"`
	StandardCents int64            `json:"standard_cents"`
	TotalCents    int64            `json:"total_cents"`
	Installments  []Installment    `json:"installments"`
}

// NewSchedule creates a finite contract, never a subscription. Until a late-
// booking policy is approved, installment signups close at the first milestone.
// Milestone dates use UTC; booking availability is separately configured.
func NewSchedule(berth, plan, provider string, bookedAt time.Time) (Schedule, error) {
	var s Schedule
	if bookedAt.IsZero() || (provider != Stripe && provider != Bitcoin) || (plan != Full && plan != Installments) {
		return s, ErrInvalid
	}
	for _, f := range Fares() {
		if f.ID == berth {
			s.StandardCents = f.Cents
		}
	}
	if s.StandardCents == 0 {
		return Schedule{}, ErrInvalid
	}
	s.Quantity = 1
	s.Version, s.Berth, s.Plan, s.Provider, s.Currency = 1, berth, plan, provider, "USD"
	s.TotalCents = s.StandardCents
	if plan == Full && provider == Bitcoin {
		s.TotalCents = FullBitcoinCents(s.StandardCents)
	}
	if plan == Full {
		s.Installments = []Installment{{1, bookedAt.UTC(), s.TotalCents}}
		return s, nil
	}
	first := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if !bookedAt.Before(first) {
		return Schedule{}, ErrClosed
	}
	s.Installments = []Installment{{1, bookedAt.UTC(), s.TotalCents / 5}}
	for i, month := range []time.Month{1, 4, 7, 10} {
		s.Installments = append(s.Installments, Installment{i + 2, time.Date(2027, month, 1, 0, 0, 0, 0, time.UTC), s.TotalCents / 5})
	}
	// Keep the contract total exact even if a future fare is not divisible by five.
	s.Installments[4].Cents += s.TotalCents % 5
	return s, nil
}

// NewGroupSchedule prices each passenger first, including cent rounding of the
// bitcoin discount, then combines their finite schedules under one payer.
func NewGroupSchedule(berth, plan, provider string, quantity int, bookedAt time.Time) (Schedule, error) {
	capacity := map[string]int{"shared": 2, "triple": 3, "quad": 4}[berth]
	if quantity < 1 || quantity > capacity {
		return Schedule{}, ErrInvalid
	}
	s, err := NewSchedule(berth, plan, provider, bookedAt)
	if err != nil {
		return Schedule{}, err
	}
	s.Quantity = quantity
	s.StandardCents *= int64(quantity)
	s.TotalCents *= int64(quantity)
	for n := range s.Installments {
		s.Installments[n].Cents *= int64(quantity)
	}
	return s, nil
}
