package getters

import (
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

type SatelliteEventInput struct {
	Title          string
	Description    string
	EventURL       string
	EventType      string
	StartsAt       *time.Time
	EndsAt         *time.Time
	Location       string
	ImageURL       string
	HostName       string
	HostURL        string
	HostLogoURL    string
	SubmitterEmail string
	Status         string
	Notes          string
	ConfRef        string
}

func ListSatelliteEvents(ctx *config.AppContext, confRef string, includePending bool) ([]*types.SatelliteEvent, error) {
	if UsePostgresBackend(ctx) {
		return listSatelliteEventsPostgres(ctx, confRef, includePending)
	}
	return nil, unsupportedPostgresBackend("satellite events")
}

func ListSatelliteEventsBySubmitter(ctx *config.AppContext, email string) ([]*types.SatelliteEvent, error) {
	if UsePostgresBackend(ctx) {
		return listSatelliteEventsBySubmitterPostgres(ctx, email)
	}
	return nil, unsupportedPostgresBackend("satellite events")
}

func GetSatelliteEvent(ctx *config.AppContext, id string) (*types.SatelliteEvent, error) {
	if UsePostgresBackend(ctx) {
		return getSatelliteEventPostgres(ctx, id)
	}
	return nil, unsupportedPostgresBackend("satellite events")
}

func CreateSatelliteEvent(ctx *config.AppContext, input SatelliteEventInput) (*types.SatelliteEvent, error) {
	if UsePostgresBackend(ctx) {
		return createSatelliteEventPostgres(ctx, input)
	}
	return nil, unsupportedPostgresBackend("satellite events")
}

func UpdateSatelliteEvent(ctx *config.AppContext, id string, input SatelliteEventInput) error {
	if UsePostgresBackend(ctx) {
		return updateSatelliteEventPostgres(ctx, id, input)
	}
	return unsupportedPostgresBackend("satellite events")
}

func DeleteSatelliteEvent(ctx *config.AppContext, id string) error {
	if UsePostgresBackend(ctx) {
		return deleteSatelliteEventPostgres(ctx, id)
	}
	return unsupportedPostgresBackend("satellite events")
}
