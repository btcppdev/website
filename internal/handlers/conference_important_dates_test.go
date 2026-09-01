package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestBuildConferenceImportantDatesCombinesConfiguredAndOperationalDates(t *testing.T) {
	loc := time.FixedZone("event", -5*60*60)
	start := time.Date(2026, time.October, 1, 9, 0, 0, 0, loc)
	conf := &types.Conf{
		Tag:       "test26",
		Location:  "Test City",
		StartDate: start,
		TZ:        loc,
		Tickets: []*types.ConfTicket{
			{ID: "general", BasePrice: 150, Symbol: "$", SalesEndAt: start.AddDate(0, 0, -10)},
			{ID: "early", BasePrice: 100, Symbol: "$", SalesEndAt: start.AddDate(0, 0, -30)},
		},
	}
	custom := []*types.ConferenceMilestone{
		{Label: "Tickets go on sale", Category: "tickets", OccursAt: start.AddDate(0, 0, -90), Published: true},
		{Label: "Hidden planning date", Category: "other", OccursAt: start.AddDate(0, 0, -80), Published: false},
		{Label: "Talk applications open", Category: "talks", OccursAt: start.AddDate(0, 0, -75), Published: true},
	}

	dates := buildConferenceImportantDates(conf, custom, start.AddDate(0, 0, -60))
	if len(dates) != 6 {
		t.Fatalf("got %d important dates, want 6", len(dates))
	}
	wantLabels := []string{
		"Tickets go on sale",
		"Talk applications open",
		"Talk applications close",
		"Ticket price increases",
		"Ticket sales close",
		"Conference begins",
	}
	for _, want := range wantLabels {
		found := false
		for _, date := range dates {
			if date.Label == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q in important dates", want)
		}
	}
	if dates[2].Label != "Talk applications close" || !dates[2].IsNext || dates[2].Status != "Up next" {
		t.Fatalf("next date = %#v, want talk applications close", dates[2])
	}
	if got := dates[3].Detail; got != "$100 → $150" {
		t.Fatalf("price increase detail = %q, want %q", got, "$100 → $150")
	}
	if dates[0].URL != "#tickets" || dates[1].URL != "/talk/test26" {
		t.Fatalf("default URLs = %q, %q", dates[0].URL, dates[1].URL)
	}
}

func TestValidConferenceMilestoneURL(t *testing.T) {
	for _, value := range []string{"", "#tickets", "/talk/dev26", "https://example.com/register"} {
		if !validConferenceMilestoneURL(value) {
			t.Errorf("validConferenceMilestoneURL(%q) = false", value)
		}
	}
	for _, value := range []string{"javascript:alert(1)", "example.com/register", "mailto:test@example.com"} {
		if validConferenceMilestoneURL(value) {
			t.Errorf("validConferenceMilestoneURL(%q) = true", value)
		}
	}
}
