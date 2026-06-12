package getters

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	CompetitionStatusDraft  = "draft"
	ProjectStatusCreated    = "created"
	ProjectStatusSubmitted  = "submitted"
	ProjectMemberRoleOwner  = "owner"
	ProjectMemberRoleMember = "member"
)

func createCompetitionPostgres(ctx *config.AppContext, in CompetitionInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	in = normalizeCompetitionInput(in)
	if in.Slug == "" {
		return "", fmt.Errorf("competition slug is required")
	}
	if in.Title == "" {
		return "", fmt.Errorf("competition title is required")
	}
	if in.Status == "" {
		in.Status = CompetitionStatusDraft
	}

	var id string
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO competitions (
			conference_id, slug, title, description, status, max_team_size,
			submissions_open_at, submissions_close_at, public_gallery_at
		) VALUES (
			NULLIF($1, '')::uuid, $2, $3, $4, $5, $6, $7, $8, $9
		)
		RETURNING id::text
	`, in.ConferenceID, in.Slug, in.Title, in.Description, in.Status, in.MaxTeamSize,
		in.SubmissionsOpenAt, in.SubmissionsCloseAt, in.PublicGalleryAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert competition %q: %w", in.Slug, err)
	}
	return id, nil
}

func getCompetitionByIDPostgres(ctx *config.AppContext, competitionID string) (*types.HackathonCompetition, error) {
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("competition id is required")
	}
	competitions, err := queryCompetitionsPostgres(ctx, "competition by id", "WHERE id::text = $1", []any{competitionID})
	if err != nil || len(competitions) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("competition %s not found", competitionID)
	}
	return competitions[0], nil
}

func getCompetitionBySlugPostgres(ctx *config.AppContext, slug string) (*types.HackathonCompetition, error) {
	slug = normalizeSlug(slug)
	if slug == "" {
		return nil, fmt.Errorf("competition slug is required")
	}
	competitions, err := queryCompetitionsPostgres(ctx, "competition by slug", "WHERE slug = $1", []any{slug})
	if err != nil || len(competitions) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("competition %s not found", slug)
	}
	return competitions[0], nil
}

func listCompetitionsPostgres(ctx *config.AppContext) ([]*types.HackathonCompetition, error) {
	return queryCompetitionsPostgres(ctx, "competitions", "", nil)
}

func queryCompetitionsPostgres(ctx *config.AppContext, label, whereSQL string, args []any) ([]*types.HackathonCompetition, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT id::text, coalesce(conference_id::text, ''), slug, title, description,
			status, max_team_size, submissions_open_at, submissions_close_at,
			public_gallery_at, hacking_starts_at, hacking_ends_at, judges_meeting_at,
			expo_starts_at, expo_ends_at, expo_judging_starts_at, expo_judging_ends_at,
			finals_starts_at, finals_ends_at, finals_judging_starts_at,
			finals_judging_ends_at, awards_ceremony_at, created_at, updated_at
		FROM competitions
		`+whereSQL+`
		ORDER BY created_at DESC, title
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer rows.Close()

	var out []*types.HackathonCompetition
	for rows.Next() {
		competition, err := scanCompetition(rows)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		out = append(out, competition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return out, nil
}

func createProjectPostgres(ctx *config.AppContext, in ProjectInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	in = normalizeProjectInput(in)
	if in.CompetitionID == "" {
		return "", fmt.Errorf("project competition id is required")
	}
	if in.Slug == "" {
		return "", fmt.Errorf("project slug is required")
	}
	if in.Title == "" {
		return "", fmt.Errorf("project title is required")
	}

	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return "", fmt.Errorf("begin create project: %w", err)
	}
	defer tx.Rollback(context.Background())

	var id string
	err = tx.QueryRow(context.Background(), `
		INSERT INTO projects (
			competition_id, created_by_person_id, slug, title, short_description,
			description, github_url, demo_url, video_url, slides_url, docs_url,
			project_number, tags
		) VALUES (
			$1::uuid, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13
		)
		RETURNING id::text
	`, in.CompetitionID, in.CreatedByPersonID, in.Slug, in.Title, in.ShortDescription,
		in.Description, in.GitHubURL, in.DemoURL, in.VideoURL, in.SlidesURL,
		in.DocsURL, in.ProjectNumber, in.Tags).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert project %q: %w", in.Slug, err)
	}
	if in.CreatedByPersonID != "" {
		if err := addProjectMemberTx(ctx, tx, id, in.CreatedByPersonID, ProjectMemberRoleOwner); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(context.Background()); err != nil {
		return "", fmt.Errorf("commit create project: %w", err)
	}
	return id, nil
}

func updateProjectPostgres(ctx *config.AppContext, projectID string, in ProjectInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	projectID = strings.TrimSpace(projectID)
	in = normalizeProjectInput(in)
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	if in.Slug == "" {
		return fmt.Errorf("project slug is required")
	}
	if in.Title == "" {
		return fmt.Errorf("project title is required")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE projects
		SET slug = $2,
			title = $3,
			short_description = $4,
			description = $5,
			github_url = $6,
			demo_url = $7,
			video_url = $8,
			slides_url = $9,
			docs_url = $10,
			project_number = $11,
			tags = $12
		WHERE id = $1
	`, projectID, in.Slug, in.Title, in.ShortDescription, in.Description,
		in.GitHubURL, in.DemoURL, in.VideoURL, in.SlidesURL, in.DocsURL,
		in.ProjectNumber, in.Tags)
	if err != nil {
		return fmt.Errorf("update project %s: %w", projectID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("project %s not found", projectID)
	}
	return nil
}

func submitProjectPostgres(ctx *config.AppContext, projectID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE projects
		SET status = $2,
			submitted_at = coalesce(submitted_at, now())
		WHERE id = $1
	`, projectID, ProjectStatusSubmitted)
	if err != nil {
		return fmt.Errorf("submit project %s: %w", projectID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("project %s not found", projectID)
	}
	return nil
}

func getProjectByIDPostgres(ctx *config.AppContext, projectID string) (*types.HackathonProject, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("project id is required")
	}
	projects, err := queryProjectsPostgres(ctx, "project by id", "WHERE projects.id::text = $1", []any{projectID})
	if err != nil || len(projects) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("project %s not found", projectID)
	}
	return projects[0], nil
}

func listProjectsForCompetitionPostgres(ctx *config.AppContext, competitionID string, viewer types.HackathonViewer) ([]*types.HackathonProject, error) {
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("competition id is required")
	}
	projects, err := queryProjectsPostgres(ctx, "projects for competition", "WHERE projects.competition_id::text = $1", []any{competitionID})
	if err != nil {
		return nil, err
	}
	out := make([]*types.HackathonProject, 0, len(projects))
	for _, project := range projects {
		ok, err := canViewProjectLoadedPostgres(ctx, project, viewer)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, project)
		}
	}
	return out, nil
}

func queryProjectsPostgres(ctx *config.AppContext, label, whereSQL string, args []any) ([]*types.HackathonProject, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT projects.id::text, projects.competition_id::text,
			coalesce(projects.created_by_person_id::text, ''), projects.slug,
			projects.title, projects.short_description, projects.description,
			projects.github_url, projects.demo_url, projects.video_url,
			projects.slides_url, projects.docs_url, projects.project_number,
			projects.status, projects.tags, projects.submitted_at, projects.shipped_at,
			projects.published_at, projects.created_at, projects.updated_at
		FROM projects
		`+whereSQL+`
		ORDER BY projects.project_number NULLS LAST, projects.created_at, projects.title
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer rows.Close()

	var out []*types.HackathonProject
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		out = append(out, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return out, nil
}

func addProjectMemberPostgres(ctx *config.AppContext, projectID, personID, role string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("begin add project member: %w", err)
	}
	defer tx.Rollback(context.Background())
	if err := addProjectMemberTx(ctx, tx, projectID, personID, role); err != nil {
		return err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return fmt.Errorf("commit add project member: %w", err)
	}
	return nil
}

func addProjectMemberTx(ctx *config.AppContext, tx pgx.Tx, projectID, personID, role string) error {
	projectID = strings.TrimSpace(projectID)
	personID = strings.TrimSpace(personID)
	role = normalizeProjectMemberRole(role)
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	if personID == "" {
		return fmt.Errorf("person id is required")
	}

	var maxTeamSize sql.NullInt64
	var memberCount int64
	if err := tx.QueryRow(context.Background(), `
		SELECT competitions.max_team_size, count(project_members.person_id)
		FROM projects
		JOIN competitions ON competitions.id = projects.competition_id
		LEFT JOIN project_members ON project_members.project_id = projects.id
		WHERE projects.id = $1
		GROUP BY competitions.max_team_size
	`, projectID).Scan(&maxTeamSize, &memberCount); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("project %s not found", projectID)
		}
		return fmt.Errorf("load project team size %s: %w", projectID, err)
	}

	var alreadyMember bool
	if err := tx.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM project_members
			WHERE project_id = $1 AND person_id = $2
		)
	`, projectID, personID).Scan(&alreadyMember); err != nil {
		return fmt.Errorf("check project member %s/%s: %w", projectID, personID, err)
	}
	if maxTeamSize.Valid && !alreadyMember && memberCount >= maxTeamSize.Int64 {
		return fmt.Errorf("project %s is at max team size %d", projectID, maxTeamSize.Int64)
	}

	_, err := tx.Exec(context.Background(), `
		INSERT INTO project_members (project_id, person_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (project_id, person_id) DO UPDATE SET role = EXCLUDED.role
	`, projectID, personID, role)
	if err != nil {
		return fmt.Errorf("insert project member %s/%s: %w", projectID, personID, err)
	}
	return nil
}

func removeProjectMemberPostgres(ctx *config.AppContext, projectID, personID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		DELETE FROM project_members
		WHERE project_id = $1 AND person_id = $2
	`, strings.TrimSpace(projectID), strings.TrimSpace(personID))
	if err != nil {
		return fmt.Errorf("remove project member %s/%s: %w", projectID, personID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("project member %s/%s not found", projectID, personID)
	}
	return nil
}

func listProjectMembersPostgres(ctx *config.AppContext, projectID string) ([]*types.ProjectMember, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("project id is required")
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT project_id::text, person_id::text, role, created_at
		FROM project_members
		WHERE project_id::text = $1
		ORDER BY created_at, person_id::text
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project members %s: %w", projectID, err)
	}
	defer rows.Close()
	var out []*types.ProjectMember
	for rows.Next() {
		var member types.ProjectMember
		if err := rows.Scan(&member.ProjectID, &member.PersonID, &member.Role, &member.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan project member %s: %w", projectID, err)
		}
		out = append(out, &member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project members %s: %w", projectID, err)
	}
	return out, nil
}

func createProjectInvitePostgres(ctx *config.AppContext, projectID, email string, expiresAt *time.Time) (string, *types.ProjectInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return "", nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	token, tokenHash, err := newInviteToken()
	if err != nil {
		return "", nil, err
	}
	var invite types.ProjectInvite
	var acceptedAt, expiresAtValue pgtype.Timestamptz
	err = ctx.DB.QueryRow(context.Background(), `
		INSERT INTO project_invites (project_id, token_hash, email, expires_at)
		VALUES ($1, $2, NULLIF($3, '')::citext, $4)
		RETURNING id::text, project_id::text, coalesce(email::text, ''),
			coalesce(accepted_by_person_id::text, ''), accepted_at, expires_at, created_at
	`, strings.TrimSpace(projectID), tokenHash, strings.TrimSpace(email), expiresAt).Scan(
		&invite.ID,
		&invite.ProjectID,
		&invite.Email,
		&invite.AcceptedByPersonID,
		&acceptedAt,
		&expiresAtValue,
		&invite.CreatedAt,
	)
	if err != nil {
		return "", nil, fmt.Errorf("insert project invite: %w", err)
	}
	invite.AcceptedAt = pgTimePtr(acceptedAt)
	invite.ExpiresAt = pgTimePtr(expiresAtValue)
	return token, &invite, nil
}

func acceptProjectInvitePostgres(ctx *config.AppContext, token, personID string) (*types.ProjectInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	tokenHash := hashInviteToken(token)
	if tokenHash == "" {
		return nil, fmt.Errorf("invite token is required")
	}
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil, fmt.Errorf("person id is required")
	}

	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return nil, fmt.Errorf("begin accept invite: %w", err)
	}
	defer tx.Rollback(context.Background())

	var invite types.ProjectInvite
	var acceptedAt, expiresAt pgtype.Timestamptz
	err = tx.QueryRow(context.Background(), `
		SELECT id::text, project_id::text, coalesce(email::text, ''),
			coalesce(accepted_by_person_id::text, ''), accepted_at, expires_at, created_at
		FROM project_invites
		WHERE token_hash = $1
	`, tokenHash).Scan(
		&invite.ID,
		&invite.ProjectID,
		&invite.Email,
		&invite.AcceptedByPersonID,
		&acceptedAt,
		&expiresAt,
		&invite.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("project invite not found")
		}
		return nil, fmt.Errorf("load project invite: %w", err)
	}
	invite.AcceptedAt = pgTimePtr(acceptedAt)
	invite.ExpiresAt = pgTimePtr(expiresAt)
	if invite.AcceptedAt != nil {
		return nil, fmt.Errorf("project invite already accepted")
	}
	if invite.ExpiresAt != nil && time.Now().After(*invite.ExpiresAt) {
		return nil, fmt.Errorf("project invite expired")
	}
	if err := addProjectMemberTx(ctx, tx, invite.ProjectID, personID, ProjectMemberRoleMember); err != nil {
		return nil, err
	}
	now := time.Now()
	if _, err := tx.Exec(context.Background(), `
		UPDATE project_invites
		SET accepted_by_person_id = $2,
			accepted_at = $3
		WHERE id = $1
	`, invite.ID, personID, now); err != nil {
		return nil, fmt.Errorf("accept project invite %s: %w", invite.ID, err)
	}
	invite.AcceptedByPersonID = personID
	invite.AcceptedAt = &now
	if err := tx.Commit(context.Background()); err != nil {
		return nil, fmt.Errorf("commit accept invite: %w", err)
	}
	return &invite, nil
}

func canViewProjectPostgres(ctx *config.AppContext, projectID string, viewer types.HackathonViewer) (bool, error) {
	project, err := getProjectByIDPostgres(ctx, projectID)
	if err != nil {
		return false, err
	}
	return canViewProjectLoadedPostgres(ctx, project, viewer)
}

func canViewProjectLoadedPostgres(ctx *config.AppContext, project *types.HackathonProject, viewer types.HackathonViewer) (bool, error) {
	if project == nil {
		return false, nil
	}
	if projectIsPublicPostgres(ctx, project) {
		return true, nil
	}
	if viewer.Admin || viewer.Coordinator {
		return true, nil
	}
	viewer.PersonID = strings.TrimSpace(viewer.PersonID)
	if viewer.PersonID == "" {
		return false, nil
	}
	var allowed bool
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM project_members
			WHERE project_id = $1 AND person_id = $2
		) OR EXISTS (
			SELECT 1 FROM competition_judges
			WHERE competition_id = $3 AND person_id = $2
		)
	`, project.ID, viewer.PersonID, project.CompetitionID).Scan(&allowed); err != nil {
		return false, fmt.Errorf("check project visibility %s: %w", project.ID, err)
	}
	return allowed, nil
}

func projectIsPublicPostgres(ctx *config.AppContext, project *types.HackathonProject) bool {
	if project == nil {
		return false
	}
	if project.PublishedAt != nil {
		return true
	}
	if project.Status == ProjectStatusCreated || project.Status == "withdrawn" || project.Status == "disqualified" {
		return false
	}
	var closeAt, galleryAt pgtype.Timestamptz
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT submissions_close_at, public_gallery_at
		FROM competitions
		WHERE id = $1
	`, project.CompetitionID).Scan(&closeAt, &galleryAt); err != nil {
		return false
	}
	now := time.Now()
	if galleryAt.Valid && !galleryAt.Time.After(now) {
		return true
	}
	return closeAt.Valid && !closeAt.Time.After(now)
}

func scanCompetition(rows pgx.Rows) (*types.HackathonCompetition, error) {
	var competition types.HackathonCompetition
	var maxTeamSize sql.NullInt64
	var submissionsOpenAt, submissionsCloseAt, publicGalleryAt pgtype.Timestamptz
	var hackingStartsAt, hackingEndsAt, judgesMeetingAt pgtype.Timestamptz
	var expoStartsAt, expoEndsAt, expoJudgingStartsAt, expoJudgingEndsAt pgtype.Timestamptz
	var finalsStartsAt, finalsEndsAt, finalsJudgingStartsAt, finalsJudgingEndsAt pgtype.Timestamptz
	var awardsCeremonyAt pgtype.Timestamptz
	if err := rows.Scan(
		&competition.ID,
		&competition.ConferenceID,
		&competition.Slug,
		&competition.Title,
		&competition.Description,
		&competition.Status,
		&maxTeamSize,
		&submissionsOpenAt,
		&submissionsCloseAt,
		&publicGalleryAt,
		&hackingStartsAt,
		&hackingEndsAt,
		&judgesMeetingAt,
		&expoStartsAt,
		&expoEndsAt,
		&expoJudgingStartsAt,
		&expoJudgingEndsAt,
		&finalsStartsAt,
		&finalsEndsAt,
		&finalsJudgingStartsAt,
		&finalsJudgingEndsAt,
		&awardsCeremonyAt,
		&competition.CreatedAt,
		&competition.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if maxTeamSize.Valid {
		n := int(maxTeamSize.Int64)
		competition.MaxTeamSize = &n
	}
	competition.SubmissionsOpenAt = pgTimePtr(submissionsOpenAt)
	competition.SubmissionsCloseAt = pgTimePtr(submissionsCloseAt)
	competition.PublicGalleryAt = pgTimePtr(publicGalleryAt)
	competition.HackingStartsAt = pgTimePtr(hackingStartsAt)
	competition.HackingEndsAt = pgTimePtr(hackingEndsAt)
	competition.JudgesMeetingAt = pgTimePtr(judgesMeetingAt)
	competition.ExpoStartsAt = pgTimePtr(expoStartsAt)
	competition.ExpoEndsAt = pgTimePtr(expoEndsAt)
	competition.ExpoJudgingStartsAt = pgTimePtr(expoJudgingStartsAt)
	competition.ExpoJudgingEndsAt = pgTimePtr(expoJudgingEndsAt)
	competition.FinalsStartsAt = pgTimePtr(finalsStartsAt)
	competition.FinalsEndsAt = pgTimePtr(finalsEndsAt)
	competition.FinalsJudgingStartsAt = pgTimePtr(finalsJudgingStartsAt)
	competition.FinalsJudgingEndsAt = pgTimePtr(finalsJudgingEndsAt)
	competition.AwardsCeremonyAt = pgTimePtr(awardsCeremonyAt)
	return &competition, nil
}

func scanProject(rows pgx.Rows) (*types.HackathonProject, error) {
	var project types.HackathonProject
	var projectNumber sql.NullInt64
	var submittedAt, shippedAt, publishedAt pgtype.Timestamptz
	if err := rows.Scan(
		&project.ID,
		&project.CompetitionID,
		&project.CreatedByPersonID,
		&project.Slug,
		&project.Title,
		&project.ShortDescription,
		&project.Description,
		&project.GitHubURL,
		&project.DemoURL,
		&project.VideoURL,
		&project.SlidesURL,
		&project.DocsURL,
		&projectNumber,
		&project.Status,
		&project.Tags,
		&submittedAt,
		&shippedAt,
		&publishedAt,
		&project.CreatedAt,
		&project.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if projectNumber.Valid {
		n := int(projectNumber.Int64)
		project.ProjectNumber = &n
	}
	project.SubmittedAt = pgTimePtr(submittedAt)
	project.ShippedAt = pgTimePtr(shippedAt)
	project.PublishedAt = pgTimePtr(publishedAt)
	return &project, nil
}

func pgTimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func normalizeCompetitionInput(in CompetitionInput) CompetitionInput {
	in.ConferenceID = strings.TrimSpace(in.ConferenceID)
	in.Slug = normalizeSlug(in.Slug)
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.Status = strings.TrimSpace(in.Status)
	return in
}

func normalizeProjectInput(in ProjectInput) ProjectInput {
	in.CompetitionID = strings.TrimSpace(in.CompetitionID)
	in.CreatedByPersonID = strings.TrimSpace(in.CreatedByPersonID)
	in.Slug = normalizeSlug(in.Slug)
	in.Title = strings.TrimSpace(in.Title)
	in.ShortDescription = strings.TrimSpace(in.ShortDescription)
	in.Description = strings.TrimSpace(in.Description)
	in.GitHubURL = strings.TrimSpace(in.GitHubURL)
	in.DemoURL = strings.TrimSpace(in.DemoURL)
	in.VideoURL = strings.TrimSpace(in.VideoURL)
	in.SlidesURL = strings.TrimSpace(in.SlidesURL)
	in.DocsURL = strings.TrimSpace(in.DocsURL)
	tags := make([]string, 0, len(in.Tags))
	for _, tag := range in.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	in.Tags = tags
	return in
}

func normalizeSlug(slug string) string {
	slug = strings.TrimSpace(strings.ToLower(slug))
	slug = strings.ReplaceAll(slug, " ", "-")
	return slug
}

func normalizeProjectMemberRole(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" {
		return ProjectMemberRoleMember
	}
	return role
}

func newInviteToken() (string, string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", fmt.Errorf("generate project invite token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	return token, hashInviteToken(token), nil
}

func hashInviteToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
