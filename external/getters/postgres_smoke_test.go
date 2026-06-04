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
