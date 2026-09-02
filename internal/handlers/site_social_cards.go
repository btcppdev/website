package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

var siteSocialCardCache = struct {
	sync.RWMutex
	images map[string][]byte
}{images: make(map[string][]byte)}

func siteSocialCardPath(kind, slug string, card imgproc.SiteSocialCard) string {
	path := "/social-cards/" + url.PathEscape(kind)
	if strings.TrimSpace(slug) != "" {
		path += "/" + url.PathEscape(slug)
	}
	return path + ".jpg?v=" + imgproc.SiteSocialCardID(card)
}

func siteSocialCardImageURL(ctx *config.AppContext, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		if siteSocialCardImageHostAllowed(ctx, raw) {
			return raw
		}
		return ""
	}
	if strings.HasPrefix(raw, "/static/") {
		if data, err := os.ReadFile(strings.TrimPrefix(raw, "/")); err == nil {
			raw += "?asset=" + imgproc.ShortID(data)
		}
	}
	base := strings.TrimRight(ctx.Env.GetURI(), "/")
	if base == "" {
		base = SEOHost
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return base + raw
}

func siteSocialCardImageHostAllowed(ctx *config.AppContext, raw string) bool {
	candidate, err := url.Parse(raw)
	if err != nil || candidate.Hostname() == "" {
		return false
	}
	host := strings.ToLower(candidate.Hostname())
	allowed := []string{SEOHost, spaces.BaseURL()}
	if ctx != nil && ctx.Env != nil {
		allowed = append(allowed, ctx.Env.GetURI())
		if !ctx.Env.Prod && (host == "localhost" || host == "127.0.0.1" || host == "::1") {
			return true
		}
	}
	for _, base := range allowed {
		parsed, parseErr := url.Parse(base)
		if parseErr == nil && parsed.Hostname() != "" && strings.EqualFold(host, parsed.Hostname()) {
			return true
		}
	}
	return false
}

func normalizeSiteSocialCard(ctx *config.AppContext, card imgproc.SiteSocialCard) imgproc.SiteSocialCard {
	images := make([]string, 0, 4)
	for _, raw := range card.Images {
		if image := siteSocialCardImageURL(ctx, raw); image != "" {
			images = append(images, image)
			if len(images) == 4 {
				break
			}
		}
	}
	card.Images = images
	card.Eyebrow = truncateSiteSocialCardText(card.Eyebrow, 72)
	card.Title = truncateSiteSocialCardText(card.Title, 68)
	card.Subtitle = truncateSiteSocialCardText(card.Subtitle, 150)
	if card.Footer == "" {
		card.Footer = "btcpp.dev"
	}
	return card
}

func truncateSiteSocialCardText(value string, maximum int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return strings.TrimSpace(string(runes[:maximum-1])) + "…"
}

func homeSocialCard(ctx *config.AppContext, confs []*types.Conf) imgproc.SiteSocialCard {
	published := make([]*types.Conf, 0, len(confs))
	for _, conf := range confs {
		if conf != nil && conf.IsPublished() {
			published = append(published, conf)
		}
	}
	confs = published
	upcoming := homeUpcomingConfs(confs)
	card := imgproc.SiteSocialCard{
		Kind: "home", Eyebrow: "bitcoin++ worldwide",
		Title:    "Developing the frontier of bitcoin.",
		Subtitle: "Technical conferences, workshops, and hackathons for bitcoin builders.",
		Stats:    []imgproc.SiteSocialCardStat{{Value: strconv.Itoa(len(confs)), Label: "editions"}},
	}
	if len(upcoming) > 0 {
		next := upcoming[0]
		card.Eyebrow = "Coming up next · " + next.DateDesc
		card.Title = next.Desc
		card.Subtitle = strings.TrimSpace(strings.Join(nonEmptyStrings(next.Tagline, next.Location), " · "))
		card.Images = []string{confImagePath(next.Tag, "leading")}
		card.Stats = append(card.Stats, imgproc.SiteSocialCardStat{Value: strconv.Itoa(len(upcoming)), Label: "upcoming"})
	}
	return normalizeSiteSocialCard(ctx, card)
}

func conferenceSocialCard(ctx *config.AppContext, conf *types.Conf) imgproc.SiteSocialCard {
	if conf == nil {
		return normalizeSiteSocialCard(ctx, imgproc.SiteSocialCard{Kind: "conference", Eyebrow: "bitcoin++ event", Title: "bitcoin++", Subtitle: "Developing the frontier of bitcoin."})
	}
	return normalizeSiteSocialCard(ctx, imgproc.SiteSocialCard{
		Kind: "conference", Eyebrow: "bitcoin++ event · " + conf.DateDesc,
		Title: conf.Desc, Subtitle: strings.Join(nonEmptyStrings(conf.Tagline, conf.Location, conf.Venue), " · "),
		Images: []string{confImagePath(conf.Tag, "leading")},
	})
}

func hackathonSocialCard(ctx *config.AppContext, page *HackathonPage) imgproc.SiteSocialCard {
	card := imgproc.SiteSocialCard{Kind: "hackathon", Eyebrow: "bitcoin++ hackathon", Title: "Build something real.", Subtitle: "Find collaborators, choose a challenge, and ship your project."}
	if page == nil {
		return normalizeSiteSocialCard(ctx, card)
	}
	if page.Competition != nil && strings.TrimSpace(page.Competition.Title) != "" {
		card.Title = page.Competition.Title
	}
	if page.Conf != nil {
		card.Eyebrow = strings.TrimSpace(strings.Join(nonEmptyStrings(page.ConferenceLabel(), page.Conf.DateDesc), " · "))
		card.Images = append(card.Images, confImagePath(page.Conf.Tag, "leading"))
	}
	card.Stats = []imgproc.SiteSocialCardStat{
		{Value: strconv.Itoa(len(page.Awards)), Label: "awards"},
		{Value: strconv.Itoa(len(page.GalleryProjects())), Label: "projects"},
	}
	if sats := page.PrizePoolSats(); sats > 0 {
		card.Stats = append([]imgproc.SiteSocialCardStat{{Value: groupSatsCommas(sats), Label: "sats in prizes"}}, card.Stats...)
	}
	for _, project := range page.FeaturedProjects() {
		if project != nil && strings.TrimSpace(project.ImageURL) != "" {
			card.Images = append(card.Images, project.ImageURL)
		}
	}
	return normalizeSiteSocialCard(ctx, card)
}

func whoIsSocialCard(ctx *config.AppContext, people []*WhoIsPerson) imgproc.SiteSocialCard {
	talks, projects, editions := whoIsTotals(people)
	card := imgproc.SiteSocialCard{
		Kind: "whois", Eyebrow: "whois @ bitcoin++", Title: "The people of bitcoin++.",
		Subtitle: "Builders, researchers, speakers, and hackers from across the bitcoin++ archive.",
		Stats: []imgproc.SiteSocialCardStat{
			{Value: strconv.Itoa(len(people)), Label: "people"},
			{Value: strconv.Itoa(talks), Label: "talks"},
			{Value: strconv.Itoa(projects), Label: "projects"},
			{Value: strconv.Itoa(editions), Label: "editions"},
		},
	}
	for _, person := range people {
		if person != nil && person.Speaker != nil && strings.TrimSpace(person.Speaker.Photo) != "" {
			card.Images = append(card.Images, spaces.PublicURL("speakers/"+person.Speaker.Photo))
			if len(card.Images) == 4 {
				break
			}
		}
	}
	return normalizeSiteSocialCard(ctx, card)
}

func personSocialCard(ctx *config.AppContext, person *WhoIsPerson) imgproc.SiteSocialCard {
	card := imgproc.SiteSocialCard{Kind: "person", Eyebrow: "community profile", Title: "bitcoin++ builder", Subtitle: "Talks, projects, and appearances from the bitcoin++ archive."}
	if person == nil || person.Speaker == nil {
		return normalizeSiteSocialCard(ctx, card)
	}
	card.Title = person.Speaker.Name
	card.Eyebrow = "@" + person.PublicID + " · bitcoin++"
	details := make([]string, 0, 2)
	if len(person.Talks) > 0 && person.Talks[0] != nil && person.Talks[0].Talk != nil {
		details = append(details, "Talk: "+person.Talks[0].Talk.Name)
		if clipart := strings.TrimSpace(person.Talks[0].Talk.Clipart); clipart != "" {
			card.Images = append(card.Images, spaces.PublicURL("talks/"+clipart))
		}
	}
	for _, project := range person.Projects {
		if project == nil || project.Project == nil {
			continue
		}
		if len(project.Awards) > 0 {
			details = append(details, "Award: "+project.Awards[0].Title)
		} else if len(details) < 2 {
			details = append(details, "Project: "+project.Project.Title)
		}
		if project.Project.ImageURL != "" {
			card.Images = append(card.Images, project.Project.ImageURL)
		}
		break
	}
	if len(details) > 0 {
		card.Subtitle = strings.Join(details, " · ")
	} else if person.Speaker.Company != "" {
		card.Subtitle = person.Speaker.Company
	}
	if photo := strings.TrimSpace(person.Speaker.Photo); photo != "" {
		card.Images = append([]string{spaces.PublicURL("speakers/" + photo)}, card.Images...)
	}
	card.Stats = []imgproc.SiteSocialCardStat{
		{Value: strconv.Itoa(len(person.Editions)), Label: "editions"},
		{Value: strconv.Itoa(len(person.Talks)), Label: "talks"},
		{Value: strconv.Itoa(len(person.Projects)), Label: "projects"},
	}
	return normalizeSiteSocialCard(ctx, card)
}

func shopSocialCard(ctx *config.AppContext, products []*types.MerchProduct) imgproc.SiteSocialCard {
	card := imgproc.SiteSocialCard{
		Kind: "shop", Eyebrow: "bitcoin++ merch shop", Title: "Wear the frontier.",
		Subtitle: "Small-batch apparel, hats, and gear for people who build on bitcoin and run their own nodes.",
		Stats:    []imgproc.SiteSocialCardStat{{Value: strconv.Itoa(len(products)), Label: "items"}},
	}
	for _, product := range products {
		if product != nil {
			card.Images = append(card.Images, merchImage(product))
			if len(card.Images) == 4 {
				break
			}
		}
	}
	return normalizeSiteSocialCard(ctx, card)
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func loadHackathonSocialCard(ctx *config.AppContext, tag string) (imgproc.SiteSocialCard, error) {
	conf, err := getters.GetConfByTag(ctx, tag)
	if err != nil {
		return imgproc.SiteSocialCard{}, fmt.Errorf("load conference %q: %w", tag, err)
	}
	if conf == nil || !conf.IsPublished() {
		return imgproc.SiteSocialCard{}, fmt.Errorf("published conference %q not found", tag)
	}
	competition, err := getters.GetCompetitionByConferenceID(ctx, conf.Ref)
	if err != nil {
		return imgproc.SiteSocialCard{}, fmt.Errorf("load hackathon for %q: %w", tag, err)
	}
	if competition == nil || competition.Visibility != getters.CompetitionVisibilityPublic {
		return imgproc.SiteSocialCard{}, fmt.Errorf("public hackathon for %q not found", tag)
	}
	projects, err := getters.ListProjectsForCompetition(ctx, competition.ID, types.HackathonViewer{})
	if err != nil {
		return imgproc.SiteSocialCard{}, err
	}
	awards, prizes, pool, _, err := loadPublicHackathonAwards(ctx, competition.ID, competition.ResultsFinalizedAt != nil)
	if err != nil {
		return imgproc.SiteSocialCard{}, err
	}
	page := &HackathonPage{Competition: competition, Conf: conf, Projects: projects, Awards: awards, PrizesByAward: prizes, PrizePoolByAward: pool}
	return hackathonSocialCard(ctx, page), nil
}

func loadSiteSocialCard(ctx *config.AppContext, kind, slug string) (imgproc.SiteSocialCard, error) {
	switch kind {
	case "home":
		confs, err := getters.ListConfs(ctx)
		return homeSocialCard(ctx, confs), err
	case "conference":
		conf, err := getters.GetConfByTag(ctx, slug)
		if err == nil && (conf == nil || !conf.IsPublished()) {
			err = fmt.Errorf("published conference not found")
		}
		return conferenceSocialCard(ctx, conf), err
	case "hackathon":
		return loadHackathonSocialCard(ctx, slug)
	case "whois":
		people, err := buildWhoIsDirectory(ctx)
		return whoIsSocialCard(ctx, people), err
	case "person":
		person, err := findWhoIsPerson(ctx, slug)
		if err == nil && person == nil {
			err = fmt.Errorf("profile not found")
		}
		return personSocialCard(ctx, person), err
	case "shop":
		products, err := getters.ListMerchProducts(ctx, false)
		return shopSocialCard(ctx, products), err
	default:
		return imgproc.SiteSocialCard{}, fmt.Errorf("unknown social card type %q", kind)
	}
}

func ServeSiteSocialCard(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	kind := strings.TrimSpace(mux.Vars(r)["kind"])
	slug := strings.TrimSuffix(strings.TrimSpace(mux.Vars(r)["slug"]), ".jpg")
	card, err := loadSiteSocialCard(ctx, kind, slug)
	if err != nil {
		ctx.Err.Printf("%s social card: %s", r.URL.Path, err)
		http.NotFound(w, r)
		return
	}
	version := imgproc.SiteSocialCardID(card)
	cacheKey := kind + ":" + slug + ":" + version
	siteSocialCardCache.RLock()
	jpeg := siteSocialCardCache.images[cacheKey]
	siteSocialCardCache.RUnlock()
	if len(jpeg) == 0 {
		jpeg, err = imgproc.MakeSiteSocialCardJPEG(card)
		if err != nil {
			ctx.Err.Printf("%s social card render: %s", r.URL.Path, err)
			http.Error(w, "Unable to render social card", http.StatusInternalServerError)
			return
		}
		siteSocialCardCache.Lock()
		siteSocialCardCache.images[cacheKey] = jpeg
		siteSocialCardCache.Unlock()
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=3600, stale-while-revalidate=86400")
	w.Header().Set("ETag", `"`+version+`"`)
	if r.Header.Get("If-None-Match") == `"`+version+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(jpeg)
}

func DevSiteSocialCardPreview(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if ctx.Env.Prod {
		http.NotFound(w, r)
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = "home"
	}
	card, err := loadSiteSocialCard(ctx, kind, strings.TrimSpace(r.URL.Query().Get("slug")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	html, err := imgproc.RenderSiteSocialCardHTML(card)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}
