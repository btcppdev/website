package handlers

import (
	"fmt"
	"strings"

	"btcpp-web/internal/types"
)

func (e annualEvent) BadgeEmoji() string {
	conf := types.Conf{Tag: e.Event.Tag, Location: e.Event.Location}
	return conf.BadgeEmoji()
}

type annualEventHighlight struct {
	Event                       reportEvent
	Copy                        reportSection
	Status, Feature, FeatureURL string
	Stats                       []annualEventStat
	Speakers                    []reportPerson
}

type annualEventGlobe struct {
	Country, Image string
}

func (e annualEvent) Globe() *annualEventGlobe {
	return (annualEventHighlight{Event: e.Event}).Globe()
}

// These assets locate the event, never the participants. Unmapped events omit
// the globe instead of guessing a country from a free-form location string.
func (h annualEventHighlight) Globe() *annualEventGlobe {
	var country, slug string
	switch h.Event.Tag {
	case "floripa", "floripa26":
		country, slug = "Brazil", "brazil"
	case "vegas":
		country, slug = "United States", "united-states"
	case "vienna":
		country, slug = "Austria", "austria"
	case "nairobi":
		country, slug = "Kenya", "kenya"
	case "toronto":
		country, slug = "Canada", "canada"
	case "berlin", "berlin23", "berlin24", "berlin25", "berlin26":
		country, slug = "Germany", "germany"
	case "seoul":
		country, slug = "South Korea", "south-korea"
	default:
		return nil
	}
	return &annualEventGlobe{Country: country, Image: "/static/img/report-locations/" + slug + ".svg"}
}

// These badges are counters, not percentage gauges. Missing coverage is not zero.
type annualEventStat struct {
	Kind, Value, Label, Detail string
}

func (s annualEventStat) Compact() bool { return len(s.Value) > 5 }

func annualEventStats(e annualEvent) []annualEventStat {
	var stats []annualEventStat
	if e.Talks > 0 {
		stats = append(stats, annualEventStat{Kind: "talks", Value: fmt.Sprint(e.Talks), Label: "talks & keynotes", Detail: "Accepted program talks and keynotes; workshops and panels counted separately"})
	}
	media := uniqueReportMedia(e.Media)
	if len(media) > 0 {
		var total int64
		for _, m := range media {
			total += m.Views
		}
		stats = append(stats, annualEventStat{Kind: "views", Value: compactReportSats(total), Label: "video views", Detail: "Recorded X and YouTube video views; each video counted once per platform"})
	}
	if e.HackKnown() && e.Sats > 0 {
		prizes := annualEventStat{Kind: "prizes", Value: e.SatsDisplay(), Label: e.PrizeUnit(), Detail: "Recorded sent or claimed hackathon prize distributions"}
		if e.AwardBased {
			prizes.Detail = "Configured sat prizes assigned to finalized winning projects; not a settlement total"
		}
		if e.OfferedBased {
			prizes.Detail = "Configured sats per winning project × available winner slots, including unawarded prizes; independent of payouts"
		}
		if e.PrizesEstimated {
			prizes.Detail = "Approximate prize pool supplied by the organizer on 13 November 2026; not an awarded or paid amount"
		}
		stats = append(stats, prizes)
	}
	if e.HackKnown() && e.HackathonRetrospective != nil {
		stats = append(stats, annualEventStat{Kind: "bugs", Value: fmt.Sprint(e.HackathonRetrospective.Bugs), Label: "bugs found", Detail: "Reported in the Insider hackathon retrospective"})
	}
	return stats
}

type annualEventHighlightPage struct {
	Folio      string
	Highlights []annualEventHighlight
}

func legacyAnnualEventHighlightCopy(year int, e reportEvent) string {
	if year == 2026 {
		switch e.Tag {
		case "floripa26":
			return "An exploits edition in Florianópolis put Bitcoin’s failure modes in focus. The program and hackathon offered a place to examine vulnerabilities, test assumptions, and explore how to make Bitcoin software more resilient."
		case "vegas":
			return "Villains edition brought an adversarial perspective to the year’s technical conversation. A gathering for people interested in how Bitcoin can be attacked—and what developers can learn from thinking like an attacker."
		case "vienna":
			return "Our first economics edition brought developers into conversation with the people who say ‘developers don’t understand economics’. Bridging that gap was a highlight of the year, creating room to examine Bitcoin’s technical choices alongside their economic implications."
		case "nairobi":
			return "Taking bitcoin++ to Africa was one of the year’s proudest moments. Nairobi’s open-source edition brought talks, workshops, and a hackathon together, making room for conversations about contributing to Bitcoin and building tools for the communities using it."
		case "toronto":
			return "Consensus edition focused the conversation on how Bitcoin’s rules evolve. Talks and panels—including a discussion of fork strategies—gave participants a place to explore the technical choices and disagreements that shape Bitcoin’s future."
		case "berlin26":
			return "Payments edition brought the focus to using Bitcoin. The program explored merchant tools, ecash, and the challenges of building payment systems, alongside a hackathon that gave teams time to turn their ideas into projects."
		case "seoul":
			return "Next, privacy edition takes bitcoin++ to Seoul. The upcoming gathering continues the year’s global conversation, with a focus on privacy and the tools people need to use Bitcoin."
		}
	}
	return e.Title + " brought the bitcoin++ community together in " + e.Location + ". Explore the program for the talks, workshops, and conversations that shaped this edition."
}
func annualEventHighlightCopy(year int, e reportEvent) string {
	if year == 2026 {
		copy := map[string]string{
			"floripa26": "In 22 hours, approximately 60 participants submitted 21 projects and found 10 bugs in open-source Bitcoin software. Overall winner MineExploit found three bugs in Stratum V2 repositories.",
			"vegas":     "The villains edition brought an attacker’s perspective to Bitcoin’s security and design assumptions.",
			"vienna":    "Our first economics edition brought developers and economists together to examine incentives and protocol design.",
			"nairobi":   "Our first gathering in Africa paired open-source learning with a hackathon. Winner WestToEast explored Lightning payments between West African currencies and Kenyan shillings.",
			"toronto":   "A consensus edition exploring how Bitcoin’s rules evolve. Winner template-watcher made mining-pool behavior visible through a Bitcoin-node-powered dashboard.",
			"berlin26":  "Merchant payments and ecash took center stage. Winner cashu player prototyped pay-as-you-listen podcast payments through a single channel.",
			"seoul":     "Privacy ideas became working prototypes. Winning team BlackIce demonstrated a Signet Silent Payment using two hardware devices.",
		}
		if text := copy[e.Tag]; text != "" {
			return text
		}
	}
	return legacyAnnualEventHighlightCopy(year, e)
}

func (p annualReportPage) eventHighlight(e annualEvent, status string) annualEventHighlight {
	key := "event_highlight_" + e.Event.ID
	copy := annualEventHighlightCopy(p.Year, e.Event)
	if status != "" {
		copy = e.Event.Title + " will bring the bitcoin++ community together in " + e.Event.Location + ". Explore the upcoming event and its program."
	}
	if text, ok := p.Saved[key]; ok {
		oldFloripa := e.Event.Tag == "floripa26" && text == "An exploits edition exploring Bitcoin’s vulnerabilities through technical sessions and a hackathon."
		if p.Frozen != "" || (text != legacyAnnualEventHighlightCopy(p.Year, e.Event) && !oldFloripa) {
			copy = text
		}
	}
	h := annualEventHighlight{Event: e.Event, Status: status, Copy: reportSection{Key: key, Title: e.Event.Location + " highlight", Text: copy}}
	h.Feature = "Explore the program"
	h.FeatureURL = "https://btcpp.dev/" + e.Event.Tag + "/agenda"
	if status != "" {
		h.Feature = "Explore the upcoming event"
		h.FeatureURL = "https://btcpp.dev/" + e.Event.Tag
		return h
	}
	h.Stats = annualEventStats(e)
	h.Speakers = annualSpeakerPortraits(e, 3)
	media := uniqueReportMedia(e.Media)
	if len(media) > 0 {
		h.Feature = "Watch the recording"
		h.FeatureURL = media[0].URL
	} else if e.HackKnown() && len(e.WinningProjects) > 0 {
		w := e.WinningProjects[0]
		h.Feature = "Meet the builders"
		h.FeatureURL = w.ReportURL(e.Event.Tag)
	}
	if e.HackKnown() && e.HackathonRetrospective != nil {
		h.Feature, h.FeatureURL = "Read the hackathon retrospective", e.HackathonURL()
	}
	return h
}
func (p annualReportPage) EventHighlightEditors() []annualEventHighlight {
	var out []annualEventHighlight
	for _, e := range p.Events {
		status := ""
		for _, future := range p.Forthcoming() {
			if future.Event.ID == e.Event.ID {
				status = "Upcoming"
				break
			}
		}
		out = append(out, p.eventHighlight(e, status))
	}
	return out
}
func (p annualReportPage) EventHighlightPages() []annualEventHighlightPage {
	var pages []annualEventHighlightPage
	for _, e := range p.Included() {
		if len(pages) == 0 || len(pages[len(pages)-1].Highlights) == 3 {
			pages = append(pages, annualEventHighlightPage{})
		}
		pages[len(pages)-1].Highlights = append(pages[len(pages)-1].Highlights, p.eventHighlight(e, ""))
	}
	// Balance the final two pages rather than giving one event a whole spread.
	if n := len(pages); n > 1 && len(pages[n-1].Highlights) == 1 {
		previous := &pages[n-2]
		last := len(previous.Highlights) - 1
		pages[n-1].Highlights = append([]annualEventHighlight{previous.Highlights[last]}, pages[n-1].Highlights...)
		previous.Highlights = previous.Highlights[:last]
	}
	for i := range pages {
		pages[i].Folio = fmt.Sprintf("%02d / %02d", 3+i, p.ReportPageCount())
	}
	return pages
}
func (p annualReportPage) UpcomingHighlights() []annualEventHighlight {
	var out []annualEventHighlight
	for _, future := range p.Forthcoming() {
		out = append(out, p.eventHighlight(annualEvent{Event: future.Event}, future.Status))
	}
	return out
}
func (p annualReportPage) ReportFolio(base int) string {
	n := (len(p.Included()) + 2) / 3
	page := base
	if base > 2 {
		page += n
	}
	if base >= 8 {
		page += p.extraWinnerPages()
		if p.HasFollowUp() {
			page++
		}
	}
	return fmt.Sprintf("%02d / %02d", page, p.ReportPageCount())
}

func (p annualReportPage) extraWinnerPages() int {
	if winners := len(p.BriefProjects()); winners > 0 {
		return (winners - 1) / 4
	}
	return 0
}

func (p annualReportPage) ReportPageCount() int {
	count := 8 + (len(p.Included())+2)/3 + p.extraWinnerPages()
	if p.HasFollowUp() {
		count++
	}
	return count
}

func (p annualReportPage) HasFollowUp() bool {
	return strings.TrimSpace(p.BriefSection("lessons").Text) != "" || strings.TrimSpace(p.BriefSection("next").Text) != ""
}

func (p annualReportPage) FollowUpFolio() string {
	return fmt.Sprintf("%02d / %02d", p.ReportPageCount()-1, p.ReportPageCount())
}
func (h annualEventHighlight) Edition() string {
	title := h.Event.Title
	for _, prefix := range []string{"bitcoin++ ", "Bitcoin++ "} {
		title = strings.TrimPrefix(title, prefix)
	}
	return title
}
