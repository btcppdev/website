// refresh-social-cards reconciles stored talk and speaker images after a layout
// release, including past events that the server's active-event worker skips.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync/atomic"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/handlers"
	"btcpp-web/internal/types"
)

type errorLog struct{ count atomic.Int64 }

func (w *errorLog) Write(p []byte) (int, error) {
	w.count.Add(1)
	return os.Stderr.Write(p)
}

func selectedConference(conf *types.Conf, tag string, all bool) bool {
	return conf != nil && conf.IsPublished() && (all || conf.Tag == tag)
}

func main() {
	tag := flag.String("conf", "", "published conference tag")
	all := flag.Bool("all-published", false, "include every published event, including archives")
	apply := flag.Bool("apply", false, "regenerate changed talk and speaker images in Spaces")
	flag.Parse()
	if flag.NArg() != 0 || (*tag == "") == !*all {
		log.Fatal("choose exactly one of -conf TAG or -all-published; add -apply to upload")
	}
	env, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	env.HMACKey, err = types.DeriveHMACKey(env.HMACSecret)
	if err != nil {
		log.Fatal(err)
	}
	if *apply && !env.Prod {
		log.Fatal("image uploads require the production environment")
	}
	pool, err := db.Open(context.Background(), env.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	errors := &errorLog{}
	app := &config.AppContext{Env: env, DB: pool, InProduction: env.Prod, Infos: log.New(os.Stdout, "", log.LstdFlags), Err: log.New(errors, "ERROR ", log.LstdFlags)}
	confs, err := getters.ListConfs(app)
	if err != nil {
		log.Fatal(err)
	}
	var talks []*types.Talk
	selected := 0
	for _, conf := range confs {
		if !selectedConference(conf, *tag, *all) {
			continue
		}
		selected++
		items, err := getters.LoadTalksFromConfTalks(app, conf.Tag)
		if err != nil {
			log.Fatalf("load %s: %v", conf.Tag, err)
		}
		fmt.Printf("%s: %d talks\n", conf.Tag, len(items))
		talks = append(talks, items...)
	}
	if selected == 0 {
		log.Fatal("no published conferences matched")
	}
	fmt.Printf("Selected %d talks across %d published events; apply=%t\n", len(talks), selected, *apply)
	if !*apply {
		return
	}
	spaces.Init(env.Spaces)
	if !spaces.IsConfigured() {
		log.Fatal("Spaces is not configured")
	}
	if err := handlers.PreloadCardHashes(app); err != nil {
		log.Fatal(err)
	}
	// Keep content-hash deduplication, while allowing archived conferences.
	handlers.RefreshTalkCardsForceOpt(app, talks, false)
	if n := errors.count.Load(); n > 0 {
		log.Fatalf("refresh recorded %d errors; inspect the log and rerun to retry", n)
	}
}
