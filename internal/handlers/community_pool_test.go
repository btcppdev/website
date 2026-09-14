package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/prizepool"
	"btcpp-web/internal/types"
	"bytes"
	"encoding/json"
	"github.com/skip2/go-qrcode"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCommunityPoolIntegration(t *testing.T) {
	old, _ := os.Getwd()
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	conf := &types.Conf{Tag: "berlin26", Desc: "bitcoin++ Berlin", Location: "Berlin", Tagline: "Build on Bitcoin", ShowHackathon: true, StartDate: now, DateDesc: "September 2026"}
	pool := &prizepool.Pool{ID: "preview-community", Slug: conf.Tag, Domain: "zap.btcplusplus.dev", TotalMSat: 1250000000, Count: 42, Status: "open", SyncedAt: &now, Offer: "lno1preview-not-a-payable-offer"}
	competition := &types.HackathonCompetition{ID: "preview-hackathon", Title: "Build on Bitcoin", Visibility: "public"}
	confPage := &ConfPage{Conf: conf, Hackathon: competition, CommunityPool: pool}
	hackPage := &HackathonPage{Conf: conf, Competition: competition, CommunityPool: pool, CommunityShare: true}
	for _, c := range []struct {
		name string
		data any
	}{{"conf/generic.tmpl", confPage}, {"hackathon.tmpl", hackPage}} {
		var b bytes.Buffer
		if err := app.TemplateCache.ExecuteTemplate(&b, c.name, c.data); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, needle := range []string{"community-prize", "1250000", "2M sats", "berlin26@zap.btcplusplus.dev"} {
			if !strings.Contains(b.String(), needle) {
				t.Fatalf("%s missing %s", c.name, needle)
			}
		}
	}
	card := communitySocialCard(app, conf, pool)
	if card.Value != "1250000" || card.Title != "Community prize" {
		t.Fatalf("wrong community unfurl: %#v", card)
	}
	if hackPage.SEOPath() != "/berlin26/hackathon?prize=community" {
		t.Fatal("missing dedicated share URL")
	}
	if os.Getenv("COMMUNITY_POOL_PREVIEW") != "1" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/preview/community-card", func(w http.ResponseWriter, r *http.Request) {
		preview := card
		preview.Images = []string{"http://127.0.0.1:8104/static/img/rebrand/hackathon-trophy.jpg"}
		b, err := imgproc.RenderSiteSocialCardHTML(preview)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(b)
	})
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/navigation/account", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`<a href="/signin">Sign in</a>`)) })
	mux.HandleFunc("/berlin26/prize-pool/events", func(w http.ResponseWriter, r *http.Request) {
		serveCommunityStream(w, r, make(chan struct{}), func() (string, error) { b, err := json.Marshal(communityStatusData(pool)); return string(b), err })
	})
	mux.HandleFunc("/berlin26/prize-pool/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sats": pool.Sats(), "goal": pool.GoalLabel(), "progress": pool.Progress(), "milestone": pool.Milestone(), "count": pool.Count, "stale": false, "status": "open"})
	})
	mux.HandleFunc("/berlin26/prize-pool/qr", func(w http.ResponseWriter, r *http.Request) {
		b, _ := qrcode.Encode("Preview only. No payment can be made with this QR.", qrcode.Medium, 256)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/berlin26/hackathon", func(w http.ResponseWriter, r *http.Request) {
		if err := app.TemplateCache.ExecuteTemplate(w, "hackathon.tmpl", hackPage); err != nil {
			t.Log(err)
		}
	})
	mux.HandleFunc("/berlin26", func(w http.ResponseWriter, r *http.Request) {
		if err := app.TemplateCache.ExecuteTemplate(w, "conf/generic.tmpl", confPage); err != nil {
			t.Log(err)
		}
	})
	t.Log("Synthetic preview: http://127.0.0.1:8104/berlin26#hackathon — no real invoices")
	t.Fatal(http.ListenAndServe("127.0.0.1:8104", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})))
}

func TestCommunityPrizeTotals(t *testing.T) {
	page := &HackathonPage{CommunityPool: &prizepool.Pool{TotalMSat: 1250000000, LinkedPrizeIDs: []string{"linked"}}, PrizesByAward: map[string][]*types.Prize{"award": {{ID: "sponsor", ValueText: "500000"}, {ID: "linked", ValueText: "1000000"}}}}
	if got := page.PrizePoolSats(); got != 1750000 {
		t.Fatalf("community funding must count once: %d", got)
	}
	if got := page.PublicAwardCount(); got != 1 {
		t.Fatalf("community award missing: %d", got)
	}
	page.CommunityPool = nil
	if got := page.PrizePoolSats(); got != 1500000 {
		t.Fatalf("existing prize total changed: %d", got)
	}
}
