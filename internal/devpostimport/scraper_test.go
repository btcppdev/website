package devpostimport

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSatoshiValue(t *testing.T) {
	tests := map[string]int64{
		"2.5 Million Satoshis + tickets": 2_500_000,
		"750k sats":                      750_000,
		"210,000 Satoshis":               210_000,
		"tickets only":                   0,
	}
	for input, want := range tests {
		if got := parseSatoshiValue(input); got != want {
			t.Errorf("parseSatoshiValue(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestScrapePaginatesAndMapsWinners(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/project-gallery" && r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, galleryFixture(server.URL+"/software/two", "2", "Two"))
			return
		}
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html><head><meta name="description" content="Build things"></head><body>
			<script id="challenge-json-ld" type="application/ld+json">{"name":"Test Hack","description":"&lt;p&gt;Hello&lt;/p&gt;","startDate":"2025-01-01T10:00:00Z","endDate":"2025-01-02T10:00:00Z","image":"https://img.example/hero.png"}</script>
			<article id="prizes"><div class="prize" id="prize_1"><div class="prize-title"><div>First Place</div></div><div class="prize-content"><div class="prize-value">Bitcoin</div><div class="prize-winners">1 winner</div><p>2.5 Million Satoshis</p></div></div></article>
			<article id="judges"><div class="challenge_judge"><img src="//img.example/judge.png"><p><strong>Ada</strong><br><i>Bitcoin++</i></p></div></article>
			</body></html>`)
		case "/project-gallery":
			fmt.Fprint(w, galleryFixture(server.URL+"/software/one", "1", "One")+`<ul class="pagination"><li><a rel="next" href="/project-gallery?page=2">2</a></li></ul>`)
		case "/software/one":
			fmt.Fprint(w, projectFixture("One", "First Place"))
		case "/software/two":
			fmt.Fprint(w, projectFixture("Two", ""))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	scraper := NewScraper()
	scraper.Delay = 0
	manifest, err := scraper.Scrape(context.Background(), server.URL, "test25")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(manifest.Projects))
	}
	if len(manifest.Awards) != 1 || len(manifest.Awards[0].Winners) != 1 || manifest.Awards[0].Winners[0] != "one" {
		t.Fatalf("awards = %+v", manifest.Awards)
	}
	if manifest.Awards[0].SatoshiValue != 2_500_000 {
		t.Fatalf("satoshi value = %d", manifest.Awards[0].SatoshiValue)
	}
	if len(manifest.Judges) != 1 || manifest.Judges[0].Name != "Ada" {
		t.Fatalf("judges = %+v", manifest.Judges)
	}
}

func TestDownloadAssetsArchivesProjectImages(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer server.Close()
	manifest := &Manifest{Projects: []Project{{Slug: "demo", Images: []Asset{{SourceURL: server.URL + "/image.png"}}}}}
	root := t.TempDir()
	if err := NewScraper().DownloadAssets(context.Background(), manifest, root); err != nil {
		t.Fatal(err)
	}
	asset := manifest.Projects[0].Images[0]
	if asset.LocalPath == "" || asset.ContentType != "image/png" {
		t.Fatalf("asset = %+v", asset)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(asset.LocalPath))); err != nil {
		t.Fatal(err)
	}
}

func galleryFixture(projectURL, id, title string) string {
	return fmt.Sprintf(`<html><body><div class="gallery-item" data-software-id="%s"><a class="link-to-software" href="%s"><div class="software-entry"><img class="software_thumbnail_image" src="https://img.example/%s.png"><h5>%s</h5><p class="tagline">Tagline</p></div></a></div></body></html>`, id, projectURL, id, title)
}

func projectFixture(title, award string) string {
	winner := ""
	if award != "" {
		winner = `<div id="submissions"><li><span class="winner">Winner</span> ` + award + `</li></div>`
	}
	return `<html><body><header id="software-header"><h1 id="app-title">` + title + `</h1><p>Tagline</p></header><div id="app-details-left"><div id="gallery"><a data-lightbox="1" data-title="Screenshot" href="https://img.example/original.png"><img></a></div><div><h2>What it does</h2><p>Useful.</p></div><div id="built-with"><span class="cp-tag">bitcoin</span></div></div><section id="app-team"><li class="software-team-member"><a class="user-profile-link" href="https://devpost.com/person">Person</a></li></section>` + winner + `</body></html>`
}
