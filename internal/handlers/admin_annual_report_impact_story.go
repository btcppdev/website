package handlers

import (
	"fmt"
	"strings"
	"time"
)

type annualYearComparison struct {
	PreviousYear, Year, Previous, Current int
	Through                               string
}

type annualPartnerEvent struct {
	Event    reportEvent
	Headline bool
}

func (p annualReportPage) PartnerSummary() string {
	if len(p.PartnerEvents) == 0 {
		return ""
	}
	headline := true
	for _, event := range p.PartnerEvents {
		headline = headline && event.Headline
	}
	role := "a sponsor"
	if headline {
		role = "a headline sponsor"
	}
	count := fmt.Sprintf("%d gatherings", len(p.PartnerEvents))
	if len(p.PartnerEvents) == 1 {
		count = "one gathering"
	} else if len(p.PartnerEvents) == len(p.Included()) {
		count = "all " + count
	}
	return fmt.Sprintf("%s backed %s in this report as %s.", p.Recipient, count, role)
}

func (c annualYearComparison) PreviousWidth() float64 {
	return 100 * float64(c.Previous) / float64(max(c.Previous, c.Current))
}
func (c annualYearComparison) CurrentWidth() float64 {
	return 100 * float64(c.Current) / float64(max(c.Previous, c.Current))
}

// Compare published, completed gatherings through the same local calendar date.
// Excluding current-year events disables the comparison rather than changing its
// denominator silently. Registration and prize histories have different coverage.
func annualGatheringComparison(p annualReportPage, previous []reportEvent) *annualYearComparison {
	cutoff, err := time.Parse(time.DateOnly, p.Cutoff)
	if err != nil || cutoff.Year() != p.Year || len(p.Included()) == 0 {
		return nil
	}
	for _, event := range p.Events {
		if event.Eligible && !event.Included {
			return nil
		}
	}
	day := min(cutoff.Day(), time.Date(p.Year-1, cutoff.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day())
	priorCutoff := time.Date(p.Year-1, cutoff.Month(), day, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
	c := &annualYearComparison{PreviousYear: p.Year - 1, Year: p.Year, Current: len(p.Included()), Through: cutoff.Format("2 January")}
	for _, event := range previous {
		if annualEligible(event, p.Year-1, priorCutoff) {
			c.Previous++
		}
	}
	if c.Previous == 0 {
		return nil
	}
	return c
}

func (p annualReportPage) RepeatParticipation() *reportMetric {
	if p.RegistrationBased && p.RepeatRegistrants > 0 && p.RepeatRegistrants <= p.UniqueRegistrants {
		return &reportMetric{Value: compactReportSats(p.RepeatRegistrants), Label: "people registered for 2+ gatherings"}
	}
	if !p.RegistrationBased && p.ReturningAttendees > 0 && p.ReturningAttendees <= p.UniqueAttendees {
		return &reportMetric{Value: compactReportSats(p.ReturningAttendees), Label: "people checked in at 2+ gatherings"}
	}
	return nil
}

func annualSections(year int, recipient string) []reportSection {
	sections := legacyAnnualSections(year, recipient)
	support := "[Add the specific work this partner funded, using the sponsorship agreement or organizer-confirmed allocation. Name the activity and what it enabled.]"
	quote := "[Add a participant quote cleared for publication, followed by their name and event. Focus on a connection, opportunity, or piece of work that came from the gathering.]"
	var reachQuote, closingQuote string
	copy := map[string]string{
		"community": "",
		"building":  "",
		"program":   "",
		"reach":     "",
		"lessons":   "[Add one concrete lesson from this year: what was difficult, what changed, and how that will improve the next gathering.]",
		"next":      "**Bring builders together.**\n\n**Keep technical learning accessible.**\n\n**Support the work beyond the demo.**",
	}
	if year == 2026 {
		quote = "“You do a great job to make all of us meet.\n\nThank you so much. ☺️”"
		reachQuote = "“I’ve been watching since you sent me the x link and it’s all been high quality, academic level content. Thank you.”"
		closingQuote = "“Thank you for keeping the Bitcoin community alive”"
		opening := "Thank you for backing the people building Bitcoin. Your support brings developers together to meet collaborators, test ideas, and share what they learn."
		if recipient == "NYDIG" {
			opening = "Thank you for backing the people building Bitcoin. As a headline sponsor, you helped fund hackathons, crew travel, catering, event space, and professional video recordings."
			support = "NYDIG’s headline sponsorship funded hackathons, crew travel, catering, event space, and professional video recordings."
		}
		copy["note"] = fmt.Sprintf("To our friends at %s,\n\n%s\n\nThree highlights from 2026: bringing bitcoin++ to Africa, shipping our hackathon platform with help from our summer intern Jake from Tulsa, and hosting our first economics edition. Bringing developers into the same room as the people who say ‘developers don’t understand economics’ was a particular highlight.\n\nThank you for making room for this work. We would love to build on it with you in %d.\n\nnifty", recipient, opening, year+1)
	}
	for i := range sections {
		if value, ok := copy[sections[i].Key]; ok {
			sections[i].Text = value
		}
	}
	return append(sections,
		reportSection{Key: "support", Title: recipient + "’s contribution", Text: support},
		reportSection{Key: "resource_use", Title: "How support was used", Text: "[Add a compact spending breakdown from finance records, with currency and period. Keep spending separate from the offered prize pool.]"},
		reportSection{Key: "community_quote", Title: "A participant’s voice", Text: quote},
		reportSection{Key: "reach_quote", Title: "Recording feedback", Text: reachQuote},
		reportSection{Key: "closing_quote", Title: "Community feedback · closing", Text: closingQuote},
	)
}

// Upgrade only untouched old defaults in live drafts. Preserve custom writing,
// intentional blanks, and the text stored in previously frozen editions.
func (p *annualReportPage) loadNarrativeSections() {
	p.Sections = annualSections(p.Year, p.Recipient)
	old := map[string]string{}
	for _, section := range legacyAnnualSections(p.Year, p.Recipient) {
		old[section.Key] = section.Text
	}
	for i, section := range p.Sections {
		if value, ok := p.Saved[section.Key]; ok {
			if original, exists := old[section.Key]; exists && value != "" && value == original {
				continue
			}
			p.Sections[i].Text = value
		}
	}
}

type annualStoryField struct {
	Key, Label, Text, Placeholder string
}

type annualProjectStory struct {
	Problem, Built, Next, Source string
}

func (p annualReportPage) projectStory(w reportWinner) annualProjectStory {
	story := annualProjectStory{Built: w.ShortDescription()}
	// Editorial summaries of the teams' public submissions. Follow-up stays empty
	// until there is evidence of what happened after the event.
	if p.Year == 2026 && p.Frozen == "" {
		switch w.ID {
		case "0907cf92-a330-4088-8d6a-3c352eed3356":
			story.Problem = "Connecting mobile-money payments across African currencies."
			story.Built = "A Lightning and LNURL bridge from XOF, XAF, and GNF payments to Kenyan shillings, using Banxaas and Tando."
		case "43a0dcc8-c4eb-45fc-abd7-6546a86d1037":
			story.Problem = "Making miner behavior observable during a soft-fork debate."
			story.Built = "A dashboard comparing mining pools’ BIP-110 compliance with their signaling, using data from the builder’s own Bitcoin node."
			if p.Cutoff >= "2026-10-17" {
				story.Next = "On 17 October, the maintainer documented the project’s conclusion and added an AGPL-3.0 license."
				story.Source = "https://github.com/thecurtic/template-watcher/commit/d09f014fbc4a33e83846b608bd95f156e5c3744a"
			}
		case "b4626052-58f0-4973-8680-30d726e0c9f3":
			story.Problem = "Streaming small podcast payments with fewer mint round trips."
			story.Built = "A pay-as-you-listen Cashu player using a Spilman channel, with an end-to-end mint test described in the team’s submission."
			if p.Cutoff >= "2026-10-21" {
				story.Next = "Channel-handling fixes were committed on 21 October, continuing work on the hackathon branch."
				story.Source = "https://gitlab.com/nodesignal/nodesignal-cashu-player/-/commit/032fa399087fa70060b151c938fe664797fbde58"
			}
		case "33249dbf-181b-4cc6-9074-1a742f71cb60":
			story.Problem = "Sending Silent Payments from a hardware multisig."
			story.Built = "A FROST signing prototype coordinated over Marmot. The team documented a 1,000-sat Signet payment using two Frostsnap devices."
		}
	}
	for key, value := range map[string]*string{"problem": &story.Problem, "built": &story.Built, "next": &story.Next, "source": &story.Source} {
		if saved, ok := p.Saved["project_"+key+"_"+w.ID]; ok {
			*value = saved
		}
	}
	return story
}

func (h annualProjectHighlight) StoryFields() []annualStoryField {
	return []annualStoryField{
		{Key: "project_problem_" + h.Project.ID, Label: "The problem", Text: h.Story.Problem, Placeholder: "What problem was the team trying to solve?"},
		{Key: "project_built_" + h.Project.ID, Label: "What they built", Text: h.Story.Built, Placeholder: "Describe the working prototype or demo."},
		{Key: "project_next_" + h.Project.ID, Label: "After the event", Text: h.Story.Next, Placeholder: "Add a verified follow-up: what shipped, changed, or continued. Include a source or date. Leave blank until known."},
		{Key: "project_source_" + h.Project.ID, Label: "Follow-up source URL", Text: h.Story.Source, Placeholder: "https://… Link to the commit, release, or published follow-up."},
	}
}

func annualProjectStoryKey(key string) (string, bool) {
	for _, prefix := range []string{"project_problem_", "project_built_", "project_next_", "project_source_"} {
		if strings.HasPrefix(key, prefix) {
			return strings.TrimPrefix(key, prefix), true
		}
	}
	return "", false
}
