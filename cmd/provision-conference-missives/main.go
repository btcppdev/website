package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	texttemplate "text/template"

	"btcpp-web/internal/envconfig"
	conferencemissives "btcpp-web/templates/missives"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	envPath := flag.String("env", ".env", "environment file containing DATABASE_URL")
	databaseURL := flag.String("database-url", "", "PostgreSQL URL override")
	apply := flag.Bool("apply", false, "commit missing one-shot templates (default is dry-run)")
	flag.Parse()

	env, err := envconfig.Load(*envPath)
	if err != nil {
		log.Fatal(err)
	}
	dsn := strings.TrimSpace(*databaseURL)
	if dsn == "" {
		dsn = strings.TrimSpace(env.DatabaseURL)
	}
	if dsn == "" {
		log.Fatal("DATABASE_URL is required; use -env or -database-url")
	}
	definitions, err := conferencemissives.Definitions()
	if err != nil {
		log.Fatal(err)
	}
	for _, definition := range definitions {
		if _, err := texttemplate.New("subject").Parse(definition.Title); err != nil {
			log.Fatalf("invalid subject for %s: %v", definition.OnlyFor, err)
		}
		if _, err := texttemplate.New("body").Parse(definition.Markdown); err != nil {
			log.Fatalf("invalid body for %s: %v", definition.OnlyFor, err)
		}
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)

	created := 0
	for _, definition := range definitions {
		status, err := provision(ctx, tx, definition)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%-46s %s\n", definition.OnlyFor, status)
		if status == "create" {
			created++
		}
	}
	if !*apply {
		fmt.Printf("dry run: %d template(s) would be created; rerun with -apply to commit\n", created)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("applied: %d template(s) created; existing templates were left unchanged\n", created)
}

func provision(ctx context.Context, tx pgx.Tx, definition conferencemissives.Definition) (string, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM missives WHERE dedupe_key = $1)
	`, "conference-template:"+definition.Kind).Scan(&exists); err != nil {
		return "", fmt.Errorf("check %s: %w", definition.OnlyFor, err)
	}
	if exists {
		return "exists", nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO missives
			(public_uid, title, newsletters, only_for, markdown, send_at_expr, dedupe_key)
		VALUES
			((SELECT COALESCE(max(public_uid), 0) + 1 FROM missives), $1, '{}'::text[], $2, $3, '', $4)
	`, definition.Title, definition.OnlyFor, definition.Markdown, "conference-template:"+definition.Kind); err != nil {
		return "", fmt.Errorf("create %s: %w", definition.OnlyFor, err)
	}
	return "create", nil
}
