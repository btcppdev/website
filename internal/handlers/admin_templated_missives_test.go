package handlers

import (
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
)

func TestFormFromTemplatedLetterHydratesShortcodes(t *testing.T) {
	letter := &mtypes.Letter{
		UID:         42,
		PageID:      "page-id",
		Title:       "Vienna dispatch",
		SendAt:      "6/12/2026",
		Newsletters: []string{"newsletter", "vienna"},
		Markdown: `---
template: "announce"
palette: "signal"
issue: "No. 9"
hero: "https://example.com/hero.jpg"
ticker:
  - Vienna | June 2026
  - Tickets | live
---

{{ lead "§ VIENNA" "Bitcoin++ Vienna" "A compact systems conference." }}

{{ stats "20 | talks + panels" "2 | days" "21M | coins" }}

{{ newsList "Talks | Deep protocol work | PROGRAM | https://example.com/program" "Tickets | Join us in Vienna | TICKETS | https://example.com/tickets" }}

Some freeform body copy.

{{ button "Read the full issue" "https://example.com/weekly?from=email&issue=9" }}

{{ pullquote "Bring your weirdest debugging story." "The organizers" }}

{{ cta "JOIN US" "Get a ticket" "Seats are limited." "BUY TICKETS" "https://example.com/tickets" }}
`,
	}

	form := formFromTemplatedLetter(letter)

	if form.Template != "announce" {
		t.Fatalf("Template = %q, want announce", form.Template)
	}
	if form.Palette != "signal" {
		t.Fatalf("Palette = %q, want signal", form.Palette)
	}
	if form.Issue != "No. 9" {
		t.Fatalf("Issue = %q, want No. 9", form.Issue)
	}
	if form.Hero != "https://example.com/hero.jpg" {
		t.Fatalf("Hero = %q", form.Hero)
	}
	if form.Ticker != "Vienna | June 2026\nTickets | live" {
		t.Fatalf("Ticker = %q", form.Ticker)
	}
	if form.LeadEyebrow != "§ VIENNA" || form.LeadTitle != "Bitcoin++ Vienna" || form.LeadDeck != "A compact systems conference." {
		t.Fatalf("lead fields not hydrated: %#v", form)
	}
	if form.Stats != "20 | talks + panels\n2 | days\n21M | coins" {
		t.Fatalf("Stats = %q", form.Stats)
	}
	if !strings.Contains(form.NewsItems, "Talks | Deep protocol work | PROGRAM | https://example.com/program") {
		t.Fatalf("NewsItems = %q", form.NewsItems)
	}
	if form.Pullquote != "Bring your weirdest debugging story." || form.PullquoteBy != "The organizers" {
		t.Fatalf("pullquote fields not hydrated: %#v", form)
	}
	if form.CTAEyebrow != "JOIN US" || form.CTATitle != "Get a ticket" || form.CTASubtitle != "Seats are limited." || form.CTALabel != "BUY TICKETS" || form.CTAURL != "https://example.com/tickets" {
		t.Fatalf("CTA fields not hydrated: %#v", form)
	}
	if form.ContentMarkdown != "Some freeform body copy.\n\n{{ button \"Read the full issue\" \"https://example.com/weekly?from=email&issue=9\" }}" {
		t.Fatalf("ContentMarkdown = %q", form.ContentMarkdown)
	}
	if strings.Contains(form.ContentMarkdown, "{{ stats") {
		t.Fatalf("ContentMarkdown still contains shortcode: %q", form.ContentMarkdown)
	}
}

func TestParseTemplatedShortcodeLineHandlesEscapes(t *testing.T) {
	name, args, ok := parseTemplatedShortcodeLine(`{{ pullquote "He said \"ship it\"" "Ops" }}`)
	if !ok {
		t.Fatal("parseTemplatedShortcodeLine did not parse shortcode")
	}
	if name != "pullquote" {
		t.Fatalf("name = %q, want pullquote", name)
	}
	if len(args) != 2 || args[0] != `He said "ship it"` || args[1] != "Ops" {
		t.Fatalf("args = %#v", args)
	}
}

func TestTemplatedMissiveTestLetterUsesCurrentFormWithoutSchedulingState(t *testing.T) {
	form := TemplatedMissiveForm{
		Title:       "Draft newsletter",
		SendAt:      "6/12/2026",
		Newsletters: "vienna, !newsletter",
		Template:    "announce",
		Palette:     "ember",
		LeadTitle:   "Current editor content",
		TestEmail:   "test@example.com",
	}

	letter := templatedMissiveTestLetter(form)
	if letter.Title != "[TEST] Draft newsletter" {
		t.Fatalf("Title = %q", letter.Title)
	}
	if letter.SendAt != "now" {
		t.Fatalf("SendAt = %q, want now", letter.SendAt)
	}
	if strings.Contains(letter.Markdown, `date:`) {
		t.Fatalf("test markdown should not include date frontmatter: %q", letter.Markdown)
	}
	if letter.OnlyFor != mtypes.OnlyForTemplated {
		t.Fatalf("OnlyFor = %q", letter.OnlyFor)
	}
	if !strings.Contains(letter.Markdown, "Current editor content") {
		t.Fatalf("Markdown did not use current form content: %q", letter.Markdown)
	}

	sub := subscriberForTemplatedMissiveTest("test@example.com", letter)
	if sub.Email != "test@example.com" {
		t.Fatalf("subscriber email = %q", sub.Email)
	}
	if got := strings.Join(sub.SubNames(), ","); got != "vienna" {
		t.Fatalf("subscriber lists = %q, want vienna", got)
	}
}

func TestBuildTemplatedMissiveMarkdownDoesNotWriteDateFrontmatter(t *testing.T) {
	markdown := buildTemplatedMissiveMarkdown(TemplatedMissiveForm{
		Title:           "No date",
		SendAt:          "5/25/2026",
		Newsletters:     "newsletter",
		Template:        "roundup",
		ContentMarkdown: "Body.",
	})
	if strings.Contains(markdown, "\ndate:") {
		t.Fatalf("templated missive markdown should not include date frontmatter: %q", markdown)
	}
}

func TestBuildTemplatedMissiveMarkdownPreservesInlineButtonShortcode(t *testing.T) {
	const button = `{{ button "Read the full issue" "https://example.com/weekly?from=email&issue=9" }}`
	markdown := buildTemplatedMissiveMarkdown(TemplatedMissiveForm{
		Template:        "roundup",
		ContentMarkdown: "Latest reporting.\n\n" + button,
	})
	if !strings.Contains(markdown, button) {
		t.Fatalf("templated missive markdown lost inline button: %q", markdown)
	}

	letter := &mtypes.Letter{Markdown: markdown}
	form := formFromTemplatedLetter(letter)
	if !strings.Contains(form.ContentMarkdown, button) {
		t.Fatalf("inline button was not restored to the body editor: %q", form.ContentMarkdown)
	}
}

func TestNextWeeklyNewsletterSendAt(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "monday schedules tomorrow",
			now:  time.Date(2026, time.August, 10, 14, 0, 0, 0, chicago),
			want: time.Date(2026, time.August, 11, 10, 0, 0, 0, chicago),
		},
		{
			name: "tuesday before ten schedules today",
			now:  time.Date(2026, time.January, 13, 9, 59, 0, 0, chicago),
			want: time.Date(2026, time.January, 13, 10, 0, 0, 0, chicago),
		},
		{
			name: "tuesday at ten schedules next week",
			now:  time.Date(2026, time.January, 13, 10, 0, 0, 0, chicago),
			want: time.Date(2026, time.January, 20, 10, 0, 0, 0, chicago),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextWeeklyNewsletterSendAt(tt.now)
			if !got.Equal(tt.want) || got.Location().String() != "America/Chicago" {
				t.Fatalf("nextWeeklyNewsletterSendAt(%s) = %s, want %s", tt.now, got, tt.want)
			}
		})
	}
}

func TestPrepareTemplatedMissiveIndexFiltersAndOrdersBySentDate(t *testing.T) {
	older := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)
	letters := []*mtypes.Letter{
		{UID: 1, Title: "Older newsletter", Newsletters: []string{"newsletter"}, OnlyFor: mtypes.OnlyForTemplated, SentAt: &older},
		{UID: 4, Title: "Newest draft", Newsletters: []string{"newsletter", "insider"}, OnlyFor: mtypes.OnlyForTemplated},
		{UID: 2, Title: "Newer newsletter", Newsletters: []string{"newsletter"}, OnlyFor: mtypes.OnlyForTemplated, SentAt: &newer},
		{UID: 3, Title: "Insider draft", Newsletters: []string{"insider"}, OnlyFor: mtypes.OnlyForTemplated},
	}

	visible, options, counts := prepareTemplatedMissiveIndex(letters, "newsletter", missiveViewSent)
	if got, want := strings.Join(options, ","), "insider,newsletter"; got != want {
		t.Fatalf("newsletter options = %q, want %q", got, want)
	}
	if counts.Unsent != 1 || counts.SentScheduled != 2 || counts.OneShots != 0 {
		t.Fatalf("tab counts = %#v, want 0 one-shots, 1 unsent, 2 sent/scheduled", counts)
	}
	if len(visible) != 2 {
		t.Fatalf("visible missives = %d, want 2", len(visible))
	}
	if got := []uint64{visible[0].UID, visible[1].UID}; got[0] != 2 || got[1] != 1 {
		t.Fatalf("ordered UIDs = %v, want [2 1]", got)
	}
	if letters[0].UID != 1 || letters[1].UID != 4 {
		t.Fatalf("index preparation mutated source order: %#v", letters)
	}
}

func TestPrepareTemplatedMissiveIndexSeparatesOneShotsDraftsAndScheduled(t *testing.T) {
	sentAt := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	letters := []*mtypes.Letter{
		{UID: 1, OnlyFor: "volapp", Title: "Volunteer application"},
		{UID: 2, OnlyFor: mtypes.OnlyForTemplated, Title: "Blank draft"},
		{UID: 3, OnlyFor: mtypes.OnlyForTemplated, Title: "Immediate draft", SendAt: "now"},
		{UID: 4, OnlyFor: mtypes.OnlyForTemplated, Title: "Scheduled issue", SendAt: "2026-08-11T10:00:00-05:00"},
		{UID: 5, OnlyFor: mtypes.OnlyForTemplated, Title: "Sent issue", SentAt: &sentAt},
	}

	ones, _, counts := prepareTemplatedMissiveIndex(letters, "", missiveViewOneShots)
	if len(ones) != 1 || ones[0].UID != 1 {
		t.Fatalf("one-shot tab = %#v, want UID 1", ones)
	}
	if counts.OneShots != 1 || counts.Unsent != 2 || counts.SentScheduled != 2 {
		t.Fatalf("tab counts = %#v, want 1/2/2", counts)
	}

	unsent, _, _ := prepareTemplatedMissiveIndex(letters, "", "invalid-view")
	if got := []uint64{unsent[0].UID, unsent[1].UID}; got[0] != 3 || got[1] != 2 {
		t.Fatalf("default unsent tab UIDs = %v, want [3 2]", got)
	}

	sent, _, _ := prepareTemplatedMissiveIndex(letters, "", missiveViewSent)
	if got := []uint64{sent[0].UID, sent[1].UID}; got[0] != 4 || got[1] != 5 {
		t.Fatalf("sent/scheduled tab UIDs = %v, want [4 5]", got)
	}

	labels := oneShotMissiveLabels()
	if labels["volapp"] != "Volunteer application received" || labels["ticket"] != "Ticket receipt" ||
		labels["conference-attendee-final"] != "Event final details and tickets" {
		t.Fatalf("one-shot labels missing expected names: %#v", labels)
	}
}

func TestWeeklyNewsletterFormPrefillsEditorialStructureAndUpcomingConference(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	sendAt := time.Date(2026, time.August, 11, 10, 0, 0, 0, chicago)
	conf := &types.Conf{
		Tag:               "berlin",
		Active:            true,
		PublicationStatus: "published",
		Desc:              "bitcoin++ Berlin",
		DateDesc:          "October 1–3, 2026",
		Location:          "Berlin",
		StartDate:         time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC),
	}

	updates := &getters.WeeklyNewsletterUpdateBundle{
		TalkOfWeek: &getters.WeeklyNewsletterTalk{
			ConfTag: "dev26", ConfTitle: "bitcoin++ Local Dev 2026", TalkID: "featured-panel",
			Title: "The Closing Panel", Description: "A panel about where Bitcoin development goes next.",
			SpeakerNames: "Ada, Linus", YouTubeURL: "https://youtu.be/closing-panel",
		},
		SpeakerGroups: []getters.WeeklyNewsletterSpeakerGroup{{
			ConfTag: "berlin", ConfTitle: "bitcoin++ Berlin", Speakers: []getters.WeeklyNewsletterSpeaker{{Name: "Ada"}, {Name: "Linus"}},
		}},
		SupportingSponsors: []getters.WeeklyNewsletterSponsor{
			{Name: "Anchor Labs", URL: "https://anchor.example", Level: "Headline"},
			{Name: "Node House", Level: "Gold"},
		},
		MerchUpdates: []getters.WeeklyNewsletterMerchUpdate{
			{Slug: "node-runner-tee", Name: "Node Runner Tee", Kind: "added"},
			{Slug: "core-hat", Name: "Core Hat", Kind: "restocked"},
		},
	}
	insider := &getters.InsiderWeeklyIssue{Link: "https://insider.btcpp.dev/p/weekly", Bullets: []getters.InsiderWeeklyBullet{{Text: "One", Link: "https://example.com/one"}, {Text: "Two"}, {Text: "Three"}}}
	form := weeklyNewsletterForm(sendAt, conf, updates, insider)
	if form.SendAt != "2026-08-11T10:00:00-05:00" {
		t.Fatalf("SendAt = %q", form.SendAt)
	}
	if form.LeadEyebrow != "" {
		t.Fatalf("LeadEyebrow = %q", form.LeadEyebrow)
	}
	if form.LeadTitle != "what's new in the bitcoin++ universe" {
		t.Fatalf("LeadTitle = %q", form.LeadTitle)
	}
	if form.Ticker != "BITCOIN++ WEEKLY\nTALKS RELEASED\nSPEAKERS ADDED\nEVENTS\nMERCHANDISE" {
		t.Fatalf("Ticker = %q", form.Ticker)
	}
	if form.CTAEyebrow != "subscriber offer" || form.CTATitle != "Join us in Berlin" || form.CTASubtitle != "Since you're a bitcoin++ subscriber, use this 20% discount code: **SUBSCRIBER20**" || form.CTALabel != "Get your ticket" || form.CTAURL != "{{ .URI }}/berlin?code=SUBSCRIBER20#tickets" {
		t.Fatalf("weekly newsletter subscriber CTA = %#v", form)
	}
	if !strings.Contains(form.ContentMarkdown, "New speakers confirmed for [bitcoin++ Berlin]({{ .URI }}/berlin/talks):\n    - Ada.\n    - Linus.") {
		t.Fatalf("ContentMarkdown did not prefill database updates: %q", form.ContentMarkdown)
	}
	for _, want := range []string{"### § What's Happening at bitcoin++", "### § Last week in Bitcoin", "### § Talk of the week"} {
		if !strings.Contains(form.ContentMarkdown, want) {
			t.Errorf("ContentMarkdown does not contain %q", want)
		}
	}
	for _, want := range []string{
		"[The Closing Panel](https://youtu.be/closing-panel) by Ada, Linus · bitcoin++ Local Dev 2026.",
		"A panel about where Bitcoin development goes next.",
		"New merch: [Node Runner Tee]({{ .URI }}/shop/node-runner-tee) is now available in the bitcoin++ shop.",
		"Back in stock: [Core Hat]({{ .URI }}/shop/core-hat) is available again in the bitcoin++ shop.",
	} {
		if !strings.Contains(form.ContentMarkdown, want) {
			t.Errorf("ContentMarkdown does not contain generated Talk of the Week copy %q", want)
		}
	}
	if strings.Contains(form.ContentMarkdown, "[Choose a past bitcoin++ talk") {
		t.Errorf("ContentMarkdown retained Talk of the Week placeholder: %q", form.ContentMarkdown)
	}
	thanks := "We'd like to thank [Anchor Labs](https://anchor.example) and Node House for their support in making bitcoin++ possible."
	if !strings.Contains(form.ContentMarkdown, thanks) {
		t.Errorf("ContentMarkdown does not contain sponsor thanks: %q", form.ContentMarkdown)
	}
	updatesHeading := "### § What's Happening at bitcoin++"
	if strings.Index(form.ContentMarkdown, thanks) > strings.Index(form.ContentMarkdown, updatesHeading) {
		t.Errorf("sponsor thanks should appear above What's Happening: %q", form.ContentMarkdown)
	}
	updatesIntro := "*Nothing ever happens, except at the frontier of bitcoin. Here's what's new in the bitcoin++ universe.*"
	if !strings.Contains(form.ContentMarkdown, updatesHeading+"\n\n"+updatesIntro) {
		t.Errorf("ContentMarkdown does not contain the What's Happening introduction: %q", form.ContentMarkdown)
	}
	if strings.Contains(form.ContentMarkdown, "[DISCOUNT CODE]") {
		t.Errorf("ContentMarkdown still contains the discount placeholder: %q", form.ContentMarkdown)
	}
	if strings.Contains(form.ContentMarkdown, "See who's speaking") || strings.Contains(form.ContentMarkdown, "#agenda") {
		t.Errorf("ContentMarkdown still contains the agenda link: %q", form.ContentMarkdown)
	}
	if strings.Contains(form.ContentMarkdown, "Get your ticket") || strings.Contains(form.ContentMarkdown, "SUBSCRIBER20") {
		t.Errorf("ticket CTA copy remains duplicated in Markdown: %q", form.ContentMarkdown)
	}
	markdown := buildTemplatedMissiveMarkdown(form)
	if strings.Contains(markdown, "{{ button ") {
		t.Errorf("weekly newsletter contains retired inline button formatting: %q", markdown)
	}
	if strings.Contains(markdown, "§ TOP OF THE LINE") || strings.Contains(markdown, "§ FEATURE") {
		t.Errorf("weekly newsletter still contains a roundup label: %q", markdown)
	}
	for _, want := range []string{`{{ cta "subscriber offer" "Join us in Berlin" "Since you're a bitcoin++ subscriber, use this 20% discount code: **SUBSCRIBER20**" "Get your ticket"`, `(print .URI "/berlin?code=SUBSCRIBER20#tickets")`} {
		if !strings.Contains(markdown, want) {
			t.Errorf("weekly newsletter CTA missing %q: %q", want, markdown)
		}
	}
	if weeklyNewsletterDedupeKey(sendAt) != "weekly-newsletter:2026-08-11" {
		t.Fatalf("dedupe key = %q", weeklyNewsletterDedupeKey(sendAt))
	}
}

func TestNextNewsletterConferenceSelectsNearestActivePublishedEvent(t *testing.T) {
	after := time.Date(2026, time.August, 11, 10, 0, 0, 0, time.UTC)
	confs := []*types.Conf{
		{Tag: "past", Active: true, PublicationStatus: "published", StartDate: after.Add(-time.Hour)},
		{Tag: "draft", Active: true, PublicationStatus: "draft", StartDate: after.Add(24 * time.Hour)},
		{Tag: "later", Active: true, PublicationStatus: "published", StartDate: after.Add(72 * time.Hour)},
		{Tag: "next", Active: true, PublicationStatus: "published", StartDate: after.Add(48 * time.Hour)},
	}
	if got := nextNewsletterConference(confs, after); got == nil || got.Tag != "next" {
		t.Fatalf("nextNewsletterConference = %#v, want next", got)
	}
}

func TestWeeklyNewsletterUpdatesMarkdownIncludesDatabaseItems(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	updates := &getters.WeeklyNewsletterUpdateBundle{
		Talks: []getters.WeeklyNewsletterTalk{{
			ConfTag: "nairobi", Title: "CTV & Covenants", SpeakerNames: "Ada", YouTubeURL: "https://youtu.be/talk", PublishAt: time.Date(2026, time.August, 12, 10, 0, 0, 0, chicago),
		}},
		SpeakerGroups: []getters.WeeklyNewsletterSpeakerGroup{{
			ConfTag: "berlin", ConfTitle: "bitcoin++ Berlin", Speakers: []getters.WeeklyNewsletterSpeaker{{Name: "Linus", Company: "Kernel Labs", XURL: "https://x.com/linus", NostrURL: "https://njump.me/npub1linus", WebsiteURL: "https://linus.example"}, {Name: "Satoshi"}},
		}},
		NewSponsorGroups: []getters.WeeklyNewsletterSponsorGroup{{
			ConfTag: "berlin", ConfTitle: "bitcoin++ Berlin",
			Sponsors: []getters.WeeklyNewsletterSponsor{{Name: "Bitco", URL: "https://bitco.example", Level: "Gold Sponsors"}},
		}},
		TicketChanges: []getters.WeeklyNewsletterTicketChange{{
			Conf:    &types.Conf{Tag: "berlin", Desc: "bitcoin++ Berlin"},
			Current: &types.ConfTicket{BasePrice: 75, Symbol: "$", SalesEndAt: time.Date(2026, time.August, 15, 0, 0, 0, 0, chicago)},
			Next:    &types.ConfTicket{BasePrice: 120, Symbol: "$"},
		}},
		HackathonWinners: []getters.WeeklyNewsletterHackathonWinner{{
			ConfTag: "berlin", Competition: "Protocol Hackathon", ProjectID: "project-1", ProjectTitle: "BoltBoard", Awards: "Best Overall",
		}},
	}
	markdown := weeklyNewsletterUpdatesMarkdown(updates)
	for _, want := range []string{
		"New speakers confirmed for [bitcoin++ Berlin]({{ .URI }}/berlin/talks):\n    - Linus of Kernel Labs. [x.com](https://x.com/linus) [nostr](https://njump.me/npub1linus) [website](https://linus.example)\n    - Satoshi.",
		"New talk: [CTV & Covenants](https://youtu.be/talk) by Ada",
		"New sponsors for [bitcoin++ Berlin]({{ .URI }}/berlin):\n    - [Bitco](https://bitco.example) — Gold sponsor",
		"rise from $75 to $120 on Saturday, August 15. Get them before they're gone.",
		"[BoltBoard]({{ .URI }}/berlin/hackathon/projects/project-1) won Best Overall",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("weekly markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "Publishing ") {
		t.Errorf("weekly markdown advertises a future publication:\n%s", markdown)
	}
	if strings.Contains(markdown, "or when the current tier sells out") {
		t.Errorf("weekly markdown contains the retired ticket copy:\n%s", markdown)
	}
}

func TestNewsletterEnglishList(t *testing.T) {
	tests := []struct {
		items []string
		want  string
	}{
		{[]string{"One"}, "One"},
		{[]string{"One", "Two"}, "One and Two"},
		{[]string{"One", "Two", "Three"}, "One, Two, and Three"},
	}
	for _, tt := range tests {
		if got := newsletterEnglishList(tt.items); got != tt.want {
			t.Errorf("newsletterEnglishList(%v) = %q, want %q", tt.items, got, tt.want)
		}
	}
}

func TestOnlyForTemplateFieldsDescribeTriggeredEmailContexts(t *testing.T) {
	contains := func(groups []EmailFieldGroup, want string) bool {
		for _, group := range groups {
			for _, item := range group.Items {
				if item == want {
					return true
				}
			}
		}
		return false
	}
	for _, tc := range []struct {
		onlyFor string
		fields  []string
	}{
		{"volapp", []string{".Name", ".Volunteer.Name", ".Conf.Desc", ".VolInfo.OrientLink"}},
		{"talkconfirmed", []string{".TalkConfirmLink", ".Proposal.Title", ".Speaker.Name", ".Conf.Tag"}},
		{"ticket", []string{".DayCount", ".DashboardLink", ".Conf.DoorsOpen"}},
		{"vollogin", []string{".Email", ".VolShiftLink", ".URI"}},
		{"conference-speaker-onboarding", []string{".SpeakerDinnerTime", ".GeneratedUpdates", ".Conf.Venue"}},
	} {
		groups := onlyForTemplateFields(tc.onlyFor)
		for _, field := range tc.fields {
			if !contains(groups, field) {
				t.Errorf("%s field catalog missing %s", tc.onlyFor, field)
			}
		}
	}
}

func TestWeeklyNewsletterFormOmitsEmptyUpdatesSection(t *testing.T) {
	sendAt := time.Date(2026, time.August, 11, 10, 0, 0, 0, time.FixedZone("CDT", -5*60*60))
	form := weeklyNewsletterForm(sendAt, nil, &getters.WeeklyNewsletterUpdateBundle{}, nil)
	if strings.Contains(form.ContentMarkdown, "What's Happening at bitcoin++") {
		t.Fatalf("empty database updates should not produce a section: %q", form.ContentMarkdown)
	}
	if strings.Contains(form.ContentMarkdown, "Last week in Bitcoin") {
		t.Fatalf("missing Monday issue should omit Insider section: %q", form.ContentMarkdown)
	}
	if !strings.Contains(form.ContentMarkdown, "Talk of the week") {
		t.Fatalf("talk-of-the-week editor was lost: %q", form.ContentMarkdown)
	}
}

func TestWeeklyNewsletterBroadcastsListsThreeMostRecentPublishedTalks(t *testing.T) {
	published := time.Date(2026, time.August, 10, 12, 0, 0, 0, weeklyNewsletterCentralLocation())
	updates := &getters.WeeklyNewsletterUpdateBundle{
		Broadcasts: []getters.WeeklyNewsletterTalk{
			{Title: "One", YouTubeURL: "https://youtu.be/one", SpeakerNames: "Alice", PublishAt: published},
			{Title: "Two", YouTubeURL: "https://youtu.be/two", PublishAt: published},
			{Title: "Three", YouTubeURL: "https://youtu.be/three", PublishAt: published},
			{Title: "Four", YouTubeURL: "https://youtu.be/four", SpeakerNames: "Bob", PublishAt: published},
		},
	}
	markdown := weeklyNewsletterBroadcastsMarkdown(updates)
	if !strings.HasPrefix(markdown, "### § bitcoin++ broadcasts\n\n*New talks were posted. Check out the latest on our [YouTube](https://www.youtube.com/@btcplusplus/videos).*\n\n") {
		t.Fatalf("broadcast heading missing: %s", markdown)
	}
	if strings.Contains(markdown, "[One]") {
		t.Errorf("oldest broadcast should be omitted: %s", markdown)
	}
	for _, title := range []string{"Two", "Three", "Four"} {
		if !strings.Contains(markdown, "["+title+"]") {
			t.Errorf("broadcast list missing %s: %s", title, markdown)
		}
	}
	if strings.Count(markdown, "Published Monday, August 10.") != 3 {
		t.Fatalf("broadcast publication dates missing: %s", markdown)
	}
}

func TestWeeklyNewsletterUpdatesCapsSpeakersAndHackathonItems(t *testing.T) {
	updates := &getters.WeeklyNewsletterUpdateBundle{
		SpeakerGroups: []getters.WeeklyNewsletterSpeakerGroup{{
			ConfTag: "dev26", ConfTitle: "bitcoin++ Local Dev",
			Speakers: []getters.WeeklyNewsletterSpeaker{
				{Name: "One", Company: "One Co", XURL: "https://x.com/one"},
				{Name: "Two"}, {Name: "Three"}, {Name: "Four"}, {Name: "Five"}, {Name: "Six"},
			},
		}},
		HackathonWinners: []getters.WeeklyNewsletterHackathonWinner{
			{ConfTag: "dev26", Competition: "Hack", ProjectID: "one", ProjectTitle: "Project One", Awards: "First"},
			{ConfTag: "dev26", Competition: "Hack", ProjectID: "two", ProjectTitle: "Project Two", Awards: "Second"},
			{ConfTag: "dev26", Competition: "Hack", ProjectID: "three", ProjectTitle: "Project Three", Awards: "Third"},
			{ConfTag: "dev26", Competition: "Hack", ProjectID: "four", ProjectTitle: "Project Four", Awards: "Fourth"},
		},
	}
	markdown := weeklyNewsletterUpdatesMarkdown(updates)
	for _, want := range []string{"    - One of One Co. [x.com](https://x.com/one)", "    - Five.", "Project One", "Project Three"} {
		if !strings.Contains(markdown, want) {
			t.Errorf("weekly updates missing %q: %s", want, markdown)
		}
	}
	for _, excluded := range []string{"    - Six.", "Project Four"} {
		if strings.Contains(markdown, excluded) {
			t.Errorf("weekly updates exceeded cap with %q: %s", excluded, markdown)
		}
	}
}

func TestWeeklyNewsletterInsiderMarkdownUsesOnlyThreeBullets(t *testing.T) {
	markdown := weeklyNewsletterInsiderMarkdown(&getters.InsiderWeeklyIssue{
		Link: "https://insider.btcpp.dev/p/weekly",
		Bullets: []getters.InsiderWeeklyBullet{
			{Text: "First", Link: "https://example.com/first"},
			{Text: "Second"},
			{Text: "Third", Link: "https://example.com/third"},
			{Text: "Fourth", Link: "https://example.com/fourth"},
		},
	})
	for _, want := range []string{"- First [Link](https://example.com/first)", "- Second", "- Third [Link](https://example.com/third)", `→ [Read the full weekly summary](https://insider.btcpp.dev/p/weekly)`} {
		if !strings.Contains(markdown, want) {
			t.Errorf("Insider markdown missing %q: %s", want, markdown)
		}
	}
	if strings.Contains(markdown, "- [First]") || strings.Contains(markdown, "- [Third]") {
		t.Fatalf("Insider markdown links an entire bullet: %s", markdown)
	}
	if strings.Contains(markdown, "Fourth") {
		t.Fatalf("Insider markdown included a fourth bullet: %s", markdown)
	}
	if strings.Contains(markdown, "{{ button ") {
		t.Fatalf("Insider markdown contains button formatting: %s", markdown)
	}
	if !strings.HasPrefix(markdown, "### § Last week in Bitcoin") {
		t.Fatalf("Insider markdown does not use the newsletter section heading: %s", markdown)
	}
}
