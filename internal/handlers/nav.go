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

const publicConfCacheTTL = time.Minute

var publicConfCache = struct {
	sync.Mutex
	app        *config.AppContext
	confs      []*types.Conf
	expires    time.Time
	refreshing bool
}{}

var publicConfBuildMu sync.Mutex

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
	confs, err := cachedPublicConfs(ctx)
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
	return NavConfList{Upcoming: upcoming, Past: past}
}

// cachedPublicConfs shares the conference/ticket graph used by public pages
// and the global navigation. Development remains uncached so fixture edits
// appear immediately. In production, an expired snapshot is served while one
// coalesced refresh runs in the background; only the first cold load waits.
func cachedPublicConfs(ctx *config.AppContext) ([]*types.Conf, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, nil
	}
	if !ctx.InProduction {
		return loadPublicConfs(ctx)
	}

	publicConfCache.Lock()
	if publicConfCache.app == ctx && publicConfCache.confs != nil {
		confs := clonePublicConfs(publicConfCache.confs)
		startRefresh := time.Now().After(publicConfCache.expires) && !publicConfCache.refreshing
		if startRefresh {
			publicConfCache.refreshing = true
		}
		publicConfCache.Unlock()
		if startRefresh {
			go func() {
				_, err := rebuildPublicConfs(ctx)
				publicConfCache.Lock()
				if publicConfCache.app == ctx {
					publicConfCache.refreshing = false
				}
				publicConfCache.Unlock()
				if err != nil && ctx.Err != nil {
					ctx.Err.Printf("public conference background refresh: %s", err)
				}
			}()
		}
		return confs, nil
	}
	publicConfCache.Unlock()
	return rebuildPublicConfs(ctx)
}

func rebuildPublicConfs(ctx *config.AppContext) ([]*types.Conf, error) {
	publicConfBuildMu.Lock()
	defer publicConfBuildMu.Unlock()

	publicConfCache.Lock()
	if publicConfCache.app == ctx && publicConfCache.confs != nil && time.Now().Before(publicConfCache.expires) {
		confs := clonePublicConfs(publicConfCache.confs)
		publicConfCache.Unlock()
		return confs, nil
	}
	publicConfCache.Unlock()

	confs, err := loadPublicConfs(ctx)
	if err != nil {
		publicConfCache.Lock()
		if publicConfCache.app == ctx && publicConfCache.confs != nil {
			if ctx.Err != nil {
				ctx.Err.Printf("public conference refresh failed; serving stale cache: %s", err)
			}
			publicConfCache.expires = time.Now().Add(30 * time.Second)
			stale := clonePublicConfs(publicConfCache.confs)
			publicConfCache.Unlock()
			return stale, nil
		}
		publicConfCache.Unlock()
		return nil, err
	}

	publicConfCache.Lock()
	publicConfCache.app = ctx
	publicConfCache.confs = clonePublicConfs(confs)
	publicConfCache.expires = time.Now().Add(publicConfCacheTTL)
	publicConfCache.Unlock()
	return clonePublicConfs(confs), nil
}

func loadPublicConfs(ctx *config.AppContext) ([]*types.Conf, error) {
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		return nil, err
	}
	sort.Sort(types.ConfList(confs))
	return confs, nil
}

func invalidatePublicConfCache() {
	publicConfCache.Lock()
	publicConfCache.expires = time.Time{}
	publicConfCache.Unlock()
}

func clonePublicConfs(confs []*types.Conf) []*types.Conf {
	cloned := make([]*types.Conf, len(confs))
	for i, conf := range confs {
		if conf == nil {
			continue
		}
		copyConf := *conf
		copyConf.SpeakerDinnerStart = cloneTime(conf.SpeakerDinnerStart)
		copyConf.ConferenceEmailCampaignsEnabled = cloneBool(conf.ConferenceEmailCampaignsEnabled)
		copyConf.CountdownStart = cloneTime(conf.CountdownStart)
		copyConf.CountdownEnd = cloneTime(conf.CountdownEnd)
		copyConf.Tickets = make([]*types.ConfTicket, len(conf.Tickets))
		for ticketIndex, ticket := range conf.Tickets {
			if ticket != nil {
				copyTicket := *ticket
				copyConf.Tickets[ticketIndex] = &copyTicket
			}
		}
		cloned[i] = &copyConf
	}
	return cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
