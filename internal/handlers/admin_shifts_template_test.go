package handlers

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestAdminShiftsTemplateIncludesInlineVolunteerControls(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}

	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}

	start := time.Date(2026, time.September, 12, 8, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	shift := &types.WorkShift{
		Ref:          "shift-one",
		Name:         "Morning A/V",
		MaxVols:      2,
		AssigneesRef: []string{"vol-one"},
		ShiftTime:    &types.Times{Start: start, End: &end},
	}
	assigned := &types.Volunteer{Ref: "vol-one", Name: "Ada Assigned", Email: "ada@example.test", Status: "Scheduled"}
	candidate := &types.Volunteer{Ref: "vol-two", Name: "Vera Volunteer", Email: "vera@example.test", Status: "PendingShifts"}
	page := &VolAdminShiftsPage{
		Conf:       &types.Conf{Tag: "test26", Desc: "Test 2026", StartDate: start, EndDate: end},
		Days:       []*ShiftDayGroup{{Date: shift.DayOf(), DateDesc: shift.DayOfDesc(), MinHour: 7, MaxHour: 11, Shifts: []*types.WorkShift{shift}}},
		VolMap:     map[string]*types.Volunteer{assigned.Ref: assigned},
		Candidates: map[string][]*types.Volunteer{shift.Ref: {candidate}},
		Flash:      "Volunteer added",
	}

	var rendered bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&rendered, "volunteers/admin_shifts.tmpl", page); err != nil {
		t.Fatalf("render admin shifts: %v", err)
	}
	for _, expected := range []string{
		"Volunteer added",
		`action="/test26/volcoord/shifts/shift-one/volunteers"`,
		`value="vol-two"`,
		"Vera Volunteer",
		`action="/test26/volcoord/shifts/shift-one/volunteers/vol-one/remove"`,
		"Ada Assigned",
	} {
		if !strings.Contains(rendered.String(), expected) {
			t.Fatalf("admin shifts omitted %q: %s", expected, rendered.String())
		}
	}
}
