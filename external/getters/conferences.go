package getters

import (
	"fmt"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

type ConfDetailsInput struct {
	Description   string
	OGFlavor      string
	Emoji         string
	Tagline       string
	DateDesc      string
	StartDate     *time.Time
	EndDate       *time.Time
	Timezone      string
	Location      string
	Venue         string
	VenueMap      string
	VenueWebsite  string
	ShowHackathon bool
	HasSatellites bool
}

func getConfs(ctx *config.AppContext) {
	var err error
	ctx.Infos.Printf("getting confs...")
	if UsePostgresBackend(ctx) {
		confs, err = listConferencesPostgres(ctx)
	} else {
		confs, err = ListConferencesNotion(ctx.Notion)
	}

	if err != nil {
		ctx.Err.Printf("error fetching confs %s", err)
	} else {
		ctx.Infos.Printf("Loaded %d confs!", len(confs))
		writeCache("confs", confs)
	}
}

func FetchConfsCached(ctx *config.AppContext) ([]*types.Conf, error) {
	now := time.Now()
	deadline := now.Add(-cacheTTL)
	if confs == nil || lastConfsFetch.Before(deadline) {
		lastConfsFetch = time.Now()
		queueRefresh(JobConfs)
	}

	return confs, nil
}

func ListConfTickets(n *types.Notion) ([]*types.ConfTicket, error) {
	return ListConfTicketsNotion(n)
}

func ListConferences(n *types.Notion) ([]*types.Conf, error) {
	return ListConferencesNotion(n)
}

func ListConferencesOnly(n *types.Notion) ([]*types.Conf, error) {
	return ListConferencesOnlyNotion(n)
}

func ConfUpdateOrientCalNotif(ctx *config.AppContext, confRef string, calnotif string) error {
	if UsePostgresBackend(ctx) {
		return confUpdateOrientCalNotifPostgres(ctx, confRef, calnotif)
	}
	return confUpdateOrientCalNotifNotion(ctx.Notion, confRef, calnotif)
}

func UpdateConfActive(ctx *config.AppContext, confRef string, active bool) error {
	if UsePostgresBackend(ctx) {
		return updateConfActivePostgres(ctx, confRef, active)
	}
	return fmt.Errorf("UpdateConfActive requires postgres backend")
}

func UpdateConfDetails(ctx *config.AppContext, confRef string, in ConfDetailsInput) error {
	if UsePostgresBackend(ctx) {
		return updateConfDetailsPostgres(ctx, confRef, in)
	}
	return fmt.Errorf("UpdateConfDetails requires postgres backend")
}
