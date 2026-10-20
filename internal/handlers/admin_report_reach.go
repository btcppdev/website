package handlers

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type reportReachMetric struct {
	Key, Label, Value string
}
type reportReachPlatform struct {
	Key, Name, Source, Period, Date, Coverage string
	Metrics                                   []reportReachMetric
}

// Each platform has its own export provenance and event-specific selection.
// Blank values remain unknown; platform totals are never summed as unique reach.
func (p sponsorReportPage) ReachPlatforms() []reportReachPlatform {
	platforms := []reportReachPlatform{
		{Key: "youtube", Name: "YouTube", Metrics: []reportReachMetric{
			{Key: "views", Label: "Event video views"},
			{Key: "impressions", Label: "Thumbnail impressions"},
			{Key: "watch_minutes", Label: "Minutes watched"},
			{Key: "external_views", Label: "Views from external sources"},
		}},
		{Key: "x", Name: "X", Metrics: []reportReachMetric{
			{Key: "impressions", Label: "Event post impressions"},
			{Key: "views", Label: "Event video views"},
		}},
	}
	for i := range platforms {
		v := &platforms[i]
		prefix := "reach_" + v.Key + "_"
		v.Source, v.Period = p.Saved[prefix+"source"], p.Saved[prefix+"period"]
		v.Date, v.Coverage = p.Saved[prefix+"date"], p.Saved[prefix+"coverage"]
		for j := range v.Metrics {
			v.Metrics[j].Key = prefix + v.Metrics[j].Key
			v.Metrics[j].Value = p.Saved[v.Metrics[j].Key]
		}
	}
	return platforms
}

func (p reportReachPlatform) Ready() bool {
	if p.Source == "" || p.Period == "" || p.Coverage == "" {
		return false
	}
	if _, err := time.Parse("2006-01-02", p.Date); err != nil {
		return false
	}
	for _, m := range p.Metrics {
		if n, err := reportOptionalCount(m.Value); err == nil && n != nil {
			return true
		}
	}
	return false
}

func (m reportReachMetric) Display() string {
	if n, err := reportOptionalCount(m.Value); err == nil && n != nil {
		return compactReportSats(*n)
	}
	return ""
}

func saveReportReach(form url.Values, saved map[string]string, now time.Time) error {
	for _, platform := range (sponsorReportPage{}).ReachPlatforms() {
		prefix := "reach_" + platform.Key + "_"
		for _, suffix := range []string{"source", "period", "date", "coverage"} {
			key := prefix + suffix
			value := strings.TrimSpace(form.Get(key))
			if len(value) > 12000 {
				return fmt.Errorf("%s exceeds 12,000 bytes", key)
			}
			saved[key] = value
		}
		hasValue := false
		for _, m := range platform.Metrics {
			value := strings.TrimSpace(form.Get(m.Key))
			n, err := reportOptionalCount(value)
			if err != nil {
				return fmt.Errorf("%s: %w", platform.Name, err)
			}
			hasValue = hasValue || n != nil
			saved[m.Key] = value
		}
		if hasValue {
			for _, suffix := range []string{"source", "period", "coverage"} {
				if saved[prefix+suffix] == "" {
					return fmt.Errorf("add %s %s for event reach", platform.Name, suffix)
				}
			}
			date, err := time.Parse("2006-01-02", saved[prefix+"date"])
			if err != nil || date.After(now.UTC()) {
				return fmt.Errorf("add a valid %s export date, not in the future", platform.Name)
			}
		}
	}
	return nil
}
