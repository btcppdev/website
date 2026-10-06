package handlers

import (
	"btcpp-web/internal/types"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
)

func TestTemplatesParse(t *testing.T) {
	repoRoot := findRepoRoot(t)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() = %v", err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("Chdir(%q) = %v", repoRoot, err)
	}
	defer func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	}()

	ctx := &config.AppContext{}
	if err := loadTemplates(ctx); err != nil {
		t.Fatalf("loadTemplates() = %v", err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() = %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "templates")); err == nil {
			return wd
		}
		next := filepath.Dir(wd)
		if next == wd {
			t.Fatalf("could not find repo root from %q", wd)
		}
		wd = next
	}
}

func TestShouldNoIndexPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/", false},
		{"/berlin26", false},
		{"/talk/berlin26", false},
		{"/volunteer/berlin26", false},
		{"/auth", true},
		{"/dashboard", true},
		{"/dashboard/berlin26/edit", true},
		{"/logout", true},
		{"/navigation/account", true},
		{"/live/status", true},
		{"/invite-speaker/proposal-id", true},
		{"/ticket/abc", true},
		{"/tix/ticket/checkout", true},
		{"/conf/berlin26/success", true},
		{"/berlin26/admin/applicants", true},
		{"/berlin26/volcoord", true},
		{"/berlin26/success", true},
		{"/berlin26/talk/session/calendar.ics", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := shouldNoIndexPath(tt.path); got != tt.want {
				t.Fatalf("shouldNoIndexPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestAbsoluteSEOURL(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"", SEOHost + "/"},
		{"/talk", SEOHost + "/talk"},
		{"talk", SEOHost + "/talk"},
		{"https://example.com/card.png", "https://example.com/card.png"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := absoluteSEOURL(tt.path); got != tt.want {
				t.Fatalf("absoluteSEOURL(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestSEOMultilingualSitemapXML(t *testing.T) {
	var body bytes.Buffer
	body.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">`)
	languages := conferenceCatalog.Languages("seoul", "en")
	for _, language := range languages {
		writeSitemapURL(&body, absoluteSEOURL(language.URL), "", "weekly", "0.8", languages...)
	}
	writeSitemapURL(&body, "https://btcpp.dev/test?a=1&b=2", "", "", "")
	body.WriteString(`</urlset>`)
	var doc struct {
		URLs []struct {
			Loc   string `xml:"loc"`
			Links []struct {
				Lang string `xml:"hreflang,attr"`
				Href string `xml:"href,attr"`
			} `xml:"http://www.w3.org/1999/xhtml link"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.URLs) != 3 || doc.URLs[1].Loc != "https://btcpp.dev/ko/seoul" {
		t.Fatalf("missing localized URL: %s", body.String())
	}
	for _, entry := range doc.URLs[:2] {
		if len(entry.Links) != 2 || entry.Links[1].Href != "https://btcpp.dev/ko/seoul" {
			t.Fatalf("missing reciprocal alternates: %+v", entry)
		}
	}
	if doc.URLs[2].Loc != "https://btcpp.dev/test?a=1&b=2" {
		t.Fatal("XML URL escaping changed URL")
	}
}

func TestSEOEventDatesAndMetadata(t *testing.T) {
	t.Chdir(findRepoRoot(t))
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{Tag: "seoul", Desc: `Seoul "privacy" <edition>`, Location: "Seoul", StartDate: time.Date(2026, 11, 5, 0, 0, 0, 0, loc), EndDate: time.Date(2026, 11, 6, 0, 0, 0, 0, loc), TZ: loc, PickupAddressLine1: "1 Test Street", PickupAddressCity: "Seoul", PickupAddressCountry: "KR"}
	page := &ConfPage{Conf: conf, Tix: &types.ConfTicket{ID: "pass", BasePrice: 389, Currency: "USD"}, TixLeft: 1}
	for _, language := range []string{"en", "ko"} {
		templates := ctx.TemplateCache
		if language == "ko" {
			templates = ctx.LocalizedTemplates[language]
		}
		var body bytes.Buffer
		if err := templates.ExecuteTemplate(&body, "event_jsonld", page); err != nil {
			t.Fatal(err)
		}
		content := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(body.String())
		var event struct {
			Start       string `json:"startDate"`
			URL         string `json:"url"`
			Description string `json:"description"`
			Offers      struct {
				URL   string `json:"url"`
				Price int    `json:"price"`
			}
			Location struct{ Address map[string]any }
		}
		if len(content) != 2 {
			t.Fatal("missing structured data")
		}
		if err := json.Unmarshal([]byte(content[1]), &event); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, body.String())
		}
		root := "https://btcpp.dev/seoul"
		if language == "ko" {
			root = "https://btcpp.dev/ko/seoul"
		}
		if event.Start != "2026-11-05" || event.URL != root || event.Offers.URL != root+"#tickets" || event.Offers.Price != 389 {
			t.Fatalf("wrong event metadata: %+v", event)
		}
		if event.Description == "" || event.Location.Address["streetAddress"] != "1 Test Street" {
			t.Fatal("missing description/address")
		}
	}
	if got := eventSEODate(conf, conf.StartDate.Add(10*time.Hour)); got != "2026-11-05T10:00:00+09:00" {
		t.Fatalf("explicit time lost: %s", got)
	}
	if eventSEODate(conf, time.Time{}) != "" {
		t.Fatal("invented unknown event date")
	}
}

func TestSEOOperationalRoutesDoNotHidePublicContent(t *testing.T) {
	for _, path := range []string{"/shop/cart", "/shop/checkout", "/shop/success/test", "/seoul/hackathon/projects/new", "/seoul/hackathon/projects/123/edit", "/seoul/hackathon/judging", "/reauth", "/signer/authorize"} {
		if !shouldNoIndexPath(path) {
			t.Errorf("indexable operational route %s", path)
		}
	}
	for _, path := range []string{"/shop", "/shop/product/shirt", "/ko/seoul", "/seoul/hackathon", "/seoul/hackathon/projects/123", "/seoul/agenda", "/seoul/speakers"} {
		if shouldNoIndexPath(path) {
			t.Errorf("public content excluded %s", path)
		}
	}
}

func TestSEOPublicProjectMetadata(t *testing.T) {
	t.Chdir(findRepoRoot(t))
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	page := &HackathonPage{Conf: &types.Conf{Tag: "seoul"}, Competition: &types.HackathonCompetition{Title: "Privacy hackathon"}, Project: &types.HackathonProject{ID: "project-id", Title: "Test project", ShortDescription: "Build private payments", Status: "submitted"}}
	var body bytes.Buffer
	if err := ctx.TemplateCache.ExecuteTemplate(&body, "hackathon_project.tmpl", page); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`rel="canonical" href="https://btcpp.dev/seoul/hackathon/projects/project-id"`, `name="description" content="Build private payments"`, `property="og:title"`} {
		if !strings.Contains(body.String(), want) {
			t.Errorf("missing %s", want)
		}
	}
}
