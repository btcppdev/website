package expedition

import "testing"

func TestPartyQuote(t *testing.T) {
	selections := []CabinSelection{{"shared", 2, "male"}, {"shared", 1, "female"}}
	q, err := QuoteParty(3, selections, Full, Bitcoin, bookingTime)
	if err != nil || q.TotalCents != 4759500 || q.Selections[0].Preference != "coed" || q.Selections[1].Preference != "female" {
		t.Fatal(q, err)
	}
	q, err = QuoteParty(3, selections, Installments, Stripe, bookingTime)
	if err != nil || len(q.Installments) != 5 || q.Installments[0].Cents != 1002000 {
		t.Fatal(q, err)
	}
	if _, err = QuoteParty(4, selections, Full, Stripe, bookingTime); err == nil {
		t.Fatal("unallocated spot accepted")
	}
	if _, err = QuoteParty(2, []CabinSelection{{"triple", 1, "coed"}, {"triple", 1, "coed"}}, Full, Stripe, bookingTime); err == nil {
		t.Fatal("two triple cabins accepted")
	}
	q, err = QuoteParty(1, []CabinSelection{{"quad", 1, "female"}}, Full, Stripe, bookingTime)
	if err != nil || q.Selections[0].Preference != "coed" {
		t.Fatal("quad did not default to co-ed", q, err)
	}
}
