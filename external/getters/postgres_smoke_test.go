package getters

import (
	"context"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresSmokeSpeakerCreateAndLookup(t *testing.T) {
	ctx := postgresSmokeContext(t)
	suffix := postgresSmokeSuffix()
	email := "speaker-" + suffix + "@example.test"

	speakerID, err := CreateSpeaker(ctx, SpeakerInput{
		Name:     "Smoke Speaker " + suffix,
		Email:    email,
		Phone:    "+15551230000",
		Signal:   "smoke." + suffix,
		Twitter:  "smoketest",
		Website:  "https://example.test/smoke",
		TShirt:   "MM",
		Photo:    "smoke.jpg",
		Telegram: "smoke_tg",
	})
	if err != nil {
		t.Fatalf("CreateSpeaker postgres: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id::text = $1`, speakerID)
	})

	speakers, err := GetSpeakersByEmail(ctx, strings.ToUpper(email))
	if err != nil {
		t.Fatalf("GetSpeakersByEmail postgres: %v", err)
	}
	if len(speakers) != 1 {
		t.Fatalf("GetSpeakersByEmail returned %d speakers, want 1", len(speakers))
	}
	got := speakers[0]
	if got.ID != speakerID || got.Email != email || got.Signal != "smoke."+suffix || got.TShirt != "MM" {
		t.Fatalf("speaker mismatch: %+v", got)
	}
}

func TestPostgresSmokeDiscountScopedToConference(t *testing.T) {
	ctx := postgresSmokeContext(t)
	confID, tag := insertSmokeConference(t, ctx)
	code := "SMOKE" + strings.ToUpper(postgresSmokeSuffix())

	discountID, err := CreateDiscount(ctx, DiscountInput{
		CodeName:     code,
		DiscountExpr: "%42",
		ConfRef:      confID,
	})
	if err != nil {
		t.Fatalf("CreateDiscount postgres: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM discounts WHERE id::text = $1 OR code_name = $2`, discountID, code)
	})

	discounts, err = listDiscountsPostgres(ctx)
	if err != nil {
		t.Fatalf("listDiscountsPostgres: %v", err)
	}
	lastDiscountFetch = time.Now()

	found, err := FindDiscount(ctx, strings.ToLower(code))
	if err != nil {
		t.Fatalf("FindDiscount postgres: %v", err)
	}
	if found == nil {
		t.Fatalf("FindDiscount(%q) returned nil", code)
	}
	if found.Ref != discountID || found.CodeName != code || found.DiscType != '%' || found.Amount != 42 {
		t.Fatalf("discount mismatch: %+v", found)
	}
	if len(found.ConfRef) != 1 || found.ConfRef[0] != confID {
		t.Fatalf("discount conf refs = %v, want [%s] for %s", found.ConfRef, confID, tag)
	}
}

func TestPostgresSmokeVolunteerInfoOrientationUpdate(t *testing.T) {
	ctx := postgresSmokeContext(t)
	confID, _ := insertSmokeConference(t, ctx)

	var volInfoID string
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO volunteer_info (conference_id, notes)
		VALUES ($1::uuid, 'smoke volunteer info')
		RETURNING id::text
	`, confID).Scan(&volInfoID)
	if err != nil {
		t.Fatalf("insert volunteer_info: %v", err)
	}

	start := time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	link := "https://example.test/orientation/" + postgresSmokeSuffix()
	if err := UpdateVolInfoOrientation(ctx, volInfoID, start, end, link); err != nil {
		t.Fatalf("UpdateVolInfoOrientation postgres: %v", err)
	}

	info, err := GetVolInfo(ctx, confID)
	if err != nil {
		t.Fatalf("GetVolInfo postgres: %v", err)
	}
	if info.Ref != volInfoID || info.OrientLink != link {
		t.Fatalf("volinfo mismatch: %+v", info)
	}
	if info.OrientTimes == nil || !info.OrientTimes.Start.Equal(start) || info.OrientTimes.End == nil || !info.OrientTimes.End.Equal(end) {
		t.Fatalf("volinfo orientation times = %+v, want %s - %s", info.OrientTimes, start, end)
	}
}

func TestPostgresSmokeConfTalkScheduleUsesConferenceTimezone(t *testing.T) {
	ctx := postgresSmokeContext(t)
	tag := "smoke-nairobi-" + postgresSmokeSuffix()

	var confID string
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO conferences (
			tag, active, description, date_desc, start_date, end_date, timezone, location, venue
		)
		VALUES (
			$1, true, 'Nairobi Smoke Test Conf', 'July 1-2, 2026',
			'2026-07-01 00:00:00+03', '2026-07-02 23:59:00+03',
			'Africa/Nairobi', 'Nairobi', 'Smoke Venue'
		)
		RETURNING id::text
	`, tag).Scan(&confID)
	if err != nil {
		t.Fatalf("insert nairobi conference: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id::text = $1 OR tag = $2`, confID, tag)
	})

	var confTalkID string
	err = ctx.DB.QueryRow(context.Background(), `
		INSERT INTO conf_talks (conference_id, scheduled_start, scheduled_end, venue)
		VALUES ($1::uuid, '2026-07-01 10:00:00+03', '2026-07-01 10:45:00+03', 'Mainstage')
		RETURNING id::text
	`, confID).Scan(&confTalkID)
	if err != nil {
		t.Fatalf("insert nairobi conf talk: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM conf_talks WHERE id::text = $1`, confTalkID)
	})

	originalConfs := confs
	originalLastConfsFetch := lastConfsFetch
	t.Cleanup(func() {
		confs = originalConfs
		lastConfsFetch = originalLastConfsFetch
	})
	confs, err = listConferencesPostgres(ctx)
	if err != nil {
		t.Fatalf("listConferencesPostgres: %v", err)
	}
	lastConfsFetch = time.Now()

	talks, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.id::text = $1", []interface{}{confTalkID}, map[string]*types.Proposal{})
	if err != nil {
		t.Fatalf("queryConfTalksPostgres: %v", err)
	}
	if len(talks) != 1 {
		t.Fatalf("queryConfTalksPostgres returned %d talks, want 1", len(talks))
	}

	sched := talks[0].Sched
	if sched == nil || sched.End == nil {
		t.Fatalf("schedule missing: %+v", talks[0])
	}
	if got := sched.Start.Location().String(); got != "Africa/Nairobi" {
		t.Fatalf("start location = %q, want Africa/Nairobi", got)
	}
	if sched.Start.Hour() != 10 || sched.Start.Minute() != 0 {
		t.Fatalf("start time = %s, want 10:00 Africa/Nairobi", sched.Start)
	}
	if got := sched.End.Location().String(); got != "Africa/Nairobi" {
		t.Fatalf("end location = %q, want Africa/Nairobi", got)
	}
	if sched.End.Hour() != 10 || sched.End.Minute() != 45 {
		t.Fatalf("end time = %s, want 10:45 Africa/Nairobi", *sched.End)
	}
}

func TestPostgresSmokeWorkShiftScheduleUsesConferenceTimezone(t *testing.T) {
	ctx := postgresSmokeContext(t)
	tag := "smoke-shift-nairobi-" + postgresSmokeSuffix()

	var confID string
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO conferences (
			tag, active, description, date_desc, start_date, end_date, timezone, location, venue
		)
		VALUES (
			$1, true, 'Nairobi Shift Smoke Test Conf', 'July 1-2, 2026',
			'2026-07-01 00:00:00+03', '2026-07-02 23:59:00+03',
			'Africa/Nairobi', 'Nairobi', 'Smoke Venue'
		)
		RETURNING id::text
	`, tag).Scan(&confID)
	if err != nil {
		t.Fatalf("insert nairobi conference: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id::text = $1 OR tag = $2`, confID, tag)
	})

	var shiftID string
	err = ctx.DB.QueryRow(context.Background(), `
		INSERT INTO work_shifts (conference_id, name, max_vols, shift_start, shift_end, priority)
		VALUES ($1::uuid, 'Registration Desk', 2, '2026-07-01 10:00:00+03', '2026-07-01 11:30:00+03', 1)
		RETURNING id::text
	`, confID).Scan(&shiftID)
	if err != nil {
		t.Fatalf("insert nairobi work shift: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM work_shifts WHERE id::text = $1`, shiftID)
	})

	originalConfs := confs
	originalLastConfsFetch := lastConfsFetch
	originalJobs := jobs
	originalLastJobTypeFetch := lastJobTypeFetch
	t.Cleanup(func() {
		confs = originalConfs
		lastConfsFetch = originalLastConfsFetch
		jobs = originalJobs
		lastJobTypeFetch = originalLastJobTypeFetch
	})
	confs, err = listConferencesPostgres(ctx)
	if err != nil {
		t.Fatalf("listConferencesPostgres: %v", err)
	}
	lastConfsFetch = time.Now()
	jobs = nil
	lastJobTypeFetch = time.Now()

	shifts, err := listWorkShiftsPostgres(ctx)
	if err != nil {
		t.Fatalf("listWorkShiftsPostgres: %v", err)
	}
	var got *types.WorkShift
	for _, shift := range shifts {
		if shift.Ref == shiftID {
			got = shift
			break
		}
	}
	if got == nil {
		t.Fatalf("shift %s not returned", shiftID)
	}
	if got.ShiftTime == nil || got.ShiftTime.End == nil {
		t.Fatalf("shift time missing: %+v", got)
	}
	if loc := got.ShiftTime.Start.Location().String(); loc != "Africa/Nairobi" {
		t.Fatalf("start location = %q, want Africa/Nairobi", loc)
	}
	if got.ShiftTime.Start.Hour() != 10 || got.ShiftTime.Start.Minute() != 0 {
		t.Fatalf("start time = %s, want 10:00 Africa/Nairobi", got.ShiftTime.Start)
	}
	if loc := got.ShiftTime.End.Location().String(); loc != "Africa/Nairobi" {
		t.Fatalf("end location = %q, want Africa/Nairobi", loc)
	}
	if got.ShiftTime.End.Hour() != 11 || got.ShiftTime.End.Minute() != 30 {
		t.Fatalf("end time = %s, want 11:30 Africa/Nairobi", *got.ShiftTime.End)
	}
	if desc := got.TimeDesc(); desc != "10:00am - 11:30am" {
		t.Fatalf("TimeDesc = %q, want local Nairobi time", desc)
	}
}

func postgresSmokeContext(t *testing.T) *config.AppContext {
	t.Helper()
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("set BTCPP_POSTGRES_SMOKE=1 to run local Postgres smoke tests")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for local Postgres smoke tests")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	var schemaReady bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.conferences') IS NOT NULL`).Scan(&schemaReady); err != nil {
		t.Fatalf("check schema: %v", err)
	}
	if !schemaReady {
		t.Fatalf("postgres schema is not migrated; run btcpp_pg_migrate first")
	}

	return &config.AppContext{
		Env:   &types.EnvConfig{DataBackend: dataBackendPostgres},
		DB:    pool,
		Err:   log.New(io.Discard, "", 0),
		Infos: log.New(io.Discard, "", 0),
	}
}

func insertSmokeConference(t *testing.T, app *config.AppContext) (string, string) {
	t.Helper()
	tag := "smoke-" + postgresSmokeSuffix()
	var id string
	err := app.DB.QueryRow(context.Background(), `
		INSERT INTO conferences (
			tag, active, description, date_desc, start_date, end_date, timezone, location, venue
		)
		VALUES (
			$1, true, 'Smoke Test Conf', 'July 1-2, 2026',
			'2026-07-01 09:00:00+00', '2026-07-02 17:00:00+00',
			'UTC', 'Smoke City', 'Smoke Venue'
		)
		RETURNING id::text
	`, tag).Scan(&id)
	if err != nil {
		t.Fatalf("insert conference: %v", err)
	}
	t.Cleanup(func() {
		_, _ = app.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id::text = $1 OR tag = $2`, id, tag)
	})
	return id, tag
}

func postgresSmokeSuffix() string {
	return strings.ToLower(strings.ReplaceAll(time.Now().UTC().Format("20060102T150405.000000000"), ".", ""))
}
