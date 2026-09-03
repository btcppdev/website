package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/publicid"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
	"golang.org/x/sync/singleflight"
)

var siteSocialCardCache = struct {
	sync.RWMutex
	images map[string][]byte
}{images: make(map[string][]byte)}

const siteSocialCardMemoryCacheLimit = 64

var siteSocialCardRenderGroup singleflight.Group

var siteSocialCardStorage = struct {
	isConfigured func() bool
	exists       func(string) bool
	upload       func(string, []byte, string, string) (string, error)
	publicURL    func(string) string
}{
	isConfigured: spaces.IsConfigured,
	exists:       spaces.Exists,
	upload:       spaces.UploadImmutable,
	publicURL:    spaces.PublicURL,
}

var renderSiteSocialCardJPEG = imgproc.MakeSiteSocialCardJPEG

type siteSocialCardRenderResult struct {
	jpeg       []byte
	publicURL  string
	persistErr error
}

type siteSocialCardPalette struct {
	Accent string
	Ink    string
}

func siteSocialCardPaletteForConf(conf *types.Conf) siteSocialCardPalette {
	accent := types.DefaultConferenceAccentColor
	if conf != nil {
		if normalized, valid := types.NormalizeConferenceAccentColor(conf.AccentColor); valid {
			accent = normalized
		}
	}
	red, _ := strconv.ParseInt(accent[1:3], 16, 16)
	green, _ := strconv.ParseInt(accent[3:5], 16, 16)
	blue, _ := strconv.ParseInt(accent[5:7], 16, 16)
	ink := "#ffffff"
	if (299*red+587*green+114*blue)/1000 >= 150 {
		ink = "#0a0a0a"
	}
	return siteSocialCardPalette{Accent: accent, Ink: ink}
}

func applySiteSocialCardPalette(card *imgproc.SiteSocialCard, conf *types.Conf) {
	if card == nil {
		return
	}
	palette := siteSocialCardPaletteForConf(conf)
	card.AccentColor = palette.Accent
	card.TextColor = palette.Ink
}

func siteSocialCardPath(kind, slug string, card imgproc.SiteSocialCard) string {
	path := "/social-cards/" + url.PathEscape(kind)
	if strings.TrimSpace(slug) != "" {
		path += "/" + url.PathEscape(slug)
	}
	return path + ".jpg?v=" + imgproc.SiteSocialCardID(card)
}

func siteSocialCardObjectKey(kind, slug, version string) string {
	kind = safeSiteSocialCardKeyPart(kind, "card")
	slug = safeSiteSocialCardKeyPart(slug, "index")
	version = safeSiteSocialCardKeyPart(version, "current")
	return "social-cards/site/" + kind + "/" + slug + "/" + version + ".jpg"
}

func safeSiteSocialCardKeyPart(value, fallback string) string {
	value = strings.TrimSpace(value)
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	cleaned := strings.Trim(out.String(), "-.")
	if cleaned == "" {
		return fallback
	}
	return cleaned
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
	imageLimit := 4
	if card.Kind == "events" {
		imageLimit = 8
	} else if card.Kind == "home" {
		imageLimit = 6
	} else if card.Kind == "whois" {
		imageLimit = 12
	} else if card.Kind == "conference" {
		imageLimit = 3
	} else if card.Kind == "person" {
		imageLimit = 5
	}
	images := make([]string, 0, imageLimit)
	labels := make([]string, 0, imageLimit)
	for index, raw := range card.Images {
		if image := siteSocialCardImageURL(ctx, raw); image != "" {
			images = append(images, image)
			label := ""
			if index < len(card.ImageLabels) {
				label = truncateSiteSocialCardText(socialCardWithoutWordmark(card.ImageLabels[index]), 48)
			}
			labels = append(labels, label)
			if len(images) == imageLimit {
				break
			}
		}
	}
	card.Images = images
	card.ImageLabels = labels
	card.ProfileHandle = truncateSiteSocialCardText(strings.TrimPrefix(strings.TrimSpace(card.ProfileHandle), "@"), 32)
	card.XHandle = truncateSiteSocialCardText(strings.TrimPrefix(strings.TrimSpace(card.XHandle), "@"), 24)
	card.GitHubHandle = truncateSiteSocialCardText(strings.TrimPrefix(strings.TrimSpace(card.GitHubHandle), "@"), 32)
	detailLimit := 3
	if card.Kind == "person" {
		detailLimit = 12
	}
	details := make([]string, 0, min(detailLimit, len(card.Details)))
	for _, detail := range card.Details {
		detail = truncateSiteSocialCardText(socialCardWithoutWordmark(detail), 56)
		if detail != "" {
			details = append(details, detail)
		}
		if len(details) == detailLimit {
			break
		}
	}
	card.Details = details
	badges := make([]string, 0, min(2, len(card.Badges)))
	for _, badge := range card.Badges {
		badge = truncateSiteSocialCardText(socialCardWithoutWordmark(badge), 28)
		if badge != "" {
			badges = append(badges, badge)
		}
		if len(badges) == 2 {
			break
		}
	}
	card.Badges = badges
	card.HeroImage = siteSocialCardImageURL(ctx, card.HeroImage)
	sponsorLogos := make([]string, 0, len(card.SponsorLogos))
	sponsorNames := make([]string, 0, len(card.SponsorLogos))
	for index, raw := range card.SponsorLogos {
		if logo := siteSocialCardImageURL(ctx, raw); logo != "" {
			sponsorLogos = append(sponsorLogos, logo)
			name := ""
			if index < len(card.SponsorNames) {
				name = truncateSiteSocialCardText(socialCardWithoutWordmark(card.SponsorNames[index]), 48)
			}
			sponsorNames = append(sponsorNames, name)
		}
	}
	card.SponsorLogos = sponsorLogos
	card.SponsorNames = sponsorNames
	poweredByLogos := make([]string, 0, len(card.PoweredByLogos))
	poweredByNames := make([]string, 0, len(card.PoweredByLogos))
	for index, raw := range card.PoweredByLogos {
		if logo := siteSocialCardImageURL(ctx, raw); logo != "" {
			poweredByLogos = append(poweredByLogos, logo)
			name := ""
			if index < len(card.PoweredByNames) {
				name = truncateSiteSocialCardText(socialCardWithoutWordmark(card.PoweredByNames[index]), 48)
			}
			poweredByNames = append(poweredByNames, name)
		}
	}
	card.PoweredByLogos = poweredByLogos
	card.PoweredByNames = poweredByNames
	card.MapImage = siteSocialCardImageURL(ctx, card.MapImage)
	card.Eyebrow = truncateSiteSocialCardText(socialCardWithoutWordmark(card.Eyebrow), 72)
	card.Title = truncateSiteSocialCardText(socialCardWithoutWordmark(card.Title), 68)
	card.TitleSuffix = truncateSiteSocialCardText(socialCardWithoutWordmark(card.TitleSuffix), 68)
	card.Subtitle = truncateSiteSocialCardText(socialCardWithoutWordmark(card.Subtitle), 150)
	card.Location = truncateSiteSocialCardText(socialCardWithoutWordmark(card.Location), 96)
	calloutLimit := 54
	if card.Kind == "award" {
		calloutLimit = 96
	}
	card.Callout = truncateSiteSocialCardText(socialCardWithoutWordmark(card.Callout), calloutLimit)
	if card.Footer == "" {
		card.Footer = "btcpp.dev"
	}
	return card
}

func socialCardWithoutWordmark(value string) string {
	for {
		index := strings.Index(strings.ToLower(value), "bitcoin++")
		if index < 0 {
			return strings.Join(strings.Fields(value), " ")
		}
		value = value[:index] + value[index+len("bitcoin++"):]
	}
}

func truncateSiteSocialCardText(value string, maximum int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return strings.TrimSpace(string(runes[:maximum-1])) + "…"
}

func siteSocialCardStoredImage(prefix, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "https://") || strings.HasPrefix(name, "/static/") {
		return name
	}
	// Seeded development records point directly at checked-in assets. Sending
	// those paths through the Spaces hostname produces a valid-looking URL for
	// an object that does not exist.
	if strings.HasPrefix(name, "../static/") {
		return strings.TrimPrefix(name, "..")
	}
	if strings.HasPrefix(name, "static/") {
		return "/" + name
	}
	return spaces.PublicURL(strings.Trim(prefix, "/") + "/" + strings.TrimPrefix(name, "/"))
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
	sponsorYear := time.Now().Year()
	card := imgproc.SiteSocialCard{
		Kind: "home", Eyebrow: "technical bitcoin · worldwide",
		Title:    "Developing the frontier of bitcoin.",
		Subtitle: "Technical conferences for builders, researchers, and hackers.",
		MapImage: "/static/img/home/worldmap.svg",
	}
	applySiteSocialCardPalette(&card, nil)
	if len(upcoming) > 0 {
		next := upcoming[0]
		sponsorYear = next.StartDate.In(next.Loc()).Year()
		applySiteSocialCardPalette(&card, next)
		if editionName := socialCardWithoutWordmark(next.Desc); editionName != "" {
			card.Title = editionName
		}
		location := strings.TrimSpace(strings.SplitN(next.Location, ",", 2)[0])
		if location == "" {
			location = strings.TrimSpace(next.Desc)
			if strings.HasPrefix(strings.ToLower(location), "bitcoin++ ") {
				location = strings.TrimSpace(location[len("bitcoin++ "):])
			}
		}
		card.Eyebrow = "Up next"
		card.Subtitle = strings.Join(nonEmptyStrings(location, next.DateDesc), " · ")
	}
	if ctx != nil && ctx.DB != nil {
		sponsorships, err := getters.ListSponsorships(ctx, "")
		if err != nil {
			if ctx.Err != nil {
				ctx.Err.Printf("home social card headline sponsors: %s", err)
			}
		} else {
			card.SponsorLogos, card.SponsorNames = homeSocialCardHeadlineSponsors(sponsorships, sponsorYear)
			if len(card.SponsorLogos) > 0 {
				card.SponsorLabel = fmt.Sprintf("%d headline partners", sponsorYear)
			}
		}
	}
	mapPointIndexes := make(map[string]int)
	for _, marker := range homeMapMarkers(confs) {
		key := fmt.Sprintf("%.2f|%.2f", marker.X, marker.Y)
		if index, exists := mapPointIndexes[key]; exists {
			card.MapPoints[index].Upcoming = card.MapPoints[index].Upcoming || marker.Upcoming
			continue
		}
		mapPointIndexes[key] = len(card.MapPoints)
		card.MapPoints = append(card.MapPoints, imgproc.SiteSocialCardMapPoint{X: marker.X, Y: marker.Y, Upcoming: marker.Upcoming})
	}
	sort.Slice(card.MapPoints, func(i, j int) bool {
		if card.MapPoints[i].X != card.MapPoints[j].X {
			return card.MapPoints[i].X < card.MapPoints[j].X
		}
		return card.MapPoints[i].Y < card.MapPoints[j].Y
	})
	// Put the nearest current/upcoming conference first so it sits on top of
	// the visual stack, followed by the newest events in the archive.
	imageConfs := append(append([]*types.Conf{}, upcoming...), homePastConfs(confs)...)
	editionNumbers := conferenceEditionNumbers(confs)
	for _, conf := range imageConfs {
		if image := confImagePath(conf.Tag, "leading"); image != "" && image != "/static/img/rebrand/light-sketch-bg.avif" {
			card.Images = append(card.Images, image)
			card.ImageLabels = append(card.ImageLabels, conferenceArchiveCardLabel(conf, editionNumbers[conf.Tag]))
			if len(card.Images) == 6 {
				break
			}
		}
	}
	if ctx != nil && ctx.Env != nil && !ctx.Env.Prod {
		seenImages := make(map[string]bool, len(card.Images))
		for _, image := range card.Images {
			seenImages[image] = true
		}
		for _, tag := range []string{"toronto", "vienna", "floripa26", "taipei", "berlin25", "istanbul", "riga", "atx25", "nairobi", "seoul", "vegas", "durham"} {
			image := confImagePath(tag, "leading")
			if image == "" || image == "/static/img/rebrand/light-sketch-bg.avif" || seenImages[image] {
				continue
			}
			seenImages[image] = true
			card.Images = append(card.Images, image)
			card.ImageLabels = append(card.ImageLabels, "")
			if len(card.Images) == 6 {
				break
			}
		}
	}
	return normalizeSiteSocialCard(ctx, card)
}

func homeSocialCardHeadlineSponsors(sponsorships []*types.Sponsorship, year int) ([]string, []string) {
	matching := make([]*types.Sponsorship, 0, len(sponsorships))
	for _, sponsorship := range sponsorships {
		if sponsorship == nil {
			continue
		}
		level := normalizeLevel(sponsorship.Level)
		if level != "Headline" && level != "Diamond" {
			continue
		}
		for _, conf := range sponsorship.Confs {
			if conf != nil && !conf.StartDate.IsZero() && conf.StartDate.In(conf.Loc()).Year() == year {
				matching = append(matching, sponsorship)
				break
			}
		}
	}
	return hackathonSocialCardSponsorsForLevels(matching, map[string]bool{"Headline": true, "Diamond": true})
}

func eventsSocialCard(ctx *config.AppContext, confs []*types.Conf) imgproc.SiteSocialCard {
	published := make([]*types.Conf, 0, len(confs))
	for _, conf := range confs {
		if conf != nil && conf.IsPublished() {
			published = append(published, conf)
		}
	}
	upcoming := homeUpcomingConfs(published)
	card := imgproc.SiteSocialCard{
		Kind:        "events",
		Eyebrow:     "the permanent archive",
		Title:       "events archive.",
		Subtitle:    "Explore the talks, hackathons, cities, and livestreams from every edition.",
		Footer:      "btcpp.dev/events",
		AccentColor: "#f6f3ee",
		TextColor:   "#0a0a0a",
	}
	editionNumbers := conferenceEditionNumbers(published)
	imageConfs := append(append([]*types.Conf{}, upcoming...), homePastConfs(published)...)
	seenImages := make(map[string]bool)
	for _, conf := range imageConfs {
		if image := confImagePath(conf.Tag, "leading"); image != "" && image != "/static/img/rebrand/light-sketch-bg.avif" {
			if !seenImages[image] {
				seenImages[image] = true
				card.Images = append(card.Images, image)
				card.ImageLabels = append(card.ImageLabels, conferenceArchiveCardLabel(conf, editionNumbers[conf.Tag]))
			}
			if len(card.Images) == 8 {
				break
			}
		}
	}
	// The development database intentionally contains only a few editions.
	// Fill its preview with checked-in artwork from real archived events so the
	// contact-sheet design can be reviewed at its production density.
	if ctx != nil && ctx.Env != nil && !ctx.Env.Prod {
		fallbacks := []struct {
			tag     string
			theme   string
			edition int
		}{
			{tag: "toronto", theme: "consensus", edition: 19},
			{tag: "vienna", theme: "economics", edition: 17},
			{tag: "floripa26", theme: "exploits", edition: 15},
			{tag: "taipei", theme: "sovereignty", edition: 14},
			{tag: "berlin25", theme: "lightning", edition: 12},
			{tag: "istanbul", theme: "scaling", edition: 11},
			{tag: "riga", theme: "privacy", edition: 10},
			{tag: "atx25", theme: "mempool", edition: 9},
		}
		// A sparse development database cannot assign meaningful global edition
		// numbers. Use a real slice of the archive for the visual preview.
		if len(card.Images) < 4 {
			card.Images = nil
			card.ImageLabels = nil
			seenImages = make(map[string]bool)
		}
		for _, fallback := range fallbacks {
			image := confImagePath(fallback.tag, "leading")
			if image == "" || image == "/static/img/rebrand/light-sketch-bg.avif" || seenImages[image] {
				continue
			}
			seenImages[image] = true
			card.Images = append(card.Images, image)
			card.ImageLabels = append(card.ImageLabels, archiveEditionLabel(fallback.theme, fallback.edition))
			if len(card.Images) == 8 {
				break
			}
		}
	}
	return normalizeSiteSocialCard(ctx, card)
}

func conferenceEditionNumbers(confs []*types.Conf) map[string]int {
	chronological := make([]*types.Conf, 0, len(confs))
	for _, conf := range confs {
		if conf != nil && conf.IsPublished() {
			chronological = append(chronological, conf)
		}
	}
	sort.SliceStable(chronological, func(i, j int) bool {
		left, right := chronological[i], chronological[j]
		if left.StartDate.Equal(right.StartDate) {
			return left.Tag < right.Tag
		}
		if left.StartDate.IsZero() {
			return false
		}
		if right.StartDate.IsZero() {
			return true
		}
		return left.StartDate.Before(right.StartDate)
	})
	numbers := make(map[string]int, len(chronological))
	for index, conf := range chronological {
		numbers[conf.Tag] = index + 1
	}
	return numbers
}

func conferenceArchiveCardLabel(conf *types.Conf, edition int) string {
	if conf == nil {
		return archiveEditionLabel("edition", edition)
	}
	theme := strings.TrimSpace(conf.ArchiveEdition())
	for _, suffix := range []string{" edition", " edtion"} {
		if strings.HasSuffix(strings.ToLower(theme), suffix) {
			theme = strings.TrimSpace(theme[:len(theme)-len(suffix)])
			break
		}
	}
	if theme == "" {
		theme = strings.TrimSpace(conf.Tagline)
	}
	if theme == "" {
		theme = "archive"
	}
	return archiveEditionLabel(theme, edition)
}

func archiveEditionLabel(theme string, edition int) string {
	theme = strings.TrimSpace(socialCardWithoutWordmark(theme))
	if edition <= 0 {
		return theme
	}
	return fmt.Sprintf("%s / edition %d", theme, edition)
}

func conferenceSocialCard(ctx *config.AppContext, conf *types.Conf, talkSets ...[]*types.Talk) imgproc.SiteSocialCard {
	if conf == nil {
		card := imgproc.SiteSocialCard{Kind: "conference", Eyebrow: "technical bitcoin event", Title: "Upcoming edition", Subtitle: "Developing the frontier of bitcoin.", Location: "Worldwide"}
		applySiteSocialCardPalette(&card, nil)
		return normalizeSiteSocialCard(ctx, card)
	}
	card := imgproc.SiteSocialCard{
		Kind: "conference", Eyebrow: conf.DateDesc,
		Title:    conf.Desc,
		Subtitle: strings.Join(nonEmptyStrings(conf.Location, conf.Venue), " · "),
		Location: strings.Join(nonEmptyStrings(conf.Location, conf.Venue), " · "),
		MapImage: "/static/img/home/worldmap.svg",
	}
	applyConferenceSocialCardTicketPrice(&card, conf, time.Now())
	if title, suffix, found := strings.Cut(card.Title, ","); found {
		card.Title = strings.TrimSpace(title)
		card.TitleSuffix = strings.TrimSpace(suffix)
	}
	// The conference's leading artwork is always the first card. Speaker faces
	// and talk clipart from its public program then make the stack feel like the
	// event being shared, rather than a collage of unrelated editions.
	seenArtwork := make(map[string]bool)
	addArtwork := func(image, label string) {
		if image == "" || seenArtwork[image] {
			return
		}
		seenArtwork[image] = true
		card.Images = append(card.Images, image)
		card.ImageLabels = append(card.ImageLabels, label)
	}
	addArtwork(confImagePath(conf.Tag, "leading"), "")
	if len(talkSets) > 0 {
		var speakerImages, speakerLabels, clipartImages, clipartLabels []string
		seenSpeakers := make(map[string]bool)
		for _, talk := range talkSets[0] {
			if talk == nil || (talk.Status != StatusAccepted && talk.Status != StatusScheduled) {
				continue
			}
			if clipart := siteSocialCardStoredImage("talks", talk.Clipart); clipart != "" {
				clipartImages = append(clipartImages, clipart)
				clipartLabels = append(clipartLabels, talk.Name)
			}
			for _, speaker := range talk.Speakers {
				if speaker == nil || seenSpeakers[speaker.ID] {
					continue
				}
				if photo := siteSocialCardStoredImage("speakers", speaker.Photo); photo != "" {
					seenSpeakers[speaker.ID] = true
					speakerImages = append(speakerImages, photo)
					speakerLabels = append(speakerLabels, speaker.Name)
				}
			}
		}
		// Prefer a human face, then a piece of talk art; a second face wins
		// the final slot when no clipart is available.
		if len(speakerImages) > 0 {
			addArtwork(speakerImages[0], speakerLabels[0])
		}
		if len(clipartImages) > 0 {
			addArtwork(clipartImages[0], clipartLabels[0])
		}
		for index := 1; len(card.Images) < 3 && index < len(speakerImages); index++ {
			addArtwork(speakerImages[index], speakerLabels[index])
		}
		for index := 1; len(card.Images) < 3 && index < len(clipartImages); index++ {
			addArtwork(clipartImages[index], clipartLabels[index])
		}
	}
	// Before speakers and talks are announced, use the same city and venue
	// photography shown on this conference's public venue section. Never fill
	// an edition card with imagery from an unrelated event.
	for _, image := range confVenueImages(conf.Tag) {
		if len(card.Images) == 3 {
			break
		}
		addArtwork(image, conf.Location)
	}
	if x, y, ok := homeMapPosition(conf); ok {
		card.MapPoints = []imgproc.SiteSocialCardMapPoint{{X: x, Y: y, Upcoming: true}}
	}
	applySiteSocialCardPalette(&card, conf)
	if ctx != nil && ctx.DB != nil {
		if sponsorships, err := getters.ListSponsorships(ctx, conf.Ref); err != nil {
			if ctx.Err != nil {
				ctx.Err.Printf("conference social card %s sponsors: %s", conf.Tag, err)
			}
		} else {
			applyConferenceSocialCardSponsors(&card, sponsorships)
		}
	}
	return normalizeSiteSocialCard(ctx, card)
}

func applyConferenceSocialCardTicketPrice(card *imgproc.SiteSocialCard, conf *types.Conf, now time.Time) {
	if card == nil || conf == nil {
		return
	}
	current := types.CurrentConfTicketAt(conf.Tickets, conf.TixSold, now)
	if current == nil || current.StandardPrice() == 0 {
		return
	}
	card.ValueLabel = "Tickets now"
	card.Value = conferenceTicketPriceLabel(current)
	next := types.NextConfTicketAfter(conf.Tickets, current, conf.TixSold)
	if next == nil || next.StandardPrice() <= current.StandardPrice() || current.SalesEndAt.IsZero() {
		return
	}
	card.Callout = "Price rises by " + current.SalesEndAt.In(conf.Loc()).Format("Jan 2, 2006")
}

func hackathonSocialCard(ctx *config.AppContext, page *HackathonPage) imgproc.SiteSocialCard {
	card := imgproc.SiteSocialCard{
		Kind:        "hackathon",
		Eyebrow:     "hackathon",
		Title:       "Build something real.",
		ValueLabel:  "",
		Value:       "Coming soon",
		Callout:     "Enter the hackathon",
		Images:      []string{"/static/img/rebrand/hackathon-trophy.jpg"},
		AccentColor: types.DefaultConferenceAccentColor,
		TextColor:   "#ffffff",
	}
	if page == nil {
		return normalizeSiteSocialCard(ctx, card)
	}
	card.SponsorLogos, card.SponsorNames = hackathonSocialCardSponsors(page.Sponsorships)
	if page.Competition != nil && strings.TrimSpace(page.Competition.Title) != "" {
		card.Title = page.Competition.Title
	}
	if page.Conf != nil {
		applySiteSocialCardPalette(&card, page.Conf)
		card.Eyebrow = "hackathon"
		date := strings.TrimSpace(page.Conf.DateDesc)
		if date == "" && !page.Conf.StartDate.IsZero() {
			date = page.Conf.StartDate.Format("Jan 2, 2006")
		}
		card.Subtitle = strings.Join(nonEmptyStrings(page.ConferenceLabel(), page.Conf.Location, date), " · ")
	}
	if total := page.PrizePoolSats(); total > 0 {
		card.Value = strings.TrimSuffix(compactSatoshiLabel(total), " satoshis")
		card.ValueSuffix = "sats"
	}
	return normalizeSiteSocialCard(ctx, card)
}

func hackathonSocialCardSponsors(sponsorships []*types.Sponsorship) ([]string, []string) {
	keep := map[string]bool{"Headline": true, "Diamond": true, "Title": true, "Hackathon": true, "Workshop": true}
	return hackathonSocialCardSponsorsForLevels(sponsorships, keep)
}

func applyConferenceSocialCardSponsors(card *imgproc.SiteSocialCard, sponsorships []*types.Sponsorship) {
	if card == nil {
		return
	}
	card.PoweredByLogos, card.PoweredByNames = hackathonSocialCardSponsorsForLevels(sponsorships, map[string]bool{"Title": true})
	headlineLogos, headlineNames := hackathonSocialCardSponsorsForLevels(sponsorships, map[string]bool{"Headline": true, "Diamond": true})
	poweredNames := make(map[string]bool, len(card.PoweredByNames))
	for _, name := range card.PoweredByNames {
		poweredNames[strings.ToLower(strings.TrimSpace(name))] = true
	}
	for index, logo := range headlineLogos {
		name := ""
		if index < len(headlineNames) {
			name = headlineNames[index]
		}
		if poweredNames[strings.ToLower(strings.TrimSpace(name))] {
			continue
		}
		card.SponsorLogos = append(card.SponsorLogos, logo)
		card.SponsorNames = append(card.SponsorNames, name)
	}
}

func hackathonSocialCardSponsorsForLevels(sponsorships []*types.Sponsorship, keep map[string]bool) ([]string, []string) {
	seen := make(map[string]bool)
	var logos, names []string
	for _, tier := range groupSponsorTiers(sponsorships) {
		if tier == nil || !keep[tier.Level] {
			continue
		}
		for _, sponsorship := range tier.Sponsors {
			if sponsorship == nil || sponsorship.Org == nil {
				continue
			}
			org := sponsorship.Org
			key := strings.TrimSpace(org.Ref)
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(org.Name))
			}
			if key == "" || seen[key] {
				continue
			}
			logo := strings.TrimSpace(org.LogoLight)
			if logo == "" {
				logo = strings.TrimSpace(org.LogoDark)
			}
			if logo == "" {
				continue
			}
			seen[key] = true
			logos = append(logos, logo)
			names = append(names, org.Name)
		}
	}
	return logos, names
}

func awardSocialCard(ctx *config.AppContext, page *HackathonPage, award *types.Award) imgproc.SiteSocialCard {
	card := imgproc.SiteSocialCard{
		Kind:       "award",
		Eyebrow:    "hackathon prize",
		Title:      "Hackathon prize",
		ValueLabel: "Prize package",
		Footer:     "btcpp.dev",
	}
	if page != nil && page.Conf != nil {
		applySiteSocialCardPalette(&card, page.Conf)
		card.Footer = strings.TrimSpace(strings.Join(nonEmptyStrings(page.ConferenceLabel()+" hackathon", "btcpp.dev"), " · "))
		date := strings.TrimSpace(page.Conf.DateDesc)
		if date == "" && !page.Conf.StartDate.IsZero() {
			date = page.Conf.StartDate.Format("Jan 2, 2006")
		}
		competition := ""
		if page.Competition != nil {
			competition = strings.TrimSpace(page.Competition.Title)
		}
		card.Callout = strings.Join(nonEmptyStrings(page.ConferenceLabel(), competition), " · ")
		card.Location = strings.Join(nonEmptyStrings(page.Conf.Location, date), " · ")
	}
	if award == nil {
		return normalizeSiteSocialCard(ctx, card)
	}
	card.Title = award.Title
	card.Subtitle = socialCardPlainText(award.Description)
	if page != nil {
		if page.AwardHasSponsor(award) {
			card.Eyebrow = page.AwardSponsorLabel(award)
			logo := ""
			if org := page.AwardSponsorOrg(award); org != nil {
				// Match the public award card's light surface and logo choice.
				logo = strings.TrimSpace(org.LogoLight)
				if logo == "" {
					logo = strings.TrimSpace(org.LogoDark)
				}
			}
			if logo != "" {
				card.Images = []string{logo}
			}
		}
		var total int64
		prizes := page.AwardPrizes(award)
		for _, prize := range prizes {
			total += prizeValueSats(prize)
			if prize != nil && prizeValueSats(prize) == 0 {
				detail := strings.TrimSpace(prize.Title)
				if detail == "" {
					detail = strings.TrimSpace(prize.Description)
				}
				if detail != "" {
					card.Details = append(card.Details, detail)
				}
			}
		}
		if total > 0 {
			card.ValueLabel = "Total prize value"
			card.Value = socialCardPrizeSatoshiLabel(total)
			card.ValueSuffix = "sats"
		} else if len(prizes) > 0 {
			card.Value = strconv.Itoa(len(prizes))
			if len(prizes) == 1 {
				card.ValueSuffix = "prize"
			} else {
				card.ValueSuffix = "prizes"
			}
		}
	}
	return normalizeSiteSocialCard(ctx, card)
}

func socialCardPrizeSatoshiLabel(sats int64) string {
	label := strings.TrimSuffix(compactSatoshiLabel(sats), " satoshis")
	if (strings.HasSuffix(label, "M") || strings.HasSuffix(label, "k")) && !strings.Contains(label, ".") {
		label = label[:len(label)-1] + ".0" + label[len(label)-1:]
	}
	return label
}

func socialCardPlainText(value string) string {
	replacer := strings.NewReplacer(
		"\r", " ", "\n", " ", "**", "", "__", "", "`", "", "#", "", "*", "", "_", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(value)), " ")
}

func whoIsSocialCard(ctx *config.AppContext, people []*WhoIsPerson) imgproc.SiteSocialCard {
	talks, projects, editions := whoIsTotals(people)
	card := imgproc.SiteSocialCard{
		Kind: "whois", Eyebrow: "whois directory", Title: "The people building bitcoin.",
		Subtitle: "Builders, researchers, speakers, and hackers from across the archive.",
		Stats: []imgproc.SiteSocialCardStat{
			{Value: strconv.Itoa(len(people)), Label: "people"},
			{Value: strconv.Itoa(talks), Label: "talks"},
			{Value: strconv.Itoa(projects), Label: "projects"},
			{Value: strconv.Itoa(editions), Label: "events"},
		},
	}
	applySiteSocialCardPalette(&card, nil)
	var featured []*types.Speaker
	if ctx != nil && ctx.DB != nil {
		var err error
		featured, err = getters.ListHomepageFeaturedSpeakers(ctx)
		if err != nil && ctx.Err != nil {
			ctx.Err.Printf("whois social card featured speakers (continuing): %s", err)
		}
	}
	appendWhoIsCardSpeakers(&card, people, featured)
	return normalizeSiteSocialCard(ctx, card)
}

func appendWhoIsCardSpeakers(card *imgproc.SiteSocialCard, people []*WhoIsPerson, featured []*types.Speaker) {
	if card == nil {
		return
	}
	seen := make(map[string]bool, len(people))
	appendSpeaker := func(speaker *types.Speaker) {
		if speaker == nil || len(card.Images) >= 12 || strings.TrimSpace(speaker.Photo) == "" {
			return
		}
		key := strings.TrimSpace(speaker.ID)
		if key == "" {
			key = strings.TrimSpace(speaker.Photo)
		}
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		card.Images = append(card.Images, siteSocialCardStoredImage("speakers", speaker.Photo))
		card.ImageLabels = append(card.ImageLabels, publicid.ProfileHandle(speaker.Github, "github.com"))
	}

	// The homepage's curated speakers lead the composition. The remainder use a
	// stable content-derived shuffle: visually varied without changing a cached
	// social card on every request.
	for _, speaker := range featured {
		appendSpeaker(speaker)
	}
	candidates := make([]*types.Speaker, 0, len(people))
	for _, person := range people {
		if person != nil && person.Speaker != nil {
			candidates = append(candidates, person.Speaker)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := whoIsCardShuffleKey(candidates[i])
		right := whoIsCardShuffleKey(candidates[j])
		if left == right {
			return candidates[i].ID < candidates[j].ID
		}
		return left < right
	})
	for _, speaker := range candidates {
		appendSpeaker(speaker)
	}
}

func whoIsCardShuffleKey(speaker *types.Speaker) uint64 {
	if speaker == nil {
		return 0
	}
	// FNV-1a is sufficient here: this is presentation ordering, not security.
	hash := uint64(1469598103934665603)
	for _, value := range []string{speaker.ID, speaker.Photo, speaker.Name} {
		for _, char := range []byte(value) {
			hash ^= uint64(char)
			hash *= 1099511628211
		}
		hash ^= 0xff
		hash *= 1099511628211
	}
	return hash
}

func personSocialCard(ctx *config.AppContext, person *WhoIsPerson) imgproc.SiteSocialCard {
	card := imgproc.SiteSocialCard{Kind: "person", Eyebrow: "bitcoin++ contributor", Title: "Community builder", Subtitle: "Talks, projects, and appearances from the archive."}
	applySiteSocialCardPalette(&card, nil)
	if person == nil || person.Speaker == nil {
		return normalizeSiteSocialCard(ctx, card)
	}
	card.Title = person.Speaker.Name
	card.Eyebrow = "bitcoin++ contributor"
	card.ProfileHandle = person.PublicID
	card.XHandle = person.Speaker.Twitter.Handle
	card.GitHubHandle = publicid.ProfileHandle(person.Speaker.Github, "github.com")
	if bio := personSocialCardBio(person.Speaker.Bio); bio != "" {
		card.Subtitle = bio
	} else if company := strings.TrimSpace(person.Speaker.Company); company != "" {
		card.Subtitle = company
	}
	var latestEdition *types.Conf
	for _, edition := range person.Editions {
		if edition != nil && (latestEdition == nil || edition.StartDate.After(latestEdition.StartDate)) {
			latestEdition = edition
		}
	}
	if latestEdition != nil {
		applySiteSocialCardPalette(&card, latestEdition)
	}
	details := make([]string, 0, len(person.Talks)+len(person.Projects)+len(person.Editions))
	detailSeen := make(map[string]bool)
	appendDetail := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		detail := label + " · " + value
		key := strings.ToLower(detail)
		if detailSeen[key] {
			return
		}
		detailSeen[key] = true
		details = append(details, detail)
	}
	seenImages := make(map[string]bool)
	appendArtifact := func(image string) {
		image = strings.TrimSpace(image)
		if image == "" || seenImages[image] || len(card.Images) >= 3 {
			return
		}
		seenImages[image] = true
		card.Images = append(card.Images, image)
	}
	var talkArtifacts []string
	for _, talk := range person.Talks {
		if talk == nil || talk.Talk == nil {
			continue
		}
		appendDetail("Talk", talk.Talk.Name)
		if clipart := strings.TrimSpace(talk.Talk.Clipart); clipart != "" {
			talkArtifacts = append(talkArtifacts, siteSocialCardStoredImage("talks", clipart))
		}
	}
	var eventArtifacts []string
	for _, edition := range person.Editions {
		if edition == nil {
			continue
		}
		eventName := strings.TrimSpace(edition.Desc)
		if eventName == "" {
			eventName = edition.Tag
		}
		appendDetail("Event", eventName)
		if image := confImagePath(edition.Tag, "leading"); image != "" && image != "/static/img/rebrand/light-sketch-bg.avif" {
			eventArtifacts = append(eventArtifacts, image)
		}
	}
	var projectArtifacts []string
	for _, project := range person.Projects {
		if project == nil || project.Project == nil {
			continue
		}
		appendDetail("Project", project.Project.Title)
		if len(project.Awards) > 0 {
			for _, award := range project.Awards {
				if award != nil {
					appendDetail("Award", award.Title)
					if len(card.Badges) < 2 {
						card.Badges = append(card.Badges, award.Title)
					}
				}
			}
		}
		if project.Project.ImageURL != "" {
			projectArtifacts = append(projectArtifacts, project.Project.ImageURL)
		}
	}
	card.Details = details
	// Keep both kinds of archival evidence visible: first a talk image, then
	// an attended-event image. Remaining space goes to additional talk art,
	// event art, and finally a hackathon project image.
	if len(talkArtifacts) > 0 {
		appendArtifact(talkArtifacts[0])
	}
	if len(eventArtifacts) > 0 {
		appendArtifact(eventArtifacts[0])
	}
	remainingTalkArtifacts := talkArtifacts
	if len(remainingTalkArtifacts) > 0 {
		remainingTalkArtifacts = remainingTalkArtifacts[1:]
	}
	remainingEventArtifacts := eventArtifacts
	if len(remainingEventArtifacts) > 0 {
		remainingEventArtifacts = remainingEventArtifacts[1:]
	}
	for _, artifact := range append(append(remainingTalkArtifacts, remainingEventArtifacts...), projectArtifacts...) {
		appendArtifact(artifact)
	}
	if photo := strings.TrimSpace(person.Speaker.Photo); photo != "" {
		photo = siteSocialCardStoredImage("speakers", photo)
		if photo != "" {
			card.Images = append([]string{photo}, card.Images...)
		}
	}
	// A complete, evenly paced image rail is more legible than stretching two
	// archival images into oversized crops. Repeat the available supporting
	// work when necessary; keep the profile photo as the first panel.
	if len(card.Images) > 0 && len(card.Images) < 5 {
		repeat := card.Images
		if len(card.Images) > 1 {
			repeat = card.Images[1:]
		}
		for index := 0; len(card.Images) < 5; index++ {
			card.Images = append(card.Images, repeat[index%len(repeat)])
		}
	}
	card.Stats = []imgproc.SiteSocialCardStat{
		{Value: strconv.Itoa(len(person.Editions)), Label: siteSocialCardCountLabel(len(person.Editions), "event")},
		{Value: strconv.Itoa(len(person.Talks)), Label: siteSocialCardCountLabel(len(person.Talks), "talk")},
		{Value: strconv.Itoa(len(person.Projects)), Label: siteSocialCardCountLabel(len(person.Projects), "project")},
	}
	return normalizeSiteSocialCard(ctx, card)
}

func siteSocialCardCountLabel(count int, singular string) string {
	if count == 1 {
		return singular
	}
	return singular + "s"
}

func personSocialCardBio(raw string) string {
	bio := strings.Join(strings.Fields(raw), " ")
	if len([]rune(bio)) <= 135 {
		return bio
	}
	for index, char := range bio {
		if char == '.' || char == '!' || char == '?' {
			return strings.TrimSpace(bio[:index+1])
		}
	}
	return bio
}

func shopSocialCard(ctx *config.AppContext, products []*types.MerchProduct) imgproc.SiteSocialCard {
	card := imgproc.SiteSocialCard{
		Kind: "shop", Eyebrow: "merch shop", Title: "Wear the", TitleSuffix: "frontier.",
		Subtitle: "Small-batch apparel, hats, and gear for people who build on bitcoin and run their own nodes.",
	}
	card.AccentColor = "#e9714f"
	card.TextColor = "#ffffff"
	for _, product := range shopSocialCardHats(products) {
		card.Images = append(card.Images, merchImage(product))
	}
	return normalizeSiteSocialCard(ctx, card)
}

func shopSocialCardHats(products []*types.MerchProduct) []*types.MerchProduct {
	var libreRelay *types.MerchProduct
	otherHats := make([]*types.MerchProduct, 0, 3)
	seenImages := make(map[string]bool)
	for _, product := range products {
		if product == nil || !isHatProduct(product) {
			continue
		}
		image := strings.TrimSpace(merchImage(product))
		if image == "" || seenImages[image] {
			continue
		}
		seenImages[image] = true
		if isLibreRelayHat(product) {
			if libreRelay == nil {
				libreRelay = product
			}
			continue
		}
		if len(otherHats) < 3 {
			otherHats = append(otherHats, product)
		}
	}
	if libreRelay == nil {
		return otherHats
	}
	if len(otherHats) == 0 {
		return []*types.MerchProduct{libreRelay}
	}
	// The center product is the visual hero in the three-card shop layout.
	hats := []*types.MerchProduct{otherHats[0], libreRelay}
	if len(otherHats) > 1 {
		hats = append(hats, otherHats[1])
	}
	return hats
}

func isHatProduct(product *types.MerchProduct) bool {
	if product == nil {
		return false
	}
	identity := strings.ToLower(strings.Join([]string{product.Tag, product.Slug, product.Name}, " "))
	return strings.Contains(identity, "hat") || isLibreRelayHat(product)
}

func isLibreRelayHat(product *types.MerchProduct) bool {
	if product == nil {
		return false
	}
	identity := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(strings.Join([]string{product.Tag, product.Slug, product.Name, merchImage(product)}, " ")))
	return strings.Contains(identity, "librerelay")
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
	sponsorships, err := getters.ListSponsorships(ctx, conf.Ref)
	if err != nil {
		return imgproc.SiteSocialCard{}, fmt.Errorf("load hackathon sponsors for %q: %w", tag, err)
	}
	page := &HackathonPage{Competition: competition, Conf: conf, Projects: projects, Awards: awards, PrizesByAward: prizes, PrizePoolByAward: pool, Sponsorships: sponsorships}
	return hackathonSocialCard(ctx, page), nil
}

func loadAwardSocialCard(ctx *config.AppContext, encodedSlug string) (imgproc.SiteSocialCard, error) {
	parts := strings.SplitN(strings.TrimSpace(encodedSlug), "--", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return imgproc.SiteSocialCard{}, fmt.Errorf("invalid award social card slug")
	}
	conf, err := getters.GetConfByTag(ctx, parts[0])
	if err != nil || conf == nil || !conf.IsPublished() {
		return imgproc.SiteSocialCard{}, fmt.Errorf("published conference %q not found", parts[0])
	}
	competition, err := getters.GetCompetitionByConferenceID(ctx, conf.Ref)
	if err != nil || competition == nil || competition.Visibility != getters.CompetitionVisibilityPublic {
		return imgproc.SiteSocialCard{}, fmt.Errorf("public hackathon for %q not found", parts[0])
	}
	awards, prizes, pool, awardees, err := loadPublicHackathonAwards(ctx, competition.ID, competition.ResultsFinalizedAt != nil)
	if err != nil {
		return imgproc.SiteSocialCard{}, err
	}
	orgs, err := loadHackathonOrgMap(ctx)
	if err != nil {
		return imgproc.SiteSocialCard{}, err
	}
	page := &HackathonPage{Competition: competition, Conf: conf, Awards: awards, PrizesByAward: prizes, PrizePoolByAward: pool, AwardeesByAward: awardees, OrgsByID: orgs}
	award := page.AwardBySlug(parts[1])
	if award == nil {
		return imgproc.SiteSocialCard{}, fmt.Errorf("public award %q not found", parts[1])
	}
	return awardSocialCard(ctx, page, award), nil
}

func loadSiteSocialCard(ctx *config.AppContext, kind, slug string) (imgproc.SiteSocialCard, error) {
	switch kind {
	case "home":
		confs, err := getters.ListConfs(ctx)
		return homeSocialCard(ctx, confs), err
	case "events":
		confs, err := getters.ListConfs(ctx)
		return eventsSocialCard(ctx, confs), err
	case "conference":
		conf, err := getters.GetConfByTag(ctx, slug)
		if err == nil && (conf == nil || !conf.IsPublished()) {
			err = fmt.Errorf("published conference not found")
		}
		var talks []*types.Talk
		if err == nil {
			if sold, soldErr := getters.SoldTix(ctx, conf); soldErr == nil {
				conf.TixSold = sold
			} else if ctx.Err != nil {
				ctx.Err.Printf("conference social card %s sold tickets: %s", slug, soldErr)
			}
			if loaded, talkErr := getters.GetTalksFor(ctx, slug); talkErr == nil {
				talks = loaded
			} else {
				ctx.Err.Printf("conference social card %s talks: %s", slug, talkErr)
			}
		}
		return conferenceSocialCard(ctx, conf, talks), err
	case "hackathon":
		return loadHackathonSocialCard(ctx, slug)
	case "award":
		return loadAwardSocialCard(ctx, slug)
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

func cachedSiteSocialCardJPEG(cacheKey string) []byte {
	siteSocialCardCache.RLock()
	jpeg := siteSocialCardCache.images[cacheKey]
	siteSocialCardCache.RUnlock()
	return jpeg
}

func rememberSiteSocialCardJPEG(cacheKey string, jpeg []byte) {
	if len(jpeg) == 0 {
		return
	}
	siteSocialCardCache.Lock()
	if len(siteSocialCardCache.images) >= siteSocialCardMemoryCacheLimit {
		for key := range siteSocialCardCache.images {
			delete(siteSocialCardCache.images, key)
			break
		}
	}
	siteSocialCardCache.images[cacheKey] = jpeg
	siteSocialCardCache.Unlock()
}

func loadOrCreateSiteSocialCard(card imgproc.SiteSocialCard, objectKey, cacheKey, version string) (siteSocialCardRenderResult, error) {
	value, err, _ := siteSocialCardRenderGroup.Do(cacheKey, func() (any, error) {
		if jpeg := cachedSiteSocialCardJPEG(cacheKey); len(jpeg) > 0 {
			return siteSocialCardRenderResult{jpeg: jpeg}, nil
		}
		if siteSocialCardStorage.isConfigured() && siteSocialCardStorage.exists(objectKey) {
			return siteSocialCardRenderResult{publicURL: siteSocialCardStorage.publicURL(objectKey)}, nil
		}
		jpeg, renderErr := renderSiteSocialCardJPEG(card)
		if renderErr != nil {
			return siteSocialCardRenderResult{}, renderErr
		}
		if siteSocialCardStorage.isConfigured() {
			publicURL, uploadErr := siteSocialCardStorage.upload(objectKey, jpeg, "image/jpeg", version)
			if uploadErr == nil {
				return siteSocialCardRenderResult{publicURL: publicURL}, nil
			}
			rememberSiteSocialCardJPEG(cacheKey, jpeg)
			return siteSocialCardRenderResult{jpeg: jpeg, persistErr: uploadErr}, nil
		}
		rememberSiteSocialCardJPEG(cacheKey, jpeg)
		return siteSocialCardRenderResult{jpeg: jpeg}, nil
	})
	if err != nil {
		return siteSocialCardRenderResult{}, err
	}
	return value.(siteSocialCardRenderResult), nil
}

func redirectToSiteSocialCardObject(w http.ResponseWriter, publicURL string) {
	w.Header().Set("Cache-Control", "public, max-age=3600, stale-while-revalidate=86400")
	w.Header().Set("Location", publicURL)
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusTemporaryRedirect)
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
	w.Header().Set("Cache-Control", "public, max-age=3600, stale-while-revalidate=86400")
	w.Header().Set("ETag", `"`+version+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == `"`+version+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		objectKey := siteSocialCardObjectKey(kind, slug, version)
		if siteSocialCardStorage.isConfigured() && siteSocialCardStorage.exists(objectKey) {
			redirectToSiteSocialCardObject(w, siteSocialCardStorage.publicURL(objectKey))
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		return
	}
	objectKey := siteSocialCardObjectKey(kind, slug, version)
	cacheKey := kind + ":" + slug + ":" + version
	result, err := loadOrCreateSiteSocialCard(card, objectKey, cacheKey, version)
	if err != nil {
		ctx.Err.Printf("%s social card render: %s", r.URL.Path, err)
		http.Error(w, "Unable to render social card", http.StatusInternalServerError)
		return
	}
	if result.persistErr != nil {
		ctx.Err.Printf("%s social card persistence failed (serving directly): %s", r.URL.Path, result.persistErr)
	}
	if result.publicURL != "" {
		redirectToSiteSocialCardObject(w, result.publicURL)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(result.jpeg)
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
	if kind == "person" {
		if name := strings.TrimSpace(r.URL.Query().Get("name")); name != "" {
			card.Title = truncateSiteSocialCardText(name, 68)
		}
	}
	html, err := imgproc.RenderSiteSocialCardHTML(card)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}
