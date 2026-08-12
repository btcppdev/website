package getters

import (
	"context"
	"fmt"
	"testing"
	"time"

	"btcpp-web/internal/types"
	conferencemissives "btcpp-web/templates/missives"
)

func TestConferenceCampaignTimingsWorkBackwardFromFinal(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{
		StartDate: time.Date(2026, time.August, 14, 9, 0, 0, 0, loc),
		Timezone:  "America/Chicago",
		TZ:        loc,
	}
	timings := ConferenceCampaignTimings(conf)
	byKind := make(map[string]ConferenceCampaignTiming, len(timings))
	for _, timing := range timings {
		byKind[timing.Kind] = timing
	}
	for kind, want := range map[string]time.Time{
		types.ConferenceCampaignAttendeeReminder70: time.Date(2026, time.June, 5, 10, 0, 0, 0, loc),
		types.ConferenceCampaignAttendeeReminder49: time.Date(2026, time.June, 26, 10, 0, 0, 0, loc),
		types.ConferenceCampaignAttendeeReminder28: time.Date(2026, time.July, 17, 10, 0, 0, 0, loc),
		types.ConferenceCampaignAttendeeFinal:      time.Date(2026, time.August, 7, 10, 0, 0, 0, loc),
		types.ConferenceCampaignSpeakerReminder:    time.Date(2026, time.July, 24, 10, 0, 0, 0, loc),
		types.ConferenceCampaignVolunteerOrient:    time.Date(2026, time.August, 13, 9, 0, 0, 0, loc),
	} {
		got := byKind[kind]
		if !got.SendAt.Equal(want) {
			t.Errorf("%s SendAt = %s, want %s", kind, got.SendAt, want)
		}
		if !got.BuildAt.Equal(want.AddDate(0, 0, -1)) {
			t.Errorf("%s BuildAt = %s, want one local day before %s", kind, got.BuildAt, want)
		}
	}
}

func TestConferenceCampaignPersistenceIsIdempotent(t *testing.T) {
	ctx := postgresSmokeContext(t)
	definitions, err := conferencemissives.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
			INSERT INTO missives
				(public_uid, title, newsletters, only_for, markdown, send_at_expr, dedupe_key)
			VALUES
				((SELECT COALESCE(max(public_uid), 0) + 1 FROM missives), $1, '{}'::text[], $2, $3, '', $4)
			ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
		`, definition.Title, definition.OnlyFor, definition.Markdown, "conference-template:"+definition.Kind); err != nil {
			t.Fatalf("provision %s test template: %v", definition.Kind, err)
		}
	}
	suffix := postgresSmokeSuffix()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2031, time.September, 12, 9, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 2)
	var confID string
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO conferences
			(tag, description, start_date, end_date, timezone, location, venue, publication_status)
		VALUES ($1, $2, $3, $4, 'America/Chicago', 'Test City', 'Test Venue', 'draft')
		RETURNING id::text
	`, "campaign-"+suffix, "Campaign Test "+suffix, start, end).Scan(&confID)
	if err != nil {
		t.Fatalf("insert campaign test conference: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id = $1::uuid`, confID)
	})
	conf := &types.Conf{
		Ref: confID, Tag: "campaign-" + suffix, Desc: "Campaign Test " + suffix,
		StartDate: start, EndDate: end, Timezone: "America/Chicago", TZ: loc,
	}
	now := start.AddDate(0, 0, -100)
	for i := 0; i < 2; i++ {
		if err := EnsureConferenceEmailCampaigns(ctx, conf, now); err != nil {
			t.Fatalf("EnsureConferenceEmailCampaigns pass %d: %v", i+1, err)
		}
	}
	campaigns, err := ListConferenceEmailCampaigns(ctx, confID)
	if err != nil {
		t.Fatal(err)
	}
	if len(campaigns) != len(conferenceCampaignDefaults) {
		t.Fatalf("campaign count = %d, want %d", len(campaigns), len(conferenceCampaignDefaults))
	}
	for _, campaign := range campaigns {
		if campaign.TemplateMissiveID == "" || campaign.TemplateMissiveUID == 0 {
			t.Errorf("%s campaign is not linked to its source missive", campaign.Kind)
		}
	}
	occurrences, err := ListConferenceEmailOccurrences(ctx, confID)
	if err != nil {
		t.Fatal(err)
	}
	if len(occurrences) != len(conferenceCampaignDefaults)-1 {
		t.Fatalf("occurrence count = %d, want %d", len(occurrences), len(conferenceCampaignDefaults)-1)
	}
	for _, occurrence := range occurrences {
		if occurrence.Status != "planned" {
			t.Errorf("%s status = %s, want planned", occurrence.CampaignKind, occurrence.Status)
		}
		if occurrence.SendAt.Before(occurrence.BuildAt) {
			t.Errorf("%s sends before it builds", occurrence.CampaignKind)
		}
	}
	buildTarget := occurrences[0]
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE conference_email_occurrences SET status = 'building' WHERE id = $1::uuid`, buildTarget.ID); err != nil {
		t.Fatal(err)
	}
	draft, err := CreateConferenceOccurrenceDraft(ctx, buildTarget, "Generated test", "Generated body", &end)
	if err != nil {
		t.Fatalf("CreateConferenceOccurrenceDraft: %v", err)
	}
	standardLetters, err := GetLetters(ctx, conf.Tag)
	if err != nil {
		t.Fatal(err)
	}
	for _, letter := range standardLetters {
		if letter.UID == draft.UID {
			t.Fatalf("generated occurrence MISS-%d leaked into standard newsletter scheduling", draft.UID)
		}
	}
	if campaigns[0].ConferenceID != confID {
		t.Fatal(fmt.Sprintf("campaign conference = %s, want %s", campaigns[0].ConferenceID, confID))
	}
}

func TestConferenceCampaignThursdayIsStrictlyBeforeEvent(t *testing.T) {
	loc := time.UTC
	conf := &types.Conf{StartDate: time.Date(2026, time.August, 13, 9, 0, 0, 0, loc), TZ: loc}
	for _, timing := range ConferenceCampaignTimings(conf) {
		if timing.Kind != types.ConferenceCampaignVolunteerOrient {
			continue
		}
		want := time.Date(2026, time.August, 6, 9, 0, 0, 0, loc)
		if !timing.SendAt.Equal(want) {
			t.Fatalf("orientation send = %s, want %s", timing.SendAt, want)
		}
		return
	}
	t.Fatal("orientation timing missing")
}

func TestLatestConferenceTalkClipartIsScopedToEventAndUpdateOrder(t *testing.T) {
	ctx := databaseSmokeContext(t)
	confID, _ := insertSmokeConference(t, ctx)
	otherConfID, _ := insertSmokeConference(t, ctx)
	oldAt := time.Now().UTC().Add(-2 * time.Hour)
	newAt := oldAt.Add(time.Hour)
	for _, row := range []struct {
		confID, clipart string
		when            time.Time
		scheduled       time.Time
	}{
		{confID, "older.png", oldAt, oldAt},
		{confID, "newest.png", newAt, newAt},
		{otherConfID, "other-event.png", newAt.Add(time.Hour), newAt.Add(time.Hour)},
	} {
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
			INSERT INTO conf_talks (conference_id, clipart_path, scheduled_start, updated_at)
			VALUES ($1::uuid, $2, $3, $4)
		`, row.confID, row.clipart, row.scheduled, row.when); err != nil {
			t.Fatalf("insert clipart fixture: %v", err)
		}
	}

	got, err := LatestConferenceTalkClipart(ctx, confID)
	if err != nil {
		t.Fatal(err)
	}
	if got != "newest.png" {
		t.Fatalf("LatestConferenceTalkClipart() = %q, want newest.png", got)
	}
}

func TestEnsureConferenceEmailCampaignsHonorsEventOptOut(t *testing.T) {
	ctx := databaseSmokeContext(t)
	confID, tag := insertSmokeConference(t, ctx)
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE conferences SET conference_email_campaigns_enabled = false WHERE id = $1::uuid
	`, confID); err != nil {
		t.Fatal(err)
	}
	disabled := false
	conf := &types.Conf{
		Ref: confID, Tag: tag, StartDate: time.Now().AddDate(0, 1, 0),
		ConferenceEmailCampaignsEnabled: &disabled,
	}
	if err := EnsureConferenceEmailCampaigns(ctx, conf, time.Now()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT count(*) FROM conference_email_campaigns WHERE conference_id = $1::uuid
	`, confID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("disabled event created %d conference campaigns, want 0", count)
	}
	loaded, err := GetConfByRef(ctx, confID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConferenceEmailCampaignsEnabled == nil || *loaded.ConferenceEmailCampaignsEnabled {
		t.Fatal("conference campaign opt-out did not round-trip from the database")
	}
}
