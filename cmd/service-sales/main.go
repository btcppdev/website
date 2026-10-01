// service-sales records the first step of permanent service closure. It does not
// call providers, cancel renewals, end access or send notifications.
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
	actor := flag.String("actor", "", "active global admin UUID authorizing the closure")
	reason := flag.String("reason", "", "permanent closure reason")
	stop := flag.Bool("confirm-stop-sales", false, "permanently stop new purchases; does not cancel existing provider sessions or renewals")
	flag.Parse()
	if flag.NArg() != 0 || (*service != "alerts" && *service != "insider") || (!*stop && (*actor != "" || *reason != "")) {
		log.Fatal("provide -service alerts|insider; writes additionally require -confirm-stop-sales -actor -reason")
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
	if *stop {
		if err := getters.StopServiceSales(ctx, app, *actor, *service, *reason); err != nil {
			log.Fatal(err)
		}
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT coalesce((SELECT jsonb_build_object('service',service,'stopped_at',stopped_at,'actor_id',actor_id,'reason',reason) FROM service_sales_closures WHERE service=$1),jsonb_build_object('service',$1::text,'stopped_at',null))`, *service).Scan(&raw); err != nil {
		log.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		log.Fatal(err)
	}
}
