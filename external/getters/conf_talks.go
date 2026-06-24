package getters

import (
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func ListConfTalks(ctx *config.AppContext, proposalMap map[string]*types.Proposal) ([]*types.ConfTalk, error) {
	if UsePostgresBackend(ctx) {
		return listConfTalksPostgres(ctx, proposalMap)
	}
	return listConfTalksNotion(ctx, proposalMap)
}

func GetConfTalkByID(ctx *config.AppContext, id string) (*types.ConfTalk, error) {
	if UsePostgresBackend(ctx) {
		return getConfTalkByIDPostgres(ctx, id)
	}
	return getConfTalkByIDNotion(ctx, id)
}

// GetConfTalkByProposal looks up the ConfTalk linked to a proposal.
func GetConfTalkByProposal(ctx *config.AppContext, proposalID string) (*types.ConfTalk, error) {
	if UsePostgresBackend(ctx) {
		return getConfTalkByProposalPostgres(ctx, proposalID)
	}

	return getConfTalkByProposalNotion(ctx, proposalID)
}

// LoadTalkFromConfTalk returns a single Talk-shaped value built from the
// ConfTalk identified by confTalkID.
func LoadTalkFromConfTalk(ctx *config.AppContext, confTalkID string) (*types.Talk, error) {
	if UsePostgresBackend(ctx) {
		return loadTalkFromConfTalkPostgres(ctx, confTalkID)
	}
	return loadTalkFromConfTalkNotion(ctx, confTalkID)
}

func TalkUpdateCalNotif(ctx *config.AppContext, talkID string, calnotif string) error {
	if UsePostgresBackend(ctx) {
		return talkUpdateCalNotifPostgres(ctx, talkID, calnotif)
	}
	return talkUpdateCalNotifNotion(ctx.Notion, talkID, calnotif)
}

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
		ID:          ct.ID,
		Clipart:     ct.Clipart,
		Sched:       ct.Sched,
		Venue:       ct.Venue,
		Section:     ct.Section,
		CalNotif:    ct.CalNotif,
		TalkCardURL: ct.SocialCard,
	}
	if ct.Conf != nil {
		talk.Event = ct.Conf.Tag
	}
	if talk.Sched != nil {
		talk.TimeDesc = talk.Sched.Desc()
	}
	if rec, err := GetRecordingByConfTalk(ctx, ct.ID); err == nil && rec != nil {
		talk.YTLink = rec.YTLink
	} else if err != nil && ctx != nil && ctx.Err != nil {
		ctx.Err.Printf("talk %s recording lookup: %s", ct.ID, err)
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
	if UsePostgresBackend(ctx) && confTag != "" {
		return loadTalksFromConfTalksForConfPostgres(ctx, confTag)
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
	speakers, err := listSpeakersForBackend(ctx)
	if err != nil {
		return nil, err
	}
	speakerMap := make(map[string]*types.Speaker, len(speakers))
	for _, sp := range speakers {
		speakerMap[sp.ID] = sp
	}

	sps, err := ListSpeakerConfs(ctx, speakerMap, proposalMap)
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
	for _, ct := range confTalks {
		talks = append(talks, talkFromConfTalk(ctx, ct, ct.Proposal))
	}
	return talks, nil
}

func listSpeakersForBackend(ctx *config.AppContext) ([]*types.Speaker, error) {
	if UsePostgresBackend(ctx) {
		return listSpeakersPostgres(ctx)
	}
	return ListSpeakersNotion(ctx.Notion)
}
