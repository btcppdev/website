package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"time"
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
	speakers, err := ListSpeakers(ctx)
	if err != nil {
		return nil, err
	}
	speakerMap := make(map[string]*types.Speaker, len(speakers))
	for _, sp := range speakers {
		speakerMap[sp.ID] = sp
	}

	var speakerConfIDs []string
	for _, proposal := range proposalMap {
		if proposal != nil {
			speakerConfIDs = append(speakerConfIDs, proposal.SpeakerConfRefs...)
		}
	}
	sps, err := ListSpeakerConfsByIDs(ctx, speakerConfIDs, speakerMap, proposalMap)
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
	return queryConfTalksPostgres(ctx, "WHERE conf_talks.conference_id::text = $1", []interface{}{confRef}, proposalMap)
}

func GetConfTalkByProposal(ctx *config.AppContext, proposalID string) (*types.ConfTalk, error) {
	rows, err := queryConfTalksPostgres(ctx, "WHERE proposal_id::text = $1", []interface{}{proposalID}, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func GetConfTalkByID(ctx *config.AppContext, confTalkID string) (*types.ConfTalk, error) {
	rows, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.id::text = $1", []interface{}{confTalkID}, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func LoadTalkFromConfTalk(ctx *config.AppContext, confTalkID string) (*types.Talk, error) {
	rows, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.id::text = $1", []interface{}{confTalkID}, nil)
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
		}
		return talk, nil
	}

	proposalMap := map[string]*types.Proposal{ct.Proposal.ID: ct.Proposal}
	sps, err := ListSpeakerConfs(ctx, nil, proposalMap)
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

	confTalks, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.conference_id::text = $1", []interface{}{conf.Ref}, proposalMap)
	if err != nil {
		return nil, err
	}
	return talksFromConfTalks(ctx, confTalks, proposalMap)
}

func queryConfTalksPostgres(ctx *config.AppContext, where string, args []interface{}, proposalMap map[string]*types.Proposal) ([]*types.ConfTalk, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
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
			&ct.GithubRepoURL,
			&ct.SlidesURL,
			&ct.SlidesObjectKey,
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

func UpdateConfTalkSchedule(ctx *config.AppContext, confTalkID, venue string, start, end time.Time) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
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
