package emaildirectory

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func directoryTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("EMAIL_DIRECTORY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("EMAIL_DIRECTORY_TEST_DATABASE_URL is required for isolated PostgreSQL tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("directory_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS citext; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(); admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); admin.Close() })
	_, err = pool.Exec(ctx, `CREATE TABLE person_emails(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),person_id uuid NOT NULL,email citext UNIQUE NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
 CREATE TABLE subscribers(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),email citext UNIQUE NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
 CREATE TABLE subscriber_subscriptions(subscriber_id uuid REFERENCES subscribers(id) ON DELETE CASCADE,name text NOT NULL,PRIMARY KEY(subscriber_id,name));
 CREATE TABLE newsletter_consent_requests(email citext,newsletter text,subscribed boolean,recorded_at timestamptz NOT NULL DEFAULT now());`)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func applyDirectoryMigration(t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	sql, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "129_email_directory.sql"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, string(sql)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func TestDirectoryMigrationAndLegacyWrites(t *testing.T) {
	pool := directoryTestPool(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO person_emails(person_id,email) VALUES(gen_random_uuid(),'Example+tag@Example.test');
 INSERT INTO subscribers(email) VALUES('example+tag@example.test'),('anonymous@example.test'),('optout@example.test');
 INSERT INTO subscriber_subscriptions SELECT id,'newsletter' FROM subscribers WHERE email<>'optout@example.test';
 INSERT INTO subscriber_subscriptions SELECT id,'insider' FROM subscribers;
 INSERT INTO newsletter_consent_requests(email,newsletter,subscribed) VALUES('optout@example.test','newsletter',false);`)
	if err != nil {
		t.Fatal(err)
	}
	before, err := Audit(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if !before.CanMigrate || before.UniqueContacts != 3 || before.SharedContacts != 1 || before.Memberships != 2 || before.IgnoredInsiderMemberships != 3 {
		t.Fatalf("audit: %+v", before)
	}
	if err = applyDirectoryMigration(t, pool); err != nil {
		t.Fatal(err)
	}
	var contacts, memberships, events int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM email_contacts),(SELECT count(*) FROM email_memberships),(SELECT count(*) FROM email_preference_events)`).Scan(&contacts, &memberships, &events); err != nil {
		t.Fatal(err)
	}
	if contacts != 3 || memberships != 3 || events != 3 {
		t.Fatalf("counts %d %d %d", contacts, memberships, events)
	}
	var linked bool
	if err = pool.QueryRow(ctx, `SELECT p.contact_id=s.contact_id FROM person_emails p JOIN subscribers s ON s.email=p.email`).Scan(&linked); err != nil || !linked {
		t.Fatalf("account contact linkage: %v %v", linked, err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM email_memberships m JOIN email_contacts c ON c.id=m.contact_id WHERE c.email='optout@example.test'`).Scan(&status); err != nil || status != "unsubscribed" {
		t.Fatalf("opt-out: %s %v", status, err)
	}
	// No fake consent date for legacy memberships.
	var invented int
	pool.QueryRow(ctx, `SELECT count(*) FROM email_memberships WHERE consented_at IS NOT NULL`).Scan(&invented)
	if invented != 0 {
		t.Fatal("invented legacy consent timestamps")
	}
	// Repeat an unchanged choice, then unsubscribe: exactly one new event.
	_, err = pool.Exec(ctx, `INSERT INTO subscriber_subscriptions SELECT id,'newsletter',contact_id FROM subscribers WHERE email='anonymous@example.test' ON CONFLICT DO NOTHING;
 DELETE FROM subscriber_subscriptions WHERE name='newsletter' AND subscriber_id=(SELECT id FROM subscribers WHERE email='anonymous@example.test');`)
	if err != nil {
		t.Fatal(err)
	}
	pool.QueryRow(ctx, `SELECT count(*) FROM email_preference_events`).Scan(&events)
	if events != 4 {
		t.Fatalf("events=%d", events)
	}
	// New account for an anonymous subscriber reuses identity, never resets opt-out.
	_, err = pool.Exec(ctx, `INSERT INTO person_emails(person_id,email) VALUES(gen_random_uuid(),'anonymous@example.test')`)
	if err != nil {
		t.Fatal(err)
	}
	pool.QueryRow(ctx, `SELECT status FROM email_memberships m JOIN email_contacts c ON c.id=m.contact_id WHERE c.email='anonymous@example.test'`).Scan(&status)
	if status != "unsubscribed" {
		t.Fatalf("account signup reset opt-out: %s", status)
	}
	// Address changes cannot rewrite shared identity or silently move preferences.
	if _, err = pool.Exec(ctx, `UPDATE email_contacts SET email='changed@example.test' WHERE email='anonymous@example.test'`); err == nil {
		t.Fatal("mutable shared address")
	}
	if _, err = pool.Exec(ctx, `UPDATE subscribers SET email='changed@example.test' WHERE email='anonymous@example.test'`); err == nil {
		t.Fatal("silently transferred subscriptions")
	}
	// Account removal retains independent newsletter identity and preferences.
	if _, err = pool.Exec(ctx, `DELETE FROM person_emails WHERE email='anonymous@example.test'`); err != nil {
		t.Fatal(err)
	}
	pool.QueryRow(ctx, `SELECT count(*) FROM email_contacts`).Scan(&contacts)
	if contacts != 3 {
		t.Fatalf("contacts=%d", contacts)
	}
}

func TestDirectoryAuditBlocksNormalizationCollision(t *testing.T) {
	pool := directoryTestPool(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO person_emails(person_id,email) VALUES(gen_random_uuid(),'a@example.test'),(gen_random_uuid(),' a@example.test ')`)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Audit(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if report.CanMigrate || report.AccountNormalizationConflicts != 1 {
		t.Fatalf("audit: %+v", report)
	}
	if err = applyDirectoryMigration(t, pool); err == nil {
		t.Fatal("migration accepted ambiguous ownership")
	}
	var exists bool
	pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.email_contacts') IS NOT NULL`).Scan(&exists)
	if exists {
		t.Fatal("failed migration left partial schema")
	}
}

func TestDirectoryAuditBlocksConflictingConsent(t *testing.T) {
	pool := directoryTestPool(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO subscribers(email) VALUES('conflict@example.test');
 INSERT INTO subscriber_subscriptions SELECT id,'newsletter' FROM subscribers;
 INSERT INTO newsletter_consent_requests(email,newsletter,subscribed) VALUES('conflict@example.test','newsletter',false)`)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Audit(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if report.CanMigrate || report.PreferenceConflicts != 1 {
		t.Fatalf("audit: %+v", report)
	}
	if err = applyDirectoryMigration(t, pool); err == nil {
		t.Fatal("migration ignored explicit opt-out conflict")
	}
}

func TestDirectoryAuditBeforeConsentAPIMigration(t *testing.T) {
	pool := directoryTestPool(t)
	ctx := context.Background()
	// Prevent search_path from finding the public fixture's consent table.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var schema string
	if err = conn.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	conn.Release()
	// Rename the local table to simulate the older schema; audit resolves the
	// public table if one exists, so use a pool with a schema-only search path.
	if _, err = pool.Exec(ctx, `DROP TABLE newsletter_consent_requests`); err != nil {
		t.Fatal(err)
	}
	cfg := pool.Config()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	older, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer older.Close()
	// Audit's empty history should use built-in types, independent of extensions.
	if _, err = Audit(ctx, older); err != nil {
		t.Fatal(err)
	}
}
