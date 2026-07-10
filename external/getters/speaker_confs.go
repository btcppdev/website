package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"time"
)

func FetchSpeakerConfsForSpeaker(ctx *config.AppContext, speakerID string) []*types.SpeakerConf {
	if speakerID == "" {
		return nil
	}
	speaker, err := FetchSpeakerByID(ctx, speakerID)
	if err != nil || speaker == nil {
		return nil
	}
	rows, err := listSpeakerConfsForSpeaker(ctx, speaker)
	if err != nil {
		return nil
	}
	return rows
}

func GetSpeakerConfByID(ctx *config.AppContext, id string) (*types.SpeakerConf, error) {
	return FetchSpeakerConfWithSpeaker(ctx, id)
}

// FetchSpeakerConfWithSpeaker reads a SpeakerConf by ID with its speaker
// relation resolved.

func UpsertSpeakerConf(ctx *config.AppContext, in SpeakerConfInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(in.SpeakerID) == "" {
		return "", fmt.Errorf("UpsertSpeakerConf: SpeakerID required")
	}

	existingID, err := findSpeakerConfForConfPostgres(ctx, in.SpeakerID, in.ConfTag)
	if err != nil {
		return "", fmt.Errorf("find speaker conf: %w", err)
	}
	if existingID != "" {
		if in.ProposalID != "" {
			if err := AddSpeakerConfToProposal(ctx, in.ProposalID, existingID); err != nil {
				return "", err
			}
		}
		return existingID, nil
	}

	availability := in.Availability
	if availability == nil {
		availability = []string{}
	}

	var speakerConfID string
	err = ctx.DB.QueryRow(context.Background(), `
		INSERT INTO speaker_confs (
			speaker_id, organization_id, coming_from, availability, record_ok,
			visa, first_event, dinner_rsvp, sponsor, company, org_photo_path
		) VALUES (
			$1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
		RETURNING id::text
	`, strings.TrimSpace(in.SpeakerID), strings.TrimSpace(in.OrgID), in.ComingFrom,
		availability, in.RecordOK, in.Visa, in.FirstEvent, in.DinnerRSVP,
		in.Sponsor, in.Company, in.OrgPhoto).Scan(&speakerConfID)
	if err != nil {
		return "", fmt.Errorf("insert speaker conf for speaker %q: %w", in.SpeakerID, err)
	}
	if in.ProposalID != "" {
		if err := AddSpeakerConfToProposal(ctx, in.ProposalID, speakerConfID); err != nil {
			return "", err
		}
	}
	if err := replaceSpeakerConfOtherEventsPostgres(ctx, speakerConfID, in.OtherEventTags); err != nil {
		return "", err
	}
	return speakerConfID, nil
}

func ListSpeakerConfs(ctx *config.AppContext, speakerMap map[string]*types.Speaker, proposalMap map[string]*types.Proposal) ([]*types.SpeakerConf, error) {
	return querySpeakerConfsPostgres(ctx, "", nil, speakerMap, proposalMap)
}

func listSpeakerConfsForSpeaker(ctx *config.AppContext, speaker *types.Speaker) ([]*types.SpeakerConf, error) {
	if speaker == nil || strings.TrimSpace(speaker.ID) == "" {
		return nil, nil
	}
	speakerMap := map[string]*types.Speaker{speaker.ID: speaker}
	return querySpeakerConfsPostgres(ctx, "WHERE speaker_confs.speaker_id::text = $1", []interface{}{speaker.ID}, speakerMap, nil)
}

func FetchSpeakerConfWithSpeaker(ctx *config.AppContext, speakerConfID string) (*types.SpeakerConf, error) {
	scs, err := querySpeakerConfsPostgres(ctx, "WHERE speaker_confs.id::text = $1", []interface{}{speakerConfID}, nil, nil)
	if err != nil {
		return nil, err
	}
	if len(scs) == 0 {
		return nil, nil
	}
	return scs[0], nil
}

func GetSpeakerConfsByEmail(ctx *config.AppContext, email string) ([]*types.Speaker, []*types.SpeakerConf, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil, nil
	}
	if ctx == nil || ctx.DB == nil {
		return nil, nil, fmt.Errorf("database is not configured")
	}

	speakers, err := GetSpeakersByEmail(ctx, email)
	if err != nil {
		return nil, nil, fmt.Errorf("speakers by email: %w", err)
	}
	speakerMap := make(map[string]*types.Speaker)
	ids := []string{}
	for _, sp := range speakers {
		if sp == nil {
			continue
		}
		speakerMap[sp.ID] = sp
		ids = append(ids, sp.ID)
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}

	proposals, err := ListProposals(ctx)
	if err != nil {
		return nil, nil, err
	}
	proposalMap := make(map[string]*types.Proposal, len(proposals))
	for _, proposal := range proposals {
		if proposal != nil {
			proposalMap[proposal.ID] = proposal
		}
	}

	scs, err := querySpeakerConfsPostgres(ctx, "WHERE speaker_confs.speaker_id::text = ANY($1::text[])", []interface{}{ids}, speakerMap, proposalMap)
	if err != nil {
		return nil, nil, err
	}
	outSpeakers := make([]*types.Speaker, 0, len(ids))
	for _, id := range ids {
		outSpeakers = append(outSpeakers, speakerMap[id])
	}
	return outSpeakers, scs, nil
}

func querySpeakerConfsPostgres(ctx *config.AppContext, where string, args []interface{}, speakerMap map[string]*types.Speaker, proposalMap map[string]*types.Proposal) ([]*types.SpeakerConf, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if speakerMap == nil {
		speakers, err := ListSpeakers(ctx)
		if err != nil {
			return nil, err
		}
		speakerMap = make(map[string]*types.Speaker, len(speakers))
		for _, speaker := range speakers {
			if speaker != nil {
				speakerMap[speaker.ID] = speaker
			}
		}
	}
	if proposalMap == nil {
		proposals, err := ListProposals(ctx)
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

	rows, err := ctx.DB.Query(context.Background(), `
		SELECT id::text, speaker_id::text, coming_from, availability, record_ok,
			visa, first_event, dinner_rsvp, sponsor, company, org_photo_path,
			invited_at, viewed_at, accepted_at, COALESCE(featured_rank, 0)
		FROM speaker_confs
		`+where+`
		ORDER BY created_at DESC, id
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query speaker confs: %w", err)
	}
	defer rows.Close()

	var out []*types.SpeakerConf
	byID := map[string]*types.SpeakerConf{}
	ids := []string{}
	for rows.Next() {
		var sc types.SpeakerConf
		var speakerID string
		var invitedAt pgtype.Timestamptz
		var viewedAt pgtype.Timestamptz
		var acceptedAt pgtype.Timestamptz
		if err := rows.Scan(
			&sc.ID,
			&speakerID,
			&sc.ComingFrom,
			&sc.Availability,
			&sc.RecordOK,
			&sc.Visa,
			&sc.FirstEvent,
			&sc.DinnerRSVP,
			&sc.Sponsor,
			&sc.Company,
			&sc.OrgPhoto,
			&invitedAt,
			&viewedAt,
			&acceptedAt,
			&sc.FeaturedRank,
		); err != nil {
			return nil, fmt.Errorf("scan speaker conf: %w", err)
		}
		sc.Speaker = speakerMap[speakerID]
		if invitedAt.Valid {
			sc.InvitedAt = &invitedAt.Time
		}
		if viewedAt.Valid {
			sc.ViewedAt = &viewedAt.Time
		}
		if acceptedAt.Valid {
			sc.AcceptedAt = &acceptedAt.Time
		}
		out = append(out, &sc)
		byID[sc.ID] = &sc
		ids = append(ids, sc.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate speaker confs: %w", err)
	}
	rows.Close()

	if err := hydrateSpeakerConfProposalsPostgres(ctx, ids, byID, proposalMap); err != nil {
		return nil, err
	}
	if err := hydrateSpeakerConfOtherEventsPostgres(ctx, ids, byID); err != nil {
		return nil, err
	}
	return out, nil
}

func hydrateSpeakerConfProposalsPostgres(ctx *config.AppContext, ids []string, byID map[string]*types.SpeakerConf, proposalMap map[string]*types.Proposal) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT speaker_conf_id::text, proposal_id::text
		FROM proposals_speaker_confs
		WHERE speaker_conf_id::text = ANY($1::text[])
	`, ids)
	if err != nil {
		return fmt.Errorf("query speaker-conf proposal links: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var speakerConfID string
		var proposalID string
		if err := rows.Scan(&speakerConfID, &proposalID); err != nil {
			return fmt.Errorf("scan speaker-conf proposal link: %w", err)
		}
		if sc := byID[speakerConfID]; sc != nil {
			if proposal := proposalMap[proposalID]; proposal != nil {
				sc.Proposals = append(sc.Proposals, proposal)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate speaker-conf proposal links: %w", err)
	}
	return nil
}

func hydrateSpeakerConfOtherEventsPostgres(ctx *config.AppContext, ids []string, byID map[string]*types.SpeakerConf) error {
	if len(ids) == 0 {
		return nil
	}
	confs, err := listConferencesOnlyPostgres(ctx)
	if err != nil {
		return err
	}
	confByID := make(map[string]*types.Conf, len(confs))
	for _, conf := range confs {
		if conf != nil {
			confByID[conf.Ref] = conf
		}
	}

	rows, err := ctx.DB.Query(context.Background(), `
		SELECT speaker_conf_id::text, conference_id::text
		FROM speaker_confs_conferences
		WHERE speaker_conf_id::text = ANY($1::text[])
	`, ids)
	if err != nil {
		return fmt.Errorf("query speaker-conf other events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var speakerConfID string
		var confID string
		if err := rows.Scan(&speakerConfID, &confID); err != nil {
			return fmt.Errorf("scan speaker-conf other event: %w", err)
		}
		if sc := byID[speakerConfID]; sc != nil {
			if conf := confByID[confID]; conf != nil {
				sc.OtherEvents = append(sc.OtherEvents, conf)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate speaker-conf other events: %w", err)
	}
	return nil
}

func UpdateSpeakerConf(ctx *config.AppContext, speakerConfID string, in SpeakerConfFields) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(speakerConfID) == "" {
		return fmt.Errorf("UpdateSpeakerConf: empty speakerConfID")
	}

	setParts := []string{
		"first_event = $2",
		"dinner_rsvp = $3",
		"sponsor = $4",
	}
	args := []interface{}{speakerConfID, in.FirstEvent, in.DinnerRSVP, in.Sponsor}
	addSet := func(column string, value interface{}) {
		args = append(args, value)
		setParts = append(setParts, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if in.ComingFrom != "" {
		addSet("coming_from", in.ComingFrom)
	}
	if in.Company != "" {
		addSet("company", in.Company)
	}
	if in.RecordOK != "" {
		addSet("record_ok", in.RecordOK)
	}
	if in.Visa != "" {
		addSet("visa", in.Visa)
	}
	if in.Availability != nil {
		addSet("availability", in.Availability)
	}
	if in.OrgPhoto != "" {
		addSet("org_photo_path", in.OrgPhoto)
	}
	if in.OrgID != "" {
		args = append(args, in.OrgID)
		setParts = append(setParts, fmt.Sprintf("organization_id = NULLIF($%d, '')::uuid", len(args)))
	}
	if in.FeaturedRank != nil {
		rank := *in.FeaturedRank
		if rank < 0 || rank > 6 {
			return fmt.Errorf("featured rank must be between 1 and 6, or blank")
		}
		if rank == 0 {
			setParts = append(setParts, "featured_rank = NULL")
		} else {
			addSet("featured_rank", rank)
		}
	}

	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE speaker_confs
		SET `+strings.Join(setParts, ", ")+`
		WHERE id = $1
	`, args...)
	if err != nil {
		return fmt.Errorf("update speaker conf %s: %w", speakerConfID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("speaker conf %s not found", speakerConfID)
	}
	return nil
}

func UpdateSpeakerConfFeaturedRank(ctx *config.AppContext, speakerConfID string, rank int) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(speakerConfID) == "" {
		return fmt.Errorf("UpdateSpeakerConfFeaturedRank: empty speakerConfID")
	}
	if rank < 0 || rank > 6 {
		return fmt.Errorf("featured rank must be between 1 and 6, or blank")
	}

	var (
		commandTag pgconn.CommandTag
		err        error
	)
	if rank == 0 {
		commandTag, err = ctx.DB.Exec(context.Background(), `
			UPDATE speaker_confs
			SET featured_rank = NULL
			WHERE id = $1
		`, speakerConfID)
	} else {
		commandTag, err = ctx.DB.Exec(context.Background(), `
			UPDATE speaker_confs
			SET featured_rank = $2
			WHERE id = $1
		`, speakerConfID, rank)
	}
	if err != nil {
		return fmt.Errorf("update speaker conf featured rank %s: %w", speakerConfID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("speaker conf %s not found", speakerConfID)
	}
	return nil
}

func AddSpeakerConfToProposal(ctx *config.AppContext, proposalID, speakerConfID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO proposals_speaker_confs (proposal_id, speaker_conf_id)
		VALUES ($1, $2)
		ON CONFLICT (proposal_id, speaker_conf_id) DO NOTHING
	`, proposalID, speakerConfID); err != nil {
		return fmt.Errorf("link speaker conf %s to proposal %s: %w", speakerConfID, proposalID, err)
	}
	return nil
}

func RemoveProposalFromSpeakerConf(ctx *config.AppContext, speakerConfID, proposalID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		DELETE FROM proposals_speaker_confs
		WHERE proposal_id = $1
			AND speaker_conf_id = $2
	`, proposalID, speakerConfID); err != nil {
		return fmt.Errorf("unlink speaker conf %s from proposal %s: %w", speakerConfID, proposalID, err)
	}
	return nil
}

func setSpeakerConfDate(ctx *config.AppContext, speakerConfID, column string, when time.Time, onlyIfEmpty bool) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	query := `
		UPDATE speaker_confs
		SET ` + column + ` = $2
		WHERE id = $1
	`
	if onlyIfEmpty {
		query += ` AND ` + column + ` IS NULL`
	}
	commandTag, err := ctx.DB.Exec(context.Background(), query, speakerConfID, when)
	if err != nil {
		return fmt.Errorf("set %s on speakerconf %s: %w", column, speakerConfID, err)
	}
	if commandTag.RowsAffected() == 0 && !onlyIfEmpty {
		return fmt.Errorf("speaker conf %s not found", speakerConfID)
	}
	return nil
}

func findSpeakerConfForConfPostgres(ctx *config.AppContext, speakerID, confTag string) (string, error) {
	if strings.TrimSpace(confTag) == "" {
		return "", nil
	}
	var speakerConfID string
	err := ctx.DB.QueryRow(context.Background(), `
		SELECT sc.id::text
		FROM speaker_confs sc
		JOIN proposals_speaker_confs psc ON psc.speaker_conf_id = sc.id
		JOIN proposals p ON p.id = psc.proposal_id
		JOIN conferences c ON c.id = p.conference_id
		WHERE sc.speaker_id = $1
			AND c.tag = $2
		ORDER BY sc.created_at DESC
		LIMIT 1
	`, speakerID, confTag).Scan(&speakerConfID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return speakerConfID, nil
}

func replaceSpeakerConfOtherEventsPostgres(ctx *config.AppContext, speakerConfID string, confTags []string) error {
	if _, err := ctx.DB.Exec(context.Background(), `
		DELETE FROM speaker_confs_conferences
		WHERE speaker_conf_id = $1
	`, speakerConfID); err != nil {
		return fmt.Errorf("clear speaker conf other events %s: %w", speakerConfID, err)
	}
	for _, tag := range confTags {
		confID, err := proposalConferenceIDPostgres(ctx, tag)
		if err != nil {
			return err
		}
		if confID == nil {
			continue
		}
		if _, err := ctx.DB.Exec(context.Background(), `
			INSERT INTO speaker_confs_conferences (speaker_conf_id, conference_id)
			VALUES ($1, $2)
			ON CONFLICT (speaker_conf_id, conference_id) DO NOTHING
		`, speakerConfID, *confID); err != nil {
			return fmt.Errorf("insert speaker conf other event %s/%s: %w", speakerConfID, tag, err)
		}
	}
	return nil
}
