package getters

import (
	"fmt"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5/pgtype"
)

const YouTubePublishChannel = "btcpp-youtube"
const defaultYouTubeSlotTimezone = "America/Chicago"

func ensureYouTubePublishSlotsPostgres(ctx *config.AppContext) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		CREATE TABLE IF NOT EXISTS youtube_publish_slots (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			channel text NOT NULL DEFAULT 'youtube',
			weekday integer NOT NULL,
			publish_time time NOT NULL,
			timezone text NOT NULL DEFAULT 'America/Chicago',
			active boolean NOT NULL DEFAULT true,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (channel, weekday, publish_time, timezone),
			CHECK (channel <> ''),
			CHECK (weekday >= 0 AND weekday <= 6),
			CHECK (timezone <> '')
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure youtube publish slots table: %w", err)
	}
	_, _ = ctx.DB.Exec(ctx.DatabaseContext(), `
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_trigger WHERE tgname = 'youtube_publish_slots_set_updated_at'
			) THEN
				CREATE TRIGGER youtube_publish_slots_set_updated_at
				BEFORE UPDATE ON youtube_publish_slots
				FOR EACH ROW EXECUTE FUNCTION set_updated_at();
			END IF;
		END $$;
	`)
	_, err = ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO youtube_publish_slots (channel, weekday, publish_time, timezone)
		VALUES
			('youtube', 1, '10:05', 'America/Chicago'),
			('youtube', 1, '14:04', 'America/Chicago'),
			('youtube', 1, '19:00', 'America/Chicago'),
			('youtube', 2, '10:05', 'America/Chicago'),
			('youtube', 2, '14:04', 'America/Chicago'),
			('youtube', 2, '19:00', 'America/Chicago'),
			('youtube', 3, '10:05', 'America/Chicago'),
			('youtube', 3, '14:04', 'America/Chicago'),
			('youtube', 3, '19:00', 'America/Chicago'),
			('youtube', 4, '10:05', 'America/Chicago'),
			('youtube', 4, '14:04', 'America/Chicago'),
			('youtube', 4, '19:00', 'America/Chicago'),
			('youtube', 5, '10:05', 'America/Chicago'),
			('youtube', 5, '14:04', 'America/Chicago'),
			('youtube', 5, '19:00', 'America/Chicago'),
			('youtube', 6, '14:05', 'America/Chicago'),
			('youtube', 0, '17:45', 'America/Chicago')
		ON CONFLICT DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("seed youtube publish slots: %w", err)
	}
	return nil
}

func ListYouTubePublishSlots(ctx *config.AppContext) ([]*types.YouTubePublishSlot, error) {
	return listYouTubePublishSlots(ctx, YouTubePublishChannel)
}

func listYouTubePublishSlots(ctx *config.AppContext, channel string) ([]*types.YouTubePublishSlot, error) {
	if err := ensureYouTubePublishSlotsPostgres(ctx); err != nil {
		return nil, err
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, channel, weekday, publish_time, timezone, active
		FROM youtube_publish_slots
		WHERE channel = $1
		ORDER BY weekday, publish_time, id
	`, channel)
	if err != nil {
		return nil, fmt.Errorf("query youtube publish slots: %w", err)
	}
	defer rows.Close()

	var out []*types.YouTubePublishSlot
	for rows.Next() {
		var slot types.YouTubePublishSlot
		var weekday int
		var publishTime pgtype.Time
		if err := rows.Scan(&slot.ID, &slot.Channel, &weekday, &publishTime, &slot.Timezone, &slot.Active); err != nil {
			return nil, fmt.Errorf("scan youtube publish slot: %w", err)
		}
		slot.Weekday = time.Weekday(weekday)
		slot.TimeOfDay = pgTimeString(publishTime)
		out = append(out, &slot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate youtube publish slots: %w", err)
	}
	return out, nil
}

func ReplaceYouTubePublishSlots(ctx *config.AppContext, slots []*types.YouTubePublishSlot) error {
	return replaceYouTubePublishSlots(ctx, YouTubePublishChannel, slots)
}

func replaceYouTubePublishSlots(ctx *config.AppContext, channel string, slots []*types.YouTubePublishSlot) error {
	if err := ensureYouTubePublishSlotsPostgres(ctx); err != nil {
		return err
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin replace youtube publish slots: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	if _, err := tx.Exec(ctx.DatabaseContext(), `DELETE FROM youtube_publish_slots WHERE channel = $1`, channel); err != nil {
		return fmt.Errorf("clear youtube publish slots: %w", err)
	}
	for _, slot := range slots {
		if slot == nil || slot.TimeOfDay == "" {
			continue
		}
		timezone := slot.Timezone
		if timezone == "" {
			timezone = defaultYouTubeSlotTimezone
		}
		_, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO youtube_publish_slots (channel, weekday, publish_time, timezone, active)
			VALUES ($1, $2, $3::time, $4, $5)
		`, channel, int(slot.Weekday), slot.TimeOfDay, timezone, slot.Active)
		if err != nil {
			return fmt.Errorf("insert youtube publish slot %s %s: %w", slot.Weekday, slot.TimeOfDay, err)
		}
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return fmt.Errorf("commit youtube publish slots: %w", err)
	}
	return nil
}

func pgTimeString(value pgtype.Time) string {
	if !value.Valid {
		return ""
	}
	t := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(value.Microseconds) * time.Microsecond)
	return t.Format("15:04")
}
