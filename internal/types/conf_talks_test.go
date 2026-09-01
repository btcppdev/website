package types

import (
	"testing"
	"time"
)

func TestTalksOpenAtClosesAtDeadline(t *testing.T) {
	start := time.Date(2027, time.October, 20, 0, 0, 0, 0, time.UTC)
	conf := &Conf{
		Active:            true,
		PublicationStatus: "published",
		StartDate:         start,
		EndDate:           start.AddDate(0, 0, 2),
	}
	deadline := conf.TalksDueDate()

	if !conf.TalksOpenAt(deadline.Add(-time.Nanosecond)) {
		t.Fatal("talk applications should remain open immediately before the deadline")
	}
	if conf.TalksOpenAt(deadline) {
		t.Fatal("talk applications should close at the deadline")
	}
	conf.Active = false
	if conf.TalksOpenAt(deadline.Add(-24 * time.Hour)) {
		t.Fatal("inactive conference accepted talk applications")
	}
}
