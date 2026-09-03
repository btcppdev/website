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

// GetConfTalkByProposal looks up the ConfTalk linked to a proposal.

// LoadTalkFromConfTalk returns a single Talk-shaped value built from the
// ConfTalk identified by confTalkID.

// resolveProposalSpeakers fills in Proposal.Speakers from SpeakerConfRefs using
// the supplied speakerConfMap. Unknown refs are silently skipped.
func resolveProposalSpeakers(p *types.Proposal, speakerConfMap map[string]*types.SpeakerConf) {
	if p == nil {
		return
	}
	p.Speakers = p.Speakers[:0]
	for _, ref := range p.SpeakerConfRefs {
		if sc, ok := speakerConfMap[ref]; ok {
			p.Speakers = append(p.Speakers, sc)
		}
	}
}

// talkFromConfTalk denormalizes a (ConfTalk, Proposal) pair plus the proposal's
// resolved Speakers list into the legacy *types.Talk shape used by templates,
// media generation, and social publishing.
func talkFromConfTalk(ctx *config.AppContext, ct *types.ConfTalk, proposal *types.Proposal) *types.Talk {
	talk := &types.Talk{
		ID:              ct.ID,
		Clipart:         ct.Clipart,
		Sched:           ct.Sched,
		Venue:           ct.Venue,
		Section:         ct.Section,
		CalNotif:        ct.CalNotif,
		TalkCardURL:     ct.SocialCard,
		GithubRepoURL:   ct.GithubRepoURL,
		SlidesURL:       ct.SlidesURL,
		SlidesObjectKey: ct.SlidesObjectKey,
	}
	if ct.Conf != nil {
		talk.Event = ct.Conf.Tag
	}
	if talk.Sched != nil {
		talk.TimeDesc = talk.Sched.Desc()
	}
	if proposal != nil {
		talk.Name = proposal.Title
		talk.Description = proposal.Description
		talk.Type = proposal.TalkType
		talk.Status = proposal.Status
		for _, sc := range proposal.Speakers {
			if sc == nil {
				continue
			}
			switch recordingEmojiForRecordOK(sc.RecordOK) {
			case "":
			case "🔇":
				talk.RecordingAudioOnly = true
			case "🛑":
				talk.RecordingRestricted = true
			}
			if sc.Speaker == nil {
				continue
			}
			view := *sc.Speaker
			view.Company = sc.Company
			view.OrgLogo = sc.OrgPhoto
			view.RecordingEmoji = recordingEmojiForRecordOK(sc.RecordOK)
			talk.Speakers = append(talk.Speakers, &view)
		}
	}
	return talk
}

func recordingEmojiForRecordOK(recordOK string) string {
	switch strings.ToLower(strings.TrimSpace(recordOK)) {
	case "", "recordok", "recordingok":
		return ""
	case "audioonly", "audio only":
		return "🔇"
	case "norecord", "norecording", "no recording", "noface", "no face":
		return "🛑"
	default:
		return ""
	}
}

// LoadTalksFromConfTalks returns Talk-shaped values populated from the new
// ConfTalk -> Proposal -> speakers chain for a given conf tag. Pass an empty
// string to load talks for every conf.
func LoadTalksFromConfTalks(ctx *config.AppContext, confTag string) ([]*types.Talk, error) {
	if confTag != "" {
		return loadTalksFromConfTalksForConf(ctx, confTag)
	}

	proposals, err := ListProposals(ctx)
	if err != nil {
		return nil, err
	}
	proposalMap := make(map[string]*types.Proposal, len(proposals))
	for _, p := range proposals {
		proposalMap[p.ID] = p
	}

	allConfTalks, err := ListConfTalks(ctx, proposalMap)
	if err != nil {
		return nil, err
	}
	confTalks := make([]*types.ConfTalk, 0, len(allConfTalks))
	for _, ct := range allConfTalks {
		if confTag == "" {
			confTalks = append(confTalks, ct)
			continue
		}
		if ct.Conf != nil && ct.Conf.Tag == confTag {
			confTalks = append(confTalks, ct)
		}
	}
	if len(confTalks) == 0 {
		return nil, nil
	}

	return talksFromConfTalks(ctx, confTalks, proposalMap)
}

func talksFromConfTalks(ctx *config.AppContext, confTalks []*types.ConfTalk, proposalMap map[string]*types.Proposal) ([]*types.Talk, error) {
	if len(confTalks) == 0 {
		return nil, nil
	}
	var speakerConfIDs []string
	for _, proposal := range proposalMap {
		if proposal != nil {
			speakerConfIDs = append(speakerConfIDs, proposal.SpeakerConfRefs...)
		}
	}
	sps, err := ListSpeakerConfsByIDs(ctx, speakerConfIDs, nil, proposalMap)
	if err != nil {
		return nil, err
	}
	speakerConfMap := make(map[string]*types.SpeakerConf, len(sps))
	for _, sc := range sps {
		speakerConfMap[sc.ID] = sc
	}

	for _, p := range proposalMap {
		resolveProposalSpeakers(p, speakerConfMap)
	}

	talks := make([]*types.Talk, 0, len(confTalks))
	confTalkIDs := make([]string, 0, len(confTalks))
	for _, ct := range confTalks {
		if ct != nil {
			confTalkIDs = append(confTalkIDs, ct.ID)
		}
	}
	recordings, err := ListRecordingsForConfTalks(ctx, confTalkIDs)
	if err != nil {
		return nil, err
	}
	recordingByTalk := make(map[string]*types.Recording, len(recordings))
	for _, recording := range recordings {
		if recording != nil {
			recordingByTalk[recording.ConfTalkID] = recording
		}
	}
	for _, ct := range confTalks {
		talk := talkFromConfTalk(ctx, ct, ct.Proposal)
		if recording := recordingByTalk[ct.ID]; recording != nil {
			talk.YTLink = recording.YTLink
			talk.RecordingID = recording.ID
		}
		talks = append(talks, talk)
	}
	return talks, nil
}

func CreateConfTalk(ctx *config.AppContext, in ConfTalkInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
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
			return existingID, nil
		}
		if err != pgx.ErrNoRows {
			return "", fmt.Errorf("lookup conf talk for proposal %q: %w", in.ProposalID, err)
		}
	}

	var confTalkID string
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
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
				return existingID, nil
			}
		}
		return "", fmt.Errorf("insert conf talk for proposal %q: %w", in.ProposalID, err)
	}
	return confTalkID, nil
}

func activeConfTalkIDForProposalPostgres(ctx *config.AppContext, proposalID string) (string, error) {
	var existingID string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
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

func ListConfTalks(ctx *config.AppContext, proposalMap map[string]*types.Proposal) ([]*types.ConfTalk, error) {
	return queryConfTalksPostgres(ctx, "", nil, proposalMap)
}

func ListConfTalksForConf(ctx *config.AppContext, confRef string, proposalMap map[string]*types.Proposal) ([]*types.ConfTalk, error) {
	if strings.TrimSpace(confRef) == "" {
		return nil, nil
	}
	return queryConfTalksPostgres(ctx, "WHERE conf_talks.conference_id = $1::uuid", []interface{}{confRef}, proposalMap)
}

func ListConfTalksByIDs(ctx *config.AppContext, ids []string) ([]*types.ConfTalk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return queryConfTalksPostgres(ctx, "WHERE conf_talks.id = ANY($1::uuid[])", []interface{}{ids}, nil)
}

// LatestConferenceTalkClipart returns the clipart on the most recently
// updated, non-archived talk for one event. Clipart uploads update the
// ConfTalk row, so this stays event-scoped instead of using the global Spaces
// upload order.
func LatestConferenceTalkClipart(ctx *config.AppContext, confRef string) (string, error) {
	if ctx == nil || ctx.DB == nil || strings.TrimSpace(confRef) == "" {
		return "", fmt.Errorf("conference clipart lookup is not configured")
	}
	var clipart string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT clipart_path
		FROM conf_talks
		WHERE conference_id = $1::uuid
			AND archived_at IS NULL
			AND btrim(clipart_path) <> ''
		ORDER BY updated_at DESC, created_at DESC, id DESC
		LIMIT 1
	`, strings.TrimSpace(confRef)).Scan(&clipart)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query latest conference talk clipart: %w", err)
	}
	return strings.TrimSpace(clipart), nil
}

// ListConferenceIDsWithPublicAgenda returns the conferences for which the
// public agenda can render at least one talk. It mirrors publicAgendaTalk's
// status rules without hydrating the full talk/speaker graph per conference.
func ListConferenceIDsWithPublicAgenda(ctx *config.AppContext) (map[string]bool, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT DISTINCT conf_talks.conference_id::text
		FROM conf_talks
		JOIN proposals ON proposals.id = conf_talks.proposal_id
		JOIN conferences ON conferences.id = conf_talks.conference_id
		WHERE conf_talks.archived_at IS NULL
			AND conf_talks.scheduled_start IS NOT NULL
			AND (
				proposals.status = 'Scheduled'
				OR (conferences.end_date IS NOT NULL AND conferences.end_date < now() AND proposals.status = 'Accepted')
			)
	`)
	if err != nil {
		return nil, fmt.Errorf("query conferences with public agendas: %w", err)
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var conferenceID string
		if err := rows.Scan(&conferenceID); err != nil {
			return nil, fmt.Errorf("scan conference with public agenda: %w", err)
		}
		out[conferenceID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conferences with public agendas: %w", err)
	}
	return out, nil
}

func GetConfTalkByProposal(ctx *config.AppContext, proposalID string) (*types.ConfTalk, error) {
	rows, err := queryConfTalksPostgres(ctx, "WHERE proposal_id = $1::uuid", []interface{}{proposalID}, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func GetConfTalkByID(ctx *config.AppContext, confTalkID string) (*types.ConfTalk, error) {
	rows, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.id = $1::uuid", []interface{}{confTalkID}, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func LoadTalkFromConfTalk(ctx *config.AppContext, confTalkID string) (*types.Talk, error) {
	rows, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.id = $1::uuid", []interface{}{confTalkID}, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("conf talk %s not found", confTalkID)
	}
	ct := rows[0]
	if ct.Proposal == nil {
		talk := talkFromConfTalk(ctx, ct, nil)
		if recording, err := GetRecordingByConfTalk(ctx, ct.ID); err != nil {
			return nil, err
		} else if recording != nil {
			talk.YTLink = recording.YTLink
			talk.RecordingID = recording.ID
		}
		return talk, nil
	}

	proposalMap := map[string]*types.Proposal{ct.Proposal.ID: ct.Proposal}
	sps, err := ListSpeakerConfsByIDs(ctx, ct.Proposal.SpeakerConfRefs, nil, proposalMap)
	if err != nil {
		return nil, err
	}
	speakerConfMap := make(map[string]*types.SpeakerConf, len(sps))
	for _, sc := range sps {
		speakerConfMap[sc.ID] = sc
	}
	resolveProposalSpeakers(ct.Proposal, speakerConfMap)
	talk := talkFromConfTalk(ctx, ct, ct.Proposal)
	if recording, err := GetRecordingByConfTalk(ctx, ct.ID); err != nil {
		return nil, err
	} else if recording != nil {
		talk.YTLink = recording.YTLink
		talk.RecordingID = recording.ID
	}
	return talk, nil
}

func loadTalksFromConfTalksForConf(ctx *config.AppContext, confTag string) ([]*types.Talk, error) {
	if strings.TrimSpace(confTag) == "" {
		proposals, err := ListProposals(ctx)
		if err != nil {
			return nil, err
		}
		proposalMap := make(map[string]*types.Proposal, len(proposals))
		for _, proposal := range proposals {
			if proposal != nil {
				proposalMap[proposal.ID] = proposal
			}
		}

		confTalks, err := queryConfTalksPostgres(ctx, "", nil, proposalMap)
		if err != nil {
			return nil, err
		}
		return talksFromConfTalks(ctx, confTalks, proposalMap)
	}

	conf, err := GetConfByTag(ctx, confTag)
	if err != nil {
		return nil, err
	}
	if conf == nil {
		return nil, nil
	}

	proposals, err := ListProposalsForConf(ctx, conf.Ref)
	if err != nil {
		return nil, err
	}
	proposalMap := make(map[string]*types.Proposal, len(proposals))
	for _, proposal := range proposals {
		if proposal != nil {
			proposalMap[proposal.ID] = proposal
		}
	}

	confTalks, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.conference_id = $1::uuid", []interface{}{conf.Ref}, proposalMap)
	if err != nil {
		return nil, err
	}
	return talksFromConfTalks(ctx, confTalks, proposalMap)
}

func queryConfTalksPostgres(ctx *config.AppContext, where string, args []interface{}, proposalMap map[string]*types.Proposal) ([]*types.ConfTalk, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
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

	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, conference_id::text, coalesce(proposal_id::text, ''),
			clipart_path, scheduled_start, scheduled_end, production_notes,
			venue, section, cal_notif, social_card_path,
			github_repo_url, slides_url, slides_object_key
		FROM conf_talks
		`+where+`
		ORDER BY scheduled_start NULLS LAST, id
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query conf talks: %w", err)
	}
	defer rows.Close()

	type confTalkRow struct {
		confTalk       *types.ConfTalk
		confID         string
		proposalID     string
		scheduledStart pgtype.Timestamptz
		scheduledEnd   pgtype.Timestamptz
	}
	var scanned []confTalkRow
	proposalIDs := make([]string, 0)
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
			&ct.GithubRepoURL,
			&ct.SlidesURL,
			&ct.SlidesObjectKey,
		); err != nil {
			return nil, fmt.Errorf("scan conf talk: %w", err)
		}
		if proposalMap == nil && proposalID != "" {
			proposalIDs = append(proposalIDs, proposalID)
		}
		scanned = append(scanned, confTalkRow{
			confTalk:       &ct,
			confID:         confID,
			proposalID:     proposalID,
			scheduledStart: scheduledStart,
			scheduledEnd:   scheduledEnd,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conf talks: %w", err)
	}
	rows.Close()

	if proposalMap == nil {
		proposals, err := ListProposalsByIDs(ctx, proposalIDs)
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

	out := make([]*types.ConfTalk, 0, len(scanned))
	for _, row := range scanned {
		ct := row.confTalk
		ct.Conf = confByID[row.confID]
		ct.Proposal = proposalMap[row.proposalID]
		if row.scheduledStart.Valid {
			start := confTalkTimeInConference(row.scheduledStart.Time, ct.Conf)
			ct.Sched = &types.Times{Start: start}
			if row.scheduledEnd.Valid {
				end := confTalkTimeInConference(row.scheduledEnd.Time, ct.Conf)
				ct.Sched.End = &end
			}
		}
		out = append(out, ct)
	}
	return out, nil
}

func UpdateConfTalkSchedule(ctx *config.AppContext, confTalkID, venue string, start, end time.Time) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	dbCtx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbCtx)
	if err != nil {
		return fmt.Errorf("begin conftalk schedule update: %w", err)
	}
	defer tx.Rollback(dbCtx)
	if _, err := lockConfTalkSchedule(dbCtx, tx, confTalkID); err != nil {
		return err
	}
	if err := updateConfTalkScheduleTx(dbCtx, tx, confTalkID, venue, start, end); err != nil {
		return err
	}
	if err := tx.Commit(dbCtx); err != nil {
		return fmt.Errorf("commit conftalk %s schedule: %w", confTalkID, err)
	}
	return nil
}

type ScheduleConflict string

const (
	ScheduleConflictNone    ScheduleConflict = ""
	ScheduleConflictVenue   ScheduleConflict = "venue"
	ScheduleConflictSpeaker ScheduleConflict = "speaker"
)

// UpdateConfTalkScheduleAtomic serializes schedule changes for one conference,
// checks the current committed schedule while holding that lock, and updates
// the talk in the same transaction. A competing request therefore cannot pass
// conflict validation using stale schedule data.
func UpdateConfTalkScheduleAtomic(ctx *config.AppContext, confTalkID, venue string, start, end time.Time) (ScheduleConflict, error) {
	if ctx == nil || ctx.DB == nil {
		return ScheduleConflictNone, fmt.Errorf("database is not configured")
	}
	dbCtx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbCtx)
	if err != nil {
		return ScheduleConflictNone, fmt.Errorf("begin atomic conftalk schedule update: %w", err)
	}
	defer tx.Rollback(dbCtx)

	conferenceID, err := lockConfTalkSchedule(dbCtx, tx, confTalkID)
	if err != nil {
		return ScheduleConflictNone, err
	}
	var venueConflict bool
	if err := tx.QueryRow(dbCtx, `
		SELECT EXISTS (
			SELECT 1
			FROM conf_talks
			WHERE conference_id = $1::uuid
				AND id <> $2::uuid
				AND archived_at IS NULL
				AND scheduled_start < $4
				AND scheduled_end > $3
				AND venue = $5
		)
	`, conferenceID, confTalkID, start, end, venue).Scan(&venueConflict); err != nil {
		return ScheduleConflictNone, fmt.Errorf("check conftalk venue collision: %w", err)
	}
	if venueConflict {
		return ScheduleConflictVenue, nil
	}

	var speakerConflict bool
	if err := tx.QueryRow(dbCtx, `
		SELECT EXISTS (
			SELECT 1
			FROM conf_talks AS existing
			JOIN proposals_speaker_confs AS existing_link ON existing_link.proposal_id = existing.proposal_id
			JOIN speaker_confs AS existing_speaker ON existing_speaker.id = existing_link.speaker_conf_id
			JOIN conf_talks AS target ON target.id = $2::uuid
			JOIN proposals_speaker_confs AS target_link ON target_link.proposal_id = target.proposal_id
			JOIN speaker_confs AS target_speaker ON target_speaker.id = target_link.speaker_conf_id
			WHERE existing.conference_id = $1::uuid
				AND existing.id <> target.id
				AND existing.archived_at IS NULL
				AND existing.scheduled_start < $4
				AND existing.scheduled_end > $3
				AND existing_speaker.speaker_id = target_speaker.speaker_id
		)
	`, conferenceID, confTalkID, start, end).Scan(&speakerConflict); err != nil {
		return ScheduleConflictNone, fmt.Errorf("check conftalk speaker collision: %w", err)
	}
	if speakerConflict {
		return ScheduleConflictSpeaker, nil
	}
	if err := updateConfTalkScheduleTx(dbCtx, tx, confTalkID, venue, start, end); err != nil {
		return ScheduleConflictNone, err
	}
	if err := tx.Commit(dbCtx); err != nil {
		return ScheduleConflictNone, fmt.Errorf("commit atomic conftalk %s schedule: %w", confTalkID, err)
	}
	return ScheduleConflictNone, nil
}

func lockConfTalkSchedule(dbCtx context.Context, tx pgx.Tx, confTalkID string) (string, error) {
	var conferenceID string
	if err := tx.QueryRow(dbCtx, `
		SELECT conference_id::text
		FROM conf_talks
		WHERE id = $1::uuid AND archived_at IS NULL
	`, confTalkID).Scan(&conferenceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("conf talk %s not found", confTalkID)
		}
		return "", fmt.Errorf("load conftalk %s conference: %w", confTalkID, err)
	}
	if _, err := tx.Exec(dbCtx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, conferenceID); err != nil {
		return "", fmt.Errorf("lock conference %s schedule: %w", conferenceID, err)
	}
	return conferenceID, nil
}

func updateConfTalkScheduleTx(dbCtx context.Context, tx pgx.Tx, confTalkID, venue string, start, end time.Time) error {
	commandTag, err := tx.Exec(dbCtx, `
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

func DeleteConfTalk(ctx *config.AppContext, confTalkID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
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

func ConfTalkSetSocialCard(ctx *config.AppContext, confTalkID, path string) error {
	return updateConfTalkStringPostgres(ctx, confTalkID, "social_card_path", path)
}

func ConfTalkSetClipart(ctx *config.AppContext, confTalkID, filename string) error {
	return updateConfTalkStringPostgres(ctx, confTalkID, "clipart_path", filename)
}

func TalkUpdateCalNotif(ctx *config.AppContext, talkID string, calnotif string) error {
	return updateConfTalkStringPostgres(ctx, talkID, "cal_notif", calnotif)
}

func UpdateConfTalkResources(ctx *config.AppContext, confTalkID, githubRepoURL, slidesURL, slidesObjectKey string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE conf_talks
		SET github_repo_url = $2,
			slides_url = $3,
			slides_object_key = $4
		WHERE id = $1
	`, confTalkID, strings.TrimSpace(githubRepoURL), strings.TrimSpace(slidesURL), strings.TrimSpace(slidesObjectKey))
	if err != nil {
		return fmt.Errorf("update conf talk %s resources: %w", confTalkID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conf talk %s not found", confTalkID)
	}
	return nil
}

func updateConfTalkStringPostgres(ctx *config.AppContext, confTalkID, column, value string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
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
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
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
