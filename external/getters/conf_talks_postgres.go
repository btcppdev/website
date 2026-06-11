package getters

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func createConfTalkPostgres(ctx *config.AppContext, in ConfTalkInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	confID, err := proposalConferenceIDPostgres(ctx, in.ConfTag)
	if err != nil {
		return "", err
	}
	if confID == nil && strings.TrimSpace(in.ProposalID) != "" {
		confID, err = proposalConferenceIDForProposalPostgres(ctx, in.ProposalID)
		if err != nil {
			return "", err
		}
	}
	if confID == nil {
		return "", fmt.Errorf("CreateConfTalk: conference required")
	}

	proposalID := strings.TrimSpace(in.ProposalID)
	if proposalID != "" {
		existingID, err := activeConfTalkIDForProposalPostgres(ctx, proposalID)
		if err == nil {
			InvalidateConfTalksCache()
			return existingID, nil
		}
		if err != pgx.ErrNoRows {
			return "", fmt.Errorf("lookup conf talk for proposal %q: %w", in.ProposalID, err)
		}
	}

	var confTalkID string
	err = ctx.DB.QueryRow(context.Background(), `
		INSERT INTO conf_talks (conference_id, proposal_id)
		VALUES ($1, NULLIF($2, '')::uuid)
		ON CONFLICT (proposal_id, scheduled_start) DO UPDATE SET
			conference_id = EXCLUDED.conference_id
		RETURNING id::text
	`, *confID, proposalID).Scan(&confTalkID)
	if err != nil {
		var pgErr *pgconn.PgError
		if proposalID != "" && errors.As(err, &pgErr) && pgErr.Code == "23505" {
			existingID, lookupErr := activeConfTalkIDForProposalPostgres(ctx, proposalID)
			if lookupErr == nil {
				InvalidateConfTalksCache()
				return existingID, nil
			}
		}
		return "", fmt.Errorf("insert conf talk for proposal %q: %w", in.ProposalID, err)
	}
	return confTalkID, nil
}

func activeConfTalkIDForProposalPostgres(ctx *config.AppContext, proposalID string) (string, error) {
	var existingID string
	err := ctx.DB.QueryRow(context.Background(), `
		SELECT id::text
		FROM conf_talks
		WHERE proposal_id = $1::uuid
			AND archived_at IS NULL
		ORDER BY
			(cal_notif <> '') DESC,
			(scheduled_start IS NOT NULL) DESC,
			updated_at DESC,
			created_at DESC,
			id DESC
		LIMIT 1
	`, proposalID).Scan(&existingID)
	return existingID, err
}

func listConfTalksPostgres(ctx *config.AppContext, proposalMap map[string]*types.Proposal) ([]*types.ConfTalk, error) {
	return queryConfTalksPostgres(ctx, "", nil, proposalMap)
}

func getConfTalkByProposalPostgres(ctx *config.AppContext, proposalID string) (*types.ConfTalk, error) {
	rows, err := queryConfTalksPostgres(ctx, "WHERE proposal_id::text = $1", []interface{}{proposalID}, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func getConfTalkByIDPostgres(ctx *config.AppContext, confTalkID string) (*types.ConfTalk, error) {
	rows, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.id::text = $1", []interface{}{confTalkID}, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func loadTalkFromConfTalkPostgres(ctx *config.AppContext, confTalkID string) (*types.Talk, error) {
	rows, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.id::text = $1", []interface{}{confTalkID}, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("conf talk %s not found", confTalkID)
	}
	ct := rows[0]
	if ct.Proposal == nil {
		return talkFromConfTalk(ctx, ct, nil), nil
	}

	proposalMap := map[string]*types.Proposal{ct.Proposal.ID: ct.Proposal}
	sps, err := listSpeakerConfsPostgres(ctx, nil, proposalMap)
	if err != nil {
		return nil, err
	}
	speakerConfMap := make(map[string]*types.SpeakerConf, len(sps))
	for _, sc := range sps {
		speakerConfMap[sc.ID] = sc
	}
	resolveProposalSpeakers(ct.Proposal, speakerConfMap)
	return talkFromConfTalk(ctx, ct, ct.Proposal), nil
}

func loadTalksFromConfTalksForConfPostgres(ctx *config.AppContext, confTag string) ([]*types.Talk, error) {
	conf, err := getConferenceByTagPostgres(ctx, confTag)
	if err != nil {
		return nil, err
	}
	if conf == nil {
		return nil, nil
	}

	proposals, err := listProposalsForConfPostgres(ctx, conf.Ref)
	if err != nil {
		return nil, err
	}
	proposalMap := make(map[string]*types.Proposal, len(proposals))
	for _, proposal := range proposals {
		if proposal != nil {
			proposalMap[proposal.ID] = proposal
		}
	}

	confTalks, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.conference_id::text = $1", []interface{}{conf.Ref}, proposalMap)
	if err != nil {
		return nil, err
	}
	return talksFromConfTalks(ctx, confTalks, proposalMap)
}

func queryConfTalksPostgres(ctx *config.AppContext, where string, args []interface{}, proposalMap map[string]*types.Proposal) ([]*types.ConfTalk, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	if proposalMap == nil {
		proposals, err := listProposalsPostgres(ctx)
		if err != nil {
			return nil, err
		}
		proposalMap = make(map[string]*types.Proposal, len(proposals))
		for _, proposal := range proposals {
			if proposal != nil {
				proposalMap[proposal.ID] = proposal
			}
		}
	}

	confs, err := listConferencesOnlyPostgres(ctx)
	if err != nil {
		return nil, err
	}
	confByID := make(map[string]*types.Conf, len(confs))
	for _, conf := range confs {
		if conf != nil {
			confByID[conf.Ref] = conf
		}
	}

	if where == "" {
		where = "WHERE conf_talks.archived_at IS NULL"
	} else {
		where += " AND conf_talks.archived_at IS NULL"
	}

	rows, err := ctx.DB.Query(context.Background(), `
		SELECT id::text, conference_id::text, coalesce(proposal_id::text, ''),
			clipart_path, scheduled_start, scheduled_end, production_notes,
			venue, section, cal_notif, social_card_path
		FROM conf_talks
		`+where+`
		ORDER BY scheduled_start NULLS LAST, id
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query conf talks: %w", err)
	}
	defer rows.Close()

	var out []*types.ConfTalk
	for rows.Next() {
		var ct types.ConfTalk
		var confID string
		var proposalID string
		var scheduledStart pgtype.Timestamptz
		var scheduledEnd pgtype.Timestamptz
		if err := rows.Scan(
			&ct.ID,
			&confID,
			&proposalID,
			&ct.Clipart,
			&scheduledStart,
			&scheduledEnd,
			&ct.ProductionNotes,
			&ct.Venue,
			&ct.Section,
			&ct.CalNotif,
			&ct.SocialCard,
		); err != nil {
			return nil, fmt.Errorf("scan conf talk: %w", err)
		}
		ct.Conf = confByID[confID]
		ct.Proposal = proposalMap[proposalID]
		if scheduledStart.Valid {
			start := confTalkTimeInConference(scheduledStart.Time, ct.Conf)
			ct.Sched = &types.Times{Start: start}
			if scheduledEnd.Valid {
				end := confTalkTimeInConference(scheduledEnd.Time, ct.Conf)
				ct.Sched.End = &end
			}
		}
		out = append(out, &ct)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conf talks: %w", err)
	}
	return out, nil
}

func updateConfTalkSchedulePostgres(ctx *config.AppContext, confTalkID, venue string, start, end time.Time) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE conf_talks
		SET scheduled_start = $2,
			scheduled_end = $3,
			venue = CASE WHEN $4 = '' THEN venue ELSE $4 END
		WHERE id = $1
	`, confTalkID, start, end, venue)
	if err != nil {
		return fmt.Errorf("update conftalk %s schedule: %w", confTalkID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conf talk %s not found", confTalkID)
	}

	return nil
}

func confTalkTimeInConference(t time.Time, conf *types.Conf) time.Time {
	if conf == nil {
		return t
	}
	return t.In(conf.Loc())
}

func deleteConfTalkPostgres(ctx *config.AppContext, confTalkID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE conf_talks
		SET archived_at = now()
		WHERE id = $1
	`, confTalkID)
	if err != nil {
		return fmt.Errorf("archive conf talk %s: %w", confTalkID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conf talk %s not found", confTalkID)
	}

	return nil
}

func confTalkSetSocialCardPostgres(ctx *config.AppContext, confTalkID, path string) error {
	return updateConfTalkStringPostgres(ctx, confTalkID, "social_card_path", path)
}

func confTalkSetClipartPostgres(ctx *config.AppContext, confTalkID, filename string) error {
	return updateConfTalkStringPostgres(ctx, confTalkID, "clipart_path", filename)
}

func talkUpdateCalNotifPostgres(ctx *config.AppContext, talkID string, calnotif string) error {
	return updateConfTalkStringPostgres(ctx, talkID, "cal_notif", calnotif)
}

func updateConfTalkStringPostgres(ctx *config.AppContext, confTalkID, column, value string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE conf_talks
		SET `+column+` = $2
		WHERE id = $1
	`, confTalkID, value)
	if err != nil {
		return fmt.Errorf("update conf talk %s %s: %w", confTalkID, column, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conf talk %s not found", confTalkID)
	}
	return nil
}

func proposalConferenceIDForProposalPostgres(ctx *config.AppContext, proposalID string) (*string, error) {
	var id string
	err := ctx.DB.QueryRow(context.Background(), `
		SELECT conference_id::text
		FROM proposals
		WHERE id = $1
			AND conference_id IS NOT NULL
	`, proposalID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("proposal %q has no conference", proposalID)
		}
		return nil, fmt.Errorf("query proposal conference %q: %w", proposalID, err)
	}
	return &id, nil
}
