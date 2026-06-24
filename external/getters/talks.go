package getters

import (
	"fmt"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// listTalks loads every Talk-shaped row across all confs, sourced from the
// ConfTalk -> Proposal -> SpeakerConf[] -> Speaker[] chain. Talk.ID is the
// ConfTalk page ID in Notion and the conf_talks.id value in Postgres.
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
	if UsePostgresBackend(ctx) {
		return LoadTalksFromConfTalks(ctx, "")
	}
	return listTalks(ctx)
}

func ListTalksForConf(ctx *config.AppContext, event string) ([]*types.Talk, error) {
	if UsePostgresBackend(ctx) {
		return LoadTalksFromConfTalks(ctx, event)
	}
	talks, err := listTalks(ctx)
	if err != nil {
		return nil, err
	}
	var filtered []*types.Talk
	for _, talk := range talks {
		if talk.Event == event {
			filtered = append(filtered, talk)
		}
	}
	return filtered, nil
}

func GetTalk(ctx *config.AppContext, talkID string) (*types.Talk, error) {
	if UsePostgresBackend(ctx) {
		return LoadTalkFromConfTalk(ctx, talkID)
	}
	talks, err := listTalks(ctx)
	if err != nil {
		return nil, err
	}
	for _, talk := range talks {
		if talk.ID == talkID {
			return talk, nil
		}
	}
	return nil, fmt.Errorf("Talk %s not found", talkID)
}
