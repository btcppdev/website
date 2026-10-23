package expedition

import (
	"testing"
	"time"
)

func TestInstallmentReminderCalendar(t *testing.T) {
	due := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	deadline, err := InstallmentDeadline(due, "America/Chicago")
	if err != nil || deadline.UTC().Format(time.RFC3339) != "2027-01-02T06:00:00Z" {
		t.Fatal(deadline, err)
	}
	for _, offset := range []int{-14, -4, 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28} {
		at, err := InstallmentReminderAt(due, "America/Chicago", offset)
		if err != nil {
			t.Fatal(err)
		}
		got, ok, err := LatestReminder(due, at, "America/Chicago")
		if err != nil || !ok || got != offset {
			t.Fatal(offset, got, ok, err)
		}
	}
	early, _ := InstallmentReminderAt(due, "America/Chicago", -14)
	if _, ok, _ := LatestReminder(due, early.Add(-time.Second), "America/Chicago"); ok {
		t.Fatal("early reminder")
	}
	// Calendar days retain local midnight through daylight-saving transitions.
	march := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	d, _ := InstallmentDeadline(march, "America/Chicago")
	if d.AddDate(0, 0, 30).UTC().Format(time.RFC3339) != "2027-04-01T05:00:00Z" {
		t.Fatal("DST cutoff", d.AddDate(0, 0, 30))
	}
	if _, err := InstallmentDeadline(due, "invalid"); err == nil {
		t.Fatal("unknown timezone accepted")
	}
}
