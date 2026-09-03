package getters

import (
	"fmt"
	"sync"
	"time"

	"btcpp-web/internal/config"
)

const siteStatsCacheTTL = 5 * time.Minute

var siteStatsCache = struct {
	sync.Mutex
	app     *config.AppContext
	value   SiteStatsValues
	valid   bool
	expires time.Time
}{}

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
	if ctx == nil || ctx.DB == nil {
		return 0, fmt.Errorf("database is not configured")
	}
	var count int
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT count(*)
		FROM registrations
	`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count registrations: %w", err)
	}
	return count, nil
}

func siteStatsFromDatabase(ctx *config.AppContext) (SiteStatsValues, error) {
	if ctx == nil || ctx.DB == nil {
		return SiteStatsValues{}, fmt.Errorf("database is not configured")
	}
	var s SiteStatsValues
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT
			(SELECT count(*) FROM conferences WHERE end_date IS NOT NULL AND end_date < now()),
			(
				SELECT count(*)
				FROM conf_talks ct
				JOIN conferences c ON c.id = ct.conference_id
				JOIN proposals p ON p.id = ct.proposal_id
				WHERE c.end_date IS NOT NULL
					AND c.end_date < now()
					AND p.status = 'Accepted'
			),
			(SELECT count(*) FROM registrations)
	`).Scan(&s.PastConfs, &s.PastTalks, &s.Attendees); err != nil {
		return SiteStatsValues{}, fmt.Errorf("query site stats: %w", err)
	}
	return s, nil
}

// FetchSiteStats returns about-page counters.
func FetchSiteStats(ctx *config.AppContext) SiteStatsValues {
	if ctx != nil && ctx.InProduction {
		siteStatsCache.Lock()
		defer siteStatsCache.Unlock()
		if siteStatsCache.app == ctx && siteStatsCache.valid && time.Now().Before(siteStatsCache.expires) {
			return siteStatsCache.value
		}
	}
	s, err := siteStatsFromDatabase(ctx)
	if err != nil {
		ctx.Err.Printf("site stats aggregate: %s", err)
	} else {
		if ctx != nil && ctx.InProduction {
			siteStatsCache.app = ctx
			siteStatsCache.value = s
			siteStatsCache.valid = true
			siteStatsCache.expires = time.Now().Add(siteStatsCacheTTL)
		}
		return s
	}
	if ctx != nil && ctx.InProduction && siteStatsCache.app == ctx && siteStatsCache.valid {
		siteStatsCache.expires = time.Now().Add(30 * time.Second)
		return siteStatsCache.value
	}

	s, err = siteStatsDirect(ctx)
	if err != nil {
		ctx.Err.Printf("site stats direct: %s", err)
	}
	return s
}
