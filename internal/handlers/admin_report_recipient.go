package handlers

import (
	"btcpp-web/internal/config"
	"context"
	"fmt"
	"net/url"
	"strings"
)

type reportSponsor struct{ ID, Name, Logo string }

func loadReportSponsors(ctx context.Context, app *config.AppContext, p *sponsorReportPage) error {
	rows, err := app.DB.Query(ctx, `SELECT DISTINCT o.id::text,o.name,coalesce(nullif(o.logo_light_url,''),o.logo_dark_url,'')
 FROM organizations o JOIN sponsorships s ON s.organization_id=o.id JOIN sponsorships_conferences sc ON sc.sponsorship_id=s.id
 WHERE sc.conference_id::text=$1 AND s.archived_at IS NULL ORDER BY o.name,o.id::text`, p.Event.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s reportSponsor
		if err = rows.Scan(&s.ID, &s.Name, &s.Logo); err != nil {
			return err
		}
		s.Logo = reportSponsorLogo(s.Logo)
		p.Sponsors = append(p.Sponsors, s)
	}
	return rows.Err()
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
func (p sponsorReportPage) RecipientLogo() string { return reportSponsorLogo(p.SelectedSponsor().Logo) }
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
