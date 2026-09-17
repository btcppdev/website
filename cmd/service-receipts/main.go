package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
)

func main() {
	order := flag.String("order", "", "public order ID; omit to list up to 100 pending/review jobs")
	requeue := flag.Bool("requeue", false, "requeue a reviewed receipt; does not send mail directly")
	attempt := flag.Int64("expected-attempt", 0, "attempt number from inspection, required for requeue")
	actor := flag.String("actor", "", "operator identity for audit")
	reason := flag.String("reason", "", "reason for recovery; do not include personal data or secrets")
	flag.Parse()
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
	app := &config.AppContext{DB: pool, Env: env}
	if *requeue {
		ok, err := getters.RecoverServiceReceipt(ctx, app, *order, *attempt, *actor, *reason)
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			log.Fatal("receipt not requeued: inspect current state, attempt, payment and lease")
		}
	}
	states, err := getters.ListServiceReceipts(ctx, app, *order)
	if err != nil {
		log.Fatal(err)
	}
	if err = json.NewEncoder(os.Stdout).Encode(states); err != nil {
		log.Fatal(err)
	}
}
