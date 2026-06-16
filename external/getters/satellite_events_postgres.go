package getters

import (
	"context"
	"fmt"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5/pgtype"
)

func listSatelliteEventsPostgres(ctx *config.AppContext, confRef string, includePending bool) ([]*types.SatelliteEvent, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	statusSQL := "AND status = 'published'"
	if includePending {
		statusSQL = ""
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT id::text, conference_id::text, title, description, event_url,
			event_type, starts_at, ends_at, location, image_url,
			host_name, host_url, host_logo_url, COALESCE(submitter_email::text, ''),
			status, notes, published_at
		FROM satellite_events
		WHERE conference_id = $1 `+statusSQL+`
		ORDER BY starts_at NULLS LAST, title
	`, confRef)
	if err != nil {
		return nil, fmt.Errorf("query satellite events: %w", err)
	}
	defer rows.Close()

	var events []*types.SatelliteEvent
	for rows.Next() {
		ev, err := scanSatelliteEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate satellite events: %w", err)
	}
	return events, nil
}

func listSatelliteEventsBySubmitterPostgres(ctx *config.AppContext, email string) ([]*types.SatelliteEvent, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT id::text, conference_id::text, title, description, event_url,
			event_type, starts_at, ends_at, location, image_url,
			host_name, host_url, host_logo_url, COALESCE(submitter_email::text, ''),
			status, notes, published_at
		FROM satellite_events
		WHERE submitter_email = NULLIF($1, '')::citext
		ORDER BY starts_at NULLS LAST, title
	`, email)
	if err != nil {
		return nil, fmt.Errorf("query submitter satellite events: %w", err)
	}
	defer rows.Close()

	var events []*types.SatelliteEvent
	for rows.Next() {
		ev, err := scanSatelliteEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate submitter satellite events: %w", err)
	}
	return events, nil
}

func getSatelliteEventPostgres(ctx *config.AppContext, id string) (*types.SatelliteEvent, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	row := ctx.DB.QueryRow(context.Background(), `
		SELECT id::text, conference_id::text, title, description, event_url,
			event_type, starts_at, ends_at, location, image_url,
			host_name, host_url, host_logo_url, COALESCE(submitter_email::text, ''),
			status, notes, published_at
		FROM satellite_events
		WHERE id = $1
	`, id)
	return scanSatelliteEvent(row)
}

type satelliteEventScanner interface {
	Scan(dest ...interface{}) error
}

func scanSatelliteEvent(row satelliteEventScanner) (*types.SatelliteEvent, error) {
	var ev types.SatelliteEvent
	var startsAt pgtype.Timestamptz
	var endsAt pgtype.Timestamptz
	var publishedAt pgtype.Timestamptz
	err := row.Scan(
		&ev.ID,
		&ev.ConfRef,
		&ev.Title,
		&ev.Description,
		&ev.EventURL,
		&ev.EventType,
		&startsAt,
		&endsAt,
		&ev.Location,
		&ev.ImageURL,
		&ev.HostName,
		&ev.HostURL,
		&ev.HostLogoURL,
		&ev.SubmitterEmail,
		&ev.Status,
		&ev.Notes,
		&publishedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan satellite event: %w", err)
	}
	if startsAt.Valid {
		ev.StartsAt = &startsAt.Time
	}
	if endsAt.Valid {
		ev.EndsAt = &endsAt.Time
	}
	if publishedAt.Valid {
		ev.PublishedAt = &publishedAt.Time
	}
	return &ev, nil
}

func createSatelliteEventPostgres(ctx *config.AppContext, input SatelliteEventInput) (*types.SatelliteEvent, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	row := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO satellite_events (
			conference_id, title, description, event_url, event_type,
			starts_at, ends_at, location, image_url,
			host_name, host_url, host_logo_url, submitter_email,
			status, notes, published_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12, NULLIF($13, '')::citext,
			$14, $15,
			CASE WHEN $14 = 'published' THEN now() ELSE NULL END
		)
		RETURNING id::text, conference_id::text, title, description, event_url,
			event_type, starts_at, ends_at, location, image_url, host_name,
			host_url, host_logo_url, COALESCE(submitter_email::text, ''),
			status, notes, published_at
	`,
		input.ConfRef,
		input.Title,
		input.Description,
		input.EventURL,
		input.EventType,
		input.StartsAt,
		input.EndsAt,
		input.Location,
		input.ImageURL,
		input.HostName,
		input.HostURL,
		input.HostLogoURL,
		input.SubmitterEmail,
		input.Status,
		input.Notes,
	)
	return scanSatelliteEvent(row)
}

func updateSatelliteEventPostgres(ctx *config.AppContext, id string, input SatelliteEventInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE satellite_events
		SET title = $2,
			description = $3,
			event_url = $4,
			event_type = $5,
			starts_at = $6,
			ends_at = $7,
			location = $8,
			image_url = $9,
			host_name = $10,
			host_url = $11,
			host_logo_url = $12,
			submitter_email = NULLIF($13, '')::citext,
			status = $14,
			notes = $15,
			published_at = CASE
				WHEN $14 = 'published' AND (status <> 'published' OR published_at IS NULL) THEN now()
				ELSE published_at
			END
		WHERE id = $1
	`, id,
		input.Title,
		input.Description,
		input.EventURL,
		input.EventType,
		input.StartsAt,
		input.EndsAt,
		input.Location,
		input.ImageURL,
		input.HostName,
		input.HostURL,
		input.HostLogoURL,
		input.SubmitterEmail,
		input.Status,
		input.Notes,
	)
	if err != nil {
		return fmt.Errorf("update satellite event %s: %w", id, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("satellite event %s not found", id)
	}
	return nil
}

func deleteSatelliteEventPostgres(ctx *config.AppContext, id string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `DELETE FROM satellite_events WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete satellite event %s: %w", id, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("satellite event %s not found", id)
	}
	return nil
}
