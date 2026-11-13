package handlers

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// A report-only snapshot of the organizer's published retrospective. It does
// not create projects, participants, awards or payout records in the database.
type reportHackathonRetrospective struct {
	URL, Title, Published               string
	Projects, Participants, Hours, Bugs int64
	ParticipantsEstimated               bool
	Winners                             []reportWinner
}

func hackathonRetrospective(tag string) *reportHackathonRetrospective {
	if tag != "floripa26" {
		return nil
	}
	const source = "https://insider.btcpp.dev/p/bitcoin-exploits-edition-hackathon"
	project := func(slug, title, description, awards string, rank int) reportWinner {
		return reportWinner{
			// Stable report keys keep editorial fields and frozen snapshots usable.
			ID:    uuid.NewSHA1(uuid.NameSpaceURL, []byte(source+"#"+slug)).String(),
			Title: title, Description: description, Awards: awards, Rank: rank, SourceURL: source,
		}
	}
	return &reportHackathonRetrospective{
		URL: source, Title: "bitcoin++ exploits edition: hackathon retrospective", Published: "2026-03-24",
		Projects: 21, Participants: 60, ParticipantsEstimated: true, Hours: 22, Bugs: 10,
		// The article names two placements, not a complete winner roster. It
		// supplies no prize amounts, participant identities or team-size data.
		Winners: []reportWinner{
			project("mineexploit", "MineExploit", "Security testing tools for Bitcoin mining software. The team found three bugs in Stratum V2 repositories.", "1st overall", 1),
			project("localprobe", "LocalProbe", "Browser-based research into open-port fingerprinting and attacks against Bitcoin nodes, demonstrated by 0xB10C.", "2nd overall", 2),
		},
	}
}

func approximateReportCount(n int64, estimated bool) string {
	value := compactReportSats(n)
	if estimated {
		return "~" + value
	}
	return value
}

// Standalone reports fall back only when the legacy project roster is absent.
func (p *sponsorReportPage) applyHackathonRetrospective() {
	if p.Teams != 0 || p.Participants != 0 || len(p.Winners) != 0 {
		return
	}
	if source := hackathonRetrospective(p.Event.Tag); source != nil {
		p.HackathonRetrospective = source
		p.Teams, p.Participants = source.Projects, source.Participants
		p.Winners = source.Winners
		p.TeamSizes = nil
	}
}

func (p sponsorReportPage) ParticipantsDisplay() string {
	value := strconv.FormatInt(p.Participants, 10)
	if p.HackathonRetrospective != nil && p.HackathonRetrospective.ParticipantsEstimated {
		return "~" + value
	}
	return value
}

func (p sponsorReportPage) HackathonURL() string {
	if p.HackathonRetrospective != nil {
		return p.HackathonRetrospective.URL
	}
	return "https://btcpp.dev/" + p.Event.Tag + "/hackathon"
}

func (w reportWinner) ReportURL(tag string) string {
	if w.SourceURL != "" {
		return w.SourceURL
	}
	return "https://btcpp.dev/" + tag + "/hackathon/projects/" + w.PublicID()
}

func (e *annualEvent) applyHackathonRetrospective(year int) {
	if year != 2026 || e.Event.Tag != "floripa26" || e.HackCoverage == "excluded" || e.HackCoverage == "none" || e.HackCoverage == "records" {
		return
	}
	source := hackathonRetrospective(e.Event.Tag)
	e.HackCoverage = "retrospective"
	e.HackathonRetrospective = source
	e.Projects, e.Participants = source.Projects, source.Participants
	e.ParticipantsEstimated = source.ParticipantsEstimated
	e.WinningProjects = source.Winners
	e.Winners = 0 // The complete winner count is not published.
	// Organizer estimate supplied 13 November 2026, separate from the article.
	// This is a prize offer, never an awarded or paid amount. Prefer known records.
	if e.OfferedBased && e.Sats == 0 {
		e.Sats, e.PrizesEstimated = 8_000_000, true
	}
}

func (e annualEvent) HackathonURL() string {
	if e.HackathonRetrospective != nil {
		return e.HackathonRetrospective.URL
	}
	return "https://btcpp.dev/" + e.Event.Tag + "/hackathon"
}

func (e annualEvent) HasRetrospective() bool { return e.Event.Tag == "floripa26" }
func (e annualEvent) ParticipantsDisplay() string {
	return approximateReportCount(e.Participants, e.ParticipantsEstimated)
}

func (h annualProjectHighlight) URL() string { return h.Project.ReportURL(h.Tag) }

func (h annualProjectHighlight) LinkLabel() string {
	if h.Project.SourceURL != "" {
		return "Read retrospective"
	}
	return "Project"
}

func retrospectiveHackathonCopy() string {
	return "In 22 hours, approximately 60 participants submitted 21 projects and found 10 bugs across open-source Bitcoin software. The event paired a project-building track with a bug bounty track. MineExploit took first overall; LocalProbe placed second."
}

func (p *sponsorReportPage) applyRetrospectiveMetrics() {
	if p.HackathonRetrospective == nil {
		return
	}
	var metrics []reportMetric
	for _, metric := range p.Metrics {
		if metric.Label == "Submitted project teams" {
			metric.Value, metric.Note = compactReportSats(p.Teams), "Submitted projects reported in the published retrospective."
		}
		if metric.Label == "Hackathon participants" {
			metric.Value, metric.Note = p.ParticipantsDisplay(), "Participant estimate from the published retrospective."
		}
		if (metric.Label == "Sponsor challenges" && p.Challenges == 0) || (strings.HasPrefix(metric.Label, "Sats ") && p.Sats == 0) {
			continue
		}
		metrics = append(metrics, metric)
	}
	p.Metrics = metrics
}
