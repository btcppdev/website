package getters

import (
	"btcpp-web/internal/expedition"
	"testing"
	"time"
)

func TestExpeditionCancellationCutoff(t *testing.T) {
	for _, test := range []struct {
		at   string
		want bool
	}{{"2027-10-01T04:59:59Z", true}, {"2027-10-01T05:00:00Z", false}, {"2027-10-02T00:00:00Z", false}} {
		at, err := time.Parse(time.RFC3339, test.at)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expeditionCancellationBeforeCutoff(at)
		if err != nil || got != test.want {
			t.Fatalf("%s: eligible=%v err=%v", test.at, got, err)
		}
	}
}

func TestLateBookingRefundDeposit(t *testing.T) {
	for _, month := range []time.Month{1, 4, 7} {
		s, err := expedition.NewSchedule("shared", expedition.Installments, expedition.Stripe, time.Date(2027, month, 2, 12, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if deposit := expeditionNonrefundableDeposit(s); deposit != 334000 || deposit >= s.Installments[0].Cents {
			t.Fatalf("retained %d from first payment %d", deposit, s.Installments[0].Cents)
		}
	}
}
