package expedition

import "time"

// CabinSelection describes one arrangement within a party; a party may span
// cabins. Preferences are requests only, never a guaranteed cabin restriction.
type CabinSelection struct {
	Berth      string `json:"berth"`
	Quantity   int    `json:"quantity"`
	Preference string `json:"preference"`
}

type PartyQuote struct {
	Spots        int
	Selections   []CabinSelection
	TotalCents   int64
	Installments []Installment
}

// QuoteParty validates and prices a multi-cabin selection. It does not hold
// inventory: checkout must atomically reserve every allocation before charging.
func QuoteParty(spots int, selections []CabinSelection, plan, provider string, now time.Time) (PartyQuote, error) {
	var q PartyQuote
	if spots < 1 || spots > 25 || len(selections) == 0 || len(selections) > 11 {
		return q, ErrInvalid
	}
	limits := map[string]int{"shared": 9, "triple": 1, "quad": 1}
	used := map[string]int{}
	for _, a := range selections {
		if a.Preference != "male" && a.Preference != "female" && a.Preference != "coed" {
			return PartyQuote{}, ErrInvalid
		}
		used[a.Berth]++
		if used[a.Berth] > limits[a.Berth] {
			return PartyQuote{}, ErrInvalid
		}
		s, err := NewGroupSchedule(a.Berth, plan, provider, a.Quantity, now)
		if err != nil {
			return PartyQuote{}, err
		}
		if a.Berth != "shared" || a.Quantity == 2 {
			a.Preference = "coed"
		}
		q.Selections = append(q.Selections, a)
		q.Spots += a.Quantity
		q.TotalCents += s.TotalCents
		if q.Installments == nil {
			q.Installments = make([]Installment, len(s.Installments))
			copy(q.Installments, s.Installments)
		} else {
			for i := range s.Installments {
				q.Installments[i].Cents += s.Installments[i].Cents
			}
		}
	}
	if q.Spots != spots {
		return PartyQuote{}, ErrInvalid
	}
	return q, nil
}
