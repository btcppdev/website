package getters

import (
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
