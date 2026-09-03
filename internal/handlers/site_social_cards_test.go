package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"

	"golang.org/x/sync/singleflight"
)

func TestSiteSocialCardPathIncludesContentVersion(t *testing.T) {
	card := imgproc.SiteSocialCard{Kind: "person", Title: "Mara Chen"}
	got := siteSocialCardPath("person", "mara-chen", card)
	if !strings.HasPrefix(got, "/social-cards/person/mara-chen.jpg?v=") {
		t.Fatalf("siteSocialCardPath = %q", got)
	}
	changed := card
	changed.Title = "Mara Nakamoto"
	if got == siteSocialCardPath("person", "mara-chen", changed) {
		t.Fatal("site social card URL did not change with card content")
	}
}

func TestHomeSocialCardFeaturesNextConference(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	confs := []*types.Conf{{Tag: "atx25", PublicationStatus: "published", Desc: "bitcoin++ Austin 2100, mempool edition", Tagline: "The local edition", DateDesc: "August 2100", Location: "Austin", StartDate: time.Date(2100, time.August, 15, 0, 0, 0, 0, time.UTC), MapXPercent: 24, MapYPercent: 42}}
	card := homeSocialCard(ctx, confs)
	if card.Title != "Austin 2100, mempool edition" {
		t.Fatalf("home card title = %q", card.Title)
	}
	if card.Eyebrow != "Up next" {
		t.Fatalf("home card eyebrow = %q", card.Eyebrow)
	}
	if card.Subtitle != "Austin · August 2100" {
		t.Fatalf("home card subtitle = %q", card.Subtitle)
	}
	if len(card.Images) < 1 || len(card.Images) > 6 {
		t.Fatalf("home card images = %#v", card.Images)
	}
	if len(card.ImageLabels) != len(card.Images) || card.ImageLabels[0] != "mempool / edition 1" {
		t.Fatalf("home card image labels = %#v", card.ImageLabels)
	}
	if len(card.Stats) != 0 {
		t.Fatalf("home card stats = %#v, want none", card.Stats)
	}
	if len(card.MapPoints) != 1 || !card.MapPoints[0].Upcoming || card.MapPoints[0].X != 24 || card.MapPoints[0].Y != 42 {
		t.Fatalf("home card map points = %#v", card.MapPoints)
	}
	if !strings.HasSuffix(card.MapImage, "/static/img/home/worldmap.svg") {
		t.Fatalf("home card map image = %q", card.MapImage)
	}
}

func TestHomeSocialCardHeadlineSponsorsUsesUpcomingEventYear(t *testing.T) {
	conf2026 := &types.Conf{Ref: "conf-2026", StartDate: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)}
	conf2027 := &types.Conf{Ref: "conf-2027", StartDate: time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC)}
	sponsorships := []*types.Sponsorship{
		{Level: "Headline", Status: "Paid", Org: &types.Org{Ref: "headline", Name: "Headline Partner", LogoLight: "/headline.svg"}, Confs: []*types.Conf{conf2026}},
		{Level: "Diamond", Status: "Committed", Org: &types.Org{Ref: "legacy", Name: "Legacy Headline", LogoDark: "/legacy.svg"}, Confs: []*types.Conf{conf2026}},
		{Level: "Headline", Status: "Paid", Org: &types.Org{Ref: "future", Name: "Future Partner", LogoLight: "/future.svg"}, Confs: []*types.Conf{conf2027}},
		{Level: "Title", Status: "Paid", Org: &types.Org{Ref: "title", Name: "Title Partner", LogoLight: "/title.svg"}, Confs: []*types.Conf{conf2026}},
		{Level: "Headline", Status: "Pending", Org: &types.Org{Ref: "pending", Name: "Pending Partner", LogoLight: "/pending.svg"}, Confs: []*types.Conf{conf2026}},
	}

	logos, names := homeSocialCardHeadlineSponsors(sponsorships, 2026)
	if got, want := strings.Join(logos, ","), "/headline.svg,/legacy.svg"; got != want {
		t.Fatalf("home headline sponsor logos = %q, want %q", got, want)
	}
	if got, want := strings.Join(names, ","), "Headline Partner,Legacy Headline"; got != want {
		t.Fatalf("home headline sponsor names = %q, want %q", got, want)
	}
}

func TestHomeSponsorsFromSponsorshipsFiltersAndDeduplicates(t *testing.T) {
	org := &types.Org{Ref: "org-1", Name: "Protocol Labs", LogoLight: "/light.svg"}
	sponsors := homeSponsorsFromSponsorships([]*types.Sponsorship{
		{Level: "Workshop", Status: "Paid", Org: &types.Org{Ref: "org-2", Name: "Workshop Co"}},
		{Level: "Headline", Status: "Committed", Org: org},
		{Level: "Headline", Status: "Paid", Org: org},
		{Level: "Gold", Status: "Paid", Org: &types.Org{Ref: "org-3", Name: "Gold Co"}},
		{Level: "Headline", Status: "Pending", Org: &types.Org{Ref: "org-4", Name: "Pending Co"}},
	})
	if len(sponsors) != 2 {
		t.Fatalf("home sponsors = %#v, want two visible unique sponsors", sponsors)
	}
	if sponsors[0].Name != "Protocol Labs" || sponsors[0].Level != "Headline" {
		t.Fatalf("first home sponsor = %#v, want headline sponsor", sponsors[0])
	}
	if sponsors[1].Name != "Workshop Co" || sponsors[1].Level != "Workshop" {
		t.Fatalf("second home sponsor = %#v, want workshop sponsor", sponsors[1])
	}
}

func TestEventsSocialCardFeaturesConferenceArchive(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888", Prod: true}}
	confs := []*types.Conf{
		{Tag: "atx25", PublicationStatus: "published", Desc: "bitcoin++ Austin 2025, mempool edition", AccentColor: "#2563eb", StartDate: time.Date(2100, time.August, 15, 0, 0, 0, 0, time.UTC)},
		{Tag: "berlin25", PublicationStatus: "published", Desc: "bitcoin++ Berlin 2025, lightning edition", StartDate: time.Date(2025, time.August, 15, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2025, time.August, 17, 0, 0, 0, 0, time.UTC)},
		{Tag: "hidden", PublicationStatus: "draft", StartDate: time.Date(2101, time.August, 15, 0, 0, 0, 0, time.UTC)},
	}
	card := eventsSocialCard(ctx, confs)
	if card.Kind != "events" || card.Title != "events archive." {
		t.Fatalf("events card = %#v", card)
	}
	if card.AccentColor != "#f6f3ee" || card.TextColor != "#0a0a0a" {
		t.Fatalf("events card accent = %q", card.AccentColor)
	}
	if len(card.Images) != 2 {
		t.Fatalf("events card images = %#v", card.Images)
	}
	if got, want := strings.Join(card.ImageLabels, ", "), "mempool / edition 2, lightning / edition 1"; got != want {
		t.Fatalf("events card labels = %q, want %q", got, want)
	}
	if len(card.Stats) != 0 {
		t.Fatalf("events card stats = %#v, want none", card.Stats)
	}
}

func TestShopSocialCardFeaturesThreeDistinctHatsWithLibreRelayCentered(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	products := []*types.MerchProduct{
		{Tag: "core-hat", Name: "Core Hat", BasePriceCents: 3500},
		{Tag: "tee", Name: "bitcoin++ Tee"},
		{Tag: "librerelay-hat", Slug: "libre-relay", Name: "Libre Relay Hat", BasePriceCents: 4000},
		{Tag: "libbit-hat", Name: "Libbitcoin Hat", BasePriceCents: 4500},
		{Tag: "sticker", Name: "Sticker"},
		{Tag: "core-hat", Name: "Duplicate Core Hat"},
	}

	card := shopSocialCard(ctx, products)
	if len(card.Images) != 3 {
		t.Fatalf("shop card images = %#v, want three hats", card.Images)
	}
	if !strings.Contains(card.Images[0], "core-hat") || !strings.Contains(card.Images[1], "librerelay-hat") || !strings.Contains(card.Images[2], "libbit-hat") {
		t.Fatalf("shop card images = %#v, want core, Libre Relay, and Libbitcoin hats", card.Images)
	}
	if got := strings.Join(card.ImageLabels, ""); got != "" {
		t.Fatalf("shop card labels = %#v, want no prices on the overview card", card.ImageLabels)
	}
}

func TestNormalizeSiteSocialCardKeepsImageLabelsAligned(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	card := normalizeSiteSocialCard(ctx, imgproc.SiteSocialCard{
		Kind:        "events",
		Images:      []string{"https://example.test/rejected.jpg", "/static/img/atx25/leading.jpg"},
		ImageLabels: []string{"wrong", "mempool / edition 9"},
	})
	if len(card.Images) != 1 || len(card.ImageLabels) != 1 || card.ImageLabels[0] != "mempool / edition 9" {
		t.Fatalf("normalized images and labels are misaligned: images=%#v labels=%#v", card.Images, card.ImageLabels)
	}
}

func TestOnlyPeopleSiteSocialCardsUseMetrics(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	nonPeopleCards := []imgproc.SiteSocialCard{
		conferenceSocialCard(ctx, &types.Conf{Desc: "bitcoin++ Berlin"}),
		hackathonSocialCard(ctx, &HackathonPage{}),
		eventsSocialCard(ctx, nil),
		shopSocialCard(ctx, []*types.MerchProduct{{}}),
	}
	for _, card := range nonPeopleCards {
		if len(card.Stats) != 0 {
			t.Errorf("%s card stats = %#v, want none", card.Kind, card.Stats)
		}
	}

	person := &WhoIsPerson{PublicID: "mara", Speaker: &types.Speaker{Name: "Mara"}}
	peopleCards := []imgproc.SiteSocialCard{
		personSocialCard(ctx, person),
		whoIsSocialCard(ctx, []*WhoIsPerson{person}),
	}
	for _, card := range peopleCards {
		if len(card.Stats) == 0 {
			t.Errorf("%s card is missing people metrics", card.Kind)
		}
		hasEvents := false
		for _, stat := range card.Stats {
			if stat.Label == "editions" {
				t.Errorf("%s card uses obsolete editions label: %#v", card.Kind, card.Stats)
			}
			if stat.Label == "events" {
				hasEvents = true
			}
		}
		if !hasEvents {
			t.Errorf("%s card is missing events metric: %#v", card.Kind, card.Stats)
		}
	}
}

func TestWhoIsSocialCardPrioritizesFeaturedSpeakersAndGitHubLabels(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	featured := &types.Speaker{ID: "featured", Name: "Featured", Photo: "https://localhost/featured.jpg", Github: "https://github.com/featured-dev"}
	other := &types.Speaker{ID: "other", Name: "Other", Photo: "https://localhost/other.jpg", Github: "other-dev"}
	card := imgproc.SiteSocialCard{Kind: "whois"}
	appendWhoIsCardSpeakers(&card, []*WhoIsPerson{{Speaker: other}, {Speaker: featured}}, []*types.Speaker{featured})
	card = normalizeSiteSocialCard(ctx, card)

	if got, want := strings.Join(card.Images, ","), "https://localhost/featured.jpg,https://localhost/other.jpg"; got != want {
		t.Fatalf("whois portrait order = %q, want %q", got, want)
	}
	if got, want := strings.Join(card.ImageLabels, ","), "featured-dev,other-dev"; got != want {
		t.Fatalf("whois GitHub labels = %q, want %q", got, want)
	}
}

func TestWhoIsSocialCardUsesAtMostTwelveUniquePortraits(t *testing.T) {
	featured := &types.Speaker{ID: "speaker-07", Name: "Featured", Photo: "https://localhost/07.jpg"}
	people := make([]*WhoIsPerson, 0, 15)
	for index := 0; index < 15; index++ {
		speaker := &types.Speaker{
			ID: fmt.Sprintf("speaker-%02d", index), Name: fmt.Sprintf("Speaker %02d", index),
			Photo: fmt.Sprintf("https://localhost/%02d.jpg", index),
		}
		people = append(people, &WhoIsPerson{Speaker: speaker})
	}
	card := imgproc.SiteSocialCard{Kind: "whois"}
	appendWhoIsCardSpeakers(&card, people, []*types.Speaker{featured})

	if len(card.Images) != 12 {
		t.Fatalf("whois portrait count = %d, want 12", len(card.Images))
	}
	if card.Images[0] != featured.Photo {
		t.Fatalf("first whois portrait = %q, want featured %q", card.Images[0], featured.Photo)
	}
	seen := map[string]bool{}
	for _, image := range card.Images {
		if seen[image] {
			t.Fatalf("whois portrait %q was repeated", image)
		}
		seen[image] = true
	}
}

func TestHackathonSocialCardFeaturesTrophyAndPrizePool(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	page := &HackathonPage{
		Competition: &types.HackathonCompetition{Title: "Signet Hackathon"},
		Conf:        &types.Conf{Tag: "dev26", Desc: "bitcoin++ Local Dev", Location: "Austin", DateDesc: "August 2026", AccentColor: "#2563eb"},
		PrizePoolByAward: map[string][]*types.Prize{
			"main": {{PrizeType: getters.PrizeTypeSats, ValueText: "1750000"}},
		},
		Sponsorships: []*types.Sponsorship{
			{Level: "Hackathon", Status: "Paid", Org: &types.Org{Ref: "hack", Name: "Hack Partner", LogoLight: "https://localhost/hack.svg"}},
			{Level: "Workshop", Status: "Paid", Org: &types.Org{Ref: "workshop", Name: "Workshop Partner", LogoLight: "https://localhost/workshop.svg"}},
			{Level: "Gold", Status: "Paid", Org: &types.Org{Ref: "gold", Name: "Gold Partner", LogoLight: "https://localhost/gold.svg"}},
			{Level: "Title", Status: "Committed", Org: &types.Org{Ref: "title", Name: "Title Partner", LogoDark: "https://localhost/title.svg"}},
			{Level: "Headline", Status: "Paid", Org: &types.Org{Ref: "headline", Name: "Headline Partner", LogoLight: "https://localhost/headline.svg"}},
			{Level: "Hackathon", Status: "Pending", Org: &types.Org{Ref: "pending", Name: "Pending Partner", LogoLight: "https://localhost/pending.svg"}},
			{Level: "Title", Status: "Paid", Org: &types.Org{Ref: "hack", Name: "Hack Partner", LogoLight: "https://localhost/hack.svg"}},
		},
	}
	card := hackathonSocialCard(ctx, page)
	if !strings.Contains(strings.ToLower(card.Eyebrow), "hackathon") {
		t.Fatalf("hackathon card eyebrow = %q", card.Eyebrow)
	}
	if card.Value != "1.75M" || card.ValueSuffix != "sats" {
		t.Fatalf("hackathon card prize pool = %q %q", card.Value, card.ValueSuffix)
	}
	if card.Callout != "Enter the hackathon" {
		t.Fatalf("hackathon card callout = %q", card.Callout)
	}
	if card.Subtitle != "Local Dev · Austin · August 2026" {
		t.Fatalf("hackathon card subtitle = %q, want conference, location, and date metadata", card.Subtitle)
	}
	if len(card.Images) != 1 || card.Images[0] != "http://localhost:8888/static/img/rebrand/hackathon-trophy.jpg" {
		t.Fatalf("hackathon card images = %#v", card.Images)
	}
	if got, want := strings.Join(card.SponsorNames, ","), "Headline Partner,Hack Partner,Title Partner,Workshop Partner"; got != want {
		t.Fatalf("hackathon card sponsors = %q, want %q", got, want)
	}
	if len(card.SponsorLogos) != 4 || strings.Contains(strings.Join(card.SponsorLogos, ","), "gold.svg") || strings.Contains(strings.Join(card.SponsorLogos, ","), "pending.svg") {
		t.Fatalf("hackathon card sponsor logos = %#v", card.SponsorLogos)
	}
}

func TestConferenceSocialCardSeparatesHeadlineAndTitleSponsors(t *testing.T) {
	card := imgproc.SiteSocialCard{Kind: "conference"}
	shared := &types.Org{Ref: "shared", Name: "Shared Partner", LogoLight: "/shared.svg"}
	applyConferenceSocialCardSponsors(&card, []*types.Sponsorship{
		{Level: "Headline", Status: "Paid", Org: &types.Org{Ref: "headline", Name: "Headline Partner", LogoLight: "/headline.svg"}},
		{Level: "Diamond", Status: "Committed", Org: &types.Org{Ref: "legacy", Name: "Legacy Headline", LogoDark: "/legacy.svg"}},
		{Level: "Title", Status: "Paid", Org: &types.Org{Ref: "title", Name: "Title Partner", LogoLight: "/title.svg"}},
		{Level: "Title", Status: "Paid", Org: shared},
		{Level: "Headline", Status: "Paid", Org: shared},
		{Level: "Hackathon", Status: "Paid", Org: &types.Org{Ref: "hack", Name: "Hack Partner", LogoLight: "/hack.svg"}},
	})

	if got, want := strings.Join(card.SponsorNames, ","), "Headline Partner,Legacy Headline"; got != want {
		t.Fatalf("conference headline ticker = %q, want %q", got, want)
	}
	if got, want := strings.Join(card.PoweredByNames, ","), "Shared Partner,Title Partner"; got != want {
		t.Fatalf("conference powered-by sponsors = %q, want %q", got, want)
	}
}

func TestPersonSocialCardIncludesTalkAndAward(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	person := &WhoIsPerson{
		PublicID: "mara-chen",
		Speaker: &types.Speaker{
			Name: "Mara Chen", Photo: "../static/img/julien.jpg",
			Bio:     "Mara builds practical Bitcoin infrastructure for developers. This second sentence should not appear on the card because the complete bio is intentionally long enough to require shortening.",
			Twitter: types.Twitter{Handle: "mara_x"}, Github: "https://github.com/mara-gh",
		},
		Talks: []*WhoIsTalk{
			{Talk: &types.Talk{Name: "Building Bitcoin", Clipart: "first.png"}},
			{Talk: &types.Talk{Name: "Testing Bitcoin", Clipart: "second.png"}},
		},
		Projects: []*WhoIsProject{{Project: &types.HackathonProject{Title: "Relay Lab", ImageURL: "/static/img/project.png"}, Awards: []*getters.PublicProfileProjectAward{{Title: "Best in Show"}}}},
		Editions: []*types.Conf{{Tag: "dev26", Desc: "Local Dev 2026"}},
	}
	card := personSocialCard(ctx, person)
	if got, want := card.Subtitle, "Mara builds practical Bitcoin infrastructure for developers."; got != want {
		t.Fatalf("person card subtitle = %q, want %q", got, want)
	}
	for _, want := range []string{"Talk · Building Bitcoin", "Talk · Testing Bitcoin", "Project · Relay Lab", "Award · Best in Show", "Event · Local Dev 2026"} {
		if !slices.Contains(card.Details, want) {
			t.Fatalf("person card accomplishments %#v missing %q", card.Details, want)
		}
	}
	if card.ProfileHandle != "mara-chen" || card.XHandle != "mara_x" || card.GitHubHandle != "mara-gh" {
		t.Fatalf("person card handles = (%q, %q, %q)", card.ProfileHandle, card.XHandle, card.GitHubHandle)
	}
	if got, want := card.Stats, []imgproc.SiteSocialCardStat{{Value: "1", Label: "event"}, {Value: "2", Label: "talks"}, {Value: "1", Label: "project"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("person card stats = %#v, want %#v", got, want)
	}
	if len(card.Images) != 5 || card.Images[0] != "http://localhost:8888/static/img/julien.jpg" || !slices.Contains(card.Images, "http://localhost:8888/talks/first.png") || !slices.Contains(card.Images, "http://localhost:8888/talks/second.png") || len(card.Badges) != 1 || card.Badges[0] != "Best in Show" {
		t.Fatalf("person card artifacts = images %#v, badges %#v", card.Images, card.Badges)
	}
}

func TestSiteSocialCardImageHostAllowlist(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	if !siteSocialCardImageHostAllowed(ctx, "http://localhost:8888/static/card.jpg") {
		t.Fatal("development site image was rejected")
	}
	if siteSocialCardImageHostAllowed(ctx, "http://169.254.169.254/latest/meta-data") {
		t.Fatal("cloud metadata host was accepted")
	}
	if siteSocialCardImageHostAllowed(ctx, "http://example.test/user-controlled.jpg") {
		t.Fatal("untrusted image host was accepted")
	}
}

func TestSiteSocialCardStoredImageSupportsDevelopmentFixtures(t *testing.T) {
	if got, want := siteSocialCardStoredImage("speakers", "../static/img/julien.jpg"), "/static/img/julien.jpg"; got != want {
		t.Fatalf("siteSocialCardStoredImage fixture = %q, want %q", got, want)
	}
	if got, want := siteSocialCardStoredImage("talks", "/static/img/talk.png"), "/static/img/talk.png"; got != want {
		t.Fatalf("siteSocialCardStoredImage static = %q, want %q", got, want)
	}
}

func TestNormalizeSiteSocialCardReservesWordmarkForBrand(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	card := normalizeSiteSocialCard(ctx, imgproc.SiteSocialCard{
		Eyebrow:  "bitcoin++ worldwide",
		Title:    "The people of Bitcoin++",
		Subtitle: "The bitcoin++ archive",
	})
	for _, value := range []string{card.Eyebrow, card.Title, card.Subtitle} {
		if strings.Contains(strings.ToLower(value), "bitcoin++") {
			t.Fatalf("visible card copy still contains wordmark: %q", value)
		}
	}
}

func TestSiteSocialCardPaletteUsesConferenceAccentWithDefault(t *testing.T) {
	if got := siteSocialCardPaletteForConf(nil); got.Accent != types.DefaultConferenceAccentColor || got.Ink != "#0a0a0a" {
		t.Fatalf("default palette = %+v", got)
	}
	if got := siteSocialCardPaletteForConf(&types.Conf{AccentColor: "#2563eb"}); got.Accent != "#2563eb" || got.Ink != "#ffffff" {
		t.Fatalf("dark event palette = %+v", got)
	}
}

func TestConferenceSocialCardUsesEventPalette(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	card := conferenceSocialCard(ctx, &types.Conf{Tag: "berlin26", Desc: "bitcoin++ Berlin", Tagline: "Sovereign systems", Location: "Berlin", Venue: "Festsaal Kreuzberg", AccentColor: "#795738", MapXPercent: 53.1, MapYPercent: 32.4})
	if card.AccentColor != "#795738" || card.TextColor != "#ffffff" {
		t.Fatalf("berlin26 palette = (%q, %q)", card.AccentColor, card.TextColor)
	}
	if card.Subtitle != "Berlin · Festsaal Kreuzberg" || card.Location != "Berlin · Festsaal Kreuzberg" {
		t.Fatalf("conference card copy = subtitle %q, location %q", card.Subtitle, card.Location)
	}
	if card.MapImage == "" || len(card.MapPoints) != 1 || card.MapPoints[0].X != 53.1 || card.MapPoints[0].Y != 32.4 {
		t.Fatalf("conference card map = image %q, points %+v", card.MapImage, card.MapPoints)
	}
	if len(card.Images) == 0 {
		t.Fatal("conference artwork wall is empty")
	}
}

func TestConferenceSocialCardSplitsEditionTitleWithoutComma(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	card := conferenceSocialCard(ctx, &types.Conf{Tag: "dev26", Desc: "bitcoin++ Local Dev 2026, signet edition"})
	if card.Title != "Local Dev 2026" || card.TitleSuffix != "signet edition" {
		t.Fatalf("conference title = (%q, %q), want split lines without comma", card.Title, card.TitleSuffix)
	}
}

func TestConferenceSocialCardShowsCurrentTicketPriceAndIncreaseDate(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	current := &types.ConfTicket{ID: "early", BasePrice: 99, Symbol: "$", Max: 50, SalesEndAt: now.Add(24 * time.Hour)}
	next := &types.ConfTicket{ID: "standard", BasePrice: 149, Symbol: "$", Max: 100, SalesEndAt: now.Add(30 * 24 * time.Hour)}
	card := imgproc.SiteSocialCard{}
	applyConferenceSocialCardTicketPrice(&card, &types.Conf{Tickets: []*types.ConfTicket{next, current}}, now)

	if card.ValueLabel != "Tickets now" || card.Value != "$99" {
		t.Fatalf("conference ticket price = (%q, %q), want current $99 ticket", card.ValueLabel, card.Value)
	}
	if card.Callout != "Price rises by Sep 3, 2026" {
		t.Fatalf("conference ticket increase = %q", card.Callout)
	}
}

func TestConferenceSocialCardPrefersCurrentSpeakerAndTalkArtwork(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	conf := &types.Conf{Tag: "dev26", Desc: "bitcoin++ Local Dev", PublicationStatus: "published"}
	talk := &types.Talk{
		Name: "Build a Better Signet", Clipart: "https://localhost/talk.png", Status: StatusScheduled,
		Speakers: []*types.Speaker{{ID: "speaker-1", Name: "Mara Chen", Photo: "https://localhost/mara.jpg"}},
	}
	card := conferenceSocialCard(ctx, conf, []*types.Talk{talk})
	if len(card.Images) != 3 {
		t.Fatalf("conference artwork count = %d, want 3", len(card.Images))
	}
	if card.Images[1] != "https://localhost/mara.jpg" || card.ImageLabels[1] != "Mara Chen" {
		t.Fatalf("second artwork = (%q, %q), want featured speaker", card.Images[1], card.ImageLabels[1])
	}
	if card.Images[2] != "https://localhost/talk.png" || card.ImageLabels[2] != "Build a Better Signet" {
		t.Fatalf("third artwork = (%q, %q), want talk clipart", card.Images[2], card.ImageLabels[2])
	}
}

func TestConferenceSocialCardFallsBackToItsVenuePhotography(t *testing.T) {
	t.Chdir(findRepoRoot(t))
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	conf := &types.Conf{Tag: "berlin26", Desc: "bitcoin++ Berlin", Location: "Berlin", PublicationStatus: "published"}
	card := conferenceSocialCard(ctx, conf, nil)
	if len(card.Images) != 3 {
		t.Fatalf("conference artwork count = %d, want leading plus two venue images", len(card.Images))
	}
	if !strings.Contains(card.Images[0], "/static/img/berlin26/leading.") {
		t.Fatalf("first artwork = %q, want conference leading image", card.Images[0])
	}
	for index, filename := range []string{"one.jpg", "two.jpg"} {
		if !strings.Contains(card.Images[index+1], "/static/img/berlin26/"+filename) || card.ImageLabels[index+1] != "Berlin" {
			t.Fatalf("venue artwork %d = (%q, %q), want %s labeled Berlin", index, card.Images[index+1], card.ImageLabels[index+1], filename)
		}
	}
}

func TestAwardSocialCardMatchesPublicAward(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	award := &types.Award{ID: "award-id", Title: "Best Signet Infrastructure", Description: "**Ship** something useful.", SponsoredByOrgID: "org-id"}
	page := &HackathonPage{
		Conf:        &types.Conf{Tag: "dev26", Desc: "bitcoin++ Local Dev", AccentColor: "#2563eb"},
		Competition: &types.HackathonCompetition{Title: "Signet Hackathon"},
		PrizesByAward: map[string][]*types.Prize{
			"award-id": {
				{PrizeType: getters.PrizeTypeSats, Title: "First place", ValueText: "1750000"},
				{PrizeType: getters.PrizeTypeInKind, Title: "Hardware signing device"},
			},
		},
		OrgsByID: map[string]*types.Org{
			"org-id": {Name: "Example Sponsor", LogoLight: "/static/img/sponsors/example.png"},
		},
	}
	card := awardSocialCard(ctx, page, award)
	if card.Kind != "award" || card.Title != award.Title || card.Eyebrow != "Example Sponsor" {
		t.Fatalf("award card identity = %+v", card)
	}
	if card.Value != "1.75M" || card.ValueSuffix != "sats" {
		t.Fatalf("award card value = %q %q", card.Value, card.ValueSuffix)
	}
	if got, want := strings.Join(card.Details, ","), "Hardware signing device"; got != want {
		t.Fatalf("award card prize details = %q, want %q", got, want)
	}
	if card.HeroImage != "" || card.Callout == "" {
		t.Fatalf("award card trophy should be absent while event identity remains: %+v", card)
	}
	if card.Subtitle != "Ship something useful." {
		t.Fatalf("award card subtitle = %q", card.Subtitle)
	}
	if card.AccentColor != "#2563eb" || card.TextColor != "#ffffff" {
		t.Fatalf("award card palette = (%q, %q)", card.AccentColor, card.TextColor)
	}
}

func TestSocialCardPrizeSatoshiLabelKeepsExactValues(t *testing.T) {
	for _, test := range []struct {
		sats int64
		want string
	}{
		{1_000_000, "1.0M"},
		{1_750_000, "1.75M"},
		{250_000, "250.0k"},
	} {
		if got := socialCardPrizeSatoshiLabel(test.sats); got != test.want {
			t.Errorf("socialCardPrizeSatoshiLabel(%d) = %q, want %q", test.sats, got, test.want)
		}
	}
}

func TestSiteSocialCardObjectKeyIsContentAddressed(t *testing.T) {
	got := siteSocialCardObjectKey("person", "mara/chen", "abc123")
	if got != "social-cards/site/person/mara-chen/abc123.jpg" {
		t.Fatalf("siteSocialCardObjectKey = %q", got)
	}
	if strings.Contains(got, "..") || strings.Contains(got, "//") {
		t.Fatalf("siteSocialCardObjectKey contains unsafe path components: %q", got)
	}
}

func TestLoadOrCreateSiteSocialCardCoalescesAndPersists(t *testing.T) {
	originalStorage := siteSocialCardStorage
	originalRenderer := renderSiteSocialCardJPEG
	defer func() {
		siteSocialCardStorage = originalStorage
		renderSiteSocialCardJPEG = originalRenderer
		siteSocialCardRenderGroup = singleflight.Group{}
	}()

	var renders atomic.Int32
	var uploads atomic.Int32
	renderStarted := make(chan struct{})
	releaseRender := make(chan struct{})
	renderSiteSocialCardJPEG = func(imgproc.SiteSocialCard) ([]byte, error) {
		if renders.Add(1) == 1 {
			close(renderStarted)
		}
		<-releaseRender
		return []byte("jpeg"), nil
	}
	siteSocialCardStorage.isConfigured = func() bool { return true }
	siteSocialCardStorage.exists = func(string) bool { return false }
	siteSocialCardStorage.publicURL = func(key string) string { return "https://cdn.example/" + key }
	siteSocialCardStorage.upload = func(key string, data []byte, contentType, hash string) (string, error) {
		uploads.Add(1)
		if string(data) != "jpeg" || contentType != "image/jpeg" || hash != "abc123" {
			t.Errorf("upload = (%q, %q, %q)", data, contentType, hash)
		}
		return "https://cdn.example/" + key, nil
	}

	const callers = 8
	start := make(chan struct{})
	results := make(chan siteSocialCardRenderResult, callers)
	errors := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			result, err := loadOrCreateSiteSocialCard(imgproc.SiteSocialCard{Kind: "home"}, "social-cards/site/home/index/abc123.jpg", "home::abc123", "abc123")
			results <- result
			errors <- err
		}()
	}
	ready.Wait()
	close(start)
	<-renderStarted
	time.Sleep(20 * time.Millisecond)
	close(releaseRender)

	for range callers {
		if err := <-errors; err != nil {
			t.Fatalf("loadOrCreateSiteSocialCard: %v", err)
		}
		if result := <-results; result.publicURL == "" || len(result.jpeg) != 0 {
			t.Fatalf("result = %+v, want persisted URL", result)
		}
	}
	if renders.Load() != 1 || uploads.Load() != 1 {
		t.Fatalf("renders = %d, uploads = %d; want one of each", renders.Load(), uploads.Load())
	}
}

func TestLoadOrCreateSiteSocialCardServesJPEGWhenPersistenceFails(t *testing.T) {
	originalStorage := siteSocialCardStorage
	originalRenderer := renderSiteSocialCardJPEG
	defer func() {
		siteSocialCardStorage = originalStorage
		renderSiteSocialCardJPEG = originalRenderer
		siteSocialCardRenderGroup = singleflight.Group{}
	}()

	renderSiteSocialCardJPEG = func(imgproc.SiteSocialCard) ([]byte, error) { return []byte("jpeg"), nil }
	siteSocialCardStorage.isConfigured = func() bool { return true }
	siteSocialCardStorage.exists = func(string) bool { return false }
	siteSocialCardStorage.upload = func(string, []byte, string, string) (string, error) {
		return "", errors.New("storage unavailable")
	}

	result, err := loadOrCreateSiteSocialCard(imgproc.SiteSocialCard{}, "object", "fallback-cache-key", "version")
	if err != nil {
		t.Fatalf("loadOrCreateSiteSocialCard: %v", err)
	}
	if string(result.jpeg) != "jpeg" || result.persistErr == nil || result.publicURL != "" {
		t.Fatalf("result = %+v, want direct JPEG fallback", result)
	}
}

func TestLoadOrCreateSiteSocialCardReusesStoredObject(t *testing.T) {
	originalStorage := siteSocialCardStorage
	originalRenderer := renderSiteSocialCardJPEG
	defer func() {
		siteSocialCardStorage = originalStorage
		renderSiteSocialCardJPEG = originalRenderer
		siteSocialCardRenderGroup = singleflight.Group{}
	}()

	siteSocialCardStorage.isConfigured = func() bool { return true }
	siteSocialCardStorage.exists = func(key string) bool { return key == "stored-object" }
	siteSocialCardStorage.publicURL = func(string) string { return "https://cdn.example/stored.jpg" }
	renderSiteSocialCardJPEG = func(imgproc.SiteSocialCard) ([]byte, error) {
		t.Fatal("stored social card was rendered again")
		return nil, nil
	}

	result, err := loadOrCreateSiteSocialCard(imgproc.SiteSocialCard{}, "stored-object", "stored-cache-key", "version")
	if err != nil {
		t.Fatalf("loadOrCreateSiteSocialCard: %v", err)
	}
	if result.publicURL != "https://cdn.example/stored.jpg" || len(result.jpeg) != 0 {
		t.Fatalf("result = %+v, want existing object URL", result)
	}
}

func TestRedirectToSiteSocialCardObject(t *testing.T) {
	recorder := httptest.NewRecorder()
	redirectToSiteSocialCardObject(recorder, "https://cdn.example/card.jpg")
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusTemporaryRedirect)
	}
	if got := response.Header.Get("Location"); got != "https://cdn.example/card.jpg" {
		t.Fatalf("Location = %q", got)
	}
}
