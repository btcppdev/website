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

func ListConfs(ctx *config.AppContext) ([]*types.Conf, error) {
	if UsePostgresBackend(ctx) {
		return listConferencesPostgres(ctx)
	}
	return ListConferencesNotion(ctx.Notion)
}

func GetConfByTag(ctx *config.AppContext, tag string) (*types.Conf, error) {
	if UsePostgresBackend(ctx) {
		return getConferenceByTagPostgres(ctx, tag)
	}
	confs, err := ListConfs(ctx)
	if err != nil {
		return nil, err
	}
	for _, conf := range confs {
		if conf != nil && conf.Tag == tag {
			return conf, nil
		}
	}
	return nil, nil
}

func GetConfByRef(ctx *config.AppContext, ref string) (*types.Conf, error) {
	if UsePostgresBackend(ctx) {
		return getConferenceByRefPostgres(ctx, ref)
	}
	confs, err := ListConfs(ctx)
	if err != nil {
		return nil, err
	}
	for _, conf := range confs {
		if conf != nil && conf.Ref == ref {
			return conf, nil
		}
	}
	return nil, nil
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
