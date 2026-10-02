package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestAgendaSessionHeightTracksScheduledDuration(t *testing.T) {
	start := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	end := start.Add(45 * time.Minute)
	session := &types.Session{Sched: &types.Times{Start: start, End: &end}}

	want := 45 * agendaPixelsPerMinute
	if got := agendaSessionHeight(&AgendaDay{All: []*types.Session{session}}, session); got != want {
		t.Fatalf("agendaSessionHeight() = %.1f, want %.1f", got, want)
	}
}

func TestAgendaSessionHeightKeepsShortSessionsUsable(t *testing.T) {
	start := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	end := start.Add(15 * time.Minute)
	session := &types.Session{Sched: &types.Times{Start: start, End: &end}}

	if got := agendaSessionHeight(&AgendaDay{All: []*types.Session{session}}, session); got != agendaMinSessionHeight {
		t.Fatalf("agendaSessionHeight() = %.1f, want minimum %.1f", got, agendaMinSessionHeight)
	}
}

func TestSelectActiveAgendaDay(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.October, 1, 11, 0, 0, 0, loc)
	conf := &types.Conf{StartDate: start, EndDate: time.Date(2026, time.October, 3, 0, 0, 0, 0, loc), TZ: loc}
	for _, tc := range []struct {
		name, now string
		want      int
	}{
		{"before event", "2026-09-30T12:00:00Z", 1},
		{"first day", "2026-10-01T10:00:00Z", 1},
		{"second day", "2026-10-02T10:00:00Z", 2},
		{"local midnight before UTC changes date", "2026-10-01T22:00:00Z", 2},
		{"last day after midnight end timestamp", "2026-10-03T20:00:00Z", 3},
		{"after final local day", "2026-10-03T22:00:00Z", 1},
		{"past event", "2026-11-03T12:00:00Z", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			days := []*AgendaDay{{Idx: 1, Date: start}, {Idx: 2, Date: start.AddDate(0, 0, 1), Active: true}, {Idx: 3, Date: start.AddDate(0, 0, 2), Active: true}}
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			selectActiveAgendaDay(conf, days, now)
			for _, day := range days {
				if day.Active != (day.Idx == tc.want) {
					t.Fatalf("day %d active=%v, want active day %d", day.Idx, day.Active, tc.want)
				}
			}
		})
	}
	days := []*AgendaDay{{Idx: 2, Date: start.AddDate(0, 0, 1)}, {Idx: 3, Date: start.AddDate(0, 0, 2), Active: true}}
	selectActiveAgendaDay(conf, days, start)
	if !days[0].Active || days[1].Active {
		t.Fatal("missing day must fall back to first available day")
	}
	selectActiveAgendaDay(conf, nil, start)
}

func TestAgendaDayIndexAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []time.Time{time.Date(2026, time.March, 28, 0, 0, 0, 0, loc), time.Date(2026, time.October, 24, 0, 0, 0, 0, loc)} {
		for i := 0; i < 3; i++ {
			date := start.AddDate(0, 0, i)
			if got := dayIndex(start, date, loc); got != i+1 {
				t.Fatalf("date %s: got day %d want %d", date, got, i+1)
			}
		}
	}
}

func TestAgendaMatchingEndTimesAlignAcrossRooms(t *testing.T) {
	start := time.Date(2026, time.October, 2, 14, 0, 0, 0, time.UTC)
	halfPast := start.Add(30 * time.Minute)
	lunch := start.Add(45 * time.Minute)
	first := &types.Session{Venue: "one", Sched: &types.Times{Start: start, End: &halfPast}}
	short := &types.Session{Venue: "one", Sched: &types.Times{Start: halfPast, End: &lunch}}
	parallel := &types.Session{Venue: "two", Sched: &types.Times{Start: start, End: &lunch}}
	day := &AgendaDay{All: []*types.Session{first, parallel, short}}
	bottom := func(s *types.Session) float64 { return agendaSessionTop(day, s) + agendaSessionHeight(day, s) }
	if bottom(short) != bottom(parallel) {
		t.Fatalf("2:45pm ends differ: short=%v parallel=%v", bottom(short), bottom(parallel))
	}
	if bottom(first) != agendaSessionTop(day, short) {
		t.Fatal("back-to-back sessions overlap or leave a gap")
	}
	if agendaSessionHeight(day, short) < agendaMinSessionHeight {
		t.Fatal("short talk no longer has room for its preview")
	}
	if agendaDayHeight(day) != 60*agendaDayPixelsPerMinute(day) {
		t.Fatal("day extent uses a different time scale")
	}
	marks := agendaHourMarks(day)
	if len(marks) != 2 || marks[1].Top != agendaDayHeight(day) {
		t.Fatalf("hour labels do not share session time scale: %+v", marks)
	}
}
