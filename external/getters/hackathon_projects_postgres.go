package getters

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func createProjectPostgres(ctx *config.AppContext, in ProjectInput) (string, error) {
	return createProjectWithAwardOptInsPostgres(ctx, in, nil)
}

func createProjectWithAwardOptInsPostgres(ctx *config.AppContext, in ProjectInput, awardIDs []string) (string, error) {
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

	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return "", fmt.Errorf("begin create project: %w", err)
	}
	defer tx.Rollback(dbctx)

	var id string
	err = tx.QueryRow(dbctx, `
		INSERT INTO projects (
			competition_id, created_by_person_id, slug, title, short_description,
			description, description_format, image_url, image_urls, github_url, demo_url,
			video_url, slides_url, docs_url, project_number, tags
		) VALUES (
			$1::uuid, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16
		)
		RETURNING id::text
	`, in.CompetitionID, in.CreatedByPersonID, in.Slug, in.Title, in.ShortDescription,
		in.Description, in.DescriptionFormat, in.ImageURL, in.ImageURLs, in.GitHubURL, in.DemoURL, in.VideoURL, in.SlidesURL,
		in.DocsURL, in.ProjectNumber, in.Tags).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert project %q: %w", in.Slug, err)
	}
	if in.CreatedByPersonID != "" {
		if err := addProjectMemberTx(ctx, tx, id, in.CreatedByPersonID, ProjectMemberRoleOwner); err != nil {
			return "", err
		}
	}
	if awardIDs != nil {
		if err := replaceProjectAwardOptInsTx(dbctx, tx, id, in.CompetitionID, awardIDs); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(dbctx); err != nil {
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
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE projects
		SET slug = $2,
			title = $3,
			short_description = $4,
			description = $5,
			description_format = $6,
			image_url = $7,
			image_urls = $8,
			github_url = $9,
			demo_url = $10,
			video_url = $11,
			slides_url = $12,
			docs_url = $13,
			project_number = $14,
			tags = $15
		WHERE id = $1
	`, projectID, in.Slug, in.Title, in.ShortDescription, in.Description,
		in.DescriptionFormat, in.ImageURL, in.ImageURLs, in.GitHubURL, in.DemoURL, in.VideoURL, in.SlidesURL, in.DocsURL,
		in.ProjectNumber, in.Tags)
	if err != nil {
		return fmt.Errorf("update project %s: %w", projectID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("project %s not found", projectID)
	}
	return nil
}

func deleteProjectPostgres(ctx *config.AppContext, competitionID, projectID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	projectID = strings.TrimSpace(projectID)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM projects
		WHERE id = $1 AND competition_id = $2
	`, projectID, competitionID)
	if err != nil {
		return fmt.Errorf("delete project %s: %w", projectID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("project %s not found in competition %s", projectID, competitionID)
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
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
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

func updateProjectAdminFieldsPostgres(ctx *config.AppContext, competitionID, projectID, status string, projectNumber *int) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	projectID = strings.TrimSpace(projectID)
	status = normalizeProjectStatus(status)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE projects
		SET status = $3,
			project_number = $4,
			submitted_at = CASE WHEN $3 = $5 THEN coalesce(submitted_at, now()) ELSE submitted_at END
		WHERE id = $1 AND competition_id = $2
	`, projectID, competitionID, status, projectNumber, ProjectStatusSubmitted)
	if err != nil {
		return fmt.Errorf("update project admin fields %s: %w", projectID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("project %s not found in competition %s", projectID, competitionID)
	}
	return nil
}

func assignMissingProjectNumbersPostgres(ctx *config.AppContext, competitionID string) (int, error) {
	if ctx == nil || ctx.DB == nil {
		return 0, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return 0, fmt.Errorf("competition id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		WITH targets AS (
			SELECT id,
				row_number() OVER (
					ORDER BY coalesce(submitted_at, created_at), created_at, title, id
				) AS rn
			FROM projects
			WHERE competition_id = $1
				AND project_number IS NULL
				AND status IN ($2, $3)
		), used_numbers AS (
			SELECT DISTINCT project_number
			FROM projects
			WHERE competition_id = $1
				AND project_number IS NOT NULL
		), available_numbers AS (
			SELECT n AS project_number,
				row_number() OVER (ORDER BY n) AS rn
			FROM generate_series(1, (
				SELECT coalesce(max(project_number), 0) + (SELECT count(*) FROM targets)
				FROM projects
				WHERE competition_id = $1
			)) AS n
			WHERE NOT EXISTS (
				SELECT 1
				FROM used_numbers
				WHERE used_numbers.project_number = n
			)
		), numbered AS (
			SELECT targets.id, available_numbers.project_number
			FROM targets
			JOIN available_numbers ON available_numbers.rn = targets.rn
		)
		UPDATE projects
		SET project_number = numbered.project_number
		FROM numbered
		WHERE projects.id = numbered.id
	`, competitionID, ProjectStatusSubmitted, ProjectStatusAdvanced)
	if err != nil {
		return 0, fmt.Errorf("assign missing project numbers for competition %s: %w", competitionID, err)
	}
	return int(commandTag.RowsAffected()), nil
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

func listHackathonParticipantProjectsByEmailPostgres(ctx *config.AppContext, email string) ([]*HackathonParticipantProject, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT projects.id::text, projects.competition_id::text,
			coalesce(projects.created_by_person_id::text, ''), projects.slug,
			projects.title, projects.short_description, projects.description,
			projects.description_format, projects.image_url, projects.image_urls,
			projects.github_url, projects.demo_url, projects.video_url,
			projects.slides_url, projects.docs_url, projects.project_number,
			projects.status, projects.tags, projects.submitted_at,
			projects.created_at, projects.updated_at,
			competition.title,
			conf.id::text, conf.tag, conf.active, conf.publication_status,
			conf.description, conf.edition_type, conf.og_flavor, conf.emoji,
			conf.tagline, conf.date_desc, conf.start_date, conf.end_date,
			conf.timezone, conf.location,
			membership.role,
			(SELECT count(*) FROM project_members team WHERE team.project_id = projects.id)
		FROM project_members membership
		JOIN people person ON person.id = membership.person_id
		JOIN projects ON projects.id = membership.project_id
		JOIN competitions competition ON competition.id = projects.competition_id
		JOIN conferences conf ON conf.id = competition.conference_id
		WHERE lower(coalesce(person.email::text, '')) = lower($1)
		ORDER BY conf.start_date DESC NULLS LAST, projects.updated_at DESC,
			projects.title, projects.id,
			CASE WHEN membership.role = 'owner' THEN 0 ELSE 1 END
	`, email)
	if err != nil {
		return nil, fmt.Errorf("query hackathon participant projects for %s: %w", email, err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	var out []*HackathonParticipantProject
	for rows.Next() {
		var project types.HackathonProject
		var projectNumber sql.NullInt64
		var submittedAt pgtype.Timestamptz
		var conf types.Conf
		var confStart, confEnd pgtype.Timestamptz
		var competitionTitle, memberRole string
		var teamSize int
		if err := rows.Scan(
			&project.ID, &project.CompetitionID, &project.CreatedByPersonID,
			&project.Slug, &project.Title, &project.ShortDescription,
			&project.Description, &project.DescriptionFormat, &project.ImageURL,
			&project.ImageURLs, &project.GitHubURL, &project.DemoURL,
			&project.VideoURL, &project.SlidesURL, &project.DocsURL,
			&projectNumber, &project.Status, &project.Tags, &submittedAt,
			&project.CreatedAt, &project.UpdatedAt,
			&competitionTitle,
			&conf.Ref, &conf.Tag, &conf.Active, &conf.PublicationStatus,
			&conf.Desc, &conf.EditionType, &conf.OGFlavor, &conf.Emoji,
			&conf.Tagline, &conf.DateDesc, &confStart, &confEnd,
			&conf.Timezone, &conf.Location,
			&memberRole, &teamSize,
		); err != nil {
			return nil, fmt.Errorf("scan hackathon participant project: %w", err)
		}
		if seen[project.ID] {
			continue
		}
		seen[project.ID] = true
		if projectNumber.Valid {
			n := int(projectNumber.Int64)
			project.ProjectNumber = &n
		}
		project.Status = normalizeProjectStatus(project.Status)
		project.SubmittedAt = pgTimePtr(submittedAt)
		if conf.Timezone != "" {
			if loc, err := time.LoadLocation(conf.Timezone); err == nil {
				conf.TZ = loc
			}
		}
		if confStart.Valid {
			conf.StartDate = confStart.Time.In(conf.Loc())
		}
		if confEnd.Valid {
			conf.EndDate = confEnd.Time.In(conf.Loc())
		}
		out = append(out, &HackathonParticipantProject{
			Project:          &project,
			Conf:             &conf,
			CompetitionTitle: competitionTitle,
			MemberRole:       normalizeProjectMemberRole(memberRole),
			TeamSize:         teamSize,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hackathon participant projects for %s: %w", email, err)
	}
	return out, nil
}

func hasHackathonParticipantProjectsByEmailPostgres(ctx *config.AppContext, email string) (bool, error) {
	if ctx == nil || ctx.DB == nil {
		return false, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return false, nil
	}
	var exists bool
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT EXISTS (
			SELECT 1
			FROM project_members membership
			JOIN people person ON person.id = membership.person_id
			WHERE lower(coalesce(person.email::text, '')) = lower($1)
		)
	`, email).Scan(&exists); err != nil {
		return false, fmt.Errorf("check hackathon participant projects for %s: %w", email, err)
	}
	return exists, nil
}

func listTableProjectsForCompetitionPostgres(ctx *config.AppContext, competitionID string) ([]*types.HackathonProject, error) {
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("competition id is required")
	}
	return queryProjectsPostgres(ctx, "table projects for competition", `
		WHERE projects.competition_id::text = $1
			AND projects.status IN ($2, $3)
	`, []any{competitionID, ProjectStatusSubmitted, ProjectStatusAdvanced})
}

func queryProjectsPostgres(ctx *config.AppContext, label, whereSQL string, args []any) ([]*types.HackathonProject, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT projects.id::text, projects.competition_id::text,
			coalesce(projects.created_by_person_id::text, ''), projects.slug,
			projects.title, projects.short_description, projects.description,
			projects.description_format, projects.image_url, projects.image_urls,
			projects.github_url, projects.demo_url, projects.video_url,
			projects.slides_url, projects.docs_url, projects.project_number,
			projects.status, projects.tags, projects.submitted_at,
			projects.created_at, projects.updated_at
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
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin add project member: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if err := addProjectMemberTx(ctx, tx, projectID, personID, role); err != nil {
		return err
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
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

	var competitionID string
	var maxTeamSize sql.NullInt64
	var memberCount int64
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		SELECT projects.competition_id::text, competitions.max_team_size,
			count(project_members.person_id)
		FROM projects
		JOIN competitions ON competitions.id = projects.competition_id
		LEFT JOIN project_members ON project_members.project_id = projects.id
		WHERE projects.id = $1
		GROUP BY projects.competition_id, competitions.max_team_size
	`, projectID).Scan(&competitionID, &maxTeamSize, &memberCount); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("project %s not found", projectID)
		}
		return fmt.Errorf("load project team size %s: %w", projectID, err)
	}

	var existingProjectTitle string
	membershipErr := tx.QueryRow(ctx.DatabaseContext(), `
		SELECT existing_project.title
		FROM project_members existing_membership
		JOIN projects existing_project ON existing_project.id = existing_membership.project_id
		WHERE existing_membership.person_id = $1::uuid
			AND existing_project.competition_id = $2::uuid
			AND existing_project.id <> $3::uuid
		ORDER BY existing_membership.created_at
		LIMIT 1
	`, personID, competitionID, projectID).Scan(&existingProjectTitle)
	if membershipErr == nil {
		return fmt.Errorf("a participant can only belong to one submission per hackathon; this person is already on %q", existingProjectTitle)
	}
	if membershipErr != pgx.ErrNoRows {
		return fmt.Errorf("check existing hackathon project membership: %w", membershipErr)
	}

	var alreadyMember bool
	if err := tx.QueryRow(ctx.DatabaseContext(), `
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

	_, err := tx.Exec(ctx.DatabaseContext(), `
		INSERT INTO project_members (project_id, person_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (project_id, person_id) DO UPDATE
		SET role = CASE
			WHEN project_members.role = $4 THEN project_members.role
			ELSE EXCLUDED.role
		END
	`, projectID, personID, role, ProjectMemberRoleOwner)
	if err != nil {
		return fmt.Errorf("insert project member %s/%s: %w", projectID, personID, err)
	}
	return nil
}

func removeProjectMemberPostgres(ctx *config.AppContext, projectID, personID string, allowSubmitted bool) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM project_members AS member
		USING projects
		WHERE member.project_id = projects.id
			AND member.project_id = $1
			AND member.person_id = $2
			AND member.role <> $3
			AND ($4 OR projects.status = $5)
	`, strings.TrimSpace(projectID), strings.TrimSpace(personID), ProjectMemberRoleOwner,
		allowSubmitted, ProjectStatusCreated)
	if err != nil {
		return fmt.Errorf("remove project member %s/%s: %w", projectID, personID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("team member cannot be removed in the project's current state")
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
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT project_members.project_id::text, project_members.person_id::text,
			coalesce(people.name, ''), coalesce(people.email::text, ''),
			coalesce(people.norm_photo_path, ''), project_members.role,
			project_members.created_at
		FROM project_members
		LEFT JOIN people ON people.id = project_members.person_id
		WHERE project_members.project_id::text = $1
		ORDER BY project_members.created_at, project_members.person_id::text
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project members %s: %w", projectID, err)
	}
	defer rows.Close()
	var out []*types.ProjectMember
	for rows.Next() {
		var member types.ProjectMember
		if err := rows.Scan(&member.ProjectID, &member.PersonID, &member.Name, &member.Email, &member.Photo, &member.Role, &member.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan project member %s: %w", projectID, err)
		}
		out = append(out, &member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project members %s: %w", projectID, err)
	}
	return out, nil
}

func listProjectMembersForCompetitionPostgres(ctx *config.AppContext, competitionID string) (map[string][]*types.ProjectMember, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("competition id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT project_members.project_id::text, project_members.person_id::text,
			coalesce(people.name, ''), '', coalesce(people.norm_photo_path, ''),
			project_members.role, project_members.created_at
		FROM project_members
		JOIN projects ON projects.id = project_members.project_id
		LEFT JOIN people ON people.id = project_members.person_id
		WHERE projects.competition_id::text = $1
		ORDER BY project_members.project_id, project_members.created_at,
			project_members.person_id::text
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("query project members for competition %s: %w", competitionID, err)
	}
	defer rows.Close()
	out := make(map[string][]*types.ProjectMember)
	for rows.Next() {
		var member types.ProjectMember
		if err := rows.Scan(&member.ProjectID, &member.PersonID, &member.Name, &member.Email, &member.Photo, &member.Role, &member.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan project members for competition %s: %w", competitionID, err)
		}
		out[member.ProjectID] = append(out[member.ProjectID], &member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project members for competition %s: %w", competitionID, err)
	}
	return out, nil
}

func getPersonIDByEmailPostgres(ctx *config.AppContext, email string) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return "", fmt.Errorf("email is required")
	}
	var personID string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text
		FROM people
		WHERE email = $1::citext
		ORDER BY created_at
		LIMIT 1
	`, email).Scan(&personID); err != nil {
		if err == pgx.ErrNoRows {
			return "", fmt.Errorf("person not found for %s", email)
		}
		return "", fmt.Errorf("lookup person by email %s: %w", email, err)
	}
	return personID, nil
}

func createProjectInvitePostgres(ctx *config.AppContext, projectID, email string, expiresAt *time.Time) (string, *types.ProjectInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return "", nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	if expiresAt == nil {
		defaultExpiresAt := time.Now().Add(ProjectInviteDefaultTTL)
		expiresAt = &defaultExpiresAt
	}
	token, tokenHash, err := newInviteToken()
	if err != nil {
		return "", nil, err
	}
	var invite types.ProjectInvite
	var acceptedAt, expiresAtValue pgtype.Timestamptz
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
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

func getProjectInviteByTokenPostgres(ctx *config.AppContext, token string) (*types.ProjectInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	tokenHash := hashInviteToken(token)
	if tokenHash == "" {
		return nil, fmt.Errorf("invite token is required")
	}
	return loadProjectInviteByTokenHash(ctx, ctx.DB, tokenHash)
}

type projectInviteRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadProjectInviteByTokenHash(ctx *config.AppContext, q projectInviteRow, tokenHash string) (*types.ProjectInvite, error) {
	var invite types.ProjectInvite
	var acceptedAt, expiresAt pgtype.Timestamptz
	err := q.QueryRow(ctx.DatabaseContext(), `
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
	return &invite, nil
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

	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, fmt.Errorf("begin accept invite: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	invite, err := loadProjectInviteByTokenHash(ctx, tx, tokenHash)
	if err != nil {
		return nil, err
	}
	if invite.Email != "" {
		var personEmail string
		if err := tx.QueryRow(ctx.DatabaseContext(), `
			SELECT coalesce(email::text, '')
			FROM people
			WHERE id::text = $1
		`, personID).Scan(&personEmail); err != nil {
			if err == pgx.ErrNoRows {
				return nil, fmt.Errorf("person %s not found", personID)
			}
			return nil, fmt.Errorf("load invite recipient %s: %w", personID, err)
		}
		if !strings.EqualFold(strings.TrimSpace(invite.Email), strings.TrimSpace(personEmail)) {
			return nil, fmt.Errorf("project invite is for %s", invite.Email)
		}
	}
	if err := addProjectMemberTx(ctx, tx, invite.ProjectID, personID, ProjectMemberRoleMember); err != nil {
		return nil, err
	}
	now := time.Now()
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE project_invites
		SET accepted_by_person_id = $2,
			accepted_at = $3
		WHERE id = $1
	`, invite.ID, personID, now); err != nil {
		return nil, fmt.Errorf("accept project invite %s: %w", invite.ID, err)
	}
	invite.AcceptedByPersonID = personID
	invite.AcceptedAt = &now
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, fmt.Errorf("commit accept invite: %w", err)
	}
	return invite, nil
}

func createCompetitionJudgeInvitePostgres(ctx *config.AppContext, competitionID, email string, judgeTypes []string, expiresAt *time.Time) (string, *types.CompetitionJudgeInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return "", nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	email = strings.TrimSpace(email)
	if competitionID == "" {
		return "", nil, fmt.Errorf("competition id is required")
	}
	judgeTypes, err := normalizeJudgeInviteTypes(judgeTypes)
	if err != nil {
		return "", nil, err
	}
	if expiresAt == nil {
		defaultExpiresAt := time.Now().Add(ProjectInviteDefaultTTL)
		expiresAt = &defaultExpiresAt
	}
	token, tokenHash, err := newInviteToken()
	if err != nil {
		return "", nil, err
	}
	var invite types.CompetitionJudgeInvite
	var acceptedAt, expiresAtValue pgtype.Timestamptz
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO competition_judge_invites (competition_id, token_hash, email, judge_types, expires_at)
		VALUES ($1::uuid, $2, NULLIF($3, '')::citext, $4, $5)
		RETURNING id::text, competition_id::text,
			coalesce(email::text, ''), judge_types, coalesce(accepted_by_person_id::text, ''), accepted_at, expires_at, created_at
	`, competitionID, tokenHash, email, judgeTypes, expiresAt).Scan(
		&invite.ID,
		&invite.CompetitionID,
		&invite.Email,
		&invite.JudgeTypes,
		&invite.AcceptedByPersonID,
		&acceptedAt,
		&expiresAtValue,
		&invite.CreatedAt,
	)
	if err != nil {
		return "", nil, fmt.Errorf("insert competition judge invite: %w", err)
	}
	invite.AcceptedAt = pgTimePtr(acceptedAt)
	invite.ExpiresAt = pgTimePtr(expiresAtValue)
	return token, &invite, nil
}

func acceptCompetitionJudgeInvitePostgres(ctx *config.AppContext, token, personID string) (*types.CompetitionJudgeInvite, error) {
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

	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin accept judge invite: %w", err)
	}
	defer tx.Rollback(dbctx)

	var invite types.CompetitionJudgeInvite
	var acceptedAt, expiresAt pgtype.Timestamptz
	err = tx.QueryRow(dbctx, `
		SELECT id::text, competition_id::text, coalesce(email::text, ''),
			judge_types, coalesce(accepted_by_person_id::text, ''), accepted_at, expires_at, created_at
		FROM competition_judge_invites
		WHERE token_hash = $1
		FOR UPDATE
	`, tokenHash).Scan(
		&invite.ID,
		&invite.CompetitionID,
		&invite.Email,
		&invite.JudgeTypes,
		&invite.AcceptedByPersonID,
		&acceptedAt,
		&expiresAt,
		&invite.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("judge invite not found")
		}
		return nil, fmt.Errorf("load judge invite: %w", err)
	}
	invite.AcceptedAt = pgTimePtr(acceptedAt)
	invite.ExpiresAt = pgTimePtr(expiresAt)
	if invite.AcceptedAt != nil && invite.AcceptedByPersonID != personID {
		return nil, fmt.Errorf("judge invite already accepted by another person")
	}
	if invite.AcceptedAt == nil && invite.ExpiresAt != nil && time.Now().After(*invite.ExpiresAt) {
		return nil, fmt.Errorf("judge invite expired")
	}
	if strings.TrimSpace(invite.Email) != "" {
		var personEmail string
		if err := tx.QueryRow(dbctx, `
			SELECT coalesce(email::text, '') FROM people WHERE id = $1::uuid
		`, personID).Scan(&personEmail); err != nil {
			return nil, fmt.Errorf("load judge invite recipient: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(invite.Email), strings.TrimSpace(personEmail)) {
			return nil, fmt.Errorf("judge invite is for %s", invite.Email)
		}
	}
	if _, err := tx.Exec(dbctx, `
		WITH judge_order AS (
			SELECT COALESCE(
				(
					SELECT NULLIF(min(display_order), 0)
					FROM competition_judges
					WHERE competition_id = $1::uuid AND person_id = $2::uuid
				),
				(
					SELECT COALESCE(max(display_order), 0) + 1
					FROM competition_judges
					WHERE competition_id = $1::uuid
				)
			) AS display_order
		),
		roles AS (
			SELECT unnest($3::text[]) AS judge_type
		)
		INSERT INTO competition_judges (competition_id, person_id, judge_type, display_order)
		SELECT $1::uuid, $2::uuid, roles.judge_type, judge_order.display_order
		FROM roles
		CROSS JOIN judge_order
		ON CONFLICT (competition_id, person_id, judge_type) DO NOTHING
	`, invite.CompetitionID, personID, invite.JudgeTypes); err != nil {
		return nil, fmt.Errorf("accept judge invite %s add judge: %w", invite.ID, err)
	}
	if invite.AcceptedAt != nil {
		if err := tx.Commit(dbctx); err != nil {
			return nil, fmt.Errorf("commit repeated judge invite acceptance: %w", err)
		}
		return &invite, nil
	}
	now := time.Now()
	if _, err := tx.Exec(dbctx, `
		UPDATE competition_judge_invites
		SET accepted_by_person_id = $2,
			accepted_at = $3
		WHERE id = $1
	`, invite.ID, personID, now); err != nil {
		return nil, fmt.Errorf("accept judge invite %s: %w", invite.ID, err)
	}
	invite.AcceptedByPersonID = personID
	invite.AcceptedAt = &now
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit accept judge invite: %w", err)
	}
	return &invite, nil
}

func normalizeJudgeInviteTypes(judgeTypes []string) ([]string, error) {
	normalized := make([]string, 0, len(judgeTypes))
	seen := make(map[string]bool, len(judgeTypes))
	for _, judgeType := range judgeTypes {
		judgeType = strings.TrimSpace(strings.ToLower(judgeType))
		switch judgeType {
		case JudgeTypeExpo, JudgeTypeFinals:
		default:
			return nil, fmt.Errorf("judge invite type must be expo or finals")
		}
		if !seen[judgeType] {
			seen[judgeType] = true
			normalized = append(normalized, judgeType)
		}
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("choose at least one judge invite type")
	}
	return normalized, nil
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
	if viewer.Admin || viewer.Manager {
		return true, nil
	}
	if projectIsPublicPostgres(ctx, project) {
		return true, nil
	}
	viewer.PersonID = strings.TrimSpace(viewer.PersonID)
	if viewer.PersonID == "" {
		return false, nil
	}
	var allowed bool
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT EXISTS (
			SELECT 1 FROM project_members
			WHERE project_id = $1 AND person_id = $2
		) OR EXISTS (
			SELECT 1 FROM competition_judges
			WHERE competition_id = $3 AND person_id = $2
		) OR EXISTS (
			SELECT 1
			FROM award_judges
			JOIN awards ON awards.id = award_judges.award_id
			WHERE awards.competition_id = $3
				AND award_judges.person_id = $2
		)
	`, project.ID, viewer.PersonID, project.CompetitionID).Scan(&allowed); err != nil {
		return false, fmt.Errorf("check project visibility %s: %w", project.ID, err)
	}
	return allowed, nil
}
