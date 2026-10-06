package handlers

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/i18n"
	"btcpp-web/internal/types"
)

// staticCache wraps an http.Handler with a 1-hour Cache-Control
// header so browsers can serve repeat-visitor /static/* assets from
// cache without revalidating. http.FileServer still emits
// Last-Modified, so a deploy invalidates stale assets via a
// conditional GET → 304 cycle once the hour elapses.
//
// Short max-age (3600s) is deliberate: the legacy CSS files have no
// content hash in the filename, so a longer window could leave visitors
// on stale CSS after a deploy. Move to a fingerprinted-filename strategy
// if we want to push max-age much higher.
func staticCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

// redirectStripConfPrefix 301-redirects from the legacy `/conf/{tag}*`
// URL form to the canonical `/{tag}*` short form. The handler only
// rewrites the path; query string carries through, and the browser
// preserves the hash fragment across the redirect on its own.
func redirectStripConfPrefix(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimPrefix(r.URL.Path, "/conf")
	if target == "" || target[0] != '/' {
		target = "/" + target
	}
	if r.URL.RawQuery != "" {
		target = target + "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

func redirectToConfAgenda(w http.ResponseWriter, r *http.Request, confTag string) {
	confTag = strings.Trim(strings.TrimSpace(confTag), "/")
	if confTag == "" {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
		return
	}
	target := "/" + confTag + "/agenda"
	if r.URL.RawQuery != "" {
		target = target + "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

func noIndexRobots(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldNoIndexPath(r.URL.Path) {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		h.ServeHTTP(w, r)
	})
}

func shouldNoIndexPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return false
	}
	if strings.HasSuffix(path, "/calendar.ics") {
		return true
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case "admin", "api", "auth", "callback", "check-in", "conf-reload",
		"dashboard", "reauth", "signer", "i", "invite-speaker", "live", "login", "media", "navigation", "sendcal",
		"ticket", "tix", "logout", "trial-cal-invite", "trial-email", "vols",
		"webhook", "welcome-email":
		return true
	}
	if len(parts) >= 2 && parts[0] == "shop" {
		switch parts[1] {
		case "cart", "checkout", "success", "orders", "tax-quote":
			return true
		}
	}
	if len(parts) >= 2 && parts[1] == "hackathon" {
		if len(parts) >= 3 {
			switch parts[2] {
			case "judging", "ballot", "invites", "edit":
				return true
			}
		}
		if len(parts) >= 4 && parts[2] == "projects" && parts[3] == "new" {
			return true
		}
		if len(parts) >= 5 && parts[2] == "projects" {
			return true
		}
	}
	if len(parts) >= 2 {
		switch parts[1] {
		case "admin", "success", "volcoord":
			return true
		}
	}
	if len(parts) >= 3 && parts[0] == "conf" && parts[2] == "success" {
		return true
	}
	return false
}

func absoluteSEOURL(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return SEOHost + "/"
	}
	if strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return SEOHost + path
}

// SEOHost is the canonical absolute base used in robots.txt, the sitemap, and
// the shared site_seo metadata partial.
const SEOHost = "https://btcpp.dev"

// Robots serves /robots.txt. The file lives in the static/ tree so
// the policy is editable without a redeploy, but it's mounted at the
// site root (where crawlers look) via this handler rather than the
// /static/* prefix.
func Robots(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, "static/robots.txt")
}

// Sitemap serves /sitemap.xml — rebuilt on each request from the
// conference list so newly-published event pages are discoverable quickly.
// Published past confs stay in the map because their public pages
// are still useful archives. Priority/frequency hints are optional; Google
// ignores these fields and decides its own crawl schedule.
//
// Conf-agenda page (`/{tag}/agenda`) is only included when at
// least one of the conf's talks is Status=Scheduled — same gate as
// the nav-bar link, so the sitemap never points at a soft-empty page.
func Sitemap(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		ctx.Err.Printf("/sitemap.xml confs: %s", err)
		http.Error(w, "Unable to load confs", http.StatusInternalServerError)
		return
	}
	competitions, err := getters.ListCompetitions(ctx)
	if err != nil {
		ctx.Err.Printf("sitemap competitions: %s", err)
		http.Error(w, "Sitemap temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	byConf := map[string]*types.HackathonCompetition{}
	for _, competition := range competitions {
		if competition != nil && competition.Visibility == getters.CompetitionVisibilityPublic {
			byConf[competition.ConferenceID] = competition
		}
	}
	projectsByConf, err := getters.PublicProjectSitemapURLs(ctx)
	if err != nil {
		ctx.Err.Printf("sitemap projects: %s", err)
		http.Error(w, "Sitemap temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	// Buffer the complete map: database failures must not publish a partial 200.
	var output bytes.Buffer
	sitemapWriter := &output

	fmt.Fprintln(sitemapWriter, `<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprintln(sitemapWriter, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">`)

	// Evergreen public pages — homepage + apply / contact / legal.
	static := []struct {
		Path, Freq, Prio string
	}{
		{"/", "weekly", "1.0"},
		{"/events", "weekly", "0.8"},
		{"/shop", "weekly", "0.6"},
		{"/whois", "weekly", "0.6"},
		{"/talk", "monthly", "0.7"},
		{"/volunteer", "monthly", "0.7"},
		{"/sponsor", "monthly", "0.6"},
		{"/contact", "monthly", "0.5"},
		{"/developers/api", "monthly", "0.5"},
		{"/privacy", "yearly", "0.2"},
		{"/terms", "yearly", "0.2"},
	}
	for _, s := range static {
		writeSitemapURL(sitemapWriter, SEOHost+s.Path, "", s.Freq, s.Prio)
	}

	for _, c := range confs {
		if c == nil || c.Tag == "" || !c.IsPublished() {
			continue
		}
		prio := "0.6"
		freq := "monthly"
		if c.Active && !c.HasEnded() {
			prio = "0.9"
			freq = "daily"
		}
		landing := "/" + url.PathEscape(c.Tag)
		languages := conferenceCatalog.Languages(c.Tag, "en")
		if len(languages) == 0 {
			writeSitemapURL(sitemapWriter, SEOHost+landing, "", freq, prio)
		} else {
			alternates := append(append([]i18n.LanguageLink(nil), languages...), i18n.LanguageLink{Locale: "x-default", URL: landing})
			for _, language := range languages {
				writeSitemapURL(sitemapWriter, absoluteSEOURL(language.URL), "", freq, prio, alternates...)
			}
		}
		// Agenda page is gated on Conf.HasAgenda — populated at
		// render time, not on the cached Conf, so compute it here
		// against the live talks slice.
		talks, err := getters.GetTalksFor(ctx, c.Tag)
		if err != nil {
			ctx.Err.Printf("sitemap talks %s: %s", c.Tag, err)
			http.Error(w, "Sitemap temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		if len(acceptedSpeakersForConf(ctx, c, talks)) > 0 {
			writeSitemapURL(sitemapWriter, SEOHost+landing+"/speakers", "", freq, "0.6")
		}
		if competition := byConf[c.Ref]; competition != nil {
			writeSitemapURL(sitemapWriter, SEOHost+landing+"/hackathon", "", freq, "0.6")
			for _, projectID := range projectsByConf[c.Ref] {
				writeSitemapURL(sitemapWriter, SEOHost+landing+"/hackathon/projects/"+url.PathEscape(projectID), "", "monthly", "0.5")
			}
		}
		if anyScheduledTalk(c, talks) {
			writeSitemapURL(sitemapWriter, SEOHost+landing+"/agenda", "", freq, "0.6")
		}
	}

	fmt.Fprintln(sitemapWriter, `</urlset>`)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(output.Bytes())
}

type sitemapAlternate struct {
	XMLName  xml.Name `xml:"xhtml:link"`
	Rel      string   `xml:"rel,attr"`
	Language string   `xml:"hreflang,attr"`
	Href     string   `xml:"href,attr"`
}

type sitemapEntry struct {
	XMLName    xml.Name `xml:"url"`
	Loc        string   `xml:"loc"`
	Lastmod    string   `xml:"lastmod,omitempty"`
	Changefreq string   `xml:"changefreq,omitempty"`
	Priority   string   `xml:"priority,omitempty"`
	Alternates []sitemapAlternate
}

func writeSitemapURL(w io.Writer, loc, lastmod, changefreq, priority string, languages ...i18n.LanguageLink) {
	entry := sitemapEntry{Loc: loc, Lastmod: lastmod, Changefreq: changefreq, Priority: priority}
	for _, language := range languages {
		entry.Alternates = append(entry.Alternates, sitemapAlternate{Rel: "alternate", Language: language.Locale, Href: absoluteSEOURL(language.URL)})
	}
	data, _ := xml.MarshalIndent(entry, "  ", "  ")
	fmt.Fprintln(w, string(data))
}

// Dates entered without a time must not advertise a midnight event start.
func eventSEODate(conf *types.Conf, value time.Time) string {
	if value.IsZero() {
		return ""
	}
	if conf != nil {
		value = value.In(conf.Loc())
	}
	if value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 {
		return value.Format("2006-01-02")
	}
	return value.Format(time.RFC3339)
}

func conferenceSEODescription(conf *types.Conf) string {
	if conf == nil {
		return "Bitcoin developer conferences, workshops, and hackathons from bitcoin++."
	}
	if description := strings.TrimSpace(conf.OGFlavor); description != "" {
		return description
	}
	return fmt.Sprintf("%s · %s · %s. Explore the program, speakers, venue, and tickets.", conf.Desc, conf.DateDesc, conf.Location)
}
