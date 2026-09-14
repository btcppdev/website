package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/prizepool"
	"btcpp-web/internal/types"
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPrizePoolTemplates(t *testing.T) {
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
	conf := &types.Conf{Tag: "berlin26", Desc: "bitcoin++ Berlin", Ref: "preview"}
	admin := &HackathonAdminPage{Conf: conf, Confs: []*types.Conf{conf}, Competition: &types.HackathonCompetition{ID: "preview", Title: "Build on Bitcoin", ConferenceID: conf.Ref}, ActiveTab: "community"}
	page := prizePoolPage{HackathonAdminPage: admin, Admin: true, Conf: conf, Domain: "btcplusplus.dev", Pool: &prizepool.Pool{Slug: "berlin-prizes", Domain: "btcplusplus.dev", Status: "open", Description: "Help Berlin's Bitcoin builders", TotalMSat: 210000123, Count: 2, SyncedAt: &now}, Payments: []prizepool.Payment{{Hash: strings.Repeat("a", 64), PaidAt: now, AmountMSat: 200000000, OfferID: "offer", Description: "Help Berlin's Bitcoin builders", Note: "Build great things! <script>alert(1)</script>"}, {Hash: strings.Repeat("b", 64), PaidAt: now.Add(-time.Hour), AmountMSat: 10000123, Description: `[["text/plain","Berlin community prize"],["text/identifier","berlin-prizes@btcplusplus.dev"]]`, Late: true}}}
	for _, payments := range []bool{false, true} {
		page.PaymentsTab = payments
		var b bytes.Buffer
		if err := app.TemplateCache.ExecuteTemplate(&b, "prize_pool.tmpl", page); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"210000.123", "berlin-prizes@btcplusplus.dev", "Successful payments", "/berlin26/admin/hackathon/community-pool"} {
			if !strings.Contains(b.String(), text) {
				t.Fatal("missing " + text)
			}
		}
		if strings.Contains(b.String(), "<script>alert(1)</script>") {
			t.Fatal("payer note is executable")
		}
		if payments && !strings.Contains(b.String(), "After closing") {
			t.Fatal("missing late payment")
		}
	}
	draft := page
	draft.Pool = nil
	draft.PaymentsTab = false
	var b bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&b, "prize_pool.tmpl", draft); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `value="berlin26"`) {
		t.Fatal("missing default address")
	}
	if os.Getenv("PRIZE_ADMIN_PREVIEW") != "1" {
		return
	}
	page.Payments[0].Note = "Build great things!"
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/navigation/account", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`<a href="/signin">Sign in</a>`)) })
	mux.HandleFunc("/berlin26/admin/hackathon", func(w http.ResponseWriter, r *http.Request) {
		p := *admin
		p.IsNew = true
		p.SetupStep = 1
		p.ActiveTab = ""
		if err := app.TemplateCache.ExecuteTemplate(w, "admin/hackathon_detail.tmpl", &p); err != nil {
			t.Log(err)
		}
	})
	mux.HandleFunc("/berlin26/admin/hackathon/community-pool", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Preview only", 405)
			return
		}
		p := page
		p.PaymentsTab = r.URL.Query().Get("tab") == "payments"
		if r.URL.Query().Get("draft") == "1" {
			p.Pool = nil
		}
		p.Error = "Preview only: sample payments. No node or DNS changes."
		if err := app.TemplateCache.ExecuteTemplate(w, "prize_pool.tmpl", p); err != nil {
			t.Log(err)
		}
	})
	t.Log("Preview: http://127.0.0.1:8105/berlin26/admin/hackathon/community-pool?tab=payments")
	t.Fatal(http.ListenAndServe("127.0.0.1:8105", mux))
}
func TestCommunitySetupValues(t *testing.T) {
	for _, c := range []struct {
		slug, description, wantSlug string
		valid                       bool
	}{{"", "", "berlin26", true}, {"builders", "Fund Berlin hackers", "builders", true}, {"../bad", "", "", false}, {"UPPER", "", "", false}, {"fine", strings.Repeat("x", 501), "", false}} {
		form := url.Values{"CommunitySlug": {c.slug}, "CommunityDescription": {c.description}}
		r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		slug, description, err := communitySetupValues(r, &types.Conf{Tag: "berlin26"})
		if (err == nil) != c.valid {
			t.Fatalf("%q: %v", c.slug, err)
		}
		if c.valid && (slug != c.wantSlug || description == "") {
			t.Fatal("invalid defaults")
		}
	}
	pool := prizepool.Pool{Slug: "builders", EventTag: "berlin26", Domain: "btcplusplus.dev"}
	if pool.SharePath() != "/berlin26/hackathon?prize=community#community-prize" || pool.Address() != "builders@btcplusplus.dev" {
		t.Fatal("address name changed event route")
	}
}
