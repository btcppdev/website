package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

var confs []*types.Conf

func FetchConfsCached(ctx *config.AppContext) ([]*types.Conf, error) {
	return ListConfs(ctx)
}

func FetchSpeakersCached(ctx *config.AppContext) ([]*types.Speaker, error) {
	return ListSpeakers(ctx)
}

func InvalidateConfTalksCache() {}
