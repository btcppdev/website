package handlers

import (
	"btcpp-web/internal/config"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type annualReportRecipient struct {
	Key, ID, Name, Logo string
}

func annualRecipientKey(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "NYDIG", nil
	}
	if key := strings.ToUpper(raw); key == "NYDIG" || key == "HRF" {
		return key, nil // Preserve existing draft and frozen-edition URLs.
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return "", fmt.Errorf("choose an organization from the recipient list")
	}
	return id.String(), nil
}

func loadAnnualReportRecipients(ctx context.Context, app *config.AppContext) ([]annualReportRecipient, error) {
	// Reporting recipients need not be sponsors, or have a particular tier/status.
	rows, err := app.DB.Query(ctx, `SELECT id::text,name,coalesce(nullif(logo_light_url,''),logo_dark_url,'') FROM organizations ORDER BY lower(name),id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var recipients []annualReportRecipient
	legacy := map[string]bool{}
	for rows.Next() {
		var recipient annualReportRecipient
		if err := rows.Scan(&recipient.ID, &recipient.Name, &recipient.Logo); err != nil {
			return nil, err
		}
		recipient.Key = recipient.ID
		key := strings.ToUpper(strings.TrimSpace(recipient.Name))
		if (key == "NYDIG" || key == "HRF") && !legacy[key] {
			recipient.Key, recipient.Name = key, key
			legacy[key] = true
		}
		recipient.Logo = reportRecipientLogo(reportSponsor{Name: recipient.Name, Logo: recipient.Logo})
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Legacy editions remain accessible even if their organization is removed.
	for _, name := range []string{"NYDIG", "HRF"} {
		if !legacy[name] {
			recipients = append(recipients, annualReportRecipient{Key: name, Name: name})
		}
	}
	return recipients, nil
}

func (p annualReportPage) RecipientValue() string {
	if p.RecipientKey != "" {
		return p.RecipientKey
	}
	return p.Recipient // Editions frozen before organization recipients.
}

func (p annualReportPage) RecipientOptions() []annualReportRecipient {
	options := append([]annualReportRecipient(nil), p.Recipients...)
	if len(options) == 0 {
		options = []annualReportRecipient{{Key: "NYDIG", Name: "NYDIG"}, {Key: "HRF", Name: "HRF"}}
	}
	found := false
	for i := range options {
		if options[i].Key == p.RecipientValue() {
			options[i].Name = p.Recipient
			found = true
		}
	}
	if !found {
		options = append(options, annualReportRecipient{Key: p.RecipientValue(), ID: p.RecipientOrganizationID, Name: p.Recipient, Logo: p.RecipientLogoURL})
	}
	sort.SliceStable(options, func(i, j int) bool { return strings.ToLower(options[i].Name) < strings.ToLower(options[j].Name) })
	return options
}

func (p *annualReportPage) selectRecipient(key string) bool {
	for _, recipient := range p.Recipients {
		if recipient.Key == key || recipient.ID == key {
			p.RecipientKey, p.RecipientOrganizationID = key, recipient.ID
			p.Recipient, p.RecipientLogoURL = recipient.Name, recipient.Logo
			return true
		}
	}
	return false
}

func (p annualReportPage) ClosingDeck() string {
	if len(p.PartnerEvents) > 0 || p.RecipientValue() == "NYDIG" || p.RecipientValue() == "HRF" {
		return "For backing the people building Bitcoin."
	}
	return "A year of people, projects, and ideas."
}
