package getters

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
)

func createAwardPostgres(ctx *config.AppContext, in AwardInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	in = normalizeAwardInput(in)
	if in.CompetitionID == "" {
		return "", fmt.Errorf("award competition id is required")
	}
	if in.Title == "" {
		return "", fmt.Errorf("award title is required")
	}
	var id string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO awards (
			competition_id, sponsored_by_org_id, award_type, title, description,
			judging_instructions,
			award_rank, max_awardees, opt_in_required, finalists_only, status
		)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id::text
	`, in.CompetitionID, in.SponsoredByOrgID, in.AwardType, in.Title, in.Description,
		in.JudgingInstructions, in.AwardRank, in.MaxAwardees, in.OptInRequired, in.FinalistsOnly, in.Status).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create award %q: %w", in.Title, err)
	}
	return id, nil
}

func updateAwardPostgres(ctx *config.AppContext, awardID string, in AwardInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	awardID = strings.TrimSpace(awardID)
	in = normalizeAwardInput(in)
	if awardID == "" {
		return fmt.Errorf("award id is required")
	}
	if in.CompetitionID == "" {
		return fmt.Errorf("award competition id is required")
	}
	if in.Title == "" {
		return fmt.Errorf("award title is required")
	}

	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin update award: %w", err)
	}
	defer tx.Rollback(dbctx)

	if in.FinalistsOnly {
		var hasNonFinalistAwardee bool
		if err := tx.QueryRow(dbctx, `
			SELECT EXISTS (
				SELECT 1
				FROM project_awards
				JOIN projects ON projects.id = project_awards.project_id
				WHERE project_awards.award_id::text = $1
					AND projects.status <> $2
			)
		`, awardID, ProjectStatusAdvanced).Scan(&hasNonFinalistAwardee); err != nil {
			return fmt.Errorf("check award %s finalists-only eligibility: %w", awardID, err)
		}
		if hasNonFinalistAwardee {
			return fmt.Errorf("award has non-finalist recipients; remove them before enabling finalists only")
		}
	}

	commandTag, err := tx.Exec(dbctx, `
		UPDATE awards
		SET sponsored_by_org_id = NULLIF($3, '')::uuid,
			award_type = $4,
			title = $5,
			description = $6,
			judging_instructions = $7,
			award_rank = $8,
			max_awardees = $9,
			opt_in_required = $10,
			finalists_only = $11,
			status = $12
		WHERE id::text = $1
			AND competition_id::text = $2
			AND archived_at IS NULL
	`, awardID, in.CompetitionID, in.SponsoredByOrgID, in.AwardType, in.Title, in.Description,
		in.JudgingInstructions, in.AwardRank, in.MaxAwardees, in.OptInRequired, in.FinalistsOnly, in.Status)
	if err != nil {
		return fmt.Errorf("update award %s: %w", awardID, err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("award %s not found", awardID)
	}
	if !in.OptInRequired {
		if _, err := tx.Exec(dbctx, `
			DELETE FROM project_award_opt_ins
			WHERE award_id::text = $1
		`, awardID); err != nil {
			return fmt.Errorf("clear award opt-ins %s: %w", awardID, err)
		}
	}
	if in.SponsoredByOrgID == "" {
		if _, err := tx.Exec(dbctx, `
				DELETE FROM award_judges
				WHERE award_id::text = $1
			`, awardID); err != nil {
			return fmt.Errorf("clear award judges %s: %w", awardID, err)
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit update award: %w", err)
	}
	return nil
}

func archiveAwardPostgres(ctx *config.AppContext, competitionID, awardID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	awardID = strings.TrimSpace(awardID)
	if competitionID == "" {
		return fmt.Errorf("award competition id is required")
	}
	if awardID == "" {
		return fmt.Errorf("award id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE awards
		SET archived_at = now()
		WHERE id::text = $1
			AND competition_id::text = $2
			AND archived_at IS NULL
	`, awardID, competitionID)
	if err != nil {
		return fmt.Errorf("archive award %s: %w", awardID, err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("award %s not found", awardID)
	}
	return nil
}

func restoreAwardPostgres(ctx *config.AppContext, competitionID, awardID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	awardID = strings.TrimSpace(awardID)
	if competitionID == "" {
		return fmt.Errorf("award competition id is required")
	}
	if awardID == "" {
		return fmt.Errorf("award id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE awards
		SET archived_at = NULL
		WHERE id::text = $1
			AND competition_id::text = $2
			AND archived_at IS NOT NULL
	`, awardID, competitionID)
	if err != nil {
		return fmt.Errorf("restore award %s: %w", awardID, err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("archived award %s not found", awardID)
	}
	return nil
}

func deleteArchivedAwardPostgres(ctx *config.AppContext, competitionID, awardID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	awardID = strings.TrimSpace(awardID)
	if competitionID == "" {
		return fmt.Errorf("award competition id is required")
	}
	if awardID == "" {
		return fmt.Errorf("award id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM awards
		WHERE id::text = $1
			AND competition_id::text = $2
			AND archived_at IS NOT NULL
	`, awardID, competitionID)
	if err != nil {
		return fmt.Errorf("delete archived award %s: %w", awardID, err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("archived award %s not found", awardID)
	}
	return nil
}

func listAwardsForCompetitionPostgres(ctx *config.AppContext, competitionID string) ([]*types.Award, error) {
	return listAwardsForCompetitionByArchiveStatePostgres(ctx, competitionID, false)
}

func listArchivedAwardsForCompetitionPostgres(ctx *config.AppContext, competitionID string) ([]*types.Award, error) {
	return listAwardsForCompetitionByArchiveStatePostgres(ctx, competitionID, true)
}

func listAwardsForCompetitionByArchiveStatePostgres(ctx *config.AppContext, competitionID string, archived bool) ([]*types.Award, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("award competition id is required")
	}
	archivePredicate := "archived_at IS NULL"
	orderBy := "CASE WHEN award_rank IS NOT NULL THEN 0 WHEN award_type = 'challenge' THEN 1 ELSE 2 END, award_rank NULLS LAST, title, id"
	if archived {
		archivePredicate = "archived_at IS NOT NULL"
		orderBy = "archived_at DESC, title, id"
	}
	query := fmt.Sprintf(`
				SELECT id::text, competition_id::text, coalesce(sponsored_by_org_id::text, ''),
					award_type, title, description,
					judging_instructions, award_rank, max_awardees, opt_in_required, finalists_only, status,
					created_at, updated_at, archived_at
		FROM awards
		WHERE competition_id::text = $1
			AND %s
		ORDER BY %s
	`, archivePredicate, orderBy)
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), query, competitionID)
	if err != nil {
		return nil, fmt.Errorf("list awards for competition %s: %w", competitionID, err)
	}
	defer rows.Close()
	var out []*types.Award
	for rows.Next() {
		award, err := scanAward(rows)
		if err != nil {
			return nil, fmt.Errorf("scan award: %w", err)
		}
		out = append(out, award)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate awards for competition %s: %w", competitionID, err)
	}
	return out, nil
}

func setProjectAwardOptInsPostgres(ctx *config.AppContext, projectID string, awardIDs []string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin project award opt-ins: %w", err)
	}
	defer tx.Rollback(dbctx)

	var competitionID string
	if err := tx.QueryRow(dbctx, `
		SELECT competition_id::text
		FROM projects
		WHERE id::text = $1
	`, projectID).Scan(&competitionID); err != nil {
		return fmt.Errorf("load project %s: %w", projectID, err)
	}
	if err := replaceProjectAwardOptInsTx(dbctx, tx, projectID, competitionID, awardIDs); err != nil {
		return err
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit project award opt-ins: %w", err)
	}
	return nil
}

func replaceProjectAwardOptInsTx(dbctx context.Context, tx pgx.Tx, projectID, competitionID string, awardIDs []string) error {
	awardIDs = normalizedUniqueStrings(awardIDs)
	var resultsFinalized bool
	if err := tx.QueryRow(dbctx, `
		SELECT results_finalized_at IS NOT NULL
		FROM competitions
		WHERE id::text = $1
	`, competitionID).Scan(&resultsFinalized); err != nil {
		return fmt.Errorf("load competition %s award opt-in state: %w", competitionID, err)
	}
	if resultsFinalized {
		return fmt.Errorf("hackathon results are finalized; award opt-ins can no longer be changed")
	}
	if _, err := tx.Exec(dbctx, `
		DELETE FROM project_award_opt_ins
		WHERE project_id::text = $1
	`, projectID); err != nil {
		return fmt.Errorf("clear project award opt-ins %s: %w", projectID, err)
	}
	for _, awardID := range awardIDs {
		commandTag, err := tx.Exec(dbctx, `
			INSERT INTO project_award_opt_ins (project_id, award_id)
			SELECT $1, awards.id
			FROM awards
			WHERE awards.id::text = $2
				AND awards.competition_id::text = $3
				AND awards.opt_in_required
				AND awards.status IN ($4, $5, $6)
				AND awards.archived_at IS NULL
		`, projectID, awardID, competitionID, AwardStatusAvailable, AwardStatusUnawarded, AwardStatusAwarded)
		if err != nil {
			return fmt.Errorf("set project award opt-in %s/%s: %w", projectID, awardID, err)
		}
		if commandTag.RowsAffected() != 1 {
			return fmt.Errorf("award opt-in %s is not available for this project", awardID)
		}
	}
	return nil
}

func listProjectAwardOptInsForProjectPostgres(ctx *config.AppContext, projectID string) ([]*types.ProjectAwardOptIn, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("project id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT opt_ins.project_id::text, opt_ins.award_id::text,
			projects.title, projects.project_number, awards.title, opt_ins.opted_in_at
		FROM project_award_opt_ins opt_ins
		JOIN projects ON projects.id = opt_ins.project_id
		JOIN awards ON awards.id = opt_ins.award_id
		WHERE opt_ins.project_id::text = $1
			AND awards.archived_at IS NULL
		ORDER BY awards.title, awards.id
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project award opt-ins for project %s: %w", projectID, err)
	}
	defer rows.Close()
	return scanProjectAwardOptIns(rows, "project "+projectID)
}

func listProjectAwardOptInsForCompetitionPostgres(ctx *config.AppContext, competitionID string) ([]*types.ProjectAwardOptIn, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("award opt-in competition id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT opt_ins.project_id::text, opt_ins.award_id::text,
			projects.title, projects.project_number, awards.title, opt_ins.opted_in_at
		FROM project_award_opt_ins opt_ins
		JOIN projects ON projects.id = opt_ins.project_id
		JOIN awards ON awards.id = opt_ins.award_id
		WHERE awards.competition_id::text = $1
			AND awards.archived_at IS NULL
		ORDER BY projects.project_number NULLS LAST, projects.title, awards.title, opt_ins.opted_in_at
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("list project award opt-ins for competition %s: %w", competitionID, err)
	}
	defer rows.Close()
	return scanProjectAwardOptIns(rows, "competition "+competitionID)
}

func createPrizePostgres(ctx *config.AppContext, in PrizeInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	var err error
	in, err = validatePrizeInput(in)
	if err != nil {
		return "", err
	}
	var id string
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO prizes (
			award_id, prize_type, title, description, value_text,
			pool_percentage, pool_url, status, comments
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text
	`, in.AwardID, in.PrizeType, in.Title, in.Description, in.ValueText, in.PoolPercentage, in.PoolURL, in.Status, in.Comments).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create prize %q: %w", in.Title, err)
	}
	return id, nil
}

func updatePrizePostgres(ctx *config.AppContext, competitionID, prizeID string, in PrizeInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	prizeID = strings.TrimSpace(prizeID)
	if competitionID == "" {
		return fmt.Errorf("prize competition id is required")
	}
	if prizeID == "" {
		return fmt.Errorf("prize id is required")
	}
	var err error
	in, err = validatePrizeInput(in)
	if err != nil {
		return err
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE prizes
		SET prize_type = $4,
			title = $5,
			description = $6,
			value_text = $7,
			pool_percentage = $8,
			pool_url = $9,
			status = $10,
			comments = $11
		FROM awards
		WHERE prizes.id::text = $1
			AND prizes.award_id = awards.id
			AND prizes.award_id::text = $2
			AND awards.competition_id::text = $3
			AND awards.archived_at IS NULL
	`, prizeID, in.AwardID, competitionID, in.PrizeType, in.Title, in.Description,
		in.ValueText, in.PoolPercentage, in.PoolURL, in.Status, in.Comments)
	if err != nil {
		return fmt.Errorf("update prize %s: %w", prizeID, err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("prize %s not found", prizeID)
	}
	return nil
}

func deletePrizePostgres(ctx *config.AppContext, competitionID, prizeID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	prizeID = strings.TrimSpace(prizeID)
	if competitionID == "" {
		return fmt.Errorf("prize competition id is required")
	}
	if prizeID == "" {
		return fmt.Errorf("prize id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM prizes
		USING awards
		WHERE prizes.id::text = $1
			AND prizes.award_id = awards.id
			AND awards.competition_id::text = $2
			AND awards.archived_at IS NULL
	`, prizeID, competitionID)
	if err != nil {
		return fmt.Errorf("delete prize %s: %w", prizeID, err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("prize %s not found", prizeID)
	}
	return nil
}

func listPrizesForCompetitionPostgres(ctx *config.AppContext, competitionID string) ([]*types.Prize, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("prize competition id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT prizes.id::text, prizes.award_id::text, prizes.prize_type, prizes.title,
			prizes.description, prizes.value_text, prizes.pool_percentage, prizes.pool_url,
			prizes.status, prizes.comments, prizes.created_at, prizes.updated_at
		FROM prizes
		JOIN awards ON awards.id = prizes.award_id
		WHERE awards.competition_id::text = $1
			AND awards.archived_at IS NULL
		ORDER BY awards.title, prizes.title, prizes.id
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("list prizes for competition %s: %w", competitionID, err)
	}
	defer rows.Close()
	var out []*types.Prize
	for rows.Next() {
		prize, err := scanPrize(rows)
		if err != nil {
			return nil, fmt.Errorf("scan prize: %w", err)
		}
		out = append(out, prize)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prizes for competition %s: %w", competitionID, err)
	}
	return out, nil
}

func assignProjectAwardPostgres(ctx *config.AppContext, awardID, projectID string) error {
	return assignProjectAwardForOrganizationPostgres(ctx, "", awardID, projectID)
}

func assignSponsorProjectAwardPostgres(ctx *config.AppContext, organizationID, awardID, projectID string) error {
	organizationID = strings.TrimSpace(organizationID)
	if organizationID == "" {
		return fmt.Errorf("sponsor organization id is required")
	}
	return assignProjectAwardForOrganizationPostgres(ctx, organizationID, awardID, projectID)
}

func assignProjectAwardForOrganizationPostgres(ctx *config.AppContext, organizationID, awardID, projectID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	organizationID = strings.TrimSpace(organizationID)
	awardID = strings.TrimSpace(awardID)
	projectID = strings.TrimSpace(projectID)
	if awardID == "" {
		return fmt.Errorf("award id is required")
	}
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin assign project award: %w", err)
	}
	defer tx.Rollback(dbctx)

	var awardCompetitionID string
	var maxAwardees sql.NullInt64
	var finalistsOnly, optInRequired bool
	if err := tx.QueryRow(dbctx, `
		SELECT competition_id::text, max_awardees, finalists_only, opt_in_required
		FROM awards
		WHERE id::text = $1
			AND ($2 = '' OR sponsored_by_org_id::text = $2)
			AND archived_at IS NULL
		FOR UPDATE
	`, awardID, organizationID).Scan(&awardCompetitionID, &maxAwardees, &finalistsOnly, &optInRequired); err != nil {
		return fmt.Errorf("load award %s: %w", awardID, err)
	}
	var resultsFinalized bool
	if err := tx.QueryRow(dbctx, `
		SELECT results_finalized_at IS NOT NULL
		FROM competitions
		WHERE id::text = $1
		FOR UPDATE
	`, awardCompetitionID).Scan(&resultsFinalized); err != nil {
		return fmt.Errorf("load award competition %s: %w", awardCompetitionID, err)
	}
	if resultsFinalized {
		return fmt.Errorf("hackathon results are finalized; reopen results before changing award recipients")
	}
	var projectCompetitionID string
	var projectStatus string
	if err := tx.QueryRow(dbctx, `
		SELECT competition_id::text, status
		FROM projects
		WHERE id::text = $1
	`, projectID).Scan(&projectCompetitionID, &projectStatus); err != nil {
		return fmt.Errorf("load project %s: %w", projectID, err)
	}
	if projectCompetitionID != awardCompetitionID {
		return fmt.Errorf("project and award must belong to the same competition")
	}
	if organizationID != "" && projectStatus != ProjectStatusSubmitted && projectStatus != ProjectStatusAdvanced {
		return fmt.Errorf("only submitted projects can receive a sponsor award")
	}
	if finalistsOnly && projectStatus != ProjectStatusAdvanced {
		return fmt.Errorf("this award can only be assigned to a finalist")
	}
	if organizationID != "" && optInRequired {
		var optedIn bool
		if err := tx.QueryRow(dbctx, `
			SELECT EXISTS (
				SELECT 1
				FROM project_award_opt_ins
				WHERE award_id::text = $1 AND project_id::text = $2
			)
		`, awardID, projectID).Scan(&optedIn); err != nil {
			return fmt.Errorf("check sponsor award project opt-in: %w", err)
		}
		if !optedIn {
			return fmt.Errorf("project did not opt in to this sponsor award")
		}
	}
	var alreadyAssigned bool
	if err := tx.QueryRow(dbctx, `
		SELECT EXISTS (
			SELECT 1 FROM project_awards
			WHERE award_id::text = $1 AND project_id::text = $2
		)
	`, awardID, projectID).Scan(&alreadyAssigned); err != nil {
		return fmt.Errorf("check project award %s/%s: %w", awardID, projectID, err)
	}
	if !alreadyAssigned && maxAwardees.Valid {
		var assignedCount int64
		if err := tx.QueryRow(dbctx, `
			SELECT count(*)
			FROM project_awards
			WHERE award_id::text = $1
		`, awardID).Scan(&assignedCount); err != nil {
			return fmt.Errorf("count awardees %s: %w", awardID, err)
		}
		if assignedCount >= maxAwardees.Int64 {
			return fmt.Errorf("award already has the maximum number of awardees")
		}
	}
	if !alreadyAssigned {
		if _, err := tx.Exec(dbctx, `
			INSERT INTO project_awards (project_id, award_id)
			VALUES ($1, $2)
		`, projectID, awardID); err != nil {
			return fmt.Errorf("assign project award %s/%s: %w", awardID, projectID, err)
		}
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE awards
		SET status = $2
		WHERE id::text = $1
	`, awardID, AwardStatusAwarded); err != nil {
		return fmt.Errorf("mark award awarded %s: %w", awardID, err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit assign project award: %w", err)
	}
	return nil
}

func removeProjectAwardPostgres(ctx *config.AppContext, awardID, projectID string) error {
	return removeProjectAwardForOrganizationPostgres(ctx, "", awardID, projectID)
}

func removeSponsorProjectAwardPostgres(ctx *config.AppContext, organizationID, awardID, projectID string) error {
	organizationID = strings.TrimSpace(organizationID)
	if organizationID == "" {
		return fmt.Errorf("sponsor organization id is required")
	}
	return removeProjectAwardForOrganizationPostgres(ctx, organizationID, awardID, projectID)
}

func removeProjectAwardForOrganizationPostgres(ctx *config.AppContext, organizationID, awardID, projectID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	organizationID = strings.TrimSpace(organizationID)
	awardID = strings.TrimSpace(awardID)
	projectID = strings.TrimSpace(projectID)
	if awardID == "" {
		return fmt.Errorf("award id is required")
	}
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin remove project award: %w", err)
	}
	defer tx.Rollback(dbctx)
	var lockedAwardID, awardCompetitionID string
	if err := tx.QueryRow(dbctx, `
		SELECT id::text, competition_id::text
		FROM awards
		WHERE id::text = $1
			AND ($2 = '' OR sponsored_by_org_id::text = $2)
			AND archived_at IS NULL
		FOR UPDATE
	`, awardID, organizationID).Scan(&lockedAwardID, &awardCompetitionID); err != nil {
		return fmt.Errorf("lock award %s: %w", awardID, err)
	}
	var resultsFinalized bool
	if err := tx.QueryRow(dbctx, `
		SELECT results_finalized_at IS NOT NULL
		FROM competitions
		WHERE id::text = $1
		FOR UPDATE
	`, awardCompetitionID).Scan(&resultsFinalized); err != nil {
		return fmt.Errorf("load award competition %s: %w", awardCompetitionID, err)
	}
	if resultsFinalized {
		return fmt.Errorf("hackathon results are finalized; reopen results before changing award recipients")
	}
	commandTag, err := tx.Exec(dbctx, `
		DELETE FROM project_awards
		WHERE award_id::text = $1 AND project_id::text = $2
	`, awardID, projectID)
	if err != nil {
		return fmt.Errorf("remove project award %s/%s: %w", awardID, projectID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("project award %s/%s not found", awardID, projectID)
	}
	var remaining int64
	if err := tx.QueryRow(dbctx, `
		SELECT count(*)
		FROM project_awards
		WHERE award_id::text = $1
	`, awardID).Scan(&remaining); err != nil {
		return fmt.Errorf("count remaining awardees %s: %w", awardID, err)
	}
	if remaining == 0 {
		if _, err := tx.Exec(dbctx, `
			UPDATE awards
			SET status = $2
			WHERE id::text = $1 AND status = $3
		`, awardID, AwardStatusUnawarded, AwardStatusAwarded); err != nil {
			return fmt.Errorf("mark award unawarded %s: %w", awardID, err)
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit remove project award: %w", err)
	}
	return nil
}

func listProjectAwardsForCompetitionPostgres(ctx *config.AppContext, competitionID string) ([]*types.ProjectAward, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("project award competition id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT project_awards.project_id::text, project_awards.award_id::text,
			projects.title, projects.project_number, project_awards.awarded_at
		FROM project_awards
		JOIN awards ON awards.id = project_awards.award_id
		JOIN projects ON projects.id = project_awards.project_id
		WHERE awards.competition_id::text = $1
			AND awards.archived_at IS NULL
		ORDER BY awards.title, projects.project_number NULLS LAST, projects.title, project_awards.awarded_at
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("list project awards for competition %s: %w", competitionID, err)
	}
	defer rows.Close()
	var out []*types.ProjectAward
	for rows.Next() {
		award, err := scanProjectAward(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project award: %w", err)
		}
		out = append(out, award)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project awards for competition %s: %w", competitionID, err)
	}
	return out, nil
}

func addAwardJudgePostgres(ctx *config.AppContext, awardID, personID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	awardID = strings.TrimSpace(awardID)
	personID = strings.TrimSpace(personID)
	if awardID == "" {
		return fmt.Errorf("award id is required")
	}
	if personID == "" {
		return fmt.Errorf("person id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO award_judges (award_id, person_id)
		SELECT awards.id, people.id
		FROM awards
		JOIN people ON people.id::text = $2
		WHERE awards.id::text = $1
			AND awards.sponsored_by_org_id IS NOT NULL
			AND awards.archived_at IS NULL
		ON CONFLICT (award_id, person_id) DO NOTHING
	`, awardID, personID)
	if err != nil {
		return fmt.Errorf("add award judge %s/%s: %w", awardID, personID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("award is not linked to a sponsor or judge is already assigned")
	}
	return nil
}

func removeAwardJudgePostgres(ctx *config.AppContext, awardID, personID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	awardID = strings.TrimSpace(awardID)
	personID = strings.TrimSpace(personID)
	if awardID == "" {
		return fmt.Errorf("award id is required")
	}
	if personID == "" {
		return fmt.Errorf("person id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM award_judges
		WHERE award_id::text = $1 AND person_id::text = $2
	`, awardID, personID)
	if err != nil {
		return fmt.Errorf("remove award judge %s/%s: %w", awardID, personID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("award judge not found")
	}
	return nil
}

func listAwardJudgesForCompetitionPostgres(ctx *config.AppContext, competitionID string) ([]*types.AwardJudge, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("award judge competition id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT award_judges.award_id::text, award_judges.person_id::text,
			coalesce(people.name, ''), coalesce((
				SELECT email.email::text FROM person_emails email
				WHERE email.person_id = people.id
				ORDER BY email.is_primary DESC, email.created_at, email.id LIMIT 1
			), ''),
			coalesce(people.norm_photo_path, ''), award_judges.created_at
		FROM award_judges
		JOIN awards ON awards.id = award_judges.award_id
		LEFT JOIN people ON people.id = award_judges.person_id
		WHERE awards.competition_id::text = $1
			AND awards.archived_at IS NULL
		ORDER BY awards.title, lower(people.name), people.id
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("list award judges for competition %s: %w", competitionID, err)
	}
	defer rows.Close()
	var out []*types.AwardJudge
	for rows.Next() {
		judge, err := scanAwardJudge(rows)
		if err != nil {
			return nil, fmt.Errorf("scan award judge: %w", err)
		}
		out = append(out, judge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate award judges for competition %s: %w", competitionID, err)
	}
	return out, nil
}
