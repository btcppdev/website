package handlers

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type reportBar struct {
	Label, Detail, URL string
	Value              int64
	Width              float64
}

func compactReportSats(n int64) string {
	divisor, suffix := float64(1), ""
	if n >= 1000000 {
		divisor, suffix = 1000000, "M"
	} else if n >= 1000 {
		divisor, suffix = 1000, "k"
	}
	if suffix == "" {
		return strconv.FormatInt(n, 10)
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(float64(n)/divisor, 'f', 2, 64), "0"), ".") + suffix
}
func (p sponsorReportPage) SatsDisplay() string { return compactReportSats(p.Sats) }
func (p reportWinner) SatsDisplay() string      { return compactReportSats(p.Sats) }
func (b reportBar) SatsDisplay() string         { return compactReportSats(b.Value) }

func (b reportBar) Y() float64 { return 100 - b.Width }

type reportChart struct {
	Hero, Caption                  string
	Title, Unit, Note, Empty, Kind string
	Rows                           []reportBar
	Maximum                        int64
}

type reportSlice struct {
	Label, Color    string
	Value           int64
	Percent, Offset float64
}

// Donut categories must partition one total; winning teams are a subset,
// unlike registrations and check-ins which use different counting units.
func (c reportChart) Donut() []reportSlice {
	if len(c.Rows) == 0 {
		return nil
	}
	var rows []reportBar
	switch c.Kind {
	case "program":
		rows = c.Rows
	case "teams":
		if len(c.Rows) != 2 || c.Rows[1].Value > c.Rows[0].Value {
			return nil
		}
		rows = []reportBar{{Label: "Winning projects", Value: c.Rows[1].Value}, {Label: "Other submitted projects", Value: c.Rows[0].Value - c.Rows[1].Value}}
	default:
		return nil
	}
	var total int64
	for _, r := range rows {
		total += r.Value
	}
	if total <= 0 {
		return nil
	}
	colors := []string{"#c8b3f5", "#f7b500", "#f6b9dc"}
	slices := make([]reportSlice, 0, len(rows))
	offset := 0.0
	for i, r := range rows {
		percent := 100 * float64(r.Value) / float64(total)
		slices = append(slices, reportSlice{r.Label, colors[i%len(colors)], r.Value, percent, -offset})
		offset += percent
	}
	return slices
}
func (c reportChart) DonutTotal() int64 {
	var total int64
	for _, s := range c.Donut() {
		total += s.Value
	}
	return total
}

// Charts uses the same reviewed data as the scorecard. No sample values are
// substituted for missing analytics or unverified public-source preview data.
func (p sponsorReportPage) Charts() []reportChart {
	program := reportChart{Title: "The program, delivered", Unit: "sessions", Kind: "program", Note: "Editor-confirmed delivery. Talks include keynotes; workshops and panels are counted separately.", Empty: "Awaiting confirmation of which sessions were delivered."}
	if p.ProgramReviewed && !p.Preview {
		program.Rows = []reportBar{{Label: "Talks & keynotes", Value: p.Talks}, {Label: "Workshops", Value: p.Workshops}, {Label: "Panels", Value: p.PanelCount}}
	}
	popular := reportChart{Title: "Conversations reaching further", Unit: p.Saved["analytics_metric"], Kind: "views", Empty: "Awaiting comparable, sourced session view counts."}
	popular.Note = fmt.Sprintf("Top five among sessions with supplied analytics · %s · exported %s · Source: %s. Views do not measure in-room popularity.", p.Saved["analytics_period"], p.Saved["analytics_date"], p.Saved["analytics_source"])
	if !p.Preview && popular.Unit != "" && p.Saved["analytics_source"] != "" && p.Saved["analytics_period"] != "" && p.Saved["analytics_date"] != "" {
		for _, s := range p.Popular {
			if s.Views != nil {
				link := s.WatchURL
				if link == "" {
					link = s.YouTube
				}
				if link == "" {
					link = s.X
				}
				popular.Rows = append(popular.Rows, reportBar{Label: s.Title, Detail: s.Speakers, Value: *s.Views, URL: link})
			}
		}
	}
	if len(popular.Rows) == 0 {
		popular.Note = "Compare one platform and reporting period at a time. Missing analytics are not zero views."
	}
	prizes := reportChart{Title: "Backing the winning projects", Unit: "sats", Kind: "prizes", Note: "Sats recorded as sent or claimed for finalized winning projects. Announced pools, pending payouts and non-sats prizes are excluded; external payments need reconciliation.", Empty: "Awaiting verified payout records and finalized winning projects."}
	if !p.Preview {
		for _, w := range p.Winners {
			prizes.Rows = append(prizes.Rows, reportBar{Label: w.Title, Detail: w.Awards, Value: w.Sats, URL: w.GitHub})
		}
	}
	sort.SliceStable(prizes.Rows, func(i, j int) bool { return prizes.Rows[i].Value > prizes.Rows[j].Value })
	attendance := reportChart{Title: "The community in the room", Unit: "records / people", Kind: "attendance", Note: "Valid registration records and unique checked-in people. Registrations may include duplicates, so these are not a conversion funnel. Daily attendance is not collected.", Empty: "Awaiting event registration and check-in records."}
	teams := reportChart{Title: "From building to recognition", Unit: "project teams", Kind: "teams", Note: "Submitted or advanced projects, with finalized winning projects shown as a subset. A project with multiple awards counts once. Members and volunteers are reported separately in the scorecard.", Empty: "Awaiting event project and finalized award records."}
	if !p.Preview {
		attendance.Rows = []reportBar{{Label: "Valid registrations", Value: p.Registrations}, {Label: "Unique checked-in attendees", Value: p.Attendees}}
		teams.Rows = []reportBar{{Label: "Submitted project teams", Value: p.Teams}, {Label: "Winning projects", Value: int64(len(p.Winners))}}
	}
	histogram := reportChart{Title: "How builders teamed up", Unit: "project teams", Kind: "team-sizes", Note: "Distinct members per submitted or advanced project. The final bucket includes teams of five or more. Projects with no recorded members remain visible.", Empty: "Awaiting project membership records."}
	if !p.Preview {
		histogram.Rows = p.TeamSizes
	}
	if !p.Preview {
		teams.Hero, teams.Caption = fmt.Sprint(p.Participants), "distinct project members"
		histogram.Hero, histogram.Caption = fmt.Sprint(p.Teams), "teams, many ways to collaborate"
		prizes.Hero, prizes.Caption = compactReportSats(p.Sats), "sats recorded as distributed across the event"
	}
	var charts []reportChart
	for _, chart := range []reportChart{program, popular, prizes, attendance, teams, histogram} {
		for _, row := range chart.Rows {
			if row.Value > chart.Maximum {
				chart.Maximum = row.Value
			}
		}
		for i := range chart.Rows {
			if chart.Maximum > 0 {
				chart.Rows[i].Width = 100 * float64(chart.Rows[i].Value) / float64(chart.Maximum)
			}
		}
		if chart.Kind == "team-sizes" {
			charts = append(charts, chart)
			continue
		}
		// Keep every project, with a common scale across continuation pages.
		if len(chart.Rows) == 0 {
			charts = append(charts, chart)
			continue
		}
		for start := 0; start < len(chart.Rows); start += 5 {
			part := chart
			end := start + 5
			if end > len(chart.Rows) {
				end = len(chart.Rows)
			}
			part.Rows = chart.Rows[start:end]
			if start > 0 {
				part.Title += " · continued"
			}
			charts = append(charts, part)
		}
	}
	return charts
}
