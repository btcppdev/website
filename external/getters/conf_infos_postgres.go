package getters

import (
	"context"
	"fmt"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5/pgtype"
)

func listConfInfosPostgres(ctx *config.AppContext, confTag string) ([]*types.ConfInfo, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	sql := `
		SELECT cd.id::text, c.tag, cd.day_number, c.start_date, c.timezone,
			cd.doors_start, cd.doors_end, cd.breakfast_start, cd.breakfast_end,
			cd.lunch_start, cd.lunch_end, cd.coffee_start, cd.coffee_end,
			cd.venues
		FROM conference_days cd
		JOIN conferences c ON c.id = cd.conference_id
	`
	args := []any{}
	if confTag != "" {
		sql += " WHERE c.tag = $1"
		args = append(args, confTag)
	}
	sql += " ORDER BY c.tag, cd.day_number"

	rows, err := ctx.DB.Query(context.Background(), sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query conference days: %w", err)
	}
	defer rows.Close()

	var out []*types.ConfInfo
	for rows.Next() {
		var ci types.ConfInfo
		var confStart pgtype.Timestamptz
		var timezoneName string
		var doorsStart, doorsEnd pgtype.Time
		var breakfastStart, breakfastEnd pgtype.Time
		var lunchStart, lunchEnd pgtype.Time
		var coffeeStart, coffeeEnd pgtype.Time
		err := rows.Scan(
			&ci.ID,
			&ci.ConfTag,
			&ci.Day,
			&confStart,
			&timezoneName,
			&doorsStart,
			&doorsEnd,
			&breakfastStart,
			&breakfastEnd,
			&lunchStart,
			&lunchEnd,
			&coffeeStart,
			&coffeeEnd,
			&ci.Venues,
		)
		if err != nil {
			return nil, fmt.Errorf("scan conference day: %w", err)
		}
		anchor := confDayAnchor(confStart, timezoneName, ci.Day)
		ci.Doors = timesFromPgTime(anchor, doorsStart, doorsEnd)
		ci.Breakfast = timesFromPgTime(anchor, breakfastStart, breakfastEnd)
		ci.Lunch = timesFromPgTime(anchor, lunchStart, lunchEnd)
		ci.Coffee = timesFromPgTime(anchor, coffeeStart, coffeeEnd)
		out = append(out, &ci)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conference days: %w", err)
	}
	return out, nil
}

func confDayAnchor(confStart pgtype.Timestamptz, timezoneName string, day int) time.Time {
	loc := time.UTC
	if timezoneName != "" {
		if loaded, err := time.LoadLocation(timezoneName); err == nil {
			loc = loaded
		}
	}
	start := time.Now().In(loc)
	if confStart.Valid {
		start = confStart.Time.In(loc)
	}
	if day < 1 {
		day = 1
	}
	return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, day-1)
}

func timesFromPgTime(anchor time.Time, start pgtype.Time, end pgtype.Time) *types.Times {
	if !start.Valid {
		return nil
	}
	startTime := pgTimeOnDay(anchor, start)
	if !end.Valid {
		return &types.Times{Start: startTime}
	}
	endTime := pgTimeOnDay(anchor, end)
	return &types.Times{Start: startTime, End: &endTime}
}

func pgTimeOnDay(anchor time.Time, value pgtype.Time) time.Time {
	duration := time.Duration(value.Microseconds) * time.Microsecond
	return anchor.Add(duration)
}

func upsertConfInfoPostgres(ctx *config.AppContext, confRef string, in ConfInfoInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("postgres backend selected but AppContext.DB is nil")
	}
	if in.Day < 1 {
		return fmt.Errorf("conference day must be >= 1")
	}
	doorsStart := nullableTimeString(in.DoorsStart)
	doorsEnd := nullableTimeString(in.DoorsEnd)
	breakfastStart := nullableTimeString(in.BreakfastStart)
	breakfastEnd := nullableTimeString(in.BreakfastEnd)
	lunchStart := nullableTimeString(in.LunchStart)
	lunchEnd := nullableTimeString(in.LunchEnd)
	coffeeStart := nullableTimeString(in.CoffeeStart)
	coffeeEnd := nullableTimeString(in.CoffeeEnd)

	if in.ID != "" {
		commandTag, err := ctx.DB.Exec(context.Background(), `
			UPDATE conference_days
			SET day_number = $3,
				doors_start = $4,
				doors_end = $5,
				breakfast_start = $6,
				breakfast_end = $7,
				lunch_start = $8,
				lunch_end = $9,
				coffee_start = $10,
				coffee_end = $11,
				venues = $12
			WHERE id = $1
			  AND conference_id = $2
		`, in.ID, confRef, in.Day, doorsStart, doorsEnd, breakfastStart, breakfastEnd,
			lunchStart, lunchEnd, coffeeStart, coffeeEnd, in.Venues)
		if err != nil {
			return fmt.Errorf("update conference day %s: %w", in.ID, err)
		}
		if commandTag.RowsAffected() == 0 {
			return fmt.Errorf("conference day %s not found", in.ID)
		}
		return nil
	}

	_, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO conference_days (
			conference_id, day_number, doors_start, doors_end,
			breakfast_start, breakfast_end, lunch_start, lunch_end,
			coffee_start, coffee_end, venues
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (conference_id, day_number)
		DO UPDATE SET
			doors_start = EXCLUDED.doors_start,
			doors_end = EXCLUDED.doors_end,
			breakfast_start = EXCLUDED.breakfast_start,
			breakfast_end = EXCLUDED.breakfast_end,
			lunch_start = EXCLUDED.lunch_start,
			lunch_end = EXCLUDED.lunch_end,
			coffee_start = EXCLUDED.coffee_start,
			coffee_end = EXCLUDED.coffee_end,
			venues = EXCLUDED.venues
	`, confRef, in.Day, doorsStart, doorsEnd, breakfastStart, breakfastEnd,
		lunchStart, lunchEnd, coffeeStart, coffeeEnd, in.Venues)
	if err != nil {
		return fmt.Errorf("upsert conference day %d: %w", in.Day, err)
	}
	return nil
}

func nullableTimeString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
