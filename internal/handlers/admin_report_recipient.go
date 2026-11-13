package handlers

import (
	"btcpp-web/internal/config"
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type reportSponsor struct {
	ID, Name, Logo, Level string
	Confirmed             bool
}

func loadReportSponsors(ctx context.Context, app *config.AppContext, p *sponsorReportPage) error {
	rows, err := app.DB.Query(ctx, `SELECT o.id::text,o.name,coalesce(nullif(o.logo_light_url,''),o.logo_dark_url,''),
 coalesce(array_agg(DISTINCT coalesce(s.level,'')) FILTER (WHERE lower(trim(s.status)) IN ('paid','committed')),'{}'::text[])
 FROM organizations o JOIN sponsorships s ON s.organization_id=o.id JOIN sponsorships_conferences sc ON sc.sponsorship_id=s.id
 WHERE sc.conference_id::text=$1 AND s.archived_at IS NULL
 GROUP BY o.id,o.name,o.logo_light_url,o.logo_dark_url ORDER BY o.name,o.id::text`, p.Event.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var sponsors []reportSponsor
	for rows.Next() {
		var s reportSponsor
		var levels []string
		if err = rows.Scan(&s.ID, &s.Name, &s.Logo, &levels); err != nil {
			return err
		}
		s.Confirmed = len(levels) > 0
		// An organization can have several sponsorships; use its highest
		// confirmed tier for this event, never an unconfirmed upgrade.
		for _, level := range levels {
			if normalized := normalizeLevel(level); normalized != "" && tierRank(normalized) < tierRank(s.Level) {
				s.Level = normalized
			}
		}
		s.Logo = reportRecipientLogo(s)
		sponsors = append(sponsors, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	p.Sponsors = sponsors
	return nil
}

// Match the public event roster without limiting acknowledgments to the recipient.
func (p sponsorReportPage) EventSponsors() []reportSponsor {
	var sponsors []reportSponsor
	for _, sponsor := range p.Sponsors {
		if sponsor.Confirmed {
			sponsors = append(sponsors, sponsor)
		}
	}
	sort.Slice(sponsors, func(i, j int) bool {
		a, b := sponsors[i], sponsors[j]
		if ar, br := a.DisplayRank(), b.DisplayRank(); ar != br {
			return ar < br
		}
		if ar, br := tierRank(normalizeLevel(a.Level)), tierRank(normalizeLevel(b.Level)); ar != br {
			return ar < br
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	return sponsors
}

func (s reportSponsor) DisplayRank() int { return sponsorDisplayRank(s.Level) }

type reportSponsorRow struct {
	Rank     int
	Sponsors []reportSponsor
}

type reportSponsorPage struct {
	Rows []reportSponsorRow
}

func (p sponsorReportPage) SponsorPages() []reportSponsorPage {
	sponsors := p.EventSponsors()
	var pages []reportSponsorPage
	var page reportSponsorPage
	weight, count := 0, 0
	// Row capacity follows the card widths. Larger tiers consume more of
	// the page's height budget in both paper and slide editions.
	columns := [...]int{0, 2, 2, 3, 4, 5}
	rowWeight := [...]int{0, 5, 4, 3, 2, 2}
	for len(sponsors) > 0 {
		rank := sponsors[0].DisplayRank()
		n := 1
		for n < min(columns[rank], len(sponsors)) && sponsors[n].DisplayRank() == rank {
			n++
		}
		if weight+rowWeight[rank] > 16 || count+n > 12 {
			pages = append(pages, page)
			page, weight, count = reportSponsorPage{}, 0, 0
		}
		page.Rows = append(page.Rows, reportSponsorRow{Rank: rank, Sponsors: sponsors[:n]})
		weight += rowWeight[rank]
		count += n
		sponsors = sponsors[n:]
	}
	if len(page.Rows) > 0 {
		pages = append(pages, page)
	}
	return pages
}
func reportSponsorLogo(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if u.Scheme == "https" && u.Host != "" && u.User == nil {
		return u.String()
	}
	if u.Scheme == "" && u.Host == "" && strings.HasPrefix(u.Path, "/") && !strings.HasPrefix(raw, "//") {
		return u.String()
	}
	return ""
}
func (p sponsorReportPage) SelectedSponsor() reportSponsor {
	id := p.Saved["prepared_for_org_id"]
	for _, s := range p.Sponsors {
		if (id != "" && s.ID == id) || (id == "" && strings.EqualFold(strings.TrimSpace(p.Saved["prepared_for"]), s.Name)) {
			return s
		}
	}
	return reportSponsor{}
}
func reportRecipientLogo(s reportSponsor) string {
	// Btrust's bundled mark is white; use its dark counterpart on report paper.
	if strings.EqualFold(strings.TrimSpace(s.Name), "Btrust") {
		return "/static/img/sponsors/btrust-dark.svg"
	}
	return reportSponsorLogo(s.Logo)
}
func (p sponsorReportPage) RecipientLogo() string { return reportRecipientLogo(p.SelectedSponsor()) }
func saveReportRecipient(form url.Values, p *sponsorReportPage, saved map[string]string) error {
	if !form.Has("prepared_for_org_id") {
		saved["prepared_for_org_id"] = p.Saved["prepared_for_org_id"]
		return nil
	}
	id := form.Get("prepared_for_org_id")
	if id == "" {
		saved["prepared_for"] = ""
		saved["prepared_for_org_id"] = ""
		return nil
	}
	// Preserve a legacy free-text recipient until the editor explicitly replaces it.
	if id == "legacy" && p.SelectedSponsor().ID == "" && p.Saved["prepared_for"] != "" {
		saved["prepared_for"] = p.Saved["prepared_for"]
		saved["prepared_for_org_id"] = p.Saved["prepared_for_org_id"]
		return nil
	}
	for _, s := range p.Sponsors {
		if s.ID == id {
			saved["prepared_for_org_id"] = s.ID
			saved["prepared_for"] = s.Name
			return nil
		}
	}
	return fmt.Errorf("choose a sponsor attached to this event")
}
