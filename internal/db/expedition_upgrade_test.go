package db

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"testing"
)

// Explicit empty scratch database only: rehearse deployed master first, then the
// expedition upgrade using the real runner and its migration checksum ledger.
func TestExpeditionMasterUpgradePostgres(t *testing.T) {
	uri := os.Getenv("EXPEDITION_MIGRATION_TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("empty scratch database required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var populated bool
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public')`).Scan(&populated); err != nil || populated {
		t.Fatal("requires empty database", err)
	}
	source := "../../db/migrations"
	master := t.TempDir()
	files, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() >= "135_" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(source, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(master, file.Name()), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = MigrateDir(ctx, pool, master, nil); err != nil {
		t.Fatal("master migrations", err)
	}
	if n, err := MigrateDir(ctx, pool, source, nil); err != nil || n != 8 {
		t.Fatal("expedition upgrade", n, err)
	}
	if n, err := MigrateDir(ctx, pool, source, nil); err != nil || n != 0 {
		t.Fatal("upgrade replay", n, err)
	}
}
