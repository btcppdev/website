package getters

import (
	"context"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestHackathonCompetitionCreateStandaloneAndConferenceLinked(t *testing.T) {
	ctx := postgresSmokeContext(t)
	requireHackathonSchema(t, ctx)

	standaloneID := createSmokeCompetition(t, ctx, CompetitionInput{
		Slug:  "standalone-" + postgresSmokeSuffix(),
		Title: "Standalone Hackathon",
	})

	standalone, err := GetCompetitionByID(ctx, standaloneID)
	if err != nil {
		t.Fatalf("GetCompetitionByID standalone: %v", err)
	}
	if standalone.ConferenceID != "" {
		t.Fatalf("standalone competition conference id = %q, want empty", standalone.ConferenceID)
	}

	confID, _ := insertSmokeConference(t, ctx)
	linkedID := createSmokeCompetition(t, ctx, CompetitionInput{
		ConferenceID: confID,
		Slug:         "linked-" + postgresSmokeSuffix(),
		Title:        "Conference Hackathon",
	})
	linked, err := GetCompetitionByID(ctx, linkedID)
	if err != nil {
		t.Fatalf("GetCompetitionByID linked: %v", err)
	}
	if linked.ConferenceID != confID {
		t.Fatalf("linked competition conference id = %q, want %q", linked.ConferenceID, confID)
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
		Status:             "open",
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
	if updated.Slug != updatedSlug || updated.Title != "Updated Hackathon" || updated.Description != "Updated description" || updated.Status != "open" {
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
	token, invite, err := CreateProjectInvite(ctx, projectID, "second@example.test", nil)
	if err != nil {
		t.Fatalf("CreateProjectInvite: %v", err)
	}
	if token == "" || invite == nil || invite.ProjectID != projectID {
		t.Fatalf("bad invite token/invite: token=%q invite=%+v", token, invite)
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

func requireHackathonSchema(t *testing.T, ctx *config.AppContext) {
	t.Helper()
	var schemaReady bool
	if err := ctx.DB.QueryRow(context.Background(), `SELECT to_regclass('public.competitions') IS NOT NULL`).Scan(&schemaReady); err != nil {
		t.Fatalf("check hackathon schema: %v", err)
	}
	if !schemaReady {
		t.Fatalf("hackathon schema is not migrated; run db/migrations/002_hackathon_schema.sql")
	}
}

func createSmokeCompetition(t *testing.T, ctx *config.AppContext, in CompetitionInput) string {
	t.Helper()
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
