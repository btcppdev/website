package getters

import (
	"context"
	"fmt"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	notion "github.com/niftynei/go-notion"
)

func fetchSpeakerConfWithSpeakerNotion(ctx *config.AppContext, speakerConfID string) (*types.SpeakerConf, error) {
	page, err := ctx.Notion.Client.RetrievePage(context.Background(), speakerConfID)
	if err != nil {
		return nil, fmt.Errorf("retrieve speakerconf %s: %w", speakerConfID, err)
	}
	speakerID := parseRef(page.Properties, "speaker")
	if speakerID == "" {
		return nil, nil
	}
	spPage, err := ctx.Notion.Client.RetrievePage(context.Background(), speakerID)
	if err != nil {
		return nil, fmt.Errorf("retrieve speaker %s: %w", speakerID, err)
	}
	speaker := parseSpeaker(spPage.ID, spPage.Properties)
	speakerMap := map[string]*types.Speaker{speakerID: speaker}
	return parseSpeakerConf(ctx, page.ID, page.Properties, speakerMap, nil), nil
}

func listSpeakerConfsForSpeakerNotion(ctx *config.AppContext, speaker *types.Speaker) ([]*types.SpeakerConf, error) {
	if speaker == nil || speaker.ID == "" {
		return nil, nil
	}
	pages, _, _, err := ctx.Notion.Client.QueryDatabase(context.Background(),
		ctx.Notion.Config.SpeakerConfDb, notion.QueryDatabaseParam{
			Filter: &notion.Filter{
				Property: "speaker",
				Relation: &notion.RelationFilterCondition{Contains: speaker.ID},
			},
		})
	if err != nil {
		return nil, fmt.Errorf("query speaker confs for speaker %s: %w", speaker.ID, err)
	}
	speakerMap := map[string]*types.Speaker{speaker.ID: speaker}
	out := make([]*types.SpeakerConf, 0, len(pages))
	for _, page := range pages {
		out = append(out, parseSpeakerConf(ctx, page.ID, page.Properties, speakerMap, nil))
	}
	return out, nil
}
