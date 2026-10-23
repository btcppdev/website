package handlers

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

const reportRegistrationHeadlineMinimum int64 = 100
const reportViewsHeadlineMinimum int64 = 1000

type reportMediaTotal struct {
	Platform                      string
	Views                         int64
	Recordings                    int
	FirstCollected, LastCollected string
}

func (t reportMediaTotal) Display() string { return compactReportSats(t.Views) }

// One counter per platform/video. Prefer the latest dated snapshot, not the
// largest counter (platforms can revise counts downward).
func uniqueReportMedia(media []reportMedia) []reportMedia {
	byID := map[string]reportMedia{}
	for _, m := range media {
		if m.ID == "" || m.Views < 0 || (m.Platform != "YouTube" && m.Platform != "X") {
			continue
		}
		key := m.Platform + "/" + m.ID
		old, exists := byID[key]
		newer, _ := time.Parse(time.RFC3339Nano, m.Collected)
		older, _ := time.Parse(time.RFC3339Nano, old.Collected)
		if !exists || newer.After(older) {
			byID[key] = m
		}
	}
	result := make([]reportMedia, 0, len(byID))
	for _, m := range byID {
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Views != result[j].Views {
			return result[i].Views > result[j].Views
		}
		return result[i].Platform+result[i].ID < result[j].Platform+result[j].ID
	})
	return result
}
func reportMediaTotals(media []reportMedia) []reportMediaTotal {
	totals := map[string]reportMediaTotal{}
	for _, m := range uniqueReportMedia(media) {
		t := totals[m.Platform]
		t.Platform = m.Platform
		t.Views += m.Views
		t.Recordings++
		date := m.Collected
		if parsed, err := time.Parse(time.RFC3339Nano, date); err == nil {
			date = parsed.UTC().Format("2006-01-02")
		}
		if t.FirstCollected == "" || date < t.FirstCollected {
			t.FirstCollected = date
		}
		if date > t.LastCollected {
			t.LastCollected = date
		}
		totals[m.Platform] = t
	}
	var result []reportMediaTotal
	for _, platform := range []string{"YouTube", "X"} {
		t := totals[platform]
		if t.Views >= reportViewsHeadlineMinimum {
			result = append(result, t)
		}
	}
	return result
}
func (p sponsorReportPage) MediaTotals() []reportMediaTotal { return reportMediaTotals(p.Media()) }
func (p annualReportPage) AllMedia() []reportMedia {
	var media []reportMedia
	for _, e := range p.Included() {
		media = append(media, e.Media...)
	}
	return uniqueReportMedia(media)
}
func (p annualReportPage) MediaTotals() []reportMediaTotal { return reportMediaTotals(p.AllMedia()) }
func reportMediaHighlights(media []reportMedia) []reportMedia {
	media = uniqueReportMedia(media)
	if len(media) > 4 {
		media = media[:4]
	}
	return media
}
func (p annualReportPage) MediaPages() [][]reportMedia {
	return reportMediaPages(reportMediaHighlights(p.AllMedia()))
}

// The editor retains every value; only the sponsor presentation is curated.
func (m reportReachMetric) Headline() bool {
	n, err := strconv.ParseInt(m.Value, 10, 64)
	if err != nil {
		return false
	}
	if strings.HasSuffix(m.Key, "views") || strings.HasSuffix(m.Key, "impressions") {
		return n >= reportViewsHeadlineMinimum
	}
	return n > 0
}
func (p reportReachPlatform) HeadlineMetrics() []reportReachMetric {
	var out []reportReachMetric
	if !p.Ready() {
		return out
	}
	for _, m := range p.Metrics {
		if m.Headline() {
			out = append(out, m)
		}
	}
	return out
}
func (p sponsorReportPage) HeadlinePlatforms() []reportReachPlatform {
	var out []reportReachPlatform
	for _, v := range withApprovedPostPlatform(p.ReachPlatforms(), p.ApprovedPosts()) {
		if len(v.HeadlineMetrics()) > 0 {
			out = append(out, v)
		}
	}
	return out
}
func (p annualReportPage) HeadlinePlatforms() []reportReachPlatform {
	platforms := (sponsorReportPage{Saved: p.Saved}).HeadlinePlatforms()
	// An explicit account-wide annual total already includes the event posts.
	if p.Saved["reach_x_impressions"] == "" {
		v := reportPostsPlatform(p.ApprovedPosts())
		if len(p.ApprovedPosts()) > 0 && len(v.HeadlineMetrics()) > 0 {
			platforms = append(platforms, v)
		}
	}
	for i := range platforms {
		for j := range platforms[i].Metrics {
			platforms[i].Metrics[j].Label = strings.TrimPrefix(platforms[i].Metrics[j].Label, "Event ")
		}
	}
	return platforms
}
func (p sponsorReportPage) ShowRegistrations() bool {
	return (!p.Preview || p.DemoAttendance) && p.Registrations >= reportRegistrationHeadlineMinimum
}
func (p annualReportPage) ShowParticipation() bool {
	return p.ParticipationTotal() >= reportRegistrationHeadlineMinimum
}
func (p annualReportPage) UniquePeopleHeadline() int64 {
	n := p.UniqueAttendees
	if p.RegistrationBased {
		n = p.UniqueRegistrants
	}
	if n < reportRegistrationHeadlineMinimum {
		return 0
	}
	return n
}
func (p sponsorReportPage) HeadlineCharts() []reportChart {
	var out []reportChart
	for _, c := range p.Charts() {
		if c.Kind != "attendance" && c.Kind != "views" {
			out = append(out, c)
		}
	}
	return out
}
func (p sponsorReportPage) HeadlineMetrics() []reportMetric {
	var out []reportMetric
	for _, m := range p.Metrics {
		if strings.Contains(strings.ToLower(m.Label), "attendee") {
			continue
		}
		out = append(out, m)
	}
	if p.ShowRegistrations() {
		out = append(out, reportMetric{Label: "registration records", Value: strconv.FormatInt(p.Registrations, 10), Note: "Registrations, not confirmed attendance"})
	}
	return out
}
