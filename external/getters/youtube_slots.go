package getters

import (
	"fmt"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

const YouTubePublishChannel = "youtube"

func ListYouTubePublishSlots(ctx *config.AppContext) ([]*types.YouTubePublishSlot, error) {
	if !UsePostgresBackend(ctx) {
		return nil, fmt.Errorf("YouTube publish slots require the postgres data backend")
	}
	return listYouTubePublishSlotsPostgres(ctx, YouTubePublishChannel)
}

func ReplaceYouTubePublishSlots(ctx *config.AppContext, slots []*types.YouTubePublishSlot) error {
	if !UsePostgresBackend(ctx) {
		return fmt.Errorf("YouTube publish slots require the postgres data backend")
	}
	return replaceYouTubePublishSlotsPostgres(ctx, YouTubePublishChannel, slots)
}
