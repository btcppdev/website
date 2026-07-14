package getters

import (
	"context"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestHackathonCompetitionRequiresConference(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)

	_, err := CreateCompetition(ctx, CompetitionInput{
		Slug:  "missing-conf-" + postgresSmokeSuffix(),
		Title: "Missing Conference Hackathon",
	})
	if err == nil {
		t.Fatalf("CreateCompetition without conference succeeded")
	}
	if !strings.Contains(err.Error(), "conference is required") {
		t.Fatalf("CreateCompetition without conference err = %v, want conference required", err)
	}

	linkedID := createSmokeCompetition(t, ctx, CompetitionInput{
		Slug:  "linked-" + postgresSmokeSuffix(),
		Title: "Conference Hackathon",
	})
	linked, err := GetCompetitionByID(ctx, linkedID)
	if err != nil {
		t.Fatalf("GetCompetitionByID linked: %v", err)
	}
	if linked.ConferenceID == "" {
		t.Fatalf("linked competition conference id is empty")
	}
}

func TestHackathonCompetitionUpdate(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)

	competitionID := createSmokeCompetition(t, ctx, CompetitionInput{
		Slug:  "update-" + postgresSmokeSuffix(),
		Title: "Original Hackathon",
	})
	confID, _ := insertSmokeConference(t, ctx)
	maxTeamSize := 4
	openAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	closeAt := openAt.Add(48 * time.Hour)
	galleryAt := closeAt.Add(time.Hour)
	updatedSlug := "updated-" + postgresSmokeSuffix()
	if err := UpdateCompetition(ctx, competitionID, CompetitionInput{
		ConferenceID:       confID,
		Slug:               updatedSlug,
		Title:              "Updated Hackathon",
		Description:        "Updated description",
		Visibility:         CompetitionVisibilityPublic,
		MaxTeamSize:        &maxTeamSize,
		SubmissionsOpenAt:  &openAt,
		SubmissionsCloseAt: &closeAt,
		PublicGalleryAt:    &galleryAt,
	}); err != nil {
		t.Fatalf("UpdateCompetition: %v", err)
	}

	updated, err := GetCompetitionByID(ctx, competitionID)
	if err != nil {
		t.Fatalf("GetCompetitionByID updated: %v", err)
	}
	if updated.ConferenceID != confID {
		t.Fatalf("ConferenceID = %q, want %q", updated.ConferenceID, confID)
	}
	if updated.Slug != updatedSlug || updated.Title != "Updated Hackathon" || updated.Description != "Updated description" || updated.Visibility != CompetitionVisibilityPublic {
		t.Fatalf("updated fields mismatch: %+v", updated)
	}
	if updated.MaxTeamSize == nil || *updated.MaxTeamSize != maxTeamSize {
		t.Fatalf("MaxTeamSize = %v, want %d", updated.MaxTeamSize, maxTeamSize)
	}
	if updated.SubmissionsOpenAt == nil || !updated.SubmissionsOpenAt.Equal(openAt) {
		t.Fatalf("SubmissionsOpenAt = %v, want %v", updated.SubmissionsOpenAt, openAt)
	}
	if updated.SubmissionsCloseAt == nil || !updated.SubmissionsCloseAt.Equal(closeAt) {
		t.Fatalf("SubmissionsCloseAt = %v, want %v", updated.SubmissionsCloseAt, closeAt)
	}
	if updated.PublicGalleryAt == nil || !updated.PublicGalleryAt.Equal(galleryAt) {
		t.Fatalf("PublicGalleryAt = %v, want %v", updated.PublicGalleryAt, galleryAt)
	}
}

func TestHackathonProjectMaxTeamSizeAndInvites(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)

	maxTeamSize := 2
	competitionID := createSmokeCompetition(t, ctx, CompetitionInput{
		Slug:        "teams-" + postgresSmokeSuffix(),
		Title:       "Team Limit Hackathon",
		MaxTeamSize: &maxTeamSize,
	})
	ownerID := insertSmokePerson(t, ctx, "owner")
	projectID := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID:     competitionID,
		CreatedByPersonID: ownerID,
		Slug:              "project-" + postgresSmokeSuffix(),
		Title:             "Limited Team",
	})

	secondID := insertSmokePerson(t, ctx, "second")
	secondEmail := smokePersonEmail(t, ctx, secondID)
	beforeInvite := time.Now()
	token, invite, err := CreateProjectInvite(ctx, projectID, secondEmail, nil)
	if err != nil {
		t.Fatalf("CreateProjectInvite: %v", err)
	}
	if token == "" || invite == nil || invite.ProjectID != projectID {
		t.Fatalf("bad invite token/invite: token=%q invite=%+v", token, invite)
	}
	if invite.Email != secondEmail {
		t.Fatalf("invite email = %q, want %q", invite.Email, secondEmail)
	}
	if invite.ExpiresAt == nil {
		t.Fatalf("invite ExpiresAt is nil")
	}
	minExpiresAt := beforeInvite.Add(ProjectInviteDefaultTTL - time.Minute)
	maxExpiresAt := beforeInvite.Add(ProjectInviteDefaultTTL + time.Minute)
	if invite.ExpiresAt.Before(minExpiresAt) || invite.ExpiresAt.After(maxExpiresAt) {
		t.Fatalf("invite ExpiresAt = %v, want about %v", invite.ExpiresAt, beforeInvite.Add(ProjectInviteDefaultTTL))
	}
	accepted, err := AcceptProjectInvite(ctx, token, secondID)
	if err != nil {
		t.Fatalf("AcceptProjectInvite: %v", err)
	}
	if accepted.AcceptedByPersonID != secondID || accepted.AcceptedAt == nil {
		t.Fatalf("accepted invite mismatch: %+v", accepted)
	}

	thirdID := insertSmokePerson(t, ctx, "third")
	err = AddProjectMember(ctx, projectID, thirdID, ProjectMemberRoleMember)
	if err == nil {
		t.Fatalf("AddProjectMember third succeeded, want max team size error")
	}
	if !strings.Contains(err.Error(), "max team size") {
		t.Fatalf("AddProjectMember third err = %v, want max team size", err)
	}

	otherCompetitionID := createSmokeCompetition(t, ctx, CompetitionInput{
		Slug:  "email-invite-" + postgresSmokeSuffix(),
		Title: "Email Invite Hackathon",
	})
	otherProjectID := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID:     otherCompetitionID,
		CreatedByPersonID: ownerID,
		Slug:              "email-invite-project-" + postgresSmokeSuffix(),
		Title:             "Email Invite Project",
	})
	mismatchToken, _, err := CreateProjectInvite(ctx, otherProjectID, secondEmail, nil)
	if err != nil {
		t.Fatalf("CreateProjectInvite mismatch: %v", err)
	}
	mismatchID := insertSmokePerson(t, ctx, "mismatch")
	if _, err := AcceptProjectInvite(ctx, mismatchToken, mismatchID); err == nil {
		t.Fatalf("AcceptProjectInvite mismatch succeeded, want email error")
	} else if !strings.Contains(err.Error(), "project invite is for") {
		t.Fatalf("AcceptProjectInvite mismatch err = %v, want invite email", err)
	}
}

func TestHackathonJudgeInvites(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)

	competitionID := createSmokeCompetition(t, ctx, CompetitionInput{
		Slug:  "judge-invite-" + postgresSmokeSuffix(),
		Title: "Judge Invite Hackathon",
	})
	judgeID := insertSmokePerson(t, ctx, "judge-invite")
	beforeInvite := time.Now()
	token, invite, err := CreateCompetitionJudgeInvite(ctx, competitionID, nil)
	if err != nil {
		t.Fatalf("CreateCompetitionJudgeInvite: %v", err)
	}
	if token == "" || invite == nil || invite.CompetitionID != competitionID {
		t.Fatalf("bad judge invite: token=%q invite=%+v", token, invite)
	}
	if invite.ExpiresAt == nil {
		t.Fatalf("judge invite ExpiresAt is nil")
	}
	minExpiresAt := beforeInvite.Add(ProjectInviteDefaultTTL - time.Minute)
	maxExpiresAt := beforeInvite.Add(ProjectInviteDefaultTTL + time.Minute)
	if invite.ExpiresAt.Before(minExpiresAt) || invite.ExpiresAt.After(maxExpiresAt) {
		t.Fatalf("judge invite ExpiresAt = %v, want about %v", invite.ExpiresAt, beforeInvite.Add(ProjectInviteDefaultTTL))
	}

	accepted, err := AcceptCompetitionJudgeInvite(ctx, token, judgeID)
	if err != nil {
		t.Fatalf("AcceptCompetitionJudgeInvite: %v", err)
	}
	if accepted.AcceptedByPersonID != judgeID || accepted.AcceptedAt == nil {
		t.Fatalf("accepted judge invite mismatch: %+v", accepted)
	}
	judges, err := ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListCompetitionJudges: %v", err)
	}
	if len(judges) != 1 || judges[0].PersonID != judgeID || judges[0].JudgeType != JudgeTypeCoordinator {
		t.Fatalf("judges after invite = %+v", judges)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO competition_judges (competition_id, person_id, judge_type)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT DO NOTHING
	`, competitionID, judgeID, JudgeTypeExpo); err != nil {
		t.Fatalf("insert duplicate judge type: %v", err)
	}
	if err := AddCompetitionJudge(ctx, competitionID, judgeID, JudgeTypeCoordinator); err != nil {
		t.Fatalf("AddCompetitionJudge duplicate person: %v", err)
	}
	judges, err = ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListCompetitionJudges after duplicate type: %v", err)
	}
	if len(judges) != 1 || judges[0].PersonID != judgeID {
		t.Fatalf("judges after duplicate type = %+v, want one row for %s", judges, judgeID)
	}
	if _, err := AcceptCompetitionJudgeInvite(ctx, token, judgeID); err == nil {
		t.Fatalf("AcceptCompetitionJudgeInvite reuse succeeded, want error")
	} else if !strings.Contains(err.Error(), "already accepted") {
		t.Fatalf("AcceptCompetitionJudgeInvite reuse err = %v, want already accepted", err)
	}
}

func TestGetPersonIDByEmail(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)

	personID := insertSmokePerson(t, ctx, "invite-email")
	personEmail := smokePersonEmail(t, ctx, personID)
	got, err := GetPersonIDByEmail(ctx, personEmail)
	if err != nil {
		t.Fatalf("GetPersonIDByEmail: %v", err)
	}
	if got != personID {
		t.Fatalf("person id = %q, want %q", got, personID)
	}
}

func TestSearchPeopleByNameEmailOrPhone(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)

	personID := insertSmokePerson(t, ctx, "person-search")
	if _, err := ctx.DB.Exec(context.Background(), `
		UPDATE people
		SET phone = '+1 (555) 867-5309', company = 'Search Co'
		WHERE id::text = $1
	`, personID); err != nil {
		t.Fatalf("update person phone: %v", err)
	}
	hits, err := SearchPeopleByNameEmailOrPhone(ctx, "8675309", 10)
	if err != nil {
		t.Fatalf("SearchPeopleByNameEmailOrPhone phone: %v", err)
	}
	if !speakerIDsContain(hits, personID) {
		t.Fatalf("phone search hits = %+v, want person %s", hits, personID)
	}
	email := smokePersonEmail(t, ctx, personID)
	hits, err = SearchPeopleByNameEmailOrPhone(ctx, email, 10)
	if err != nil {
		t.Fatalf("SearchPeopleByNameEmailOrPhone email: %v", err)
	}
	if !speakerIDsContain(hits, personID) {
		t.Fatalf("email search hits = %+v, want person %s", hits, personID)
	}
	hits, err = SearchPeopleByNameEmailOrPhone(ctx, "person-search", 10)
	if err != nil {
		t.Fatalf("SearchPeopleByNameEmailOrPhone name: %v", err)
	}
	if !speakerIDsContain(hits, personID) {
		t.Fatalf("name search hits = %+v, want person %s", hits, personID)
	}
}

func TestHackathonProjectVisibility(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)

	future := time.Now().Add(24 * time.Hour)
	competitionID := createSmokeCompetition(t, ctx, CompetitionInput{
		Slug:               "visibility-" + postgresSmokeSuffix(),
		Title:              "Visibility Hackathon",
		SubmissionsCloseAt: &future,
	})
	ownerID := insertSmokePerson(t, ctx, "owner")
	judgeID := insertSmokePerson(t, ctx, "judge")
	projectID := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID:     competitionID,
		CreatedByPersonID: ownerID,
		Slug:              "private-project-" + postgresSmokeSuffix(),
		Title:             "Private Project",
	})
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO competition_judges (competition_id, person_id, judge_type)
		VALUES ($1, $2, 'expo')
	`, competitionID, judgeID); err != nil {
		t.Fatalf("insert competition judge: %v", err)
	}

	publicOK, err := CanViewProject(ctx, projectID, types.HackathonViewer{})
	if err != nil {
		t.Fatalf("CanViewProject public: %v", err)
	}
	if publicOK {
		t.Fatalf("public viewer can see private project before deadline")
	}
	memberOK, err := CanViewProject(ctx, projectID, types.HackathonViewer{PersonID: ownerID})
	if err != nil {
		t.Fatalf("CanViewProject member: %v", err)
	}
	if !memberOK {
		t.Fatalf("member cannot see own project")
	}
	judgeOK, err := CanViewProject(ctx, projectID, types.HackathonViewer{PersonID: judgeID})
	if err != nil {
		t.Fatalf("CanViewProject judge: %v", err)
	}
	if !judgeOK {
		t.Fatalf("judge cannot see private project")
	}
	adminOK, err := CanViewProject(ctx, projectID, types.HackathonViewer{Admin: true})
	if err != nil {
		t.Fatalf("CanViewProject admin: %v", err)
	}
	if !adminOK {
		t.Fatalf("admin cannot see private project")
	}

	if err := SubmitProject(ctx, projectID); err != nil {
		t.Fatalf("SubmitProject: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if _, err := ctx.DB.Exec(context.Background(), `
		UPDATE competitions
		SET submissions_close_at = $2
		WHERE id = $1
	`, competitionID, past); err != nil {
		t.Fatalf("close submissions: %v", err)
	}
	publicOK, err = CanViewProject(ctx, projectID, types.HackathonViewer{})
	if err != nil {
		t.Fatalf("CanViewProject public after close: %v", err)
	}
	if !publicOK {
		t.Fatalf("public viewer cannot see submitted project after deadline")
	}
}

func TestHackathonJudgingSetup(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)

	competitionID := createSmokeCompetition(t, ctx, CompetitionInput{
		Slug:  "judging-" + postgresSmokeSuffix(),
		Title: "Judging Hackathon",
	})
	if err := ReplaceCompetitionScheduleSegments(ctx, competitionID, []CompetitionScheduleSegmentInput{
		{
			SegmentType:            JudgeTypeExpo,
			Title:                  "Expo judging",
			DefaultDurationMinutes: 60,
		},
	}); err != nil {
		t.Fatalf("create timeline judge segment: %v", err)
	}

	events, err := ListJudgeEvents(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListJudgeEvents: %v", err)
	}
	if len(events) != 1 || events[0].Name != "Expo judging" || events[0].PlaybookType != JudgeTypeExpo {
		t.Fatalf("judge events mismatch: %+v", events)
	}
	if events[0].ScheduleSegmentID == "" {
		t.Fatalf("expected judge event to be backed by a schedule segment: %+v", events[0])
	}
	eventID := events[0].ID
	if err := UpdateJudgeEventRankLimit(ctx, competitionID, eventID, 6); err != nil {
		t.Fatalf("UpdateJudgeEventRankLimit: %v", err)
	}
	events, err = ListJudgeEvents(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListJudgeEvents after rank update: %v", err)
	}
	if len(events) != 1 || events[0].RankLimit != 6 {
		t.Fatalf("rank limit after update = %+v, want 6", events)
	}

	judgeID := insertSmokePerson(t, ctx, "judge")
	if err := AddCompetitionJudge(ctx, competitionID, judgeID, JudgeTypeFinals); err != nil {
		t.Fatalf("AddCompetitionJudge: %v", err)
	}
	judges, err := ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListCompetitionJudges: %v", err)
	}
	if len(judges) != 1 || judges[0].PersonID != judgeID || judges[0].JudgeType != JudgeTypeFinals {
		t.Fatalf("judges mismatch: %+v", judges)
	}
	ownerID := insertSmokePerson(t, ctx, "score-owner")
	projectID := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID:     competitionID,
		CreatedByPersonID: ownerID,
		Slug:              "score-project-" + postgresSmokeSuffix(),
		Title:             "Scored Project",
	})
	secondProjectID := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID:     competitionID,
		CreatedByPersonID: ownerID,
		Slug:              "score-project-two-" + postgresSmokeSuffix(),
		Title:             "Second Scored Project",
	})
	rank := 1
	scorecard, err := UpsertScorecard(ctx, ScorecardInput{
		JudgeEventID:  eventID,
		ProjectID:     projectID,
		JudgePersonID: judgeID,
		Rank:          &rank,
		Comments:      "strong demo",
	})
	if err != nil {
		t.Fatalf("UpsertScorecard: %v", err)
	}
	if scorecard.ID == "" || scorecard.SubmittedAt == nil || scorecard.Rank == nil || *scorecard.Rank != rank {
		t.Fatalf("scorecard mismatch: %+v", scorecard)
	}
	rank = 2
	scorecard, err = UpsertScorecard(ctx, ScorecardInput{
		JudgeEventID:  eventID,
		ProjectID:     projectID,
		JudgePersonID: judgeID,
		Rank:          &rank,
		NoShow:        true,
		Comments:      "updated",
	})
	if err != nil {
		t.Fatalf("UpsertScorecard update: %v", err)
	}
	if scorecard.Rank == nil || *scorecard.Rank != rank || !scorecard.NoShow || scorecard.Comments != "updated" {
		t.Fatalf("updated scorecard mismatch: %+v", scorecard)
	}
	if err := ReplaceScorecardRankings(ctx, ScorecardRankingsInput{
		JudgeEventID:  eventID,
		JudgePersonID: judgeID,
		Rankings: []ScorecardRankingInput{
			{ProjectID: projectID, Rank: 1},
			{ProjectID: secondProjectID, Rank: 2},
		},
	}); err != nil {
		t.Fatalf("ReplaceScorecardRankings: %v", err)
	}
	scorecards, err := ListScorecardsForJudge(ctx, competitionID, judgeID)
	if err != nil {
		t.Fatalf("ListScorecardsForJudge: %v", err)
	}
	if len(scorecards) != 2 {
		t.Fatalf("scorecards mismatch: %+v", scorecards)
	}
	competitionScorecards, err := ListScorecardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListScorecardsForCompetition: %v", err)
	}
	if len(competitionScorecards) != 2 {
		t.Fatalf("competition scorecards mismatch: %+v", competitionScorecards)
	}
	otherCompetitionID := createSmokeCompetition(t, ctx, CompetitionInput{
		Slug:  "score-other-" + postgresSmokeSuffix(),
		Title: "Other Scoring Hackathon",
	})
	otherProjectID := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID:     otherCompetitionID,
		CreatedByPersonID: ownerID,
		Slug:              "score-other-project-" + postgresSmokeSuffix(),
		Title:             "Other Project",
	})
	if _, err := UpsertScorecard(ctx, ScorecardInput{
		JudgeEventID:  eventID,
		ProjectID:     otherProjectID,
		JudgePersonID: judgeID,
	}); err == nil {
		t.Fatalf("UpsertScorecard with event/project mismatch succeeded")
	}
	if err := RemoveCompetitionJudge(ctx, competitionID, judgeID, JudgeTypeFinals); err != nil {
		t.Fatalf("RemoveCompetitionJudge: %v", err)
	}
	judges, err = ListCompetitionJudges(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListCompetitionJudges after remove: %v", err)
	}
	if len(judges) != 0 {
		t.Fatalf("judges after remove = %+v, want empty", judges)
	}
}

func TestHackathonAwardsAndPrizes(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)

	maxAwardees := 1
	poolPercentage := 12.5
	competitionID := createSmokeCompetition(t, ctx, CompetitionInput{
		Slug:  "awards-" + postgresSmokeSuffix(),
		Title: "Awards Hackathon",
	})
	ownerID := insertSmokePerson(t, ctx, "award-owner")
	projectID := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID:     competitionID,
		CreatedByPersonID: ownerID,
		Slug:              "award-project-" + postgresSmokeSuffix(),
		Title:             "Winning Project",
	})
	secondProjectID := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID:     competitionID,
		CreatedByPersonID: ownerID,
		Slug:              "award-second-project-" + postgresSmokeSuffix(),
		Title:             "Second Project",
	})
	awardID, err := CreateAward(ctx, AwardInput{
		CompetitionID: competitionID,
		Title:         "Best Overall",
		Description:   "Top project",
		MaxAwardees:   &maxAwardees,
		OptInRequired: true,
		Status:        AwardStatusAvailable,
	})
	if err != nil {
		t.Fatalf("CreateAward: %v", err)
	}
	if awardID == "" {
		t.Fatalf("CreateAward returned empty id")
	}
	awards, err := ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListAwardsForCompetition: %v", err)
	}
	if len(awards) != 1 || awards[0].ID != awardID || awards[0].MaxAwardees == nil || *awards[0].MaxAwardees != maxAwardees || !awards[0].OptInRequired {
		t.Fatalf("awards mismatch: %+v", awards)
	}
	if err := SetProjectAwardOptIns(ctx, projectID, []string{awardID, awardID, ""}); err != nil {
		t.Fatalf("SetProjectAwardOptIns: %v", err)
	}
	projectOptIns, err := ListProjectAwardOptInsForProject(ctx, projectID)
	if err != nil {
		t.Fatalf("ListProjectAwardOptInsForProject: %v", err)
	}
	if len(projectOptIns) != 1 || projectOptIns[0].ProjectID != projectID || projectOptIns[0].AwardID != awardID || projectOptIns[0].AwardTitle != "Best Overall" {
		t.Fatalf("project opt-ins mismatch: %+v", projectOptIns)
	}
	if err := UpdateAward(ctx, awardID, AwardInput{
		CompetitionID: competitionID,
		Title:         "Best Overall Updated",
		Description:   "Updated top project",
		MaxAwardees:   &maxAwardees,
		OptInRequired: false,
		Status:        AwardStatusAvailable,
	}); err != nil {
		t.Fatalf("UpdateAward: %v", err)
	}
	awards, err = ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListAwardsForCompetition after update: %v", err)
	}
	if len(awards) != 1 || awards[0].ID != awardID || awards[0].Title != "Best Overall Updated" || awards[0].Description != "Updated top project" || awards[0].OptInRequired {
		t.Fatalf("awards after update mismatch: %+v", awards)
	}
	projectOptIns, err = ListProjectAwardOptInsForProject(ctx, projectID)
	if err != nil {
		t.Fatalf("ListProjectAwardOptInsForProject after opt-in disabled: %v", err)
	}
	if len(projectOptIns) != 0 {
		t.Fatalf("project opt-ins after opt-in disabled = %+v, want empty", projectOptIns)
	}
	if err := UpdateAward(ctx, awardID, AwardInput{
		CompetitionID: competitionID,
		Title:         "Best Overall",
		Description:   "Top project",
		MaxAwardees:   &maxAwardees,
		OptInRequired: true,
		Status:        AwardStatusAvailable,
	}); err != nil {
		t.Fatalf("UpdateAward restore opt-in: %v", err)
	}
	if err := SetProjectAwardOptIns(ctx, projectID, []string{awardID}); err != nil {
		t.Fatalf("SetProjectAwardOptIns after opt-in restore: %v", err)
	}
	competitionOptIns, err := ListProjectAwardOptInsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListProjectAwardOptInsForCompetition: %v", err)
	}
	if len(competitionOptIns) != 1 || competitionOptIns[0].ProjectID != projectID || competitionOptIns[0].AwardID != awardID {
		t.Fatalf("competition opt-ins mismatch: %+v", competitionOptIns)
	}
	generalAwardID, err := CreateAward(ctx, AwardInput{
		CompetitionID: competitionID,
		Title:         "General Award",
		OptInRequired: false,
		Status:        AwardStatusAvailable,
	})
	if err != nil {
		t.Fatalf("CreateAward general: %v", err)
	}
	if err := SetProjectAwardOptIns(ctx, projectID, []string{generalAwardID}); err == nil {
		t.Fatalf("SetProjectAwardOptIns accepted non-opt-in award")
	}
	projectOptIns, err = ListProjectAwardOptInsForProject(ctx, projectID)
	if err != nil {
		t.Fatalf("ListProjectAwardOptInsForProject after invalid: %v", err)
	}
	if len(projectOptIns) != 1 || projectOptIns[0].AwardID != awardID {
		t.Fatalf("project opt-ins after invalid = %+v, want original opt-in", projectOptIns)
	}

	prizeID, err := CreatePrize(ctx, PrizeInput{
		AwardID:        awardID,
		PrizeType:      PrizeTypePooled,
		Title:          "Prize pool",
		Description:    "Shared sats pool",
		ValueText:      "1,000,000 sats",
		PoolPercentage: &poolPercentage,
		PoolURL:        "https://example.com/pool",
		Status:         PrizeStatusNeedsFunds,
		Comments:       "confirm sponsor",
	})
	if err != nil {
		t.Fatalf("CreatePrize: %v", err)
	}
	if prizeID == "" {
		t.Fatalf("CreatePrize returned empty id")
	}
	prizes, err := ListPrizesForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListPrizesForCompetition: %v", err)
	}
	if len(prizes) != 1 || prizes[0].ID != prizeID || prizes[0].AwardID != awardID || prizes[0].PoolPercentage == nil || *prizes[0].PoolPercentage != poolPercentage {
		t.Fatalf("prizes mismatch: %+v", prizes)
	}

	if err := AssignProjectAward(ctx, awardID, projectID); err != nil {
		t.Fatalf("AssignProjectAward: %v", err)
	}
	projectAwards, err := ListProjectAwardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListProjectAwardsForCompetition: %v", err)
	}
	if len(projectAwards) != 1 || projectAwards[0].AwardID != awardID || projectAwards[0].ProjectID != projectID || projectAwards[0].ProjectTitle != "Winning Project" {
		t.Fatalf("project awards mismatch: %+v", projectAwards)
	}
	awards, err = ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListAwardsForCompetition after assign: %v", err)
	}
	if awards[0].Status != AwardStatusAwarded {
		t.Fatalf("award status after assign = %q, want %q", awards[0].Status, AwardStatusAwarded)
	}
	if err := AssignProjectAward(ctx, awardID, secondProjectID); err == nil {
		t.Fatalf("AssignProjectAward over max succeeded")
	}
	if err := RemoveProjectAward(ctx, awardID, projectID); err != nil {
		t.Fatalf("RemoveProjectAward: %v", err)
	}
	projectAwards, err = ListProjectAwardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListProjectAwardsForCompetition after remove: %v", err)
	}
	if len(projectAwards) != 0 {
		t.Fatalf("project awards after remove = %+v, want empty", projectAwards)
	}
	awards, err = ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListAwardsForCompetition after remove: %v", err)
	}
	if awards[0].Status != AwardStatusUnawarded {
		t.Fatalf("award status after remove = %q, want %q", awards[0].Status, AwardStatusUnawarded)
	}
	if err := ArchiveAward(ctx, competitionID, awardID); err != nil {
		t.Fatalf("ArchiveAward: %v", err)
	}
	awards, err = ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListAwardsForCompetition after archive: %v", err)
	}
	for _, award := range awards {
		if award != nil && award.ID == awardID {
			t.Fatalf("archived award still listed: %+v", awards)
		}
	}
	archivedAwards, err := ListArchivedAwardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListArchivedAwardsForCompetition: %v", err)
	}
	if len(archivedAwards) != 1 || archivedAwards[0].ID != awardID || archivedAwards[0].ArchivedAt == nil {
		t.Fatalf("archived awards mismatch: %+v", archivedAwards)
	}
	prizes, err = ListPrizesForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListPrizesForCompetition after archive: %v", err)
	}
	for _, prize := range prizes {
		if prize != nil && prize.AwardID == awardID {
			t.Fatalf("archived award prize still listed: %+v", prizes)
		}
	}
	competitionOptIns, err = ListProjectAwardOptInsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListProjectAwardOptInsForCompetition after archive: %v", err)
	}
	for _, optIn := range competitionOptIns {
		if optIn != nil && optIn.AwardID == awardID {
			t.Fatalf("archived award opt-in still listed: %+v", competitionOptIns)
		}
	}
	if err := RestoreAward(ctx, competitionID, awardID); err != nil {
		t.Fatalf("RestoreAward: %v", err)
	}
	awards, err = ListAwardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListAwardsForCompetition after restore: %v", err)
	}
	var restored bool
	for _, award := range awards {
		if award != nil && award.ID == awardID && award.ArchivedAt == nil {
			restored = true
		}
	}
	if !restored {
		t.Fatalf("restored award not listed: %+v", awards)
	}
	if err := ArchiveAward(ctx, competitionID, awardID); err != nil {
		t.Fatalf("ArchiveAward before delete: %v", err)
	}
	if err := DeleteArchivedAward(ctx, competitionID, awardID); err != nil {
		t.Fatalf("DeleteArchivedAward: %v", err)
	}
	archivedAwards, err = ListArchivedAwardsForCompetition(ctx, competitionID)
	if err != nil {
		t.Fatalf("ListArchivedAwardsForCompetition after delete: %v", err)
	}
	for _, award := range archivedAwards {
		if award != nil && award.ID == awardID {
			t.Fatalf("deleted award still archived: %+v", archivedAwards)
		}
	}
	if err := DeleteArchivedAward(ctx, competitionID, generalAwardID); err == nil {
		t.Fatalf("DeleteArchivedAward deleted active award")
	}
}

func requireHackathonSchema(t *testing.T, ctx *config.AppContext) {
	t.Helper()
	var schemaReady bool
	if err := ctx.DB.QueryRow(context.Background(), `SELECT to_regclass('public.competitions') IS NOT NULL`).Scan(&schemaReady); err != nil {
		t.Fatalf("check hackathon schema: %v", err)
	}
	if !schemaReady {
		t.Fatalf("hackathon schema is not migrated; run db/migrations/018_hackathon_schema.sql")
	}
}

func createSmokeCompetition(t *testing.T, ctx *config.AppContext, in CompetitionInput) string {
	t.Helper()
	if strings.TrimSpace(in.ConferenceID) == "" {
		in.ConferenceID, _ = insertSmokeConference(t, ctx)
	}
	id, err := CreateCompetition(ctx, in)
	if err != nil {
		t.Fatalf("CreateCompetition: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM competitions WHERE id::text = $1`, id)
	})
	return id
}

func createSmokeProject(t *testing.T, ctx *config.AppContext, in ProjectInput) string {
	t.Helper()
	id, err := CreateProject(ctx, in)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return id
}

func insertSmokePerson(t *testing.T, ctx *config.AppContext, label string) string {
	t.Helper()
	suffix := postgresSmokeSuffix()
	var id string
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO people (name, email)
		VALUES ($1, $2)
		RETURNING id::text
	`, "Hackathon "+label+" "+suffix, label+"-"+suffix+"@example.test").Scan(&id)
	if err != nil {
		t.Fatalf("insert person: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id::text = $1`, id)
	})
	return id
}

func smokePersonEmail(t *testing.T, ctx *config.AppContext, personID string) string {
	t.Helper()
	var email string
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT email::text
		FROM people
		WHERE id::text = $1
	`, personID).Scan(&email); err != nil {
		t.Fatalf("lookup person email %s: %v", personID, err)
	}
	return email
}

func speakerIDsContain(speakers []*types.Speaker, speakerID string) bool {
	for _, speaker := range speakers {
		if speaker != nil && speaker.ID == speakerID {
			return true
		}
	}
	return false
}
