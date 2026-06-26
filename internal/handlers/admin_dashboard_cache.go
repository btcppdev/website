package handlers

import (
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func loadAdminDashboardProposalSnapshot(ctx *config.AppContext, conf *types.Conf) (pendingCount int, decisionedCount int, proposals []*types.Proposal, ok bool) {
	proposals = loadConfProposals(ctx, conf)
	pending, decisioned := splitProposalsByPending(proposals)
	return len(pending), len(decisioned), proposals, true
}

func loadAdminDashboardCountdown(ctx *config.AppContext, conf *types.Conf) (start *time.Time, end *time.Time, ok bool) {
	if conf == nil {
		return nil, nil, false
	}
	confCopy := *conf
	var infosByDay map[int]*types.ConfInfo
	cis, err := getters.ListConfInfos(ctx, conf.Tag)
	if err != nil {
		if ctx != nil && ctx.Err != nil {
			ctx.Err.Printf("/%s/admin countdown: %s", conf.Tag, err)
		}
		start, end = computeCountdownBounds(&confCopy, nil)
		return start, end, false
	}
	infosByDay = confInfosByDay(cis)
	start, end = computeCountdownBounds(&confCopy, infosByDay)
	return start, end, true
}
