package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

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
