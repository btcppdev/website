package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/types"
)

func main() {
	email := flag.String("email", "dev-admin@example.test", "email address for the local login link")
	next := flag.String("next", "/admin", "relative path to visit after login")
	flag.Parse()

	env, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	if err := validateDevLoginEnv(env); err != nil {
		log.Fatal(err)
	}

	databaseContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := db.Open(databaseContext, env.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	ctx := &config.AppContext{Env: env, DB: pool, Err: log.Default()}
	link := auth.MagicLink(ctx, *email, *next)
	if link == "" {
		log.Fatal("unable to create development login link")
	}
	fmt.Println(link)
}

// validateDevLoginEnv checks only what this one-purpose helper consumes.
// Full server validation includes optional runtime integrations such as X
// Studio; those should not prevent a local, database-backed login link from
// being minted.
func validateDevLoginEnv(env *types.EnvConfig) error {
	if env == nil {
		return errors.New("nil environment config")
	}
	if env.Prod {
		return errors.New("refusing to mint dev login link while PROD=true")
	}
	if strings.TrimSpace(env.DatabaseURL) == "" {
		return errors.New("missing required config: DATABASE_URL")
	}
	if strings.TrimSpace(env.Host) == "" {
		return errors.New("missing required config: HOST")
	}
	return nil
}
