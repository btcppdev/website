package handlers

import (
	"sort"
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestRecordingAutoscheduleOrdersByStageThenAgendaTime(t *testing.T) {
	base := time.Date(2026, time.July, 22, 9, 0, 0, 0, time.UTC)
	rows := []*RecordingRow{
		autoscheduleTestRow("workshop", "three", base),
		autoscheduleTestRow("talks", "two", base.Add(time.Hour)),
		autoscheduleTestRow("main-later", "one", base.Add(3*time.Hour)),
		autoscheduleTestRow("main-earlier", "main stage", base.Add(2*time.Hour)),
		autoscheduleTestRow("other", "lounge", base.Add(-time.Hour)),
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return recordingAutoscheduleSortKey(rows[i]) < recordingAutoscheduleSortKey(rows[j])
	})
	want := []string{"main-earlier", "main-later", "talks", "workshop", "other"}
	for i, id := range want {
		if got := rows[i].Recording.ID; got != id {
			t.Fatalf("row %d = %q, want %q", i, got, id)
		}
	}
}

func TestRecordingAutoscheduleStageRankSupportsLegacyVenues(t *testing.T) {
	tests := map[string]int{
		"p2pkh":       0,
		"p2wsh":       1,
		"p2sh-p2wpkh": 1,
		"multisig":    2,
		"p2tr":        2,
	}
	for venue, want := range tests {
		if got := recordingAutoscheduleStageRank(autoscheduleTestRow(venue, venue, time.Now())); got != want {
			t.Errorf("stage rank for %q = %d, want %d", venue, got, want)
		}
	}
}

func TestReorderRecordingAutoscheduleItemsReassignsConfiguredSlots(t *testing.T) {
	firstAt := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(24 * time.Hour)
	thirdAt := secondAt.Add(24 * time.Hour)
	items := []*RecordingAutoscheduleItem{
		{Row: autoscheduleTestRow("first", "one", firstAt), PublishAt: firstAt, SlotLabel: "Monday 10:00"},
		{Row: autoscheduleTestRow("second", "two", secondAt), PublishAt: secondAt, SlotLabel: "Tuesday 10:00"},
		{Row: autoscheduleTestRow("third", "three", thirdAt), PublishAt: thirdAt, SlotLabel: "Wednesday 10:00"},
	}

	got := reorderRecordingAutoscheduleItems(items, []string{"third", "unknown", "first", "third"})
	wantIDs := []string{"third", "first", "second"}
	wantTimes := []time.Time{firstAt, secondAt, thirdAt}
	for i := range wantIDs {
		if got[i].Row.Recording.ID != wantIDs[i] {
			t.Fatalf("item %d ID = %q, want %q", i, got[i].Row.Recording.ID, wantIDs[i])
		}
		if !got[i].PublishAt.Equal(wantTimes[i]) {
			t.Fatalf("item %d publish time = %s, want %s", i, got[i].PublishAt, wantTimes[i])
		}
	}
	if items[0].Row.Recording.ID != "first" || !items[0].PublishAt.Equal(firstAt) {
		t.Fatal("reorder mutated the original preview items")
	}
}

func autoscheduleTestRow(id, venue string, start time.Time) *RecordingRow {
	return &RecordingRow{
		Recording: &types.Recording{ID: id, TalkName: id},
		ConfTalk:  &types.ConfTalk{Venue: venue, Sched: &types.Times{Start: start}},
	}
}

func TestNextYouTubePublishTimesUsesCentralSlotWallClock(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load America/Chicago: %v", err)
	}
	slots := []*types.YouTubePublishSlot{
		{Weekday: time.Monday, TimeOfDay: "10:05", Timezone: "America/Chicago", Active: true},
		{Weekday: time.Monday, TimeOfDay: "14:04", Timezone: "America/Chicago", Active: true},
		{Weekday: time.Tuesday, TimeOfDay: "10:05", Timezone: "America/Chicago", Active: true},
	}
	after := time.Date(2026, 6, 8, 9, 0, 0, 0, loc)

	got, err := nextYouTubePublishTimes(slots, map[int64]bool{}, after, 3)
	if err != nil {
		t.Fatalf("nextYouTubePublishTimes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d slots, want 3", len(got))
	}
	want := []time.Time{
		time.Date(2026, 6, 8, 10, 5, 0, 0, loc),
		time.Date(2026, 6, 8, 14, 4, 0, 0, loc),
		time.Date(2026, 6, 9, 10, 5, 0, 0, loc),
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("slot %d = %s, want %s", i, got[i], want[i])
		}
		if got[i].In(loc).Format("15:04") != want[i].Format("15:04") {
			t.Fatalf("slot %d wall time = %s, want %s", i, got[i].In(loc).Format("15:04"), want[i].Format("15:04"))
		}
	}
}

func TestNextYouTubePublishTimesSkipsOccupied(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load America/Chicago: %v", err)
	}
	slots := []*types.YouTubePublishSlot{
		{Weekday: time.Monday, TimeOfDay: "10:05", Timezone: "America/Chicago", Active: true},
		{Weekday: time.Monday, TimeOfDay: "14:04", Timezone: "America/Chicago", Active: true},
	}
	occupiedSlot := time.Date(2026, 6, 8, 10, 5, 0, 0, loc)
	occupied := map[int64]bool{occupiedSlot.UTC().Unix(): true}

	got, err := nextYouTubePublishTimes(slots, occupied, time.Date(2026, 6, 8, 9, 0, 0, 0, loc), 2)
	if err != nil {
		t.Fatalf("nextYouTubePublishTimes: %v", err)
	}
	want := []time.Time{
		time.Date(2026, 6, 8, 14, 4, 0, 0, loc),
		time.Date(2026, 6, 15, 10, 5, 0, 0, loc),
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("slot %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestSlotDayGroupsDefaultShape(t *testing.T) {
	slots := []*types.YouTubePublishSlot{
		{Weekday: time.Friday, TimeOfDay: "19:00", Timezone: "America/Chicago", Active: true},
		{Weekday: time.Friday, TimeOfDay: "10:05", Timezone: "America/Chicago", Active: true},
		{Weekday: time.Saturday, TimeOfDay: "14:05", Timezone: "America/Chicago", Active: true},
		{Weekday: time.Sunday, TimeOfDay: "17:45", Timezone: "America/Chicago", Active: true},
	}
	groups := slotDayGroups(slots)
	if len(groups) != 7 {
		t.Fatalf("got %d day groups, want 7", len(groups))
	}
	if groups[5].Times != "10:05\n19:00" {
		t.Fatalf("Friday times = %q, want sorted times", groups[5].Times)
	}
	if groups[6].Times != "14:05" || groups[0].Times != "17:45" {
		t.Fatalf("weekend times = Sunday %q Saturday %q", groups[0].Times, groups[6].Times)
	}
}
