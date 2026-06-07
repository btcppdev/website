package getters

import (
	"context"
	"fmt"

	"btcpp-web/internal/config"
)

func siteStatsAttendeesPostgres(ctx *config.AppContext) (int, error) {
	if ctx == nil || ctx.DB == nil {
		return 0, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	var count int
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT count(*)
		FROM registrations
	`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count registrations: %w", err)
	}
	return count, nil
}

func siteStatsPostgres(ctx *config.AppContext) (SiteStatsValues, error) {
	if ctx == nil || ctx.DB == nil {
		return SiteStatsValues{}, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	var s SiteStatsValues
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM conferences WHERE end_date IS NOT NULL AND end_date < now()),
			(
				SELECT count(*)
				FROM conf_talks ct
				JOIN conferences c ON c.id = ct.conference_id
				JOIN proposals p ON p.id = ct.proposal_id
				WHERE c.end_date IS NOT NULL
					AND c.end_date < now()
					AND p.status = 'Accepted'
			),
			(SELECT count(*) FROM registrations)
	`).Scan(&s.PastConfs, &s.PastTalks, &s.Attendees); err != nil {
		return SiteStatsValues{}, fmt.Errorf("query site stats: %w", err)
	}
	return s, nil
}
