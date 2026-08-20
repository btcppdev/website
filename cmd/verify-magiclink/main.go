// verify-magiclink checks whether a database-backed magic login URL is still
// valid without consuming it.
//
// Usage:
//
//	go run ./cmd/verify-magiclink -url 'http://localhost:8888/auth?token=...'
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
)

func main() {
	rawURL := flag.String("url", "", "magic-link URL to verify (required)")
	flag.Parse()
	if strings.TrimSpace(*rawURL) == "" {
		log.Fatal("required: -url")
	}
	u, err := url.Parse(*rawURL)
	if err != nil {
		log.Fatalf("parse URL: %s", err)
	}
	token := strings.TrimSpace(u.Query().Get("token"))
	if token == "" {
		log.Fatal("URL is missing its token query parameter")
	}
	env, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	databaseContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := db.Open(databaseContext, env.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	valid, err := getters.MagicLoginTokenValid(&config.AppContext{Env: env, DB: pool}, token)
	if err != nil {
		log.Fatal(err)
	}
	if !valid {
		log.Fatal("magic login link is expired, consumed, or invalid")
	}
	fmt.Println("VALID — the magic login link is unexpired and has not been consumed.")
}
