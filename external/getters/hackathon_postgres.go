package getters

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
)

const (
	CompetitionVisibilityHidden           = "hidden"
	CompetitionVisibilityPublic           = "public"
	CompetitionDescriptionFormatPlain     = "plain"
	CompetitionDescriptionFormatMarkdown  = "markdown"
	CompetitionDescriptionFormatHTML      = "html"
	CompetitionLifecycleAuto              = ""
	CompetitionLifecycleUpcoming          = "upcoming"
	CompetitionLifecycleOpen              = "open"
	CompetitionLifecycleSubmissionsClosed = "submissions_closed"
	CompetitionLifecycleClosed            = "closed"
	CompetitionJudgingModeManual          = "manual"
	CompetitionJudgingModeAutomatic       = "automatic"
	ProjectInviteDefaultTTL               = 24 * time.Hour
	ProjectStatusCreated                  = "created"
	ProjectStatusSubmitted                = "submitted"
	ProjectStatusHidden                   = "hidden"
	ProjectStatusAdvanced                 = "advanced"
	ProjectMemberRoleOwner                = "owner"
	ProjectMemberRoleMember               = "member"
	JudgeTypeExpo                         = "expo"
	JudgeTypeFinals                       = "finals"
	JudgeEventStatePending                = "pending"
	JudgeEventStateOpen                   = "open"
	JudgeEventStateClosed                 = "closed"
	AwardTypeNormal                       = "normal"
	AwardTypeChallenge                    = "challenge"
	AwardStatusDraft                      = "draft"
	AwardStatusAvailable                  = "available"
	AwardStatusUnawarded                  = "unawarded"
	AwardStatusAwarded                    = "awarded"
	PrizeTypeSats                         = "sats"
	PrizeTypeInKind                       = "in_kind"
	PrizeTypeTickets                      = "tickets"
	PrizeTypePooled                       = "pooled"
	PrizeTypeTrophy                       = "trophy"
	PrizeStatusAvailable                  = "available"
	PrizeStatusNeedsFunds                 = "needs_funds"
	PrizeStatusAwarded                    = "awarded"
	PrizeStatusPaid                       = "paid"
)

func createCompetitionPostgres(ctx *config.AppContext, in CompetitionInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	in = normalizeCompetitionInput(in)
	if in.Title == "" {
		return "", fmt.Errorf("competition title is required")
	}
	if in.ConferenceID == "" {
		return "", fmt.Errorf("competition conference is required")
	}
	if in.Visibility == "" {
		in.Visibility = CompetitionVisibilityHidden
	}

	var id string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO competitions (
			conference_id, title, description, description_format, visibility, lifecycle_override, judging_mode, public_gallery_enabled, allow_late_submissions, public_tables_enabled, max_team_size
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
		RETURNING id::text
	`, in.ConferenceID, in.Title, in.Description, in.DescriptionFormat, in.Visibility, in.LifecycleOverride, in.JudgingMode, in.PublicGalleryEnabled, in.AllowLateSubmissions, in.PublicTablesEnabled, in.MaxTeamSize).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert competition %q: %w", in.Title, err)
	}
	return id, nil
}

func updateCompetitionPostgres(ctx *config.AppContext, competitionID string, in CompetitionInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	in = normalizeCompetitionInput(in)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if in.Title == "" {
		return fmt.Errorf("competition title is required")
	}
	if in.ConferenceID == "" {
		return fmt.Errorf("competition conference is required")
	}
	if in.Visibility == "" {
		in.Visibility = CompetitionVisibilityHidden
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE competitions
		SET conference_id = $2::uuid,
			title = $3,
			description = $4,
			description_format = $5,
			visibility = $6,
			lifecycle_override = $7,
			judging_mode = $8,
			public_gallery_enabled = $9,
			allow_late_submissions = $10,
			public_tables_enabled = $11,
			max_team_size = $12
		WHERE id = $1
	`, competitionID, in.ConferenceID, in.Title, in.Description, in.DescriptionFormat,
		in.Visibility, in.LifecycleOverride, in.JudgingMode, in.PublicGalleryEnabled, in.AllowLateSubmissions, in.PublicTablesEnabled, in.MaxTeamSize)
	if err != nil {
		return fmt.Errorf("update competition %s: %w", competitionID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("competition %s not found", competitionID)
	}
	return nil
}

func getCompetitionByConferenceIDPostgres(ctx *config.AppContext, conferenceID string) (*types.HackathonCompetition, error) {
	conferenceID = strings.TrimSpace(conferenceID)
	if conferenceID == "" {
		return nil, fmt.Errorf("competition conference is required")
	}
	competitions, err := queryCompetitionsPostgres(ctx, "competition by conference", "WHERE conference_id::text = $1", []any{conferenceID})
	if err != nil {
		return nil, err
	}
	if len(competitions) == 0 {
		return nil, nil
	}
	return competitions[0], nil
}

func updateCompetitionVisibilityPostgres(ctx *config.AppContext, competitionID, visibility string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	visibility = normalizeCompetitionVisibility(visibility)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if visibility == "" {
		return fmt.Errorf("competition visibility is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE competitions
		SET visibility = $2
		WHERE id = $1
	`, competitionID, visibility)
	if err != nil {
		return fmt.Errorf("update competition %s visibility: %w", competitionID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("competition %s not found", competitionID)
	}
	return nil
}

func updateCompetitionJudgingModePostgres(ctx *config.AppContext, competitionID, mode string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	mode = normalizeCompetitionJudgingMode(mode)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE competitions
		SET judging_mode = $2
		WHERE id = $1
	`, competitionID, mode)
	if err != nil {
		return fmt.Errorf("update competition %s judging mode: %w", competitionID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("competition %s not found", competitionID)
	}
	return nil
}

func finalizeCompetitionResultsPostgres(ctx *config.AppContext, competitionID, personID string) error {
	return setCompetitionResultsFinalizedPostgres(ctx, competitionID, personID, true)
}

func reopenCompetitionResultsPostgres(ctx *config.AppContext, competitionID, personID string) error {
	return setCompetitionResultsFinalizedPostgres(ctx, competitionID, personID, false)
}

func setCompetitionResultsFinalizedPostgres(ctx *config.AppContext, competitionID, personID string, finalized bool) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	personID = strings.TrimSpace(personID)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	if personID == "" {
		return fmt.Errorf("person id is required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin update competition results: %w", err)
	}
	defer tx.Rollback(dbctx)

	var changedID string
	if finalized {
		err = tx.QueryRow(dbctx, `
			UPDATE competitions
			SET results_finalized_at = now(), results_finalized_by = $2::uuid
			WHERE id::text = $1 AND results_finalized_at IS NULL
			RETURNING id::text
		`, competitionID, personID).Scan(&changedID)
	} else {
		err = tx.QueryRow(dbctx, `
			UPDATE competitions
			SET results_finalized_at = NULL, results_finalized_by = NULL
			WHERE id::text = $1 AND results_finalized_at IS NOT NULL
			RETURNING id::text
		`, competitionID).Scan(&changedID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if finalized {
			return fmt.Errorf("hackathon results are already finalized")
		}
		return fmt.Errorf("hackathon results are not finalized")
	}
	if err != nil {
		return fmt.Errorf("update competition results %s: %w", competitionID, err)
	}
	action := "reopened"
	if finalized {
		action = "finalized"
	}
	if _, err := tx.Exec(dbctx, `
		INSERT INTO competition_results_publication_events (competition_id, action, performed_by)
		VALUES ($1::uuid, $2, $3::uuid)
	`, competitionID, action, personID); err != nil {
		return fmt.Errorf("record competition results %s event: %w", action, err)
	}
	if finalized {
		if _, err := tx.Exec(dbctx, `
			INSERT INTO award_distributions (
				competition_id, award_id, project_id, prize_id, person_id,
				distribution_type, ticket_quantity, status, notes
			)
			SELECT awards.competition_id, project_awards.award_id,
				project_awards.project_id, prizes.id, project_members.person_id,
				$2, 1, 'pending', 'Automatically issued when hackathon results were finalized.'
			FROM project_awards
			JOIN awards ON awards.id = project_awards.award_id
			JOIN prizes ON prizes.award_id = awards.id AND prizes.prize_type = $2
			JOIN project_members ON project_members.project_id = project_awards.project_id
			WHERE awards.competition_id = $1::uuid AND awards.archived_at IS NULL
			ON CONFLICT (award_id, project_id, prize_id, person_id) DO NOTHING
		`, competitionID, PrizeTypeTickets); err != nil {
			return fmt.Errorf("create finalized ticket distributions: %w", err)
		}
		if _, err := tx.Exec(dbctx, `
			INSERT INTO hackathon_ticket_entitlements (
				person_id, award_distribution_id, quantity
			)
			SELECT distributions.person_id, distributions.id,
				coalesce(distributions.ticket_quantity, 1)
			FROM award_distributions distributions
			JOIN prizes ON prizes.id = distributions.prize_id
			WHERE distributions.competition_id = $1::uuid
				AND distributions.distribution_type = $2
				AND prizes.prize_type = $2
			ON CONFLICT (award_distribution_id) DO NOTHING
		`, competitionID, PrizeTypeTickets); err != nil {
			return fmt.Errorf("create finalized ticket entitlements: %w", err)
		}
	} else {
		// Reopening results permits award corrections. Remove only unclaimed
		// entitlements created automatically so re-finalization can rebuild them
		// from the current award recipients without revoking tickets already used.
		if _, err := tx.Exec(dbctx, `
			DELETE FROM award_distributions distributions
			USING hackathon_ticket_entitlements entitlements
			WHERE distributions.competition_id = $1::uuid
				AND entitlements.award_distribution_id = distributions.id
				AND distributions.distribution_type = $2
				AND distributions.notes = 'Automatically issued when hackathon results were finalized.'
				AND entitlements.claimed_at IS NULL
		`, competitionID, PrizeTypeTickets); err != nil {
			return fmt.Errorf("remove reopened ticket entitlements: %w", err)
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit competition results %s: %w", action, err)
	}
	return nil
}

func listCompetitionScheduleSegmentsPostgres(ctx *config.AppContext, competitionID string) ([]*types.CompetitionScheduleSegment, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("competition id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, competition_id::text, coalesce(proposal_id::text, ''),
			coalesce(conf_talk_id::text, ''), segment_type, title,
			default_duration_minutes, ordering, created_at, updated_at
		FROM competition_schedule_segments
		WHERE competition_id = $1::uuid
		ORDER BY ordering, created_at, id
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("list competition schedule segments %s: %w", competitionID, err)
	}
	defer rows.Close()

	var segments []*types.CompetitionScheduleSegment
	for rows.Next() {
		segment := &types.CompetitionScheduleSegment{}
		if err := rows.Scan(
			&segment.ID,
			&segment.CompetitionID,
			&segment.ProposalID,
			&segment.ConfTalkID,
			&segment.SegmentType,
			&segment.Title,
			&segment.DefaultDurationMinutes,
			&segment.Ordering,
			&segment.CreatedAt,
			&segment.UpdatedAt,
		); err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

func listCompetitionScheduleSegmentsForConferencePostgres(ctx *config.AppContext, conferenceID string) ([]*types.CompetitionScheduleSegment, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	conferenceID = strings.TrimSpace(conferenceID)
	if conferenceID == "" {
		return nil, fmt.Errorf("conference id is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT css.id::text, css.competition_id::text, coalesce(css.proposal_id::text, ''),
			coalesce(css.conf_talk_id::text, ''), css.segment_type, css.title,
			css.default_duration_minutes, css.ordering, css.created_at, css.updated_at
		FROM competition_schedule_segments css
		JOIN competitions c ON c.id = css.competition_id
		WHERE c.conference_id = $1::uuid
		ORDER BY c.created_at DESC, c.title, css.ordering, css.created_at, css.id
	`, conferenceID)
	if err != nil {
		return nil, fmt.Errorf("list conference schedule segments %s: %w", conferenceID, err)
	}
	defer rows.Close()

	var segments []*types.CompetitionScheduleSegment
	for rows.Next() {
		segment := &types.CompetitionScheduleSegment{}
		if err := rows.Scan(
			&segment.ID,
			&segment.CompetitionID,
			&segment.ProposalID,
			&segment.ConfTalkID,
			&segment.SegmentType,
			&segment.Title,
			&segment.DefaultDurationMinutes,
			&segment.Ordering,
			&segment.CreatedAt,
			&segment.UpdatedAt,
		); err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

func getCompetitionScheduleSegmentByProposalPostgres(ctx *config.AppContext, proposalID string) (*types.CompetitionScheduleSegment, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	proposalID = strings.TrimSpace(proposalID)
	if proposalID == "" {
		return nil, nil
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, competition_id::text, coalesce(proposal_id::text, ''),
			coalesce(conf_talk_id::text, ''), segment_type, title,
			default_duration_minutes, ordering, created_at, updated_at
		FROM competition_schedule_segments
		WHERE proposal_id::text = $1
	`, proposalID)
	segment, err := scanCompetitionScheduleSegment(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get schedule segment by proposal %s: %w", proposalID, err)
	}
	return segment, nil
}

func replaceCompetitionScheduleSegmentsPostgres(ctx *config.AppContext, competitionID string, inputs []CompetitionScheduleSegmentInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return fmt.Errorf("competition id is required")
	}
	competition, err := getCompetitionByIDPostgres(ctx, competitionID)
	if err != nil {
		return err
	}
	conf, err := GetConfByRef(ctx, competition.ConferenceID)
	if err != nil {
		return err
	}
	if conf == nil {
		return fmt.Errorf("competition conference not found")
	}
	existing, err := listCompetitionScheduleSegmentsPostgres(ctx, competitionID)
	if err != nil {
		return err
	}
	existingByID := make(map[string]*types.CompetitionScheduleSegment, len(existing))
	for _, segment := range existing {
		if segment != nil {
			existingByID[segment.ID] = segment
		}
	}

	kept := map[string]bool{}
	for i, input := range inputs {
		input = normalizeCompetitionScheduleSegmentInput(input, i)
		if input.Title == "" {
			continue
		}
		segment := existingByID[input.ID]
		proposalID := ""
		confTalkID := ""
		if segment != nil {
			kept[segment.ID] = true
			proposalID = segment.ProposalID
			confTalkID = segment.ConfTalkID
		}

		schedulerTitle := competitionScheduleSegmentProposalTitle(competition.Title, input.Title)
		if proposalID == "" {
			proposalID, err = CreateProposal(ctx, ProposalInput{
				Title:           schedulerTitle,
				TalkType:        "hackathon",
				DesiredDuration: input.DefaultDurationMinutes,
				AvailDuration:   input.DefaultDurationMinutes,
				ScheduleForTag:  conf.Tag,
				Status:          "Accepted",
			})
			if err != nil {
				return fmt.Errorf("create schedule segment proposal %q: %w", input.Title, err)
			}
		} else {
			if err := UpdateProposal(ctx, proposalID, ProposalInput{
				Title:           schedulerTitle,
				TalkType:        "hackathon",
				DesiredDuration: input.DefaultDurationMinutes,
				AvailDuration:   input.DefaultDurationMinutes,
			}); err != nil {
				return fmt.Errorf("update schedule segment proposal %s: %w", proposalID, err)
			}
			if err := UpdateProposalStatus(ctx, proposalID, "Accepted"); err != nil {
				return fmt.Errorf("reactivate schedule segment proposal %s: %w", proposalID, err)
			}
		}
		if confTalkID == "" {
			confTalkID, err = CreateConfTalk(ctx, ConfTalkInput{
				ConfTag:    conf.Tag,
				ProposalID: proposalID,
			})
			if err != nil {
				return fmt.Errorf("create schedule segment conf talk %q: %w", input.Title, err)
			}
		}
		if err := updatePlacedScheduleSegmentDuration(ctx, confTalkID, input.DefaultDurationMinutes); err != nil {
			return fmt.Errorf("update schedule segment duration %q: %w", input.Title, err)
		}

		if segment == nil {
			var segmentID string
			if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
				INSERT INTO competition_schedule_segments (
					competition_id, proposal_id, conf_talk_id, segment_type, title,
					default_duration_minutes, ordering
				) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7)
				RETURNING id::text
			`, competitionID, proposalID, confTalkID, input.SegmentType, input.Title,
				input.DefaultDurationMinutes, input.Ordering).Scan(&segmentID); err != nil {
				return fmt.Errorf("insert schedule segment %q: %w", input.Title, err)
			}
			if err := syncScheduleSegmentJudgeEventPostgres(ctx, &types.CompetitionScheduleSegment{
				ID:                     segmentID,
				CompetitionID:          competitionID,
				ProposalID:             proposalID,
				ConfTalkID:             confTalkID,
				SegmentType:            input.SegmentType,
				Title:                  input.Title,
				DefaultDurationMinutes: input.DefaultDurationMinutes,
				Ordering:               input.Ordering,
			}); err != nil {
				return fmt.Errorf("sync schedule segment judge event %q: %w", input.Title, err)
			}
			continue
		}
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
			UPDATE competition_schedule_segments
			SET proposal_id = $2::uuid,
				conf_talk_id = $3::uuid,
				segment_type = $4,
				title = $5,
				default_duration_minutes = $6,
				ordering = $7
			WHERE id = $1::uuid
		`, segment.ID, proposalID, confTalkID, input.SegmentType, input.Title,
			input.DefaultDurationMinutes, input.Ordering); err != nil {
			return fmt.Errorf("update schedule segment %s: %w", segment.ID, err)
		}
		if err := syncScheduleSegmentJudgeEventPostgres(ctx, &types.CompetitionScheduleSegment{
			ID:                     segment.ID,
			CompetitionID:          competitionID,
			ProposalID:             proposalID,
			ConfTalkID:             confTalkID,
			SegmentType:            input.SegmentType,
			Title:                  input.Title,
			DefaultDurationMinutes: input.DefaultDurationMinutes,
			Ordering:               input.Ordering,
		}); err != nil {
			return fmt.Errorf("sync schedule segment judge event %q: %w", input.Title, err)
		}
	}

	for _, segment := range existing {
		if segment == nil || kept[segment.ID] {
			continue
		}
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `DELETE FROM judge_events WHERE schedule_segment_id = $1::uuid`, segment.ID); err != nil {
			return fmt.Errorf("delete schedule segment judge event %s: %w", segment.ID, err)
		}
		if segment.ProposalID != "" {
			if err := UpdateProposalStatus(ctx, segment.ProposalID, "TheyDecline"); err != nil {
				return fmt.Errorf("hide removed schedule segment proposal %s: %w", segment.ProposalID, err)
			}
		}
		if segment.ConfTalkID != "" {
			if err := DeleteConfTalk(ctx, segment.ConfTalkID); err != nil {
				return fmt.Errorf("archive removed schedule segment conf talk %s: %w", segment.ConfTalkID, err)
			}
		}
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `DELETE FROM competition_schedule_segments WHERE id = $1::uuid`, segment.ID); err != nil {
			return fmt.Errorf("delete schedule segment %s: %w", segment.ID, err)
		}
	}
	return reorderCompetitionScheduleSegmentsBySchedulePostgres(ctx, competitionID)
}

func syncScheduleSegmentJudgeEventByProposalPostgres(ctx *config.AppContext, proposalID string) error {
	segment, err := getCompetitionScheduleSegmentByProposalPostgres(ctx, proposalID)
	if err != nil || segment == nil {
		return err
	}
	if err := syncScheduleSegmentConfTalkLinkPostgres(ctx, segment); err != nil {
		return err
	}
	if err := syncScheduleSegmentJudgeEventPostgres(ctx, segment); err != nil {
		return err
	}
	return reorderCompetitionScheduleSegmentsBySchedulePostgres(ctx, segment.CompetitionID)
}

func syncScheduleSegmentConfTalkLinkPostgres(ctx *config.AppContext, segment *types.CompetitionScheduleSegment) error {
	if ctx == nil || ctx.DB == nil || segment == nil || strings.TrimSpace(segment.ID) == "" {
		return nil
	}
	confTalkID := ""
	if strings.TrimSpace(segment.ProposalID) != "" {
		confTalk, err := GetConfTalkByProposal(ctx, segment.ProposalID)
		if err != nil {
			return err
		}
		if confTalk != nil {
			confTalkID = confTalk.ID
		}
	}
	if confTalkID == segment.ConfTalkID {
		return nil
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE competition_schedule_segments
		SET conf_talk_id = nullif($2, '')::uuid
		WHERE id = $1::uuid
	`, segment.ID, confTalkID)
	if err != nil {
		return fmt.Errorf("sync schedule segment conf talk %s: %w", segment.ID, err)
	}
	segment.ConfTalkID = confTalkID
	return nil
}

func reorderCompetitionScheduleSegmentsBySchedulePostgres(ctx *config.AppContext, competitionID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		WITH ordered AS (
			SELECT css.id,
				row_number() OVER (
					ORDER BY
						(ct.scheduled_start IS NULL),
						ct.scheduled_start,
						css.ordering,
						css.created_at,
						css.id
				) - 1 AS next_ordering
			FROM competition_schedule_segments css
			LEFT JOIN conf_talks ct
				ON ct.id = css.conf_talk_id
				AND ct.archived_at IS NULL
			WHERE css.competition_id = $1::uuid
		)
		UPDATE competition_schedule_segments css
		SET ordering = ordered.next_ordering
		FROM ordered
		WHERE css.id = ordered.id
			AND css.ordering IS DISTINCT FROM ordered.next_ordering
	`, competitionID)
	if err != nil {
		return fmt.Errorf("reorder competition schedule segments %s: %w", competitionID, err)
	}
	return nil
}

func syncScheduleSegmentJudgeEventsPostgres(ctx *config.AppContext, competitionID string) error {
	segments, err := listCompetitionScheduleSegmentsPostgres(ctx, competitionID)
	if err != nil {
		return err
	}
	for _, segment := range segments {
		if err := syncScheduleSegmentJudgeEventPostgres(ctx, segment); err != nil {
			return err
		}
	}
	return nil
}

func syncScheduleSegmentJudgeEventPostgres(ctx *config.AppContext, segment *types.CompetitionScheduleSegment) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	if segment == nil || strings.TrimSpace(segment.ID) == "" {
		return nil
	}
	playbookType := normalizeJudgeEventType(segment.SegmentType)
	if playbookType == "" {
		_, err := ctx.DB.Exec(ctx.DatabaseContext(), `DELETE FROM judge_events WHERE schedule_segment_id = $1::uuid`, segment.ID)
		return err
	}
	startsAt, endsAt, err := scheduleSegmentTimes(ctx, segment)
	if err != nil {
		return err
	}
	var existingID string
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text
		FROM judge_events
		WHERE schedule_segment_id = $1::uuid
	`, segment.ID).Scan(&existingID)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("lookup judge event for schedule segment %s: %w", segment.ID, err)
	}
	if existingID == "" {
		_, err = ctx.DB.Exec(ctx.DatabaseContext(), `
			INSERT INTO judge_events (
				competition_id, schedule_segment_id, name, playbook_type, ordering,
				starts_at, ends_at
			) VALUES (
				$1::uuid, $2::uuid, $3, $4, $5, $6, $7
			)
		`, segment.CompetitionID, segment.ID, segment.Title, playbookType, segment.Ordering, startsAt, endsAt)
		if err != nil {
			return fmt.Errorf("insert judge event for schedule segment %s: %w", segment.ID, err)
		}
		return nil
	}
	_, err = ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE judge_events
		SET name = $2,
			playbook_type = $3,
			ordering = $4,
			starts_at = $5,
			ends_at = $6
		WHERE id = $1::uuid
	`, existingID, segment.Title, playbookType, segment.Ordering, startsAt, endsAt)
	if err != nil {
		return fmt.Errorf("update judge event for schedule segment %s: %w", segment.ID, err)
	}
	return nil
}

func scheduleSegmentTimes(ctx *config.AppContext, segment *types.CompetitionScheduleSegment) (*time.Time, *time.Time, error) {
	confTalk, err := scheduleSegmentConfTalk(ctx, segment)
	if err != nil || confTalk == nil || confTalk.Sched == nil {
		return nil, nil, err
	}
	return &confTalk.Sched.Start, confTalk.Sched.End, nil
}

func scheduleSegmentConfTalk(ctx *config.AppContext, segment *types.CompetitionScheduleSegment) (*types.ConfTalk, error) {
	if segment == nil {
		return nil, nil
	}
	if strings.TrimSpace(segment.ConfTalkID) != "" {
		confTalk, err := GetConfTalkByID(ctx, segment.ConfTalkID)
		if err != nil || confTalk != nil {
			return confTalk, err
		}
	}
	if strings.TrimSpace(segment.ProposalID) != "" {
		return GetConfTalkByProposal(ctx, segment.ProposalID)
	}
	return nil, nil
}

func updatePlacedScheduleSegmentDuration(ctx *config.AppContext, confTalkID string, durationMinutes int) error {
	if strings.TrimSpace(confTalkID) == "" || durationMinutes <= 0 {
		return nil
	}
	confTalk, err := GetConfTalkByID(ctx, confTalkID)
	if err != nil {
		return err
	}
	if confTalk == nil || confTalk.Sched == nil || confTalk.Sched.End == nil || strings.TrimSpace(confTalk.Venue) == "" {
		return nil
	}
	return UpdateConfTalkSchedule(ctx, confTalk.ID, confTalk.Venue, confTalk.Sched.Start, confTalk.Sched.Start.Add(time.Duration(durationMinutes)*time.Minute))
}

func normalizeCompetitionScheduleSegmentInput(in CompetitionScheduleSegmentInput, index int) CompetitionScheduleSegmentInput {
	in.ID = strings.TrimSpace(in.ID)
	in.SegmentType = strings.TrimSpace(strings.ToLower(in.SegmentType))
	if in.SegmentType == "" {
		in.SegmentType = "custom"
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.DefaultDurationMinutes <= 0 {
		in.DefaultDurationMinutes = 30
	}
	in.Ordering = index
	return in
}

func competitionScheduleSegmentProposalTitle(competitionTitle, segmentTitle string) string {
	competitionTitle = strings.TrimSpace(competitionTitle)
	segmentTitle = strings.TrimSpace(segmentTitle)
	if competitionTitle == "" {
		return segmentTitle
	}
	if segmentTitle == "" {
		return competitionTitle
	}
	return competitionTitle + ": " + segmentTitle
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

func listCompetitionsPostgres(ctx *config.AppContext) ([]*types.HackathonCompetition, error) {
	return queryCompetitionsPostgres(ctx, "competitions", "", nil)
}

func queryCompetitionsPostgres(ctx *config.AppContext, label, whereSQL string, args []any) ([]*types.HackathonCompetition, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, coalesce(conference_id::text, ''), title, description, description_format,
			visibility, lifecycle_override, judging_mode, public_gallery_enabled, allow_late_submissions, public_tables_enabled, max_team_size,
			submissions_open_at, submissions_close_at, public_gallery_at,
			hacking_starts_at, hacking_ends_at, judges_meeting_at,
			expo_starts_at, expo_ends_at, expo_judging_starts_at, expo_judging_ends_at,
			finals_starts_at, finals_ends_at, finals_judging_starts_at,
			finals_judging_ends_at, awards_ceremony_at, results_finalized_at,
			coalesce(results_finalized_by::text, ''),
			coalesce((SELECT people.name FROM people WHERE people.id = competitions.results_finalized_by), ''),
			created_at, updated_at
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
