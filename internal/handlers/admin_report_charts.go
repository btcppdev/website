package handlers

import (
	"fmt"
	"sort"
)

type reportBar struct {
	Label, Detail, URL string
	Value              int64
	Width              float64
}
type reportChart struct {
	Title, Unit, Note, Empty, Kind string
	Rows                           []reportBar
	Maximum                        int64
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
	var charts []reportChart
	for _, chart := range []reportChart{program, popular, prizes} {
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
