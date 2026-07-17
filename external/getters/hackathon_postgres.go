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
	JudgeTypeCoordinator                  = "coordinator"
	JudgeEventStatePending                = "pending"
	JudgeEventStateOpen                   = "open"
	JudgeEventStateClosed                 = "closed"
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
	if in.Slug == "" {
		return "", fmt.Errorf("competition slug is required")
	}
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
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO competitions (
			conference_id, slug, title, description, description_format, visibility, lifecycle_override, public_gallery_enabled, allow_late_submissions, public_tables_enabled, max_team_size,
			submissions_open_at, submissions_close_at, public_gallery_at
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
		RETURNING id::text
	`, in.ConferenceID, in.Slug, in.Title, in.Description, in.DescriptionFormat, in.Visibility, in.LifecycleOverride, in.PublicGalleryEnabled, in.AllowLateSubmissions, in.PublicTablesEnabled, in.MaxTeamSize,
		in.SubmissionsOpenAt, in.SubmissionsCloseAt, in.PublicGalleryAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert competition %q: %w", in.Slug, err)
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
	if in.Slug == "" {
		return fmt.Errorf("competition slug is required")
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE competitions
		SET conference_id = $2::uuid,
			slug = $3,
			title = $4,
			description = $5,
			description_format = $6,
			visibility = $7,
			lifecycle_override = $8,
			public_gallery_enabled = $9,
			allow_late_submissions = $10,
			public_tables_enabled = $11,
			max_team_size = $12,
			submissions_open_at = $13,
			submissions_close_at = $14,
			public_gallery_at = $15
		WHERE id = $1
	`, competitionID, in.ConferenceID, in.Slug, in.Title, in.Description, in.DescriptionFormat,
		in.Visibility, in.LifecycleOverride, in.PublicGalleryEnabled, in.AllowLateSubmissions, in.PublicTablesEnabled, in.MaxTeamSize, in.SubmissionsOpenAt, in.SubmissionsCloseAt,
		in.PublicGalleryAt)
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
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

func listCompetitionScheduleSegmentsPostgres(ctx *config.AppContext, competitionID string) ([]*types.CompetitionScheduleSegment, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("competition id is required")
	}
	rows, err := ctx.DB.Query(context.Background(), `
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
	rows, err := ctx.DB.Query(context.Background(), `
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
	row := ctx.DB.QueryRow(context.Background(), `
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
			if err := ctx.DB.QueryRow(context.Background(), `
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
		if _, err := ctx.DB.Exec(context.Background(), `
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
		if _, err := ctx.DB.Exec(context.Background(), `DELETE FROM judge_events WHERE schedule_segment_id = $1::uuid`, segment.ID); err != nil {
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
		if _, err := ctx.DB.Exec(context.Background(), `DELETE FROM competition_schedule_segments WHERE id = $1::uuid`, segment.ID); err != nil {
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
	_, err := ctx.DB.Exec(context.Background(), `
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
	_, err := ctx.DB.Exec(context.Background(), `
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
		_, err := ctx.DB.Exec(context.Background(), `DELETE FROM judge_events WHERE schedule_segment_id = $1::uuid`, segment.ID)
		return err
	}
	startsAt, endsAt, err := scheduleSegmentTimes(ctx, segment)
	if err != nil {
		return err
	}
	var existingID string
	err = ctx.DB.QueryRow(context.Background(), `
		SELECT id::text
		FROM judge_events
		WHERE schedule_segment_id = $1::uuid
	`, segment.ID).Scan(&existingID)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("lookup judge event for schedule segment %s: %w", segment.ID, err)
	}
	if existingID == "" {
		_, err = ctx.DB.Exec(context.Background(), `
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
	_, err = ctx.DB.Exec(context.Background(), `
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
		SELECT id::text, coalesce(conference_id::text, ''), slug, title, description, description_format,
			visibility, lifecycle_override, judging_mode, public_gallery_enabled, allow_late_submissions, public_tables_enabled, max_team_size, submissions_open_at, submissions_close_at,
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
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

func queryProjectsPostgres(ctx *config.AppContext, label, whereSQL string, args []any) ([]*types.HackathonProject, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	rows, err := ctx.DB.Query(context.Background(), `
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
		SELECT project_members.project_id::text, project_members.person_id::text,
			coalesce(people.name, ''), coalesce(people.email::text, ''),
			project_members.role, project_members.created_at
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
		if err := rows.Scan(&member.ProjectID, &member.PersonID, &member.Name, &member.Email, &member.Role, &member.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan project member %s: %w", projectID, err)
		}
		out = append(out, &member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project members %s: %w", projectID, err)
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
	if err := ctx.DB.QueryRow(context.Background(), `
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
	if invite.Email != "" {
		var personEmail string
		if err := tx.QueryRow(context.Background(), `
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

func createCompetitionJudgeInvitePostgres(ctx *config.AppContext, competitionID string, expiresAt *time.Time) (string, *types.CompetitionJudgeInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return "", nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return "", nil, fmt.Errorf("competition id is required")
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
	err = ctx.DB.QueryRow(context.Background(), `
		INSERT INTO competition_judge_invites (competition_id, token_hash, expires_at)
		VALUES ($1::uuid, $2, $3)
		RETURNING id::text, competition_id::text,
			coalesce(accepted_by_person_id::text, ''), accepted_at, expires_at, created_at
	`, competitionID, tokenHash, expiresAt).Scan(
		&invite.ID,
		&invite.CompetitionID,
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

	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return nil, fmt.Errorf("begin accept judge invite: %w", err)
	}
	defer tx.Rollback(context.Background())

	var invite types.CompetitionJudgeInvite
	var acceptedAt, expiresAt pgtype.Timestamptz
	err = tx.QueryRow(context.Background(), `
		SELECT id::text, competition_id::text,
			coalesce(accepted_by_person_id::text, ''), accepted_at, expires_at, created_at
		FROM competition_judge_invites
		WHERE token_hash = $1
	`, tokenHash).Scan(
		&invite.ID,
		&invite.CompetitionID,
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
	if invite.AcceptedAt != nil {
		return nil, fmt.Errorf("judge invite already accepted")
	}
	if invite.ExpiresAt != nil && time.Now().After(*invite.ExpiresAt) {
		return nil, fmt.Errorf("judge invite expired")
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO competition_judges (competition_id, person_id, judge_type)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (competition_id, person_id, judge_type) DO NOTHING
	`, invite.CompetitionID, personID, JudgeTypeCoordinator); err != nil {
		return nil, fmt.Errorf("accept judge invite %s add judge: %w", invite.ID, err)
	}
	now := time.Now()
	if _, err := tx.Exec(context.Background(), `
		UPDATE competition_judge_invites
		SET accepted_by_person_id = $2,
			accepted_at = $3
		WHERE id = $1
	`, invite.ID, personID, now); err != nil {
		return nil, fmt.Errorf("accept judge invite %s: %w", invite.ID, err)
	}
	invite.AcceptedByPersonID = personID
	invite.AcceptedAt = &now
	if err := tx.Commit(context.Background()); err != nil {
		return nil, fmt.Errorf("commit accept judge invite: %w", err)
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
	if viewer.Admin || viewer.Coordinator {
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
	rows, err := ctx.DB.Query(context.Background(), `
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
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
	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("begin update judge event state: %w", err)
	}
	defer tx.Rollback(context.Background())
	if state == JudgeEventStateOpen {
		if _, err := tx.Exec(context.Background(), `
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
	commandTag, err := tx.Exec(context.Background(), `
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
	if err := tx.Commit(context.Background()); err != nil {
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
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
		return fmt.Errorf("judge type must be expo, finals, or coordinator")
	}
	_, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO competition_judges (competition_id, person_id, judge_type)
		SELECT $1::uuid, $2::uuid, $3
		WHERE NOT EXISTS (
			SELECT 1
			FROM competition_judges
			WHERE competition_id = $1::uuid AND person_id = $2::uuid
		)
		ON CONFLICT (competition_id, person_id, judge_type) DO NOTHING
	`, competitionID, personID, judgeType)
	if err != nil {
		return fmt.Errorf("insert competition judge %s/%s/%s: %w", competitionID, personID, judgeType, err)
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
		return fmt.Errorf("judge type must be expo, finals, or coordinator")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
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
	rows, err := ctx.DB.Query(context.Background(), `
		WITH deduped AS (
			SELECT DISTINCT ON (competition_judges.person_id)
				competition_judges.competition_id,
				competition_judges.person_id,
				competition_judges.judge_type,
				competition_judges.created_at
			FROM competition_judges
			WHERE competition_judges.competition_id::text = $1
			ORDER BY competition_judges.person_id,
				(competition_judges.judge_type = 'coordinator') DESC,
				competition_judges.created_at
		)
		SELECT competition_judges.competition_id::text, competition_judges.person_id::text,
			coalesce(people.name, ''), coalesce(people.email::text, ''),
			coalesce(people.norm_photo_path, ''),
			competition_judges.judge_type, competition_judges.created_at
		FROM deduped competition_judges
		LEFT JOIN people ON people.id = competition_judges.person_id
		ORDER BY lower(people.name), people.id
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("query competition judges %s: %w", competitionID, err)
	}
	defer rows.Close()
	var out []*types.CompetitionJudge
	for rows.Next() {
		var judge types.CompetitionJudge
		if err := rows.Scan(&judge.CompetitionID, &judge.PersonID, &judge.Name, &judge.Email, &judge.Photo, &judge.JudgeType, &judge.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan competition judge %s: %w", competitionID, err)
		}
		out = append(out, &judge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate competition judges %s: %w", competitionID, err)
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
	scorecard, err := scanScorecard(ctx.DB.QueryRow(context.Background(), `
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
	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("begin scorecard rankings transaction: %w", err)
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(context.Background(), `
		DELETE FROM scorecards
		WHERE judge_event_id::text = $1 AND judge_person_id::text = $2
	`, in.JudgeEventID, in.JudgePersonID); err != nil {
		return fmt.Errorf("clear scorecard rankings: %w", err)
	}
	for _, ranking := range in.Rankings {
		if strings.TrimSpace(ranking.ProjectID) == "" || ranking.Rank <= 0 {
			continue
		}
		commandTag, err := tx.Exec(context.Background(), `
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
	if err := tx.Commit(context.Background()); err != nil {
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
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
	rows, err := ctx.DB.Query(context.Background(), `
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
	rows, err := ctx.DB.Query(context.Background(), `
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
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO awards (
			competition_id, title, description, photo_url, max_awardees, opt_in_required, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text
	`, in.CompetitionID, in.Title, in.Description, in.PhotoURL, in.MaxAwardees, in.OptInRequired, in.Status).Scan(&id)
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

	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("begin update award: %w", err)
	}
	defer tx.Rollback(context.Background())

	commandTag, err := tx.Exec(context.Background(), `
		UPDATE awards
		SET title = $3,
			description = $4,
			photo_url = $5,
			max_awardees = $6,
			opt_in_required = $7,
			status = $8
		WHERE id::text = $1
			AND competition_id::text = $2
			AND archived_at IS NULL
	`, awardID, in.CompetitionID, in.Title, in.Description, in.PhotoURL, in.MaxAwardees, in.OptInRequired, in.Status)
	if err != nil {
		return fmt.Errorf("update award %s: %w", awardID, err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("award %s not found", awardID)
	}
	if !in.OptInRequired {
		if _, err := tx.Exec(context.Background(), `
			DELETE FROM project_award_opt_ins
			WHERE award_id::text = $1
		`, awardID); err != nil {
			return fmt.Errorf("clear award opt-ins %s: %w", awardID, err)
		}
	}
	if err := tx.Commit(context.Background()); err != nil {
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
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
	commandTag, err := ctx.DB.Exec(context.Background(), `
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
	orderBy := "title, id"
	if archived {
		archivePredicate = "archived_at IS NOT NULL"
		orderBy = "archived_at DESC, title, id"
	}
	query := fmt.Sprintf(`
		SELECT id::text, competition_id::text, coalesce(sponsored_by_org_id::text, ''),
			title, description, photo_url, max_awardees, opt_in_required, status,
			created_at, updated_at, archived_at
		FROM awards
		WHERE competition_id::text = $1
			AND %s
		ORDER BY %s
	`, archivePredicate, orderBy)
	rows, err := ctx.DB.Query(context.Background(), query, competitionID)
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
	awardIDs = normalizedUniqueStrings(awardIDs)

	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("begin project award opt-ins: %w", err)
	}
	defer tx.Rollback(context.Background())

	var competitionID string
	if err := tx.QueryRow(context.Background(), `
		SELECT competition_id::text
		FROM projects
		WHERE id::text = $1
	`, projectID).Scan(&competitionID); err != nil {
		return fmt.Errorf("load project %s: %w", projectID, err)
	}
	if _, err := tx.Exec(context.Background(), `
		DELETE FROM project_award_opt_ins
		WHERE project_id::text = $1
	`, projectID); err != nil {
		return fmt.Errorf("clear project award opt-ins %s: %w", projectID, err)
	}
	for _, awardID := range awardIDs {
		commandTag, err := tx.Exec(context.Background(), `
			INSERT INTO project_award_opt_ins (project_id, award_id)
			SELECT $1, awards.id
			FROM awards
			WHERE awards.id::text = $2
				AND awards.competition_id::text = $3
				AND awards.opt_in_required
				AND awards.status = $4
				AND awards.archived_at IS NULL
		`, projectID, awardID, competitionID, AwardStatusAvailable)
		if err != nil {
			return fmt.Errorf("set project award opt-in %s/%s: %w", projectID, awardID, err)
		}
		if commandTag.RowsAffected() != 1 {
			return fmt.Errorf("award opt-in %s is not available for this project", awardID)
		}
	}
	if err := tx.Commit(context.Background()); err != nil {
		return fmt.Errorf("commit project award opt-ins: %w", err)
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
	rows, err := ctx.DB.Query(context.Background(), `
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
	rows, err := ctx.DB.Query(context.Background(), `
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
	in = normalizePrizeInput(in)
	if in.AwardID == "" {
		return "", fmt.Errorf("prize award id is required")
	}
	if in.Title == "" {
		return "", fmt.Errorf("prize title is required")
	}
	if in.PrizeType == "" {
		return "", fmt.Errorf("prize type is required")
	}
	var id string
	err := ctx.DB.QueryRow(context.Background(), `
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

func listPrizesForCompetitionPostgres(ctx *config.AppContext, competitionID string) ([]*types.Prize, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	competitionID = strings.TrimSpace(competitionID)
	if competitionID == "" {
		return nil, fmt.Errorf("prize competition id is required")
	}
	rows, err := ctx.DB.Query(context.Background(), `
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
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	awardID = strings.TrimSpace(awardID)
	projectID = strings.TrimSpace(projectID)
	if awardID == "" {
		return fmt.Errorf("award id is required")
	}
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("begin assign project award: %w", err)
	}
	defer tx.Rollback(context.Background())

	var awardCompetitionID string
	var maxAwardees sql.NullInt64
	if err := tx.QueryRow(context.Background(), `
		SELECT competition_id::text, max_awardees
		FROM awards
		WHERE id::text = $1
			AND archived_at IS NULL
		FOR UPDATE
	`, awardID).Scan(&awardCompetitionID, &maxAwardees); err != nil {
		return fmt.Errorf("load award %s: %w", awardID, err)
	}
	var projectCompetitionID string
	if err := tx.QueryRow(context.Background(), `
		SELECT competition_id::text
		FROM projects
		WHERE id::text = $1
	`, projectID).Scan(&projectCompetitionID); err != nil {
		return fmt.Errorf("load project %s: %w", projectID, err)
	}
	if projectCompetitionID != awardCompetitionID {
		return fmt.Errorf("project and award must belong to the same competition")
	}
	var alreadyAssigned bool
	if err := tx.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM project_awards
			WHERE award_id::text = $1 AND project_id::text = $2
		)
	`, awardID, projectID).Scan(&alreadyAssigned); err != nil {
		return fmt.Errorf("check project award %s/%s: %w", awardID, projectID, err)
	}
	if !alreadyAssigned && maxAwardees.Valid {
		var assignedCount int64
		if err := tx.QueryRow(context.Background(), `
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
		if _, err := tx.Exec(context.Background(), `
			INSERT INTO project_awards (project_id, award_id)
			VALUES ($1, $2)
		`, projectID, awardID); err != nil {
			return fmt.Errorf("assign project award %s/%s: %w", awardID, projectID, err)
		}
	}
	if _, err := tx.Exec(context.Background(), `
		UPDATE awards
		SET status = $2
		WHERE id::text = $1
	`, awardID, AwardStatusAwarded); err != nil {
		return fmt.Errorf("mark award awarded %s: %w", awardID, err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		return fmt.Errorf("commit assign project award: %w", err)
	}
	return nil
}

func removeProjectAwardPostgres(ctx *config.AppContext, awardID, projectID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	awardID = strings.TrimSpace(awardID)
	projectID = strings.TrimSpace(projectID)
	if awardID == "" {
		return fmt.Errorf("award id is required")
	}
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	tx, err := ctx.DB.Begin(context.Background())
	if err != nil {
		return fmt.Errorf("begin remove project award: %w", err)
	}
	defer tx.Rollback(context.Background())
	var lockedAwardID string
	if err := tx.QueryRow(context.Background(), `
		SELECT id::text
		FROM awards
		WHERE id::text = $1
			AND archived_at IS NULL
		FOR UPDATE
	`, awardID).Scan(&lockedAwardID); err != nil {
		return fmt.Errorf("lock award %s: %w", awardID, err)
	}
	commandTag, err := tx.Exec(context.Background(), `
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
	if err := tx.QueryRow(context.Background(), `
		SELECT count(*)
		FROM project_awards
		WHERE award_id::text = $1
	`, awardID).Scan(&remaining); err != nil {
		return fmt.Errorf("count remaining awardees %s: %w", awardID, err)
	}
	if remaining == 0 {
		if _, err := tx.Exec(context.Background(), `
			UPDATE awards
			SET status = $2
			WHERE id::text = $1 AND status = $3
		`, awardID, AwardStatusUnawarded, AwardStatusAwarded); err != nil {
			return fmt.Errorf("mark award unawarded %s: %w", awardID, err)
		}
	}
	if err := tx.Commit(context.Background()); err != nil {
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
	rows, err := ctx.DB.Query(context.Background(), `
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

func projectIsPublicPostgres(ctx *config.AppContext, project *types.HackathonProject) bool {
	if project == nil {
		return false
	}
	if project.Status == ProjectStatusCreated || project.Status == ProjectStatusHidden {
		return false
	}
	var publicGalleryEnabled bool
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT public_gallery_enabled
		FROM competitions
		WHERE id = $1
	`, project.CompetitionID).Scan(&publicGalleryEnabled); err != nil {
		return false
	}
	return publicGalleryEnabled
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
		&competition.DescriptionFormat,
		&competition.Visibility,
		&competition.LifecycleOverride,
		&competition.JudgingMode,
		&competition.PublicGalleryEnabled,
		&competition.AllowLateSubmissions,
		&competition.PublicTablesEnabled,
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
	competition.Visibility = normalizeCompetitionVisibility(competition.Visibility)
	competition.LifecycleOverride = normalizeCompetitionLifecycleOverride(competition.LifecycleOverride)
	competition.JudgingMode = normalizeCompetitionJudgingMode(competition.JudgingMode)
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
	var submittedAt pgtype.Timestamptz
	if err := rows.Scan(
		&project.ID,
		&project.CompetitionID,
		&project.CreatedByPersonID,
		&project.Slug,
		&project.Title,
		&project.ShortDescription,
		&project.Description,
		&project.DescriptionFormat,
		&project.ImageURL,
		&project.ImageURLs,
		&project.GitHubURL,
		&project.DemoURL,
		&project.VideoURL,
		&project.SlidesURL,
		&project.DocsURL,
		&projectNumber,
		&project.Status,
		&project.Tags,
		&submittedAt,
		&project.CreatedAt,
		&project.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if projectNumber.Valid {
		n := int(projectNumber.Int64)
		project.ProjectNumber = &n
	}
	project.Status = normalizeProjectStatus(project.Status)
	project.SubmittedAt = pgTimePtr(submittedAt)
	return &project, nil
}

type pgScanner interface {
	Scan(dest ...any) error
}

func scanCompetitionScheduleSegment(row pgScanner) (*types.CompetitionScheduleSegment, error) {
	var segment types.CompetitionScheduleSegment
	if err := row.Scan(
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
	return &segment, nil
}

func scanJudgeEvent(rows pgx.Rows) (*types.JudgeEvent, error) {
	var event types.JudgeEvent
	var startsAt, endsAt pgtype.Timestamptz
	var startingProjectNumber sql.NullInt64
	if err := rows.Scan(
		&event.ID,
		&event.CompetitionID,
		&event.ScheduleSegmentID,
		&event.Name,
		&event.PlaybookType,
		&event.State,
		&event.Ordering,
		&startsAt,
		&endsAt,
		&startingProjectNumber,
		&event.RankLimit,
		&event.CreatedAt,
		&event.UpdatedAt,
	); err != nil {
		return nil, err
	}
	event.StartsAt = pgTimePtr(startsAt)
	event.EndsAt = pgTimePtr(endsAt)
	event.State = normalizeJudgeEventState(event.State)
	if startingProjectNumber.Valid {
		n := int(startingProjectNumber.Int64)
		event.StartingProjectNumber = &n
	}
	event.PlaybookType = normalizeJudgeType(event.PlaybookType)
	if event.RankLimit <= 0 {
		event.RankLimit = 4
	}
	return &event, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanScorecard(row scanner) (*types.Scorecard, error) {
	var scorecard types.Scorecard
	var rank sql.NullInt64
	var submittedAt pgtype.Timestamptz
	if err := row.Scan(
		&scorecard.ID,
		&scorecard.JudgeEventID,
		&scorecard.ProjectID,
		&scorecard.JudgePersonID,
		&rank,
		&scorecard.Comments,
		&submittedAt,
		&scorecard.CreatedAt,
		&scorecard.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if rank.Valid {
		n := int(rank.Int64)
		scorecard.Rank = &n
	}
	scorecard.SubmittedAt = pgTimePtr(submittedAt)
	return &scorecard, nil
}

func scanAward(rows pgx.Rows) (*types.Award, error) {
	var award types.Award
	var maxAwardees sql.NullInt64
	var archivedAt pgtype.Timestamptz
	if err := rows.Scan(
		&award.ID,
		&award.CompetitionID,
		&award.SponsoredByOrgID,
		&award.Title,
		&award.Description,
		&award.PhotoURL,
		&maxAwardees,
		&award.OptInRequired,
		&award.Status,
		&award.CreatedAt,
		&award.UpdatedAt,
		&archivedAt,
	); err != nil {
		return nil, err
	}
	if maxAwardees.Valid {
		n := int(maxAwardees.Int64)
		award.MaxAwardees = &n
	}
	award.Status = normalizeAwardStatus(award.Status)
	award.ArchivedAt = pgTimePtr(archivedAt)
	return &award, nil
}

func scanPrize(rows pgx.Rows) (*types.Prize, error) {
	var prize types.Prize
	var poolPercentage pgtype.Numeric
	if err := rows.Scan(
		&prize.ID,
		&prize.AwardID,
		&prize.PrizeType,
		&prize.Title,
		&prize.Description,
		&prize.ValueText,
		&poolPercentage,
		&prize.PoolURL,
		&prize.Status,
		&prize.Comments,
		&prize.CreatedAt,
		&prize.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if poolPercentage.Valid {
		value, err := poolPercentage.Float64Value()
		if err != nil {
			return nil, err
		}
		n := value.Float64
		prize.PoolPercentage = &n
	}
	prize.PrizeType = normalizePrizeType(prize.PrizeType)
	prize.Status = normalizePrizeStatus(prize.Status)
	return &prize, nil
}

func scanProjectAward(rows pgx.Rows) (*types.ProjectAward, error) {
	var award types.ProjectAward
	var projectNumber sql.NullInt64
	if err := rows.Scan(
		&award.ProjectID,
		&award.AwardID,
		&award.ProjectTitle,
		&projectNumber,
		&award.AwardedAt,
	); err != nil {
		return nil, err
	}
	if projectNumber.Valid {
		n := int(projectNumber.Int64)
		award.ProjectNumber = &n
	}
	return &award, nil
}

func scanProjectAwardOptIns(rows pgx.Rows, label string) ([]*types.ProjectAwardOptIn, error) {
	var out []*types.ProjectAwardOptIn
	for rows.Next() {
		optIn, err := scanProjectAwardOptIn(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project award opt-in: %w", err)
		}
		out = append(out, optIn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project award opt-ins for %s: %w", label, err)
	}
	return out, nil
}

func scanProjectAwardOptIn(rows pgx.Rows) (*types.ProjectAwardOptIn, error) {
	var optIn types.ProjectAwardOptIn
	var projectNumber sql.NullInt64
	if err := rows.Scan(
		&optIn.ProjectID,
		&optIn.AwardID,
		&optIn.ProjectTitle,
		&projectNumber,
		&optIn.AwardTitle,
		&optIn.OptedInAt,
	); err != nil {
		return nil, err
	}
	if projectNumber.Valid {
		n := int(projectNumber.Int64)
		optIn.ProjectNumber = &n
	}
	return &optIn, nil
}

func normalizedUniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
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
	in.DescriptionFormat = normalizeCompetitionDescriptionFormat(in.DescriptionFormat)
	in.Visibility = normalizeCompetitionVisibility(in.Visibility)
	in.LifecycleOverride = normalizeCompetitionLifecycleOverride(in.LifecycleOverride)
	return in
}

func normalizeCompetitionDescriptionFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CompetitionDescriptionFormatPlain:
		return CompetitionDescriptionFormatPlain
	case CompetitionDescriptionFormatMarkdown:
		return CompetitionDescriptionFormatMarkdown
	case CompetitionDescriptionFormatHTML:
		return CompetitionDescriptionFormatHTML
	case "":
		return CompetitionDescriptionFormatMarkdown
	default:
		return ""
	}
}

func normalizeCompetitionVisibility(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "draft", "hidden":
		return CompetitionVisibilityHidden
	case "public", "published", "scheduled", "open", "submissions_closed", "judging", "closed":
		return CompetitionVisibilityPublic
	default:
		return ""
	}
}

func normalizeCompetitionLifecycleOverride(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto", "automatic":
		return CompetitionLifecycleAuto
	case CompetitionLifecycleUpcoming, "scheduled":
		return CompetitionLifecycleUpcoming
	case CompetitionLifecycleOpen:
		return CompetitionLifecycleOpen
	case CompetitionLifecycleSubmissionsClosed, "closed_to_submissions", "public_gallery", "submissions_public", "public", "gallery":
		return CompetitionLifecycleSubmissionsClosed
	case CompetitionLifecycleClosed:
		return CompetitionLifecycleClosed
	default:
		return CompetitionLifecycleAuto
	}
}

func normalizeCompetitionJudgingMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CompetitionJudgingModeManual:
		return CompetitionJudgingModeManual
	case CompetitionJudgingModeAutomatic:
		return CompetitionJudgingModeAutomatic
	default:
		return CompetitionJudgingModeAutomatic
	}
}

func normalizeProjectInput(in ProjectInput) ProjectInput {
	in.CompetitionID = strings.TrimSpace(in.CompetitionID)
	in.CreatedByPersonID = strings.TrimSpace(in.CreatedByPersonID)
	in.Slug = normalizeSlug(in.Slug)
	in.Title = strings.TrimSpace(in.Title)
	in.ShortDescription = strings.TrimSpace(in.ShortDescription)
	in.Description = strings.TrimSpace(in.Description)
	in.DescriptionFormat = normalizeCompetitionDescriptionFormat(in.DescriptionFormat)
	in.ImageURL = strings.TrimSpace(in.ImageURL)
	in.ImageURLs = normalizeURLList(in.ImageURLs)
	if in.ImageURL != "" {
		in.ImageURLs = normalizeURLList(append([]string{in.ImageURL}, in.ImageURLs...))
	}
	if in.ImageURL == "" && len(in.ImageURLs) > 0 {
		in.ImageURL = in.ImageURLs[0]
	}
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

func normalizeURLList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func normalizeProjectStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProjectStatusSubmitted:
		return ProjectStatusSubmitted
	case ProjectStatusHidden, "withdrawn", "disqualified":
		return ProjectStatusHidden
	case ProjectStatusAdvanced, "finalist":
		return ProjectStatusAdvanced
	default:
		return ProjectStatusCreated
	}
}

func normalizeJudgeEventInput(in JudgeEventInput) JudgeEventInput {
	in.CompetitionID = strings.TrimSpace(in.CompetitionID)
	in.Name = strings.TrimSpace(in.Name)
	in.PlaybookType = normalizeJudgeEventType(in.PlaybookType)
	if in.RankLimit <= 0 {
		in.RankLimit = 4
	}
	return in
}

func normalizeScorecardInput(in ScorecardInput) ScorecardInput {
	in.JudgeEventID = strings.TrimSpace(in.JudgeEventID)
	in.ProjectID = strings.TrimSpace(in.ProjectID)
	in.JudgePersonID = strings.TrimSpace(in.JudgePersonID)
	in.Comments = strings.TrimSpace(in.Comments)
	return in
}

func normalizeScorecardRankingsInput(in ScorecardRankingsInput) ScorecardRankingsInput {
	in.JudgeEventID = strings.TrimSpace(in.JudgeEventID)
	in.JudgePersonID = strings.TrimSpace(in.JudgePersonID)
	rankings := make([]ScorecardRankingInput, 0, len(in.Rankings))
	for _, ranking := range in.Rankings {
		ranking.ProjectID = strings.TrimSpace(ranking.ProjectID)
		if ranking.ProjectID != "" && ranking.Rank > 0 {
			rankings = append(rankings, ranking)
		}
	}
	in.Rankings = rankings
	return in
}

func normalizeAwardInput(in AwardInput) AwardInput {
	in.CompetitionID = strings.TrimSpace(in.CompetitionID)
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.PhotoURL = strings.TrimSpace(in.PhotoURL)
	in.Status = normalizeAwardStatus(in.Status)
	return in
}

func normalizePrizeInput(in PrizeInput) PrizeInput {
	in.AwardID = strings.TrimSpace(in.AwardID)
	in.PrizeType = normalizePrizeType(in.PrizeType)
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.ValueText = strings.TrimSpace(in.ValueText)
	in.PoolURL = strings.TrimSpace(in.PoolURL)
	in.Status = normalizePrizeStatus(in.Status)
	in.Comments = strings.TrimSpace(in.Comments)
	return in
}

func normalizeSlug(slug string) string {
	slug = strings.TrimSpace(strings.ToLower(slug))
	slug = strings.ReplaceAll(slug, " ", "-")
	return slug
}

func normalizeJudgeEventType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case JudgeTypeExpo:
		return JudgeTypeExpo
	case JudgeTypeFinals:
		return JudgeTypeFinals
	default:
		return ""
	}
}

func normalizeJudgeEventState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case JudgeEventStateOpen:
		return JudgeEventStateOpen
	case JudgeEventStateClosed, "review", "finalized", "skipped":
		return JudgeEventStateClosed
	default:
		return JudgeEventStatePending
	}
}

func normalizeJudgeType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case JudgeTypeExpo:
		return JudgeTypeExpo
	case JudgeTypeFinals:
		return JudgeTypeFinals
	case JudgeTypeCoordinator:
		return JudgeTypeCoordinator
	default:
		return ""
	}
}

func normalizeAwardStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AwardStatusAvailable:
		return AwardStatusAvailable
	case AwardStatusUnawarded:
		return AwardStatusUnawarded
	case AwardStatusAwarded:
		return AwardStatusAwarded
	default:
		return AwardStatusDraft
	}
}

func normalizePrizeType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case PrizeTypeSats:
		return PrizeTypeSats
	case PrizeTypeInKind:
		return PrizeTypeInKind
	case PrizeTypeTickets:
		return PrizeTypeTickets
	case PrizeTypePooled:
		return PrizeTypePooled
	case PrizeTypeTrophy:
		return PrizeTypeTrophy
	default:
		return ""
	}
}

func normalizePrizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case PrizeStatusNeedsFunds:
		return PrizeStatusNeedsFunds
	case PrizeStatusAwarded:
		return PrizeStatusAwarded
	case PrizeStatusPaid:
		return PrizeStatusPaid
	default:
		return PrizeStatusAvailable
	}
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
