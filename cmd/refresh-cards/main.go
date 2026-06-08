// refresh-cards regenerates social-media card PNGs in Spaces for talks +
// speakers, sourced from ConfTalk → Proposal → SpeakerConf → Speaker.
//
// Filters (combine to AND-narrow):
//
//	-conf <tag>     Only talks where Talk.Event == tag.
//	-speaker <q>    Only talks where ANY speaker's name matches q
//	                (case-insensitive substring).
//
// With no flags, every talk in every conf is refreshed. Idempotent — already-
// generated cards short-circuit via the in-memory hash cache (loaded at
// startup) and a Spaces.Exists check.
//
// Usage:
//
//	go run ./cmd/refresh-cards                            # everything
//	go run ./cmd/refresh-cards -conf berlin26             # one conf
//	go run ./cmd/refresh-cards -speaker neigut            # one speaker
//	go run ./cmd/refresh-cards -conf berlin26 -speaker neigut
//	go run ./cmd/refresh-cards -conf berlin26 -sponsors-only -force
package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/handlers"
	"btcpp-web/internal/types"
)

func main() {
	confTag := flag.String("conf", "", "Restrict to talks where Talk.Event matches this conf tag")
	speakerQ := flag.String("speaker", "", "Restrict to talks containing a speaker whose name matches (case-insensitive substring)")
	orgQ := flag.String("org", "", "Restrict sponsor refresh to orgs whose name matches (case-insensitive substring). Implies sponsor-only.")
	sponsorsOnly := flag.Bool("sponsors-only", false, "Refresh only sponsor cards for -conf. Can be combined with -org and -force.")
	force := flag.Bool("force", false, "Bypass hash + Spaces.Exists short-circuits — always re-render and re-upload. Useful when a logo's bytes changed but the filename didn't.")
	activeOnly := flag.Bool("active", false, "Refresh talks/speakers/sponsors for every Active+future conf. Mutually exclusive with -conf.")
	flag.Parse()
	if *activeOnly && (*confTag != "" || *speakerQ != "" || *orgQ != "" || *sponsorsOnly) {
		log.Fatalf("-active is incompatible with -conf / -speaker / -org / -sponsors-only")
	}
	if *orgQ != "" && *confTag == "" {
		log.Fatalf("-org requires -conf")
	}
	if *sponsorsOnly && *confTag == "" {
		log.Fatalf("-sponsors-only requires -conf")
	}
	if *sponsorsOnly && *speakerQ != "" {
		log.Fatalf("-sponsors-only is incompatible with -speaker")
	}

	env, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	for k, v := range map[string]string{
		"NOTION_TOKEN":           env.Notion.Token,
		"NOTION_CONFS_DB":        env.Notion.ConfsDb,
		"NOTION_CONFSTIX_DB":     env.Notion.ConfsTixDb,
		"NOTION_SPEAKERS_DB":     env.Notion.SpeakersDb,
		"NOTION_PROPOSAL_DB":     env.Notion.ProposalDb,
		"NOTION_SPEAKER_CONF_DB": env.Notion.SpeakerConfDb,
		"NOTION_CONFTALK_DB":     env.Notion.ConfTalkDb,
	} {
		if v == "" {
			log.Fatalf("missing %s", k)
		}
	}

	nc := &env.Notion
	n := &types.Notion{Config: nc}
	n.Setup(env.Notion.Token)

	if env.Host == "" || env.Port == "" {
		log.Fatal("missing HOST / PORT — Chrome needs a reachable URL to render card templates. Run `make dev-run` first.")
	}
	appCtx := &config.AppContext{
		Env: &types.EnvConfig{
			Notion:      *nc,
			CacheTTLSec: 300,
			Host:        env.Host,
			Port:        env.Port,
			Prod:        false, // local: GetURI builds http://host:port
		},
		Notion:       n,
		InProduction: true, // skip disk cache bootstrap; doesn't affect GetURI
		Err:          log.New(os.Stderr, "ERR ", log.LstdFlags),
		Infos:        log.New(os.Stdout, "INFO ", log.LstdFlags),
	}

	// Confs cache must be warm for parseProposal/parseConfTalk to resolve
	// tag → *Conf via lookupConfByTag.
	getters.StartWorkPool(appCtx)
	defer getters.CloseWorkPool()
	getters.WaitFetch(appCtx)

	spaces.Init(env.Spaces)
	if !spaces.IsConfigured() {
		log.Fatal("spaces is not configured (check SPACES_* env vars)")
	}

	// Pull the existing hash index so unchanged cards short-circuit.
	handlers.PreloadCardHashes(appCtx)

	if *activeOnly {
		confs, _ := getters.FetchConfsCached(appCtx)
		var refreshed int
		for _, c := range confs {
			if c == nil || !c.Active || !c.InFuture() {
				continue
			}
			confTalks, err := getters.LoadTalksFromConfTalks(appCtx, c.Tag)
			if err != nil {
				log.Printf("load talks %s: %s — skipping", c.Tag, err)
				continue
			}
			log.Printf("→ %s: %d talks", c.Tag, len(confTalks))
			handlers.RefreshTalkCardsForceOpt(appCtx, confTalks, *force)
			handlers.RefreshSponsorCardsForConfOpt(appCtx, c, "", *force)
			refreshed++
		}
		log.Printf("refresh complete (%d active conf(s))", refreshed)
		return
	}

	// Sponsor-only mode skips the talk/speaker pass and refreshes
	// matching sponsors for the named conf.
	if *sponsorsOnly || *orgQ != "" {
		confs, _ := getters.FetchConfsCached(appCtx)
		var hit *types.Conf
		for _, c := range confs {
			if c != nil && c.Tag == *confTag {
				hit = c
				break
			}
		}
		if hit == nil {
			log.Fatalf("conf %q not found", *confTag)
		}
		handlers.RefreshSponsorCardsForConfOpt(appCtx, hit, *orgQ, *force)
		log.Println("refresh complete")
		return
	}

	talks, err := getters.LoadTalksFromConfTalks(appCtx, *confTag)
	if err != nil {
		log.Fatalf("load talks: %s", err)
	}

	filtered := filterTalks(talks, *speakerQ)
	log.Printf("loaded %d talks; %d match filters (conf=%q speaker=%q)",
		len(talks), len(filtered), *confTag, *speakerQ)

	if len(filtered) == 0 {
		log.Println("nothing to refresh.")
		return
	}

	handlers.RefreshTalkCardsForceOpt(appCtx, filtered, *force)

	// Sponsor cards aren't tied to a talk; refresh them per-conf when
	// -conf is set. Skipped on a no-flag full sweep because the
	// ambient RefreshSponsorCards (called from the running web app)
	// covers active/future confs already.
	if *confTag != "" && *speakerQ == "" {
		confs, _ := getters.FetchConfsCached(appCtx)
		for _, c := range confs {
			if c != nil && c.Tag == *confTag {
				handlers.RefreshSponsorCardsForConfOpt(appCtx, c, "", *force)
				break
			}
		}
	}

	log.Println("refresh complete")
}

// filterTalks narrows talks by speaker-name substring. Pass "" to skip.
// (Conf filtering is already handled by LoadTalksFromConfTalks via the
// confTag arg.)
func filterTalks(talks []*types.Talk, speakerQ string) []*types.Talk {
	if speakerQ == "" {
		return talks
	}
	q := strings.ToLower(strings.TrimSpace(speakerQ))
	var out []*types.Talk
	for _, t := range talks {
		for _, sp := range t.Speakers {
			if strings.Contains(strings.ToLower(sp.Name), q) {
				out = append(out, t)
				break
			}
		}
	}
	return out
}
