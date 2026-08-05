package getters

import (
	"context"
	"fmt"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
)

func createJudgeEventPostgres(ctx *config.AppContext, in JudgeEventInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	return "", fmt.Errorf("judge events are created from timeline segments")
}

func listJudgeEventsPostgres(ctx *config.AppContext, competitionID string) ([]*types.JudgeEvent, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("competition id is required")
	}
	if err := syncScheduleSegmentJudgeEventsPostgres(ctx, competitionID); err != nil {
		return nil, fmt.Errorf("sync schedule segment judge events for competition %s: %w", competitionID, err)
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, competition_id::text, coalesce(schedule_segment_id::text, ''),
			name, playbook_type, state, ordering,
			starts_at, ends_at, starting_project_number, rank_limit, created_at, updated_at
		FROM judge_events
		WHERE competition_id::text = $1
		ORDER BY ordering, starts_at NULLS LAST, created_at, name
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("query judge events for competition %s: %w", competitionID, err)
	}
	defer rows.Close()

	var out []*types.JudgeEvent
	for rows.Next() {
		event, err := scanJudgeEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan judge event for competition %s: %w", competitionID, err)
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate judge events for competition %s: %w", competitionID, err)
	}
	return out, nil
}

func updateJudgeEventRankLimitPostgres(ctx *config.AppContext, competitionID, judgeEventID string, rankLimit int) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	judgeEventID = strings.TrimSpace(judgeEventID)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if judgeEventID == "" {
		return fmt.Errorf("judge event is required")
	}
	if rankLimit <= 0 {
		return fmt.Errorf("rank count must be positive")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE judge_events
		SET rank_limit = $3,
			updated_at = now()
		WHERE competition_id::text = $1 AND id::text = $2
	`, competitionID, judgeEventID, rankLimit)
	if err != nil {
		return fmt.Errorf("update judge event rank limit %s: %w", judgeEventID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("judge event not found")
	}
	return nil
}

func updateJudgeEventStatePostgres(ctx *config.AppContext, competitionID, judgeEventID, state string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	judgeEventID = strings.TrimSpace(judgeEventID)
	state = normalizeJudgeEventState(state)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if judgeEventID == "" {
		return fmt.Errorf("judge event is required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin update judge event state: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if state == JudgeEventStateOpen {
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			UPDATE judge_events
			SET state = $3,
				updated_at = now()
			WHERE competition_id::text = $1
				AND id::text <> $2
				AND state = $4
		`, competitionID, judgeEventID, JudgeEventStateClosed, JudgeEventStateOpen); err != nil {
			return fmt.Errorf("close other open judge events: %w", err)
		}
	}
	commandTag, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE judge_events
		SET state = $3,
			updated_at = now()
		WHERE competition_id::text = $1 AND id::text = $2
	`, competitionID, judgeEventID, state)
	if err != nil {
		return fmt.Errorf("update judge event state %s: %w", judgeEventID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("judge event not found")
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return fmt.Errorf("commit update judge event state: %w", err)
	}
	return nil
}

func deleteJudgeEventPostgres(ctx *config.AppContext, competitionID, judgeEventID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	judgeEventID = strings.TrimSpace(judgeEventID)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if judgeEventID == "" {
		return fmt.Errorf("judge event is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM judge_events
		WHERE competition_id::text = $1 AND id::text = $2
	`, competitionID, judgeEventID)
	if err != nil {
		return fmt.Errorf("delete judge event %s: %w", judgeEventID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("judge event not found")
	}
	return nil
}

func addCompetitionJudgePostgres(ctx *config.AppContext, competitionID, personID, judgeType string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	personID = strings.TrimSpace(personID)
	judgeType = normalizeJudgeType(judgeType)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if personID == "" {
		return fmt.Errorf("person id is required")
	}
	if judgeType == "" {
		return fmt.Errorf("judge type must be expo or finals")
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
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
		)
		INSERT INTO competition_judges (competition_id, person_id, judge_type, display_order)
		SELECT $1::uuid, $2::uuid, $3, judge_order.display_order
		FROM judge_order
		ON CONFLICT (competition_id, person_id, judge_type) DO NOTHING
	`, competitionID, personID, judgeType)
	if err != nil {
		return fmt.Errorf("insert competition judge %s/%s/%s: %w", competitionID, personID, judgeType, err)
	}
	return nil
}

func setCompetitionJudgeTypePostgres(ctx *config.AppContext, competitionID, personID, judgeType string) error {
	return setCompetitionJudgeTypesPostgres(ctx, competitionID, personID, []string{judgeType})
}

func setCompetitionJudgeTypesPostgres(ctx *config.AppContext, competitionID, personID string, judgeTypes []string) error {
	return setCompetitionJudgeRolesPostgres(ctx, competitionID, map[string][]string{personID: judgeTypes})
}

func setCompetitionJudgeRolesPostgres(ctx *config.AppContext, competitionID string, rolesByPersonID map[string][]string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if len(rolesByPersonID) == 0 {
		return fmt.Errorf("at least one judge is required")
	}
	normalizedByPersonID := make(map[string][]string, len(rolesByPersonID))
	for personID, judgeTypes := range rolesByPersonID {
		personID = strings.TrimSpace(personID)
		if personID == "" {
			return fmt.Errorf("person id is required")
		}
		normalized := make([]string, 0, len(judgeTypes))
		seen := make(map[string]bool, len(judgeTypes))
		for _, judgeType := range judgeTypes {
			judgeType = normalizeJudgeType(judgeType)
			if judgeType == "" {
				return fmt.Errorf("judge type must be expo or finals")
			}
			if !seen[judgeType] {
				seen[judgeType] = true
				normalized = append(normalized, judgeType)
			}
		}
		if len(normalized) == 0 {
			return fmt.Errorf("choose at least one judge type for each judge")
		}
		normalizedByPersonID[personID] = normalized
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin set competition judge types: %w", err)
	}
	defer tx.Rollback(dbctx)
	for personID, judgeTypes := range normalizedByPersonID {
		displayOrder, err := competitionJudgeDisplayOrderTx(dbctx, tx, competitionID, personID)
		if err != nil {
			return fmt.Errorf("load competition judge order %s/%s: %w", competitionID, personID, err)
		}
		if _, err := tx.Exec(dbctx, `
			DELETE FROM competition_judges
			WHERE competition_id = $1::uuid AND person_id = $2::uuid
		`, competitionID, personID); err != nil {
			return fmt.Errorf("clear competition judge types %s/%s: %w", competitionID, personID, err)
		}
		for _, judgeType := range judgeTypes {
			if _, err := tx.Exec(dbctx, `
				INSERT INTO competition_judges (competition_id, person_id, judge_type, display_order)
				VALUES ($1::uuid, $2::uuid, $3, $4)
			`, competitionID, personID, judgeType, displayOrder); err != nil {
				return fmt.Errorf("set competition judge type %s/%s/%s: %w", competitionID, personID, judgeType, err)
			}
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit competition judge roles %s: %w", competitionID, err)
	}
	return nil
}

func competitionJudgeDisplayOrderTx(dbctx context.Context, tx pgx.Tx, competitionID, personID string) (int, error) {
	var displayOrder int
	if err := tx.QueryRow(dbctx, `
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
		)
	`, competitionID, personID).Scan(&displayOrder); err != nil {
		return 0, err
	}
	return displayOrder, nil
}

func setCompetitionJudgeOrderPostgres(ctx *config.AppContext, competitionID string, personIDs []string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	normalized := make([]string, 0, len(personIDs))
	seen := make(map[string]bool, len(personIDs))
	for _, personID := range personIDs {
		personID = strings.TrimSpace(personID)
		if personID == "" {
			continue
		}
		if seen[personID] {
			return fmt.Errorf("judge order contains a duplicate judge")
		}
		seen[personID] = true
		normalized = append(normalized, personID)
	}
	if len(normalized) == 0 {
		return fmt.Errorf("judge order is required")
	}

	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin set competition judge order: %w", err)
	}
	defer tx.Rollback(dbctx)

	rows, err := tx.Query(dbctx, `
		SELECT DISTINCT person_id::text
		FROM competition_judges
		WHERE competition_id = $1::uuid
	`, competitionID)
	if err != nil {
		return fmt.Errorf("load current competition judges %s: %w", competitionID, err)
	}
	current := make(map[string]bool)
	for rows.Next() {
		var personID string
		if err := rows.Scan(&personID); err != nil {
			rows.Close()
			return fmt.Errorf("scan current competition judge %s: %w", competitionID, err)
		}
		current[personID] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate current competition judges %s: %w", competitionID, err)
	}
	rows.Close()
	if len(current) != len(normalized) {
		return fmt.Errorf("the judge list changed. Refresh the page and try again")
	}
	for _, personID := range normalized {
		if !current[personID] {
			return fmt.Errorf("the judge list changed. Refresh the page and try again")
		}
	}
	for index, personID := range normalized {
		commandTag, err := tx.Exec(dbctx, `
			UPDATE competition_judges
			SET display_order = $3
			WHERE competition_id = $1::uuid AND person_id = $2::uuid
		`, competitionID, personID, index+1)
		if err != nil {
			return fmt.Errorf("set competition judge order %s/%s: %w", competitionID, personID, err)
		}
		if commandTag.RowsAffected() == 0 {
			return fmt.Errorf("competition judge %s/%s not found", competitionID, personID)
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit competition judge order %s: %w", competitionID, err)
	}
	return nil
}

func setCompetitionJudgePublicLabelOverridesPostgres(ctx *config.AppContext, competitionID string, overridesByPersonID map[string]string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if len(overridesByPersonID) == 0 {
		return nil
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin set competition judge public labels: %w", err)
	}
	defer tx.Rollback(dbctx)
	for personID, override := range overridesByPersonID {
		personID = strings.TrimSpace(personID)
		if personID == "" {
			return fmt.Errorf("person id is required")
		}
		commandTag, err := tx.Exec(dbctx, `
			UPDATE competition_judges
			SET public_label_override = $3
			WHERE competition_id = $1::uuid AND person_id = $2::uuid
		`, competitionID, personID, strings.TrimSpace(override))
		if err != nil {
			return fmt.Errorf("set competition judge public label %s/%s: %w", competitionID, personID, err)
		}
		if commandTag.RowsAffected() == 0 {
			return fmt.Errorf("competition judge %s/%s not found", competitionID, personID)
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit competition judge public labels %s: %w", competitionID, err)
	}
	return nil
}

func removeCompetitionJudgePostgres(ctx *config.AppContext, competitionID, personID, judgeType string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	personID = strings.TrimSpace(personID)
	judgeType = normalizeJudgeType(judgeType)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if personID == "" {
		return fmt.Errorf("person id is required")
	}
	if judgeType == "" {
		return fmt.Errorf("judge type must be expo or finals")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM competition_judges
		WHERE competition_id::text = $1 AND person_id::text = $2
	`, competitionID, personID)
	if err != nil {
		return fmt.Errorf("remove competition judge %s/%s/%s: %w", competitionID, personID, judgeType, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("competition judge %s/%s/%s not found", competitionID, personID, judgeType)
	}
	return nil
}

func listCompetitionJudgesPostgres(ctx *config.AppContext, competitionID string) ([]*types.CompetitionJudge, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("competition id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT competition_judges.competition_id::text, competition_judges.person_id::text,
			coalesce(people.name, ''), coalesce((
				SELECT email.email::text FROM person_emails email
				WHERE email.person_id = people.id
				ORDER BY email.is_primary DESC, email.created_at, email.id LIMIT 1
			), ''),
			coalesce(people.norm_photo_path, ''),
			coalesce(judge_company.company, nullif(people.company, ''), ''),
			coalesce(max(nullif(competition_judges.public_label_override, '')), ''),
			array_agg(competition_judges.judge_type ORDER BY
				CASE competition_judges.judge_type WHEN 'expo' THEN 1 WHEN 'finals' THEN 2 ELSE 3 END),
			min(competition_judges.display_order),
			min(competition_judges.created_at)
		FROM competition_judges
		JOIN competitions ON competitions.id = competition_judges.competition_id
		LEFT JOIN people ON people.id = competition_judges.person_id
		LEFT JOIN LATERAL (
			SELECT nullif(speaker_confs.company, '') AS company
			FROM speaker_confs_conferences
			JOIN speaker_confs ON speaker_confs.id = speaker_confs_conferences.speaker_conf_id
			WHERE speaker_confs_conferences.conference_id = competitions.conference_id
				AND speaker_confs.speaker_id = competition_judges.person_id
				AND nullif(speaker_confs.company, '') IS NOT NULL
			ORDER BY speaker_confs.created_at DESC
			LIMIT 1
		) judge_company ON true
		WHERE competition_judges.competition_id::text = $1
		GROUP BY competition_judges.competition_id, competition_judges.person_id,
			people.id, people.name, people.norm_photo_path, people.company, judge_company.company
		ORDER BY CASE WHEN min(competition_judges.display_order) > 0 THEN 0 ELSE 1 END,
			min(competition_judges.display_order), lower(people.name), people.id
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("query competition judges %s: %w", competitionID, err)
	}
	defer rows.Close()
	var out []*types.CompetitionJudge
	for rows.Next() {
		var judge types.CompetitionJudge
		if err := rows.Scan(&judge.CompetitionID, &judge.PersonID, &judge.Name, &judge.Email, &judge.Photo, &judge.Company, &judge.PublicLabelOverride, &judge.JudgeTypes, &judge.DisplayOrder, &judge.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan competition judge %s: %w", competitionID, err)
		}
		if len(judge.JudgeTypes) > 0 {
			judge.JudgeType = judge.JudgeTypes[0]
		}
		out = append(out, &judge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate competition judges %s: %w", competitionID, err)
	}
	return out, nil
}

func listCompetitionJudgeAssignmentsByEmailPostgres(ctx *config.AppContext, email string) ([]*types.CompetitionJudgeAssignment, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	return listCompetitionJudgeAssignmentsPostgres(ctx, `
		competition_judges.person_id = (SELECT person_id FROM person_emails WHERE email = $1::citext)
	`, email, email)
}

func listCompetitionJudgeAssignmentsByPersonIDPostgres(ctx *config.AppContext, personID string) ([]*types.CompetitionJudgeAssignment, error) {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil, fmt.Errorf("person id is required")
	}
	return listCompetitionJudgeAssignmentsPostgres(ctx, "competition_judges.person_id = $1::uuid", personID, personID)
}

func listCompetitionJudgeAssignmentsPostgres(ctx *config.AppContext, whereSQL, value, label string) ([]*types.CompetitionJudgeAssignment, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT DISTINCT competitions.id::text,
			competitions.conference_id::text,
			conferences.tag,
			competition_judges.judge_type
		FROM competition_judges
		JOIN people ON people.id = competition_judges.person_id
		JOIN competitions ON competitions.id = competition_judges.competition_id
		JOIN conferences ON conferences.id = competitions.conference_id
		WHERE `+whereSQL+`
		ORDER BY conferences.tag, competition_judges.judge_type
	`, value)
	if err != nil {
		return nil, fmt.Errorf("query competition judge assignments for %s: %w", label, err)
	}
	defer rows.Close()

	var out []*types.CompetitionJudgeAssignment
	for rows.Next() {
		var assignment types.CompetitionJudgeAssignment
		if err := rows.Scan(&assignment.CompetitionID, &assignment.ConferenceID, &assignment.ConferenceTag, &assignment.JudgeType); err != nil {
			return nil, fmt.Errorf("scan competition judge assignment for %s: %w", label, err)
		}
		out = append(out, &assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate competition judge assignments for %s: %w", label, err)
	}
	return out, nil
}

func upsertScorecardPostgres(ctx *config.AppContext, in ScorecardInput) (*types.Scorecard, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	in = normalizeScorecardInput(in)
	if in.JudgeEventID == "" {
		return nil, fmt.Errorf("scorecard judge event id is required")
	}
	if in.ProjectID == "" {
		return nil, fmt.Errorf("scorecard project id is required")
	}
	if in.JudgePersonID == "" {
		return nil, fmt.Errorf("scorecard judge person id is required")
	}
	scorecard, err := scanScorecard(ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO scorecards (
			judge_event_id, project_id, judge_person_id,
			rank, comments, submitted_at
		)
		SELECT judge_events.id, projects.id, $3,
			$4, $5, now()
		FROM judge_events
		JOIN projects ON projects.id::text = $2
			AND projects.competition_id = judge_events.competition_id
		WHERE judge_events.id::text = $1
		ON CONFLICT (judge_event_id, project_id, judge_person_id)
		DO UPDATE SET
			rank = EXCLUDED.rank,
			comments = EXCLUDED.comments,
			submitted_at = EXCLUDED.submitted_at,
			updated_at = now()
		RETURNING id::text, judge_event_id::text, project_id::text, judge_person_id::text,
			rank, comments,
			submitted_at, created_at, updated_at
	`, in.JudgeEventID, in.ProjectID, in.JudgePersonID, in.Rank, in.Comments))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("scorecard project and judge event must belong to the same competition")
		}
		return nil, fmt.Errorf("upsert scorecard: %w", err)
	}
	return scorecard, nil
}

func replaceScorecardRankingsPostgres(ctx *config.AppContext, in ScorecardRankingsInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	in = normalizeScorecardRankingsInput(in)
	if in.JudgeEventID == "" {
		return fmt.Errorf("scorecard judge event id is required")
	}
	if in.JudgePersonID == "" {
		return fmt.Errorf("scorecard judge person id is required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin scorecard rankings transaction: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	if _, err := tx.Exec(ctx.DatabaseContext(), `
		DELETE FROM scorecards
		WHERE judge_event_id::text = $1 AND judge_person_id::text = $2
	`, in.JudgeEventID, in.JudgePersonID); err != nil {
		return fmt.Errorf("clear scorecard rankings: %w", err)
	}
	for _, ranking := range in.Rankings {
		if strings.TrimSpace(ranking.ProjectID) == "" || ranking.Rank <= 0 {
			continue
		}
		commandTag, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO scorecards (
				judge_event_id, project_id, judge_person_id, rank, comments, submitted_at
			)
			SELECT judge_events.id, projects.id, $3, $4, '', now()
			FROM judge_events
			JOIN projects ON projects.id::text = $2
				AND projects.competition_id = judge_events.competition_id
			WHERE judge_events.id::text = $1
		`, in.JudgeEventID, ranking.ProjectID, in.JudgePersonID, ranking.Rank)
		if err != nil {
			return fmt.Errorf("insert scorecard ranking for project %s: %w", ranking.ProjectID, err)
		}
		if commandTag.RowsAffected() == 0 {
			return fmt.Errorf("scorecard project and judge event must belong to the same competition")
		}
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return fmt.Errorf("commit scorecard rankings: %w", err)
	}
	return nil
}

func deleteScorecardRankingsPostgres(ctx *config.AppContext, competitionID, judgeEventID, judgePersonID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	judgeEventID = strings.TrimSpace(judgeEventID)
	judgePersonID = strings.TrimSpace(judgePersonID)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if judgeEventID == "" {
		return fmt.Errorf("scorecard judge event id is required")
	}
	if judgePersonID == "" {
		return fmt.Errorf("scorecard judge person id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM scorecards
		USING judge_events
		WHERE scorecards.judge_event_id = judge_events.id
			AND judge_events.competition_id::text = $1
			AND judge_events.id::text = $2
			AND scorecards.judge_person_id::text = $3
	`, competitionID, judgeEventID, judgePersonID)
	if err != nil {
		return fmt.Errorf("delete scorecard rankings: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("no ballot found for that judge and event")
	}
	return nil
}

func listScorecardsForJudgePostgres(ctx *config.AppContext, competitionID, judgePersonID string) ([]*types.Scorecard, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	judgePersonID = strings.TrimSpace(judgePersonID)
	if competitionID == "" {
		return nil, fmt.Errorf("scorecard competition id is required")
	}
	if judgePersonID == "" {
		return nil, fmt.Errorf("scorecard judge person id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT scorecards.id::text, scorecards.judge_event_id::text,
			scorecards.project_id::text, scorecards.judge_person_id::text,
			scorecards.rank, scorecards.comments,
			scorecards.submitted_at, scorecards.created_at, scorecards.updated_at
		FROM scorecards
		JOIN judge_events ON judge_events.id = scorecards.judge_event_id
		WHERE judge_events.competition_id::text = $1
			AND scorecards.judge_person_id::text = $2
		ORDER BY scorecards.project_id, judge_events.ordering, judge_events.name
	`, competitionID, judgePersonID)
	if err != nil {
		return nil, fmt.Errorf("list scorecards for judge %s: %w", judgePersonID, err)
	}
	defer rows.Close()
	var out []*types.Scorecard
	for rows.Next() {
		scorecard, err := scanScorecard(rows)
		if err != nil {
			return nil, fmt.Errorf("scan scorecard: %w", err)
		}
		out = append(out, scorecard)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scorecards for judge %s: %w", judgePersonID, err)
	}
	return out, nil
}

func listScorecardsForCompetitionPostgres(ctx *config.AppContext, competitionID string) ([]*types.Scorecard, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("scorecard competition id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT scorecards.id::text, scorecards.judge_event_id::text,
			scorecards.project_id::text, scorecards.judge_person_id::text,
			scorecards.rank, scorecards.comments,
			scorecards.submitted_at, scorecards.created_at, scorecards.updated_at
		FROM scorecards
		JOIN judge_events ON judge_events.id = scorecards.judge_event_id
		WHERE judge_events.competition_id::text = $1
		ORDER BY scorecards.project_id, judge_events.ordering, judge_events.name, scorecards.judge_person_id
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("list scorecards for competition %s: %w", competitionID, err)
	}
	defer rows.Close()
	var out []*types.Scorecard
	for rows.Next() {
		scorecard, err := scanScorecard(rows)
		if err != nil {
			return nil, fmt.Errorf("scan scorecard: %w", err)
		}
		out = append(out, scorecard)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scorecards for competition %s: %w", competitionID, err)
	}
	return out, nil
}
