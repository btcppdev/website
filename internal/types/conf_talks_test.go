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

func TestSpeakerApplicationsCloseOverride(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 15, 9, 0, 0, 0, loc)
	closeAt := time.Date(2026, 9, 26, 0, 0, 0, 0, loc)
	conf := &Conf{Tag: "seoul", Active: true, PublicationStatus: "published", StartDate: start, EndDate: start.AddDate(0, 0, 2), TZ: loc}
	now := closeAt.Add(-24 * time.Hour)
	if conf.TalksOpenAt(now) {
		t.Fatal("default deadline should have passed")
	}
	conf.SpeakerApplicationsClose = &closeAt
	if !conf.TalksOpenAt(now) || !conf.TalksOpenAt(closeAt.Add(-time.Nanosecond)) {
		t.Fatal("override should reopen applications through Friday")
	}
	if conf.TalksOpenAt(closeAt) || conf.TalksOpenAt(closeAt.Add(time.Second)) {
		t.Fatal("applications must close at the exact deadline")
	}
	utc := closeAt.UTC()
	conf.SpeakerApplicationsClose = &utc
	if got := conf.TalksDueLabel(); got != "Sat. Sep 26, 2026 at 12:00 AM KST (UTC+09:00)" {
		t.Fatalf("local label: %s", got)
	}
	conf.PublicationStatus = "draft"
	if conf.TalksOpenAt(now) {
		t.Fatal("override must not open draft events")
	}
	conf.PublicationStatus = "published"
	conf.Active = false
	if conf.TalksOpenAt(now) {
		t.Fatal("override must not open inactive events")
	}
	conf.Active = true
	later := start.AddDate(0, 0, 30)
	conf.SpeakerApplicationsClose = &later
	if conf.TalksOpenAt(start.AddDate(0, 0, 5)) {
		t.Fatal("override must not reopen ended events")
	}
	conf.SpeakerApplicationsClose = nil
	if !conf.TalksDueDate().Equal(start.AddDate(0, 0, -45)) {
		t.Fatal("clearing override must restore default")
	}
	conf.Tag = "nairobi"
	if !conf.TalksDueDate().Equal(start.AddDate(0, 0, -35)) {
		t.Fatal("Nairobi default changed")
	}
}
