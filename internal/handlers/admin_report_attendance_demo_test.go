package handlers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The opt-in preview uses a dedicated local schema, never application tables.
// Production reports already read these aggregates via loadEventReport.
func reportPreviewAttendance(t *testing.T) func(*sponsorReportPage) error {
	t.Helper()
	if os.Getenv("REPORT_REGISTRATION_DEMO") != "1" {
		return func(*sponsorReportPage) error { return nil }
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("REPORT_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Database != "btcpp_report_demo" || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("attendance preview requires loopback btcpp_report_demo database")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "report_attendance_preview"
	cfg.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	_, err = db.Exec(ctx, `BEGIN;
CREATE SCHEMA IF NOT EXISTS report_attendance_preview;
CREATE TABLE IF NOT EXISTS report_attendance_preview.registrations(conference_id text,revoked boolean,checked_in_at timestamptz,person_id text,email text,revoked_before_account_deletion boolean);
CREATE TABLE IF NOT EXISTS report_attendance_preview.person_emails(person_id text,email text UNIQUE);
TRUNCATE report_attendance_preview.registrations, report_attendance_preview.person_emails;
INSERT INTO report_attendance_preview.registrations
 SELECT 'public-nairobi',false,CASE WHEN n<=286 THEN '2026-06-17T12:00:00Z'::timestamptz END,
 'sample-attendee-'||n,'sample-'||n||'@example.test',NULL FROM generate_series(1,320) n;
INSERT INTO report_attendance_preview.registrations VALUES
 ('public-nairobi',true,'2026-06-17','sample-revoked','revoked@example.test',NULL),
 ('other-event',false,'2026-06-17','sample-other','other@example.test',NULL);
COMMIT;`)
	if err != nil {
		t.Fatal(err)
	}
	return func(p *sponsorReportPage) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := db.QueryRow(ctx, eventReportAttendanceSQL, p.Event.ID).Scan(&p.Registrations, &p.Attendees); err != nil {
			return err
		}
		p.DemoAttendance = true
		p.Message = fmt.Sprintf("Local database preview · %d sample registrations and %d sample unique check-ins. Other figures retain their stated sources. Not for sponsor distribution.", p.Registrations, p.Attendees)
		return nil
	}
}

func TestReportAttendanceDemo(t *testing.T) {
	if os.Getenv("REPORT_REGISTRATION_DEMO") != "1" {
		t.Skip("opt-in local attendance seed")
	}
	load := reportPreviewAttendance(t)
	p := reportFixturePage(t)
	if err := load(&p); err != nil {
		t.Fatal(err)
	}
	if p.Registrations != 320 || p.Attendees != 286 || !p.DemoAttendance || !p.Preview {
		t.Fatal("incorrect demo attendance or provenance")
	}
	if p.Saved["reach_youtube_views"] != "2811" {
		t.Fatal("real video snapshot replaced by demo data")
	}
	p.Event.ID = "missing-event"
	if err := load(&p); err != nil {
		t.Fatal(err)
	}
	if p.Registrations != 0 || p.Attendees != 0 {
		t.Fatal("cross-event attendance leakage")
	}
}
