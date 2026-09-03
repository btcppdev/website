package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
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
	if env.Prod {
		log.Fatal("refusing to mint dev login link while PROD=true")
	}
	env.HMACKey, err = types.DeriveHMACKey(os.Getenv("HMAC_SECRET"))
	if err != nil {
		log.Fatal(err)
	}
	if err := env.Validate(); err != nil {
		log.Fatal(err)
	}

	databaseContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := db.Open(databaseContext, env.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	ctx := &config.AppContext{Env: env, DB: config.NewDatabase(pool), Err: log.Default()}
	link := auth.MagicLink(ctx, *email, *next)
	if link == "" {
		log.Fatal("unable to create development login link")
	}
	fmt.Println(link)
}
