package devpostimport

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type ImportOptions struct {
	Visibility   string
	CreatePeople bool
	ImportJudges bool
}

type ImportSummary struct {
	CompetitionID  string
	Projects       int
	PeopleCreated  int
	MembersSkipped int
	Awards         int
	Assignments    int
	Judges         int
	JudgesSkipped  int
}

func Import(ctx context.Context, tx pgx.Tx, manifest *Manifest, opts ImportOptions) (*ImportSummary, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	if manifest.Version != ManifestVersion {
		return nil, fmt.Errorf("unsupported manifest version %d", manifest.Version)
	}
	if opts.Visibility == "" {
		opts.Visibility = "hidden"
	}
	if opts.Visibility != "hidden" && opts.Visibility != "public" {
		return nil, fmt.Errorf("visibility must be hidden or public")
	}
	var conferenceID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM conferences WHERE lower(tag) = lower($1)`, manifest.ConferenceTag).Scan(&conferenceID); err != nil {
		return nil, fmt.Errorf("find conference %q: %w", manifest.ConferenceTag, err)
	}
	var competitionID string
	err := tx.QueryRow(ctx, `
		INSERT INTO competitions (
			conference_id, title, description, description_format, visibility,
			lifecycle_override, public_gallery_enabled, submissions_open_at,
			submissions_close_at, public_gallery_at, hacking_starts_at, hacking_ends_at
		) VALUES ($1::uuid, $2, $3, 'html', $4, 'closed', true, $5, $6, $6, $5, $6)
		ON CONFLICT (conference_id) DO UPDATE SET
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			description_format = EXCLUDED.description_format,
			visibility = EXCLUDED.visibility,
			public_gallery_enabled = true,
			submissions_open_at = coalesce(EXCLUDED.submissions_open_at, competitions.submissions_open_at),
			submissions_close_at = coalesce(EXCLUDED.submissions_close_at, competitions.submissions_close_at),
			public_gallery_at = coalesce(EXCLUDED.public_gallery_at, competitions.public_gallery_at),
			hacking_starts_at = coalesce(EXCLUDED.hacking_starts_at, competitions.hacking_starts_at),
			hacking_ends_at = coalesce(EXCLUDED.hacking_ends_at, competitions.hacking_ends_at)
		RETURNING id::text
	`, conferenceID, manifest.Competition.Title, manifest.Competition.Description, opts.Visibility, manifest.Competition.Start, manifest.Competition.End).Scan(&competitionID)
	if err != nil {
		return nil, fmt.Errorf("upsert competition: %w", err)
	}
	summary := &ImportSummary{CompetitionID: competitionID}
	projectIDs := make(map[string]string, len(manifest.Projects))
	for _, project := range manifest.Projects {
		memberIDs := make([]string, 0, len(project.Members))
		for _, member := range project.Members {
			personID, created, err := resolvePerson(ctx, tx, member, opts.CreatePeople)
			if err != nil {
				return nil, fmt.Errorf("project %s member %s: %w", project.Slug, member.Name, err)
			}
			if created {
				summary.PeopleCreated++
			}
			if personID != "" {
				memberIDs = append(memberIDs, personID)
			} else {
				summary.MembersSkipped++
			}
		}
		var creator any
		if len(memberIDs) > 0 {
			creator = memberIDs[0]
		}
		imageURLs := make([]string, 0, len(project.Images))
		for _, image := range project.Images {
			imageURLs = appendUnique(imageURLs, firstNonEmpty(image.AVIFURL, image.OriginalURL, image.SourceURL))
		}
		var imageURL string
		if len(imageURLs) > 0 {
			imageURL = imageURLs[0]
		}
		var projectID string
		err := tx.QueryRow(ctx, `
			INSERT INTO projects (
				competition_id, created_by_person_id, slug, title, short_description,
				description, description_format, image_url, image_urls, github_url,
				demo_url, video_url, slides_url, docs_url, status, tags, submitted_at
			) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, 'html', $7, $8, $9, $10, $11, $12, $13, 'submitted', $14, coalesce($15, now()))
			ON CONFLICT (competition_id, slug) DO UPDATE SET
				created_by_person_id = coalesce(EXCLUDED.created_by_person_id, projects.created_by_person_id),
				title = EXCLUDED.title,
				short_description = EXCLUDED.short_description,
				description = EXCLUDED.description,
				description_format = 'html',
				image_url = EXCLUDED.image_url,
				image_urls = EXCLUDED.image_urls,
				github_url = EXCLUDED.github_url,
				demo_url = EXCLUDED.demo_url,
				video_url = EXCLUDED.video_url,
				slides_url = EXCLUDED.slides_url,
				docs_url = EXCLUDED.docs_url,
				status = 'submitted',
				tags = EXCLUDED.tags,
				submitted_at = coalesce(projects.submitted_at, EXCLUDED.submitted_at)
			RETURNING id::text
		`, competitionID, creator, project.Slug, project.Title, project.ShortDescription,
			project.DescriptionHTML, imageURL, imageURLs, project.GitHubURL, project.DemoURL,
			project.VideoURL, project.SlidesURL, project.DocsURL, project.Tags, manifest.Competition.End).Scan(&projectID)
		if err != nil {
			return nil, fmt.Errorf("upsert project %s: %w", project.Slug, err)
		}
		projectIDs[project.Slug] = projectID
		var existingOwnerID string
		err = tx.QueryRow(ctx, `SELECT person_id::text FROM project_members WHERE project_id = $1::uuid AND role = 'owner' LIMIT 1`, projectID).Scan(&existingOwnerID)
		if err != nil && err != pgx.ErrNoRows {
			return nil, fmt.Errorf("load project owner for %s: %w", project.Slug, err)
		}
		for index, personID := range memberIDs {
			role := "member"
			if index == 0 && (existingOwnerID == "" || existingOwnerID == personID) {
				role = "owner"
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO project_members (project_id, person_id, role)
				VALUES ($1::uuid, $2::uuid, $3)
				ON CONFLICT (project_id, person_id) DO UPDATE SET role = EXCLUDED.role
			`, projectID, personID, role); err != nil {
				return nil, fmt.Errorf("add project member %s: %w", personID, err)
			}
		}
		summary.Projects++
	}

	for _, award := range manifest.Awards {
		awardID, err := upsertAward(ctx, tx, competitionID, award)
		if err != nil {
			return nil, err
		}
		summary.Awards++
		if award.SatoshiValue > 0 {
			if err := upsertSatoshiPrize(ctx, tx, awardID, award); err != nil {
				return nil, err
			}
		}
		for _, projectSlug := range award.Winners {
			projectID := projectIDs[projectSlug]
			if projectID == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO project_awards (project_id, award_id)
				VALUES ($1::uuid, $2::uuid)
				ON CONFLICT DO NOTHING
			`, projectID, awardID); err != nil {
				return nil, fmt.Errorf("assign %s to %s: %w", award.Title, projectSlug, err)
			}
			summary.Assignments++
		}
	}

	if opts.ImportJudges {
		for _, judge := range manifest.Judges {
			personID, created, err := resolvePerson(ctx, tx, judge, opts.CreatePeople)
			if err != nil {
				return nil, err
			}
			if created {
				summary.PeopleCreated++
			}
			if personID == "" {
				summary.JudgesSkipped++
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO competition_judges (competition_id, person_id, judge_type)
				VALUES ($1::uuid, $2::uuid, 'expo')
				ON CONFLICT DO NOTHING
			`, competitionID, personID); err != nil {
				return nil, fmt.Errorf("add judge %s: %w", judge.Name, err)
			}
			summary.Judges++
		}
	}
	return summary, nil
}

func resolvePerson(ctx context.Context, tx pgx.Tx, person Person, create bool) (string, bool, error) {
	name := strings.TrimSpace(person.Name)
	if name == "" {
		return "", false, nil
	}
	if person.DevpostURL != "" {
		var id string
		err := tx.QueryRow(ctx, `SELECT id::text FROM people WHERE website_url = $1 ORDER BY created_at LIMIT 1`, person.DevpostURL).Scan(&id)
		if err == nil {
			return id, false, nil
		}
		if err != pgx.ErrNoRows {
			return "", false, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM people WHERE lower(name) = lower($1) ORDER BY created_at LIMIT 2`, name)
	if err != nil {
		return "", false, err
	}
	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", false, err
		}
		matches = append(matches, id)
	}
	rows.Close()
	if len(matches) == 1 {
		return matches[0], false, nil
	}
	if !create {
		return "", false, nil
	}
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO people (name, company, website_url)
		VALUES ($1, $2, $3)
		RETURNING id::text
	`, name, person.Company, person.DevpostURL).Scan(&id); err != nil {
		return "", false, err
	}
	return id, true, nil
}

func upsertAward(ctx context.Context, tx pgx.Tx, competitionID string, award Award) (string, error) {
	var awardID string
	err := tx.QueryRow(ctx, `
		SELECT id::text FROM awards
		WHERE competition_id = $1::uuid AND lower(title) = lower($2) AND archived_at IS NULL
		ORDER BY created_at LIMIT 1
	`, competitionID, award.Title).Scan(&awardID)
	status := "available"
	if len(award.Winners) > 0 {
		status = "awarded"
	}
	var maxAwardees any
	if award.WinnerCount > 0 {
		maxAwardees = award.WinnerCount
	}
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, `
			INSERT INTO awards (competition_id, title, description, max_awardees, status)
			VALUES ($1::uuid, $2, $3, $4, $5)
			RETURNING id::text
		`, competitionID, award.Title, award.Description, maxAwardees, status).Scan(&awardID)
	} else if err == nil {
		_, err = tx.Exec(ctx, `
			UPDATE awards SET description = $3, max_awardees = $4, status = $5
			WHERE id = $1::uuid AND competition_id = $2::uuid
		`, awardID, competitionID, award.Description, maxAwardees, status)
	}
	if err != nil {
		return "", fmt.Errorf("upsert award %s: %w", award.Title, err)
	}
	return awardID, nil
}

func upsertSatoshiPrize(ctx context.Context, tx pgx.Tx, awardID string, award Award) error {
	title := award.Title + " prize"
	value := strconv.FormatInt(award.SatoshiValue, 10)
	var prizeID string
	err := tx.QueryRow(ctx, `SELECT id::text FROM prizes WHERE award_id = $1::uuid AND lower(title) = lower($2) ORDER BY created_at LIMIT 1`, awardID, title).Scan(&prizeID)
	if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `
			INSERT INTO prizes (award_id, prize_type, title, description, value_text, status, comments)
			VALUES ($1::uuid, 'sats', $2, $3, $4, $5, 'Imported from Devpost')
		`, awardID, title, award.Description, value, map[bool]string{true: "awarded", false: "available"}[len(award.Winners) > 0])
	} else if err == nil {
		_, err = tx.Exec(ctx, `UPDATE prizes SET prize_type = 'sats', description = $2, value_text = $3 WHERE id = $1::uuid`, prizeID, award.Description, value)
	}
	if err != nil {
		return fmt.Errorf("upsert prize for %s: %w", award.Title, err)
	}
	return nil
}

func baseNameFromURL(raw string) string {
	parsed, _ := url.Parse(raw)
	return path.Base(parsed.Path)
}
