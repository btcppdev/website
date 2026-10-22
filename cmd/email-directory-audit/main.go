// email-directory-audit reports migration counts without modifying the database.
package main

import (
	"btcpp-web/internal/emaildirectory"
	"btcpp-web/internal/envconfig"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"time"
)

func main() {
	// Existing environment takes precedence. Never log connection strings/errors
	// which may embed addresses or credentials.
	_ = envconfig.LoadDotEnv(".env")
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL must be set in .env or the environment")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unable to configure database connection")
		os.Exit(1)
	}
	defer pool.Close()
	report, err := emaildirectory.Audit(ctx, pool)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Directory audit failed; check database access and existing central schema")
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err = enc.Encode(report); err != nil {
		os.Exit(1)
	}
	if !report.CanMigrate {
		os.Exit(2)
	}
}
