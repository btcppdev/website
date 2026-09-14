package servicebilling

import (
	"math"
	"testing"
	"time"
)

func TestAlertsTerms(t *testing.T) {
	start := time.Date(2028, 2, 29, 12, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		provider string
		months   int
		unit     int64
		end      string
	}{
		{"stripe", 1, 100, "2028-03-29"},
		{"opennode", 12, 1200, "2029-02-28"},
		{"opennode", 24, 2400, "2030-02-28"},
		{"opennode", 120, 12000, "2038-02-28"},
	} {
		p, err := AlertsTerm(tc.provider, 3, tc.months, start)
		if err != nil || p.UnitCents != tc.unit || p.TotalCents != 3*tc.unit || p.End.Format("2006-01-02") != tc.end || p.End.Hour() != 12 || p.End.Minute() != 30 {
			t.Fatalf("%+v: price=%+v err=%v", tc, p, err)
		}
	}
	jan := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	if got := monthBoundary(jan, 1).Format("2006-01-02"); got != "2027-02-28" {
		t.Fatal(got)
	}
	if got := monthBoundary(jan, 2).Format("2006-01-02"); got != "2027-03-31" {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		provider string
		slots    int64
		months   int
	}{
		{"stripe", 1, 12}, {"opennode", 1, 1}, {"opennode", 1, 36}, {"unknown", 1, 12},
		{"stripe", 0, 1}, {"stripe", -1, 1}, {"opennode", math.MaxInt64, 120},
	} {
		if _, err := AlertsTerm(tc.provider, tc.slots, tc.months, start); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}

func TestAlertsProration(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, months := range []int{12, 24, 120} {
		full, err := AlertsTerm("opennode", 3, months, start)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			at   time.Time
			unit int64
		}{
			{start, int64(months) * 100},
			{start.Add(full.End.Sub(start) / 2), int64(months) * 50},
			{full.End.Add(-time.Nanosecond), 0},
		} {
			p, err := AlertsIncrease("opennode", 3, months, start, full.End, tc.at)
			if err != nil || p.UnitCents != tc.unit || p.TotalCents != tc.unit*3 || !p.End.Equal(full.End) || !p.Start.Equal(tc.at) {
				t.Fatalf("months=%d price=%+v err=%v", months, p, err)
			}
		}
		for _, at := range []time.Time{start.Add(-time.Nanosecond), full.End, full.End.Add(time.Second)} {
			if _, err := AlertsIncrease("opennode", 1, months, start, full.End, at); err == nil {
				t.Fatal("accepted out of period increase")
			}
		}
		if _, err := AlertsIncrease("opennode", 1, months, start, full.End.Add(time.Hour), start); err == nil {
			t.Fatal("accepted changed expiry")
		}
	}
	// A 30-day month has exactly half a cent remaining at 3h36m to expiry.
	full, _ := AlertsTerm("stripe", 2, 1, start)
	for _, tc := range []struct {
		remaining time.Duration
		want      int64
	}{
		{216 * time.Minute, 1}, {216*time.Minute - time.Nanosecond, 0},
	} {
		p, err := AlertsIncrease("stripe", 2, 1, start, full.End, full.End.Add(-tc.remaining))
		if err != nil || p.UnitCents != tc.want || p.TotalCents != 2*tc.want {
			t.Fatalf("rounding: %+v %v", p, err)
		}
	}
}
