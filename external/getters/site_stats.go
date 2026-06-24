package getters

import "btcpp-web/internal/config"

// SiteStatsValues holds the raw counts behind the about-page numbers.
// Format-for-display is left to callers.
type SiteStatsValues struct {
	PastConfs int // count of confs where EndDate is in the past
	PastTalks int // count of Accepted ConfTalks at past confs
	Attendees int // total registration rows, rough attendee count
}

func siteStatsDirect(ctx *config.AppContext) (SiteStatsValues, error) {
	var s SiteStatsValues

	confs, err := ListConfs(ctx)
	if err != nil {
		return s, err
	}
	ended := map[string]bool{}
	for _, c := range confs {
		if c != nil && c.HasEnded() {
			s.PastConfs++
			ended[c.Tag] = true
		}
	}

	talks, err := ListTalks(ctx)
	if err != nil {
		return s, err
	}
	for _, talk := range talks {
		if talk == nil || !ended[talk.Event] {
			continue
		}
		if talk.Status == "Accepted" {
			s.PastTalks++
		}
	}

	attendees, err := siteStatsAttendees(ctx)
	if err != nil {
		return s, err
	}
	s.Attendees = attendees
	return s, nil
}

func siteStatsAttendees(ctx *config.AppContext) (int, error) {
	if UsePostgresBackend(ctx) {
		return siteStatsAttendeesPostgres(ctx)
	}
	return siteStatsAttendeesNotion(ctx)
}

// FetchSiteStats returns about-page counters.
func FetchSiteStats(ctx *config.AppContext) SiteStatsValues {
	if UsePostgresBackend(ctx) {
		s, err := siteStatsPostgres(ctx)
		if err != nil {
			ctx.Err.Printf("site stats aggregate: %s", err)
		} else {
			return s
		}
	}

	s, err := siteStatsDirect(ctx)
	if err != nil {
		ctx.Err.Printf("site stats direct: %s", err)
	}
	return s
}
