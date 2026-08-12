package handlers

import (
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestConferenceAgendaMarkdownUsesAbsoluteEventURL(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Host: "localhost", Port: "8888"}}
	got := conferenceAgendaMarkdown(ctx, &types.Conf{Tag: "dev26"})
	want := "[conference agenda](http://localhost:8888/dev26#agenda)"
	if !strings.Contains(got, want) {
		t.Fatalf("conferenceAgendaMarkdown() = %q, want it to contain %q", got, want)
	}
}

func TestConferenceVenueMarkdownIncludesArrivalDetailsAndMap(t *testing.T) {
	got := conferenceVenueMarkdown(&types.Conf{
		Venue:    "Bitcoin House",
		Location: "Austin, Texas",
		DateDesc: "October 1–3, 2026",
		VenueMap: "https://maps.google.com/example",
	}, "8:30am", "9:00am")
	want := "The event will be held at Bitcoin House, Austin, Texas on October 1–3, 2026. Doors will open at 8:30am and coffee will be served at 9:00am. [View the venue on Google Maps →](https://maps.google.com/example)"
	if !strings.Contains(got, want) {
		t.Fatalf("conferenceVenueMarkdown() = %q, want it to contain %q", got, want)
	}
}

func TestConferenceVenueMarkdownOmitsUnconfiguredBreakfast(t *testing.T) {
	got := conferenceVenueMarkdown(&types.Conf{Venue: "Bitcoin House", Location: "Austin"}, "8:30am", "")
	if strings.Contains(got, "coffee") || !strings.Contains(got, "Doors will open at 8:30am.") {
		t.Fatalf("conferenceVenueMarkdown() rendered unconfigured breakfast copy: %q", got)
	}
}

func TestConferenceHotelsListMarkdownIncludesNotesAndPriceRange(t *testing.T) {
	got := conferenceHotelsListMarkdown([]*types.Hotel{
		{Name: "The Annex", URL: "https://hotel.example/annex", PriceRange: "$180–$240/night", Desc: "A short walk from the venue."},
		{Name: "Node Hostel", PriceRange: "$45–$85/night", Desc: "Shared rooms and private pods."},
	})
	for _, want := range []string{
		"- [The Annex](https://hotel.example/annex) — **Price range:** $180–$240/night\n  - A short walk from the venue.",
		"- Node Hostel — **Price range:** $45–$85/night\n  - Shared rooms and private pods.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hotel Markdown does not contain %q:\n%s", want, got)
		}
	}
}

func TestMaterializeConferenceDraftMarkdownKeepsRecipientFieldsAndPutsSponsorsLast(t *testing.T) {
	markdown := "Hello {{ .Name }},\n\n{{ .GeneratedUpdates }}\n\n{{ .SponsorAcknowledgement }}\n\n[Dashboard]({{ .DashboardLink }})"
	footer := "Big thank you to our sponsors: Acme."
	got := materializeConferenceDraftMarkdown(markdown, "### Agenda\n\nCurrent agenda details.", footer)
	if strings.Contains(got, "{{ .GeneratedUpdates }}") || !strings.Contains(got, "Current agenda details.") {
		t.Fatalf("generated updates were not materialized: %q", got)
	}
	if strings.Contains(got, "{{ .SponsorAcknowledgement }}") || !strings.HasSuffix(got, footer) {
		t.Fatalf("sponsor acknowledgement is not the final draft content: %q", got)
	}
	for _, field := range []string{"{{ .Name }}", "{{ .DashboardLink }}"} {
		if !strings.Contains(got, field) {
			t.Fatalf("recipient field %s was materialized too early: %q", field, got)
		}
	}
}

func TestConferenceCampaignHasSponsorFooterForAttendeeAndSpeakerReminders(t *testing.T) {
	for _, kind := range []string{
		types.ConferenceCampaignAttendeeReminder70,
		types.ConferenceCampaignAttendeeReminder49,
		types.ConferenceCampaignAttendeeReminder28,
		types.ConferenceCampaignAttendeeFinal,
		types.ConferenceCampaignSpeakerReminder,
	} {
		if !conferenceCampaignHasSponsorFooter(kind) {
			t.Errorf("conferenceCampaignHasSponsorFooter(%q) = false", kind)
		}
	}
	if conferenceCampaignHasSponsorFooter(types.ConferenceCampaignVolunteerOrient) {
		t.Fatal("volunteer orientation unexpectedly received the reminder sponsor footer")
	}
}

func TestConferenceEmailMarkdownWithHeroReplacesExistingEventImage(t *testing.T) {
	markdown := "---\ntemplate: announce\nhero: \"https://old.example/image.png\"\npalette: ember\n---\n\nBody."
	hero := "https://cdn.example/talks/dev26-latest.png"
	got := conferenceEmailMarkdownWithHero(markdown, hero)
	if strings.Count(got, "hero:") != 1 || !strings.Contains(got, `hero: "`+hero+`"`) {
		t.Fatalf("conferenceEmailMarkdownWithHero() did not replace hero metadata:\n%s", got)
	}
	if !strings.HasSuffix(got, "Body.") {
		t.Fatalf("conferenceEmailMarkdownWithHero() changed the email body:\n%s", got)
	}
}

func TestConferenceEmailMarkdownWithHeroAddsFrontmatterWhenMissing(t *testing.T) {
	got := conferenceEmailMarkdownWithHero("Body.", "https://btcpp.dev/static/img/dev26/leading.png")
	if !strings.HasPrefix(got, "---\nhero: \"https://btcpp.dev/static/img/dev26/leading.png\"\n---\n\nBody.") {
		t.Fatalf("conferenceEmailMarkdownWithHero() = %q", got)
	}
}

func TestConferenceSpeakerListMarkdownCapsAtSixAndIncludesTalksAndLinks(t *testing.T) {
	speakers := types.Speakers{
		{ID: "featured-1", Name: "Featured One", Company: "Anchor Labs", Website: "anchor.example", Twitter: types.Twitter{Handle: "anchor"}, Github: "anchor-labs"},
		{ID: "featured-2", Name: "Featured Two", Nostr: "npub1featured"},
		{ID: "speaker-3", Name: "Speaker Three"},
		{ID: "speaker-4", Name: "Speaker Four"},
		{ID: "speaker-5", Name: "Speaker Five"},
		{ID: "speaker-6", Name: "Speaker Six"},
		{ID: "speaker-7", Name: "Speaker Seven"},
	}
	talks := map[string][]string{
		"featured-1": {"Building Better Wallets", "Privacy by Default"},
	}
	got := conferenceSpeakerListMarkdown("https://btcpp.dev/", &types.Conf{
		Tag: "seoul", Desc: "bitcoin++ Seoul, privacy edition",
	}, speakers, talks)
	for _, want := range []string{
		"We've got 7 speakers coming to bitcoin++ Seoul, privacy edition.",
		"[Find the whole list of speakers on the website →](https://btcpp.dev/seoul#speakers)",
		"**Featured One** — Anchor Labs — Talks: *Building Better Wallets*; *Privacy by Default*",
		"[website](https://anchor.example)",
		"[x.com](https://x.com/anchor)",
		"[github](https://github.com/anchor-labs)",
		"[nostr](https://njump.me/npub1featured)",
		"**Speaker Six**",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("speaker Markdown does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Speaker Seven") {
		t.Fatalf("speaker Markdown exceeded the six-person cap:\n%s", got)
	}
	if strings.Index(got, "Featured One") > strings.Index(got, "Featured Two") {
		t.Fatalf("speaker Markdown did not retain featured ordering:\n%s", got)
	}
}

func TestConferenceSpeakerListMarkdownAlwaysLinksFullList(t *testing.T) {
	got := conferenceSpeakerListMarkdown("https://btcpp.dev", &types.Conf{
		Tag: "dev26", Desc: "Local Dev",
	}, types.Speakers{{ID: "one", Name: "Only Speaker"}}, nil)
	if !strings.Contains(got, "We've got 1 speaker coming to Local Dev.") || !strings.Contains(got, "https://btcpp.dev/dev26#speakers") {
		t.Fatalf("single-speaker Markdown is missing the count or full-list link:\n%s", got)
	}
}

func TestOrderSpeakersByAcceptedAtPutsNewestConfirmedFirst(t *testing.T) {
	oldest := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)
	newest := oldest.Add(48 * time.Hour)
	speakers := types.Speakers{
		{ID: "legacy", Name: "Ada"},
		{ID: "newest", Name: "Satoshi"},
		{ID: "oldest", Name: "Linus"},
	}
	got := orderSpeakersByAcceptedAt(speakers, map[string]*time.Time{
		"newest": &newest,
		"oldest": &oldest,
	})
	if got[0].ID != "newest" || got[1].ID != "oldest" || got[2].ID != "legacy" {
		t.Fatalf("speaker order = %s, %s, %s; want newest, oldest, legacy", got[0].ID, got[1].ID, got[2].ID)
	}
	if speakers[0].ID != "legacy" {
		t.Fatal("orderSpeakersByAcceptedAt mutated the source speaker list")
	}
}

func TestConferenceSpeakerTalkMarkdownIncludesKnownPublicDetails(t *testing.T) {
	loc := time.FixedZone("CDT", -5*60*60)
	start := time.Date(2026, time.August, 14, 15, 0, 0, 0, loc)
	end := start.Add(45 * time.Minute)
	got := conferenceSpeakerTalkMarkdown(
		&types.Conf{Timezone: "America/Chicago", TZ: loc},
		&types.Proposal{Title: "Building Better Wallets", Description: "A practical tour of wallet architecture.", TalkType: "main-stage-panel"},
		&types.ConfTalk{Sched: &types.Times{Start: start, End: &end}, Venue: "one", Section: "Wallets", SlidesURL: "https://slides.example/wallets", GithubRepoURL: "https://github.com/example/wallets"},
		"speaker-current",
		[]getters.ProposalSpeakerSummary{{SpeakerConfID: "speaker-current", Name: "Ada"}, {SpeakerConfID: "speaker-two", Name: "Linus"}, {SpeakerConfID: "speaker-three", Name: "Satoshi"}},
	)
	for _, want := range []string{
		"#### Building Better Wallets",
		"A practical tour of wallet architecture.",
		"**Type:** Main Stage Panel",
		"**When:** Friday, August 14 at 3:00 PM CDT–3:45 PM CDT",
		"**Stage:** Main Stage",
		"**Track:** Wallets",
		"**Other speakers:** Linus and Satoshi",
		"[View slides](https://slides.example/wallets)",
		"[View repository](https://github.com/example/wallets)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("talk details do not contain %q:\n%s", want, got)
		}
	}
}

func TestConferenceSpeakerTalkMarkdownShowsMissingScheduleAndResources(t *testing.T) {
	got := conferenceSpeakerTalkMarkdown(nil, &types.Proposal{Title: "Pending session", TalkType: "talk"}, nil, "speaker", nil)
	for _, want := range []string{"**When:** Not scheduled yet", "**Slides:** Not added yet", "**GitHub repository:** Not added yet"} {
		if !strings.Contains(got, want) {
			t.Errorf("talk details do not contain %q:\n%s", want, got)
		}
	}
}

func TestConferenceEmailSponsorNamesOrdersByTierThenName(t *testing.T) {
	sponsorships := []*types.Sponsorship{
		{Level: "Gold", Status: "paid", Org: &types.Org{Name: "Alpha", Website: "https://alpha.example"}},
		{Level: "Headline Sponsors", Status: "committed", Org: &types.Org{Name: "Zebra", Website: "https://zebra.example"}},
		{Level: "Title", Status: "paid", Org: &types.Org{Name: "Center", Website: "https://center.example"}},
		{Level: "Headline", Status: "paid", Org: &types.Org{Name: "Beta", Website: "https://beta.example"}},
		{Level: "Headline", Status: "pending", Org: &types.Org{Name: "Hidden"}},
		{Level: "Gold", Status: "paid", Org: &types.Org{Name: "Beta"}},
	}

	got := conferenceEmailSponsorNames(sponsorships)
	want := []string{
		"[Beta](https://beta.example)",
		"[Zebra](https://zebra.example)",
		"[Center](https://center.example)",
		"[Alpha](https://alpha.example)",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("conferenceEmailSponsorNames() = %v, want %v", got, want)
	}
	if sentence := "Big thank you to our sponsors: " + humanList(got) + "."; sentence != "Big thank you to our sponsors: [Beta](https://beta.example), [Zebra](https://zebra.example), [Center](https://center.example), and [Alpha](https://alpha.example)." {
		t.Fatalf("sponsor sentence = %q", sentence)
	}
}

func TestConferenceEmailSponsorNamesLeavesMissingWebsiteAsText(t *testing.T) {
	got := conferenceEmailSponsorNames([]*types.Sponsorship{
		{Level: "Headline", Status: "paid", Org: &types.Org{Name: "No Site"}},
	})
	if len(got) != 1 || got[0] != "No Site" {
		t.Fatalf("conferenceEmailSponsorNames() = %v, want plain-text fallback", got)
	}
}

func TestHumanListHandlesOneAndTwoSponsors(t *testing.T) {
	if got := humanList([]string{"Solo"}); got != "Solo" {
		t.Fatalf("humanList(one) = %q", got)
	}
	if got := humanList([]string{"Alpha", "Beta"}); got != "Alpha and Beta" {
		t.Fatalf("humanList(two) = %q", got)
	}
}

func TestConferenceHackathonPrizeSummaryOmitsUnfundedPrizes(t *testing.T) {
	got := conferenceHackathonPrizeSummary([]*types.Prize{
		{PrizeType: getters.PrizeTypeSats, ValueText: "2,500,000", Status: getters.PrizeStatusAvailable},
		{PrizeType: getters.PrizeTypeInKind, Title: "Hardware wallet", Status: getters.PrizeStatusAwarded},
		{PrizeType: getters.PrizeTypeInKind, Title: "Unfunded trip", Status: getters.PrizeStatusNeedsFunds},
	})
	want := "2.5M satoshis plus Hardware wallet"
	if got != want {
		t.Fatalf("conferenceHackathonPrizeSummary() = %q, want %q", got, want)
	}
}

func TestConferenceSatelliteEventsListMarkdownIncludesRequiredDetailsAndFallbackLink(t *testing.T) {
	loc := time.FixedZone("conference", -5*60*60)
	start := time.Date(2026, time.August, 13, 18, 30, 0, 0, loc)
	end := start.Add(90 * time.Minute)
	conf := &types.Conf{Tag: "dev26", TZ: loc}
	events := []*types.SatelliteEvent{
		{
			Title:       "Builder Meetup",
			Description: "Meet local builders and compare notes.",
			StartsAt:    &start,
			EndsAt:      &end,
			EventURL:    "https://example.com/builder-meetup",
		},
		{
			Title:       "After Hours",
			Description: "Informal drinks.",
			StartsAt:    &end,
		},
	}

	got := conferenceSatelliteEventsListMarkdown("https://btcpp.dev/", conf, events)
	for _, want := range []string{
		"### Satellite events",
		"**Builder Meetup** — Thu Aug 13 · 6:30 PM conference - 8:00 PM conference — Meet local builders and compare notes. — [More info](https://example.com/builder-meetup)",
		"**After Hours** — Thu Aug 13 · 8:00 PM conference — Informal drinks. — [More info](https://btcpp.dev/dev26#satellites)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("satellite Markdown does not contain %q:\n%s", want, got)
		}
	}
}

func TestConferenceGeneratedMissiveOccurrencesHidesUnbuiltScheduleRows(t *testing.T) {
	draft := &types.ConferenceEmailOccurrence{ID: "draft", MissiveID: "missive-id", MissiveUID: 42}
	sent := &types.ConferenceEmailOccurrence{ID: "sent", MissiveID: "sent-missive-id", Status: "sent"}
	got := conferenceGeneratedMissiveOccurrences([]*types.ConferenceEmailOccurrence{
		{ID: "planned", Status: "planned"},
		draft,
		nil,
		sent,
	})

	if len(got) != 2 || got[0] != draft || got[1] != sent {
		t.Fatalf("conferenceGeneratedMissiveOccurrences() = %#v, want generated draft and sent missive", got)
	}
}
