package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMigrationsSortsByVersion(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "002_second.sql", "SELECT 2;")
	writeMigration(t, dir, "001_first.sql", "SELECT 1;")

	migrations, err := LoadMigrations(dir)
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("len(migrations) = %d, want 2", len(migrations))
	}
	if migrations[0].Version != 1 || migrations[1].Version != 2 {
		t.Fatalf("versions = %d, %d; want 1, 2", migrations[0].Version, migrations[1].Version)
	}
}

func TestLoadMigrationsRejectsDuplicateVersions(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "002_first.sql", "SELECT 1;")
	writeMigration(t, dir, "002_second.sql", "SELECT 2;")

	_, err := LoadMigrations(dir)
	if err == nil {
		t.Fatal("LoadMigrations succeeded with duplicate versions")
	}
	if !strings.Contains(err.Error(), "duplicate migration version 002") {
		t.Fatalf("error = %q, want duplicate version message", err)
	}
}

func writeMigration(t *testing.T, dir string, name string, sql string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(sql), 0644); err != nil {
		t.Fatalf("write migration %s: %v", name, err)
	}
}
