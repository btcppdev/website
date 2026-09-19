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
	id := flag.String("checkout", "", "checkout UUID; omit to inspect oldest 100 unresolved cancellation jobs")
	requeue := flag.Bool("requeue", false, "requeue an inspected review job; does not call Stripe directly")
	expected := flag.Int64("expected-attempt", 0, "attempt from inspection, required for requeue")
	actor := flag.String("actor", "", "operator's bitcoin++ global-admin UUID, required for requeue audit")
	reason := flag.String("reason", "", "reason for recovery; omit personal data and secrets")
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
	app := &config.AppContext{DB: pool, Env: env}
	if *requeue {
		changed, err := getters.RecoverAccountDeletionCancellation(ctx, app, *id, *expected, *actor, *reason)
		if err != nil {
			log.Fatal(err)
		}
		if !changed {
			log.Fatal("not requeued: inspect current state, attempt and lease")
		}
	}
	items, err := getters.ListAccountDeletionCancellations(ctx, app, *id)
	if err != nil {
		log.Fatal(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(items); err != nil {
		log.Fatal(err)
	}
}
