package getters

import "testing"

func TestHackathonManagersAttachToScheduleProposals(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)
	confID, tag := insertSmokeConference(t, ctx)
	otherConfID, otherTag := insertSmokeConference(t, ctx)
	competition := createSmokeCompetition(t, ctx, CompetitionInput{Title: "Manager linkage", ConferenceID: confID})
	otherCompetition := createSmokeCompetition(t, ctx, CompetitionInput{Title: "Other linkage", ConferenceID: otherConfID})
	manager := insertSmokePerson(t, ctx, "schedule-manager")
	global := insertSmokePerson(t, ctx, "global-schedule-manager")
	speaker := insertSmokePerson(t, ctx, "existing-schedule-speaker")
	inputs := []CompetitionScheduleSegmentInput{{Title: "Hacking", SegmentType: "hacking", DefaultDurationMinutes: 120}, {Title: "Kickoff", SegmentType: "kickoff", DefaultDurationMinutes: 30}}
	if err := ReplaceCompetitionScheduleSegments(ctx, competition, inputs); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceCompetitionScheduleSegments(ctx, otherCompetition, inputs[:1]); err != nil {
		t.Fatal(err)
	}
	segments, err := ListCompetitionScheduleSegments(ctx, competition)
	if err != nil {
		t.Fatal(err)
	}
	// A pre-existing profile and participant must survive manager linking.
	sc, err := UpsertSpeakerConf(ctx, SpeakerConfInput{SpeakerID: manager, ConfTag: tag, ProposalID: segments[0].ProposalID, RecordOK: "Audio Only"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertSpeakerConf(ctx, SpeakerConfInput{SpeakerID: speaker, ConfTag: tag, OtherEventTags: []string{tag}, ProposalID: segments[0].ProposalID}); err != nil {
		t.Fatal(err)
	}
	count := func(person, comp string, want int) {
		t.Helper()
		var n int
		err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT count(*) FROM proposals_speaker_confs psc JOIN speaker_confs sc ON sc.id=psc.speaker_conf_id JOIN competition_schedule_segments seg ON seg.proposal_id=psc.proposal_id WHERE sc.speaker_id=$1::uuid AND seg.competition_id=$2::uuid`, person, comp).Scan(&n)
		if err != nil || n != want {
			t.Fatalf("links=%d want %d: %v", n, want, err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := SetSpeakerRole(ctx, manager, tag, "hackathon", true); err != nil {
			t.Fatal(err)
		}
	}
	count(manager, competition, 2)
	count(manager, otherCompetition, 0)
	count(speaker, competition, 1)
	var profiles int
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT count(*) FROM speaker_confs WHERE speaker_id=$1::uuid`, manager).Scan(&profiles); err != nil || profiles != 1 {
		t.Fatalf("profiles=%d err=%v", profiles, err)
	}
	var consent string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT record_ok FROM speaker_confs WHERE id=$1::uuid`, sc).Scan(&consent); err != nil || consent != "Audio Only" {
		t.Fatalf("changed existing profile: %q %v", consent, err)
	}
	if err := SetSpeakerRole(ctx, global, "global", "hackathon", true); err != nil {
		t.Fatal(err)
	}
	count(global, competition, 2)
	count(global, otherCompetition, 1)
	for i := range segments {
		inputs[i] = CompetitionScheduleSegmentInput{ID: segments[i].ID, Title: segments[i].Title, SegmentType: segments[i].SegmentType, DefaultDurationMinutes: segments[i].DefaultDurationMinutes}
	}
	inputs = append(inputs, CompetitionScheduleSegmentInput{Title: "Awards", SegmentType: "awards", DefaultDurationMinutes: 30})
	if err := ReplaceCompetitionScheduleSegments(ctx, competition, inputs); err != nil {
		t.Fatal(err)
	}
	count(manager, competition, 3)
	count(global, competition, 3)
	count(speaker, competition, 1)
	if err := MoveSpeakerRoleScope(ctx, manager, tag, otherTag, "hackathon"); err != nil {
		t.Fatal(err)
	}
	count(manager, otherCompetition, 1)
	if err := SetSpeakerRole(ctx, manager, otherTag, "hackathon", false); err != nil {
		t.Fatal(err)
	}
	count(manager, otherCompetition, 1) // Recorded participation is preserved.
}
