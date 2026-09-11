package handlers

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestTickerWidgets(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	page := tickerWidgetPage{SponsorsMode: true, Sponsors: []*types.Sponsorship{{Org: &types.Org{Name: "Example sponsor", LogoLight: "https://example.com/logo.png"}}}}
	rr := httptest.NewRecorder()
	renderTickerWidget(rr, httptest.NewRequest("GET", "/widgets/berlin26/sponsors", nil), app, page)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `class="sponsor-marquee sponsor-banner__viewport relative"`) || strings.Count(body, `alt="Example sponsor"`) != 2 {
		t.Fatal("widget does not reuse duplicated sponsor viewport")
	}

	for _, logo := range []string{`alt="bitcoin++ Insider Edition"`, `alt="bitcoin++"`} {
		if strings.Count(body, logo) != 2 {
			t.Fatalf("both reel groups must include %s", logo)
		}
	}
	if strings.Contains(body, "Supported by") || strings.Contains(body, `<header`) {
		t.Fatal("widget includes page chrome")
	}
	if rr.Header().Get("Content-Security-Policy") != "frame-ancestors *" || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("widget framing/cache headers missing")
	}
	router := mux.NewRouter()
	registerTickerWidgets(router, app)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/widgets/ticker", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "data-site-ticker") || !strings.Contains(rr.Body.String(), "/live/status") {
		t.Fatal("site widget route or live behavior missing")
	}
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/widgets/ticker", nil))
	if rr.Code != 405 {
		t.Fatalf("mutation method accepted: %d", rr.Code)
	}
}

func TestWidgetSponsorSelectionMatchesBanner(t *testing.T) {
	var sponsors []*types.Sponsorship
	for _, row := range []struct{ level, status string }{{"Title", "Paid"}, {"Diamond", "Committed"}, {"Gold", "Paid"}, {"Title", "Pending"}} {
		sponsors = append(sponsors, &types.Sponsorship{Level: row.level, Status: row.status, Org: &types.Org{Name: row.level}})
	}
	selected := sponsorBannerFromTiers(groupSponsorTiers(sponsors))
	if len(selected) != 2 || selected[0].Level != "Diamond" || selected[1].Level != "Title" {
		t.Fatalf("unexpected public sponsor selection: %#v", selected)
	}
}
