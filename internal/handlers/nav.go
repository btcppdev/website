package handlers

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// NavConfList drives the dynamic events flyout in the main nav. The
// template ranges over Upcoming first (next conf at the top) and Past
// after that (most-recent-first), so the same struct serves both
// desktop and mobile.
type NavConfList struct {
	Upcoming []*types.Conf
	Past     []*types.Conf
}

const navConfCacheTTL = time.Minute

var navConfCache = struct {
	sync.Mutex
	app     *config.AppContext
	value   NavConfList
	expires time.Time
}{}

type siteAccountNavView struct {
	Name          string
	Email         string
	Initial       string
	PhotoURL      string
	ProfileURL    string
	CSRF          string
	IsGlobalAdmin bool
}

// SiteAccountNavigation renders the request-specific account control used by
// the shared site header. Keeping this as a small private, no-store fragment
// lets every existing page use real session state without teaching the many
// unrelated page view models about navigation concerns.
func SiteAccountNavigation(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "Cookie")

	id, err := auth.Resolve(r, ctx)
	if err != nil {
		if ctx.Err != nil {
			ctx.Err.Printf("/navigation/account resolve: %s", err)
		}
		id = nil
	}
	if id == nil || id.PersonID == "" {
		if err := ctx.TemplateCache.ExecuteTemplate(w, "site_account_anonymous", &siteAccountNavView{}); err != nil {
			http.Error(w, "navigation unavailable", http.StatusInternalServerError)
		}
		return
	}

	csrf, err := ensureAuthMethodsCSRF(ctx, r)
	if err != nil {
		http.Error(w, "navigation unavailable", http.StatusInternalServerError)
		return
	}
	name := strings.TrimSpace(id.PrimaryEmail)
	if name == "" {
		name = strings.TrimSpace(id.LoginEmail)
	}
	view := &siteAccountNavView{
		Name:          name,
		Email:         strings.TrimSpace(id.PrimaryEmail),
		CSRF:          csrf,
		IsGlobalAdmin: id.IsGlobalAdmin(),
	}
	if id.Speaker != nil {
		if speakerName := strings.TrimSpace(id.Speaker.Name); speakerName != "" {
			view.Name = speakerName
		}
		if photo := strings.TrimSpace(id.Speaker.Photo); photo != "" {
			view.PhotoURL = spaces.PublicURL("speakers/" + photo)
		}
		view.ProfileURL = whoIsPublicPath(ctx, id.Speaker)
	}
	if view.Name == "" {
		view.Name = "Account"
	}
	view.Initial = strings.ToUpper(string([]rune(view.Name)[0]))

	if err := ctx.TemplateCache.ExecuteTemplate(w, "site_account_authenticated", view); err != nil {
		http.Error(w, "navigation unavailable", http.StatusInternalServerError)
	}
}

// buildNavConfList loads published conferences and splits using the public
// active-list grace period. Sort order is "next event soonest" for
// upcoming and "most recently ended" for past so the freshest items
// land at the top of each list.
func buildNavConfList(ctx *config.AppContext) NavConfList {
	if ctx == nil || ctx.DB == nil {
		return NavConfList{}
	}
	if ctx.InProduction {
		navConfCache.Lock()
		defer navConfCache.Unlock()
		if navConfCache.app == ctx && time.Now().Before(navConfCache.expires) {
			return cloneNavConfList(navConfCache.value)
		}
	}
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		ctx.Err.Printf("navConfs: %s", err)
		return NavConfList{}
	}
	// Tags hardcoded at the bottom of the Past flyout as YouTube
	// playlist links — exclude them from the dynamic list so they
	// don't render twice if a row exists in Notion.
	hardcodedPast := map[string]bool{"atx22": true, "cdmx22": true}

	var upcoming, past []*types.Conf
	for _, c := range confs {
		if c == nil {
			continue
		}
		if hardcodedPast[c.Tag] {
			continue
		}
		if !c.IsPublished() {
			continue
		}
		if c.IsInActiveEventList() {
			upcoming = append(upcoming, c)
		} else {
			past = append(past, c)
		}
	}
	sort.Slice(upcoming, func(i, j int) bool {
		return upcoming[i].StartDate.Before(upcoming[j].StartDate)
	})
	sort.Slice(past, func(i, j int) bool {
		return past[i].StartDate.After(past[j].StartDate)
	})
	result := NavConfList{Upcoming: upcoming, Past: past}
	if ctx.InProduction {
		navConfCache.app = ctx
		navConfCache.value = cloneNavConfList(result)
		navConfCache.expires = time.Now().Add(navConfCacheTTL)
	}
	return result
}

func cloneNavConfList(value NavConfList) NavConfList {
	return NavConfList{
		Upcoming: append([]*types.Conf(nil), value.Upcoming...),
		Past:     append([]*types.Conf(nil), value.Past...),
	}
}
