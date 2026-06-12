package getters

import (
	"context"
	"fmt"

	"btcpp-web/internal/config"
)

func UsePostgresBackend(ctx *config.AppContext) bool {
	return true
}

func unsupportedPostgresBackend(feature string) error {
	return fmt.Errorf("%s requires postgres backend", feature)
}

func updateConfActivePostgres(ctx *config.AppContext, confRef string, active bool) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE conferences
		SET active = $2
		WHERE id = $1
	`, confRef, active)
	if err != nil {
		return fmt.Errorf("update conference %s active: %w", confRef, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conference %s not found", confRef)
	}
	return nil
}

func updateConfDetailsPostgres(ctx *config.AppContext, confRef string, in ConfDetailsInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE conferences
		SET description = $2,
			og_flavor = $3,
			emoji = $4,
			tagline = $5,
			date_desc = $6,
			start_date = $7,
			end_date = $8,
			timezone = $9,
			location = $10,
			venue = $11,
			venue_map_url = $12,
			venue_website_url = $13,
			show_hackathon = $14,
			has_satellites = $15
		WHERE id = $1
	`, confRef, in.Description, in.OGFlavor, in.Emoji, in.Tagline, in.DateDesc,
		in.StartDate, in.EndDate, in.Timezone, in.Location, in.Venue,
		in.VenueMap, in.VenueWebsite, in.ShowHackathon, in.HasSatellites)
	if err != nil {
		return fmt.Errorf("update conference %s details: %w", confRef, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conference %s not found", confRef)
	}
	return nil
}
