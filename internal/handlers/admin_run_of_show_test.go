package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestRowsFromTalkPrefixesRestrictedRecording(t *testing.T) {
	start := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	rows := rowsFromTalk("vienna", &types.Talk{
		Name:                "Privacy Talk",
		Sched:               &types.Times{Start: start, End: &end},
		RecordingRestricted: true,
	}, nil)

	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if got, want := rows[0].What, "🛑 Privacy Talk (30m)"; got != want {
		t.Fatalf("What = %q, want %q", got, want)
	}
}

func TestRowsFromTalkPrefixesAudioOnly(t *testing.T) {
	start := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	rows := rowsFromTalk("vienna", &types.Talk{
		Name:               "Audio Talk",
		Sched:              &types.Times{Start: start, End: &end},
		RecordingAudioOnly: true,
	}, nil)

	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if got, want := rows[0].What, "🔇 Audio Talk (30m)"; got != want {
		t.Fatalf("What = %q, want %q", got, want)
	}
}

func TestRowsFromTalkRestrictedBeatsAudioOnlyPrefix(t *testing.T) {
	start := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	rows := rowsFromTalk("vienna", &types.Talk{
		Name:                "Mixed Consent Talk",
		Sched:               &types.Times{Start: start, End: &end},
		RecordingRestricted: true,
		RecordingAudioOnly:  true,
	}, nil)

	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if got, want := rows[0].What, "🛑 Mixed Consent Talk (30m)"; got != want {
		t.Fatalf("What = %q, want %q", got, want)
	}
}

func TestRowsFromTalkAddsRecordingEmojiToMultiSpeakerNames(t *testing.T) {
	start := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	rows := rowsFromTalk("vienna", &types.Talk{
		Name:  "Panel",
		Sched: &types.Times{Start: start, End: &end},
		Speakers: []*types.Speaker{
			{ID: "sp-1", Name: "Ada"},
			{ID: "sp-2", Name: "Grace", RecordingEmoji: "🔇"},
			{ID: "sp-3", Name: "Katherine", RecordingEmoji: "🛑"},
		},
	}, nil)

	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if got, want := rows[0].Who, "Ada, Grace 🔇, Katherine 🛑"; got != want {
		t.Fatalf("Who = %q, want %q", got, want)
	}
}

func TestRowsFromTalkDoesNotAddRecordingEmojiToSoloSpeakerName(t *testing.T) {
	start := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	rows := rowsFromTalk("vienna", &types.Talk{
		Name:  "Solo",
		Sched: &types.Times{Start: start, End: &end},
		Speakers: []*types.Speaker{
			{ID: "sp-1", Name: "Ada", RecordingEmoji: "🛑"},
		},
	}, nil)

	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if got, want := rows[0].Who, "Ada"; got != want {
		t.Fatalf("Who = %q, want %q", got, want)
	}
}

func TestRowsFromTalkUsesReadableVenueLabel(t *testing.T) {
	start := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	rows := rowsFromTalk("nairobi", &types.Talk{
		Name:  "Stage Labels",
		Venue: "three",
		Sched: &types.Times{Start: start, End: &end},
	}, nil)

	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if got, want := rows[0].Where, "Workshops Stage"; got != want {
		t.Fatalf("Where = %q, want %q", got, want)
	}
	if got, want := rows[0].VenueTag, "three"; got != want {
		t.Fatalf("VenueTag = %q, want %q", got, want)
	}
}

func TestRunOfShowLocationFallsBackForNairobi(t *testing.T) {
	loc := runOfShowLocation(&types.Conf{
		Tag:       "nairobi",
		StartDate: time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC),
	})
	if got, want := loc.String(), "Africa/Nairobi"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}

	local := time.Date(2026, 6, 17, 7, 0, 0, 0, time.UTC).In(loc)
	if got, want := formatRunOfShowTime(local), "10:00 AM"; got != want {
		t.Fatalf("formatted time = %q, want %q", got, want)
	}
}

func TestBuildPublicRunOfShowStagesGroupsByVenueAndRepeatsInfo(t *testing.T) {
	start := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	days := []*RunOfShowDay{
		{
			Idx:  1,
			Date: start,
			Rows: []*RunOfShowRow{
				{Start: start, Kind: "info", What: "Doors open"},
				{Start: start.Add(time.Hour), Kind: "talk", What: "Main talk", VenueTag: "one", Crew: []RunOfShowCrew{{Label: "Stage Manager", Names: "Sally"}}},
				{Start: start.Add(2 * time.Hour), Kind: "talk", What: "Workshop", VenueTag: "three"},
				{Start: start.Add(3 * time.Hour), Kind: "shift", What: "Volunteer shift", Who: "Vera"},
			},
		},
	}
	venues := []VenueOption{
		{Tag: "one", Label: "Main Stage"},
		{Tag: "three", Label: "Workshops Stage"},
	}

	stages := buildPublicRunOfShowStages(days, venues)
	if len(stages) != 2 {
		t.Fatalf("stages len = %d, want 2", len(stages))
	}
	if got, want := stages[0].Days[0].Rows[0].What, "Doors open"; got != want {
		t.Fatalf("stage 0 first row = %q, want %q", got, want)
	}
	if got, want := stages[0].Days[0].Rows[1].What, "Main talk"; got != want {
		t.Fatalf("stage 0 talk row = %q, want %q", got, want)
	}
	if got, want := stages[0].Days[0].Rows[1].Crew[0].Names, "Sally"; got != want {
		t.Fatalf("stage 0 crew names = %q, want %q", got, want)
	}
	if got := len(stages[0].Days[0].Rows); got != 2 {
		t.Fatalf("stage 0 rows len = %d, want 2", got)
	}
	if got, want := stages[1].Days[0].Rows[0].What, "Doors open"; got != want {
		t.Fatalf("stage 1 first row = %q, want %q", got, want)
	}
	if got, want := stages[1].Days[0].Rows[1].What, "Workshop"; got != want {
		t.Fatalf("stage 1 talk row = %q, want %q", got, want)
	}
	if got := len(stages[1].Days[0].Rows); got != 2 {
		t.Fatalf("stage 1 rows len = %d, want 2", got)
	}
}

func TestMarkRunOfShowProgressHighlightsCurrentRow(t *testing.T) {
	loc := time.FixedZone("TEST", 3*60*60)
	start := time.Date(2026, 6, 17, 10, 0, 0, 0, loc)
	end := start.Add(45 * time.Minute)
	days := []*RunOfShowDay{
		{
			Idx:  1,
			Date: start,
			Rows: []*RunOfShowRow{
				{Start: start, End: &end, Kind: "talk", What: "Current talk"},
				{Start: end.Add(15 * time.Minute), Kind: "talk", What: "Next talk"},
			},
		},
	}

	markRunOfShowProgress(days, start.Add(10*time.Minute))

	if !days[0].Rows[0].IsCurrent {
		t.Fatalf("first row IsCurrent = false, want true")
	}
	if days[0].Rows[1].NowMarkerBefore {
		t.Fatalf("next row NowMarkerBefore = true, want false while a row is current")
	}
}

func TestMarkRunOfShowProgressPlacesMarkerBeforeNextRow(t *testing.T) {
	loc := time.FixedZone("TEST", 3*60*60)
	first := time.Date(2026, 6, 17, 10, 0, 0, 0, loc)
	second := first.Add(time.Hour)
	days := []*RunOfShowDay{
		{
			Idx:  1,
			Date: first,
			Rows: []*RunOfShowRow{
				{Start: first, Kind: "info", What: "Doors open"},
				{Start: second, Kind: "talk", What: "Next talk"},
			},
		},
	}

	markRunOfShowProgress(days, first.Add(30*time.Minute))

	if days[0].Rows[0].IsCurrent {
		t.Fatalf("first row IsCurrent = true, want false for timestamp-only row")
	}
	if !days[0].Rows[1].NowMarkerBefore {
		t.Fatalf("second row NowMarkerBefore = false, want true")
	}
}

func TestRunOfShowTalkVisibleFiltersTerminalStatuses(t *testing.T) {
	cases := map[string]bool{
		"":              true,
		StatusAccepted:  true,
		StatusScheduled: true,
		"TheyDecline":   false,
		"WeDecline":     false,
		"Rejected":      false,
		"Invited":       false,
	}
	for status, want := range cases {
		t.Run(status, func(t *testing.T) {
			if got := runOfShowTalkVisible(&types.Talk{Status: status}); got != want {
				t.Fatalf("visible = %t, want %t", got, want)
			}
		})
	}
}

func TestRowsFromShiftRepeatsVolunteersOnEndRow(t *testing.T) {
	start := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)

	rows := rowsFromShift(&types.WorkShift{
		Name:           "Registration Desk",
		ShiftTime:      &types.Times{Start: start, End: &end},
		ShiftLeaderRef: "lead",
		AssigneesRef:   []string{"lead", "helper"},
	}, map[string]*types.Volunteer{
		"lead":   {Name: "Lena"},
		"helper": {Name: "Hank"},
	})

	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}
	if got, want := rows[0].Who, "Lena (lead), Hank"; got != want {
		t.Fatalf("start Who = %q, want %q", got, want)
	}
	if got, want := rows[1].What, "End: Volunteer shift: Registration Desk"; got != want {
		t.Fatalf("end What = %q, want %q", got, want)
	}
	if got, want := rows[1].Who, "Lena (lead), Hank"; got != want {
		t.Fatalf("end Who = %q, want %q", got, want)
	}
}

func TestStageCrewForTalkMatchesVenueAndRoles(t *testing.T) {
	start := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	shiftStart := start.Add(-1 * time.Hour)
	shiftEnd := start.Add(2 * time.Hour)
	otherVenueEnd := start.Add(2 * time.Hour)

	talk := &types.Talk{
		Name:  "Taproot",
		Venue: "one",
		Sched: &types.Times{Start: start, End: &end},
	}
	shifts := []*types.WorkShift{
		{
			Name:           "Showrunner — Main Stage (AM, day 1)",
			Type:           &types.JobType{Tag: "showrunner", Title: "Showrunner"},
			ShiftTime:      &types.Times{Start: shiftStart, End: &shiftEnd},
			ShiftLeaderRef: "stage-lead",
		},
		{
			Name:         "A/V Monitor — Main Stage (AM, day 1)",
			Type:         &types.JobType{Tag: "avdesk", Title: "A/V Monitor"},
			ShiftTime:    &types.Times{Start: shiftStart, End: &shiftEnd},
			AssigneesRef: []string{"av-a", "av-b"},
		},
		{
			Name:           "Showrunner — Talks Stage (AM, day 1)",
			Type:           &types.JobType{Tag: "showrunner", Title: "Showrunner"},
			ShiftTime:      &types.Times{Start: shiftStart, End: &otherVenueEnd},
			ShiftLeaderRef: "wrong-stage",
		},
	}
	vols := map[string]*types.Volunteer{
		"stage-lead":  {Name: "Sally"},
		"av-a":        {Name: "Alex"},
		"av-b":        {Name: "Avery"},
		"wrong-stage": {Name: "Taylor"},
	}

	crew := stageCrewForTalk("vienna", talk, shifts, vols)
	if len(crew) != 2 {
		t.Fatalf("crew len = %d, want 2: %#v", len(crew), crew)
	}
	if got, want := crew[0].Label, "Stage Manager"; got != want {
		t.Fatalf("crew[0].Label = %q, want %q", got, want)
	}
	if got, want := crew[0].Names, "Sally"; got != want {
		t.Fatalf("crew[0].Names = %q, want %q", got, want)
	}
	if got, want := crew[1].Label, "A/V Tech"; got != want {
		t.Fatalf("crew[1].Label = %q, want %q", got, want)
	}
	if got, want := crew[1].Names, "Alex, Avery"; got != want {
		t.Fatalf("crew[1].Names = %q, want %q", got, want)
	}
}
