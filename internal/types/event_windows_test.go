package types

import (
	"testing"
	"time"
)

func TestEventApplicationWindows(t *testing.T) {
	now := time.Date(2027, 1, 2, 10, 0, 0, 0, time.UTC)
	open := now
	close := now.Add(time.Hour)
	conf := &Conf{Tag: "tokyo27", Active: true, PublicationStatus: "published", StartDate: now.AddDate(0, 4, 0), EndDate: now.AddDate(0, 4, 2), SpeakerApplicationsOpen: &open, SpeakerApplicationsClose: &close, VolunteerApplicationsOpen: &open, VolunteerApplicationsClose: &close}
	for _, f := range []func(time.Time) bool{conf.TalksOpenAt, conf.VolunteerOpenAt} {
		if f(now.Add(-time.Second)) {
			t.Fatal("opened early")
		}
		if !f(now) {
			t.Fatal("did not open at boundary")
		}
		if f(close) {
			t.Fatal("not closed at deadline")
		}
	}
	conf.PublicationStatus = "draft"
	if conf.TalksOpenAt(now) || conf.VolunteerOpenAt(now) {
		t.Fatal("draft accepts applications")
	}
}
