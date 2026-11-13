package handlers

import (
	"fmt"
	"strings"
)

type annualEventHighlight struct {
	Event                       reportEvent
	Copy                        reportSection
	Status, Feature, FeatureURL string
	Stats                       []annualEventStat
	Speakers                    []reportPerson
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
		prizes := annualEventStat{Kind: "prizes", Value: compactReportSats(e.Sats), Label: e.PrizeUnit(), Detail: "Recorded sent or claimed hackathon prize distributions"}
		if e.AwardBased {
			prizes.Detail = "Configured sat prizes assigned to finalized winning projects; not a settlement total"
		}
		if e.OfferedBased {
			prizes.Detail = "Configured sats per winning project × available winner slots, including unawarded prizes; independent of payouts"
		}
		stats = append(stats, prizes)
	}
	return stats
}

type annualEventHighlightPage struct {
	Folio      string
	Highlights []annualEventHighlight
}

func annualEventHighlightCopy(year int, e reportEvent) string {
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
func (p annualReportPage) eventHighlight(e annualEvent, status string) annualEventHighlight {
	key := "event_highlight_" + e.Event.ID
	copy := annualEventHighlightCopy(p.Year, e.Event)
	if status != "" && !(p.Year == 2026 && e.Event.Tag == "seoul") {
		copy = e.Event.Title + " will bring the bitcoin++ community together in " + e.Event.Location + ". Explore the upcoming event and its program."
	}
	if text, ok := p.Saved[key]; ok {
		copy = text
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
		h.Feature = "Watch: " + media[0].Title
		h.FeatureURL = media[0].URL
	} else if e.HackKnown() && len(e.WinningProjects) > 0 {
		w := e.WinningProjects[0]
		h.Feature = "Meet the builders: " + w.Title
		h.FeatureURL = "https://btcpp.dev/" + e.Event.Tag + "/hackathon/projects/" + w.PublicID()
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
	for i := range pages {
		pages[i].Folio = fmt.Sprintf("%02d / %02d", 3+i, 8+len(pages))
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
	return fmt.Sprintf("%02d / %02d", page, 8+n)
}
func (h annualEventHighlight) Edition() string {
	title := h.Event.Title
	for _, prefix := range []string{"bitcoin++ ", "Bitcoin++ "} {
		title = strings.TrimPrefix(title, prefix)
	}
	return title
}
