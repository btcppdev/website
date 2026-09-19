// service-refunds inspects exceptional refund accounting and delivery. It never
// contacts payment providers, records access decisions, or sends messages.
package main

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"
)

func main() {
	attempt := flag.String("attempt", "", "payment attempt UUID; omit to list oldest unresolved refunds (up to 100)")
	flag.Parse()
	if flag.NArg() != 0 {
		log.Fatal("unexpected arguments; use -help")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	env, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	pool, err := db.Open(ctx, env.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	items, err := getters.ListServiceRefunds(ctx, &config.AppContext{DB: pool, Env: env}, *attempt)
	if err != nil {
		log.Fatal(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(items); err != nil {
		log.Fatal(err)
	}
}
