package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// listTalks loads every Talk-shaped row across all confs, sourced from the
// ConfTalk -> Proposal -> SpeakerConf[] -> Speaker[] chain. Talk.ID is the
// conf_talks.id value.
//
// SpeakerConf joins handle speaker resolution internally.
func listTalks(ctx *config.AppContext) ([]*types.Talk, error) {
	talks, err := LoadTalksFromConfTalks(ctx, "")
	if err != nil {
		return nil, err
	}
	ctx.Infos.Printf("listTalks: loaded %d talks from conf talks", len(talks))
	return talks, nil
}

func GetTalksFor(ctx *config.AppContext, event string) ([]*types.Talk, error) {
	return ListTalksForConf(ctx, event)
}

func ListTalks(ctx *config.AppContext) ([]*types.Talk, error) {
	return LoadTalksFromConfTalks(ctx, "")
}

func ListTalksForConf(ctx *config.AppContext, event string) ([]*types.Talk, error) {
	return LoadTalksFromConfTalks(ctx, event)
}

func GetTalk(ctx *config.AppContext, talkID string) (*types.Talk, error) {
	return LoadTalkFromConfTalk(ctx, talkID)
}
