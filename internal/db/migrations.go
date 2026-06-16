package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultMigrationsDir = "db/migrations"
	migrationLockKey     = int64(1705239627)
)

var migrationFilePattern = regexp.MustCompile(`^([0-9]+)_(.+)\.sql$`)

type Migration struct {
	Version  int
	Name     string
	Path     string
	SQL      string
	Checksum string
}

type migrationVersionColumn struct {
	DataType string
}

func Migrate(ctx context.Context, pool *pgxpool.Pool, logger *log.Logger) (int, error) {
	return MigrateDir(ctx, pool, defaultMigrationsDir, logger)
}

func MigrateDir(ctx context.Context, pool *pgxpool.Pool, dir string, logger *log.Logger) (int, error) {
	migrations, err := LoadMigrations(dir)
	if err != nil {
		return 0, err
	}
	if len(migrations) == 0 {
		return 0, fmt.Errorf("no migration files found in %s", dir)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return 0, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockKey)

	if err := ensureSchemaMigrations(ctx, conn); err != nil {
		return 0, err
	}
	versionColumn, err := schemaMigrationVersionColumn(ctx, conn)
	if err != nil {
		return 0, err
	}
	if err := baselineExistingSchema(ctx, conn, versionColumn, migrations, logger); err != nil {
		return 0, err
	}

	applied := 0
	for _, migration := range migrations {
		currentChecksum, ok, err := appliedMigration(ctx, conn, versionColumn, migration.Version)
		if err != nil {
			return applied, err
		}
		if ok {
			if currentChecksum != migration.Checksum {
				return applied, fmt.Errorf("migration %03d checksum mismatch; applied database checksum differs from %s", migration.Version, migration.Path)
			}
			continue
		}
		if err := applyMigration(ctx, conn, versionColumn, migration, logger); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func LoadMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %s: %w", dir, err)
	}

	seen := map[int]string{}
	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationFilePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		version, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, fmt.Errorf("parse migration version %s: %w", entry.Name(), err)
		}
		if existing := seen[version]; existing != "" {
			return nil, fmt.Errorf("duplicate migration version %03d: %s and %s", version, existing, entry.Name())
		}
		seen[version] = entry.Name()

		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", path, err)
		}
		sum := sha256.Sum256(raw)
		migrations = append(migrations, Migration{
			Version:  version,
			Name:     strings.TrimSuffix(matches[2], ".sql"),
			Path:     path,
			SQL:      string(raw),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

func ensureSchemaMigrations(ctx context.Context, conn *pgxpool.Conn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

func schemaMigrationVersionColumn(ctx context.Context, conn *pgxpool.Conn) (migrationVersionColumn, error) {
	var dataType string
	if err := conn.QueryRow(ctx, `
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
			AND table_name = 'schema_migrations'
			AND column_name = 'version'
	`).Scan(&dataType); err != nil {
		return migrationVersionColumn{}, fmt.Errorf("inspect schema_migrations.version: %w", err)
	}
	switch dataType {
	case "integer", "bigint", "smallint", "text", "character varying":
		return migrationVersionColumn{DataType: dataType}, nil
	default:
		return migrationVersionColumn{}, fmt.Errorf("unsupported schema_migrations.version type %q", dataType)
	}
}

func (c migrationVersionColumn) value(version int) any {
	switch c.DataType {
	case "text", "character varying":
		return fmt.Sprintf("%03d", version)
	default:
		return version
	}
}

func baselineExistingSchema(ctx context.Context, conn *pgxpool.Conn, versionColumn migrationVersionColumn, migrations []Migration, logger *log.Logger) error {
	var tracked int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&tracked); err != nil {
		return fmt.Errorf("count schema migrations: %w", err)
	}
	if tracked > 0 {
		return nil
	}

	exists, err := tableExists(ctx, conn, "conferences")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	for _, migration := range migrations {
		switch migration.Version {
		case 1:
			if err := recordMigration(ctx, conn, versionColumn, migration); err != nil {
				return err
			}
			if logger != nil {
				logger.Printf("database migration baseline recorded: %03d_%s", migration.Version, migration.Name)
			}
		default:
			return nil
		}
	}
	return nil
}

func tableExists(ctx context.Context, conn *pgxpool.Conn, tableName string) (bool, error) {
	var exists bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
				AND table_name = $1
		)
	`, tableName).Scan(&exists); err != nil {
		return false, fmt.Errorf("check table %s exists: %w", tableName, err)
	}
	return exists, nil
}

func appliedMigration(ctx context.Context, conn *pgxpool.Conn, versionColumn migrationVersionColumn, version int) (string, bool, error) {
	var checksum string
	err := conn.QueryRow(ctx, `
		SELECT checksum
		FROM schema_migrations
		WHERE version = $1
	`, versionColumn.value(version)).Scan(&checksum)
	if err == nil {
		return checksum, true, nil
	}
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	return "", false, fmt.Errorf("lookup migration %03d: %w", version, err)
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, versionColumn migrationVersionColumn, migration Migration, logger *log.Logger) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %03d: %w", migration.Version, err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, migration.SQL); err != nil {
		return fmt.Errorf("apply migration %03d %s: %w", migration.Version, migration.Path, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO schema_migrations (version, name, checksum)
		VALUES ($1, $2, $3)
	`, versionColumn.value(migration.Version), migration.Name, migration.Checksum); err != nil {
		return fmt.Errorf("record migration %03d: %w", migration.Version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %03d: %w", migration.Version, err)
	}
	if logger != nil {
		logger.Printf("database migration applied: %03d_%s", migration.Version, migration.Name)
	}
	return nil
}

func recordMigration(ctx context.Context, conn *pgxpool.Conn, versionColumn migrationVersionColumn, migration Migration) error {
	_, err := conn.Exec(ctx, `
		INSERT INTO schema_migrations (version, name, checksum)
		VALUES ($1, $2, $3)
		ON CONFLICT (version) DO NOTHING
	`, versionColumn.value(migration.Version), migration.Name, migration.Checksum)
	if err != nil {
		return fmt.Errorf("record migration %03d: %w", migration.Version, err)
	}
	return nil
}
