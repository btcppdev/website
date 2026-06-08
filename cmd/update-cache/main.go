package main

import (
	"log"
	"os"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/types"
)

func main() {
	configFile := ".env"
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	c, err := envconfig.Load(configFile)
	if err != nil {
		log.Fatalf("read %s: %s", configFile, err)
	}
	if c.Notion.Token == "" {
		log.Fatalf("missing NOTION_TOKEN in %s", configFile)
	}

	nc := c.Notion
	n := &types.Notion{Config: &nc}
	n.Setup(c.Notion.Token)

	// Force a fresh legacy fetch instead of bootstrapping from stale disk cache.
	if err := os.RemoveAll("_cache"); err != nil {
		log.Fatalf("clear _cache: %s", err)
	}

	appCtx := &config.AppContext{
		Env: &types.EnvConfig{
			Notion:      nc,
			CacheTTLSec: 300,
			Host:        c.Host,
			Port:        c.Port,
			Prod:        false,
		},
		Notion:       n,
		InProduction: false,
		Err:          log.New(os.Stderr, "ERR ", log.LstdFlags),
		Infos:        log.New(os.Stdout, "INFO ", log.LstdFlags),
	}

	getters.StartWorkPool(appCtx)
	defer getters.CloseWorkPool()
	getters.WaitFetch(appCtx)

	log.Printf("cache refresh complete using %s", configFile)
}
