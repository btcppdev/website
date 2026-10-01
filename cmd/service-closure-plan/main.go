// service-closure-plan records reviewed timing only. It does not stop products,
// change paid access, cancel providers or pay refunds.
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
	service := flag.String("service", "", "alerts or insider")
	record := flag.Bool("record", false, "record immutable reviewed closure timing after sales stop; does not enforce shutdown")
	actor := flag.String("actor", "", "active global admin UUID")
	reason := flag.String("reason", "", "reviewed closure reason")
	closes := flag.String("closes-at", "", "RFC3339 closure instant, with timezone and whole seconds")
	flag.Parse()
	if flag.NArg() != 0 || (*service != "alerts" && *service != "insider") || (!*record && (*actor != "" || *reason != "" || *closes != "")) {
		log.Fatal("provide -service alerts|insider; recording also requires -record -actor -reason -closes-at")
	}
	var at time.Time
	if *record {
		var err error
		at, err = time.Parse(time.RFC3339, *closes)
		if err != nil || at.Nanosecond() != 0 || *actor == "" || *reason == "" {
			log.Fatal("recording requires actor, reason and a whole-second RFC3339 closure time")
		}
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
	if *record {
		if err := getters.RecordServiceClosurePlan(ctx, app, *actor, *service, at, *reason); err != nil {
			log.Fatal(err)
		}
	}
	plan, err := getters.GetServiceClosurePlan(ctx, app, *service)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Service string                      `json:"service"`
		Plan    *getters.ServiceClosurePlan `json:"plan"`
	}{*service, plan}); err != nil {
		log.Fatal(err)
	}
}
