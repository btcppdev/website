package getters

import (
	"fmt"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// ListConferenceAttendeeSponsors loads public sponsor affiliations for an
// already-public attendee list. Membership must be active and the sponsorship
// must be Paid or Committed for this conference, as on the sponsor section.
func ListConferenceAttendeeSponsors(ctx *config.AppContext, conferenceID string, personIDs []string) (map[string][]*types.Org, error) {
	if strings.TrimSpace(conferenceID) == "" || len(personIDs) == 0 {
		return nil, nil
	}
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT m.person_id::text, o.id::text, o.name, o.logo_light_url, o.logo_dark_url
		FROM organization_memberships m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.person_id = ANY($2::uuid[])
			AND m.status = 'active'
			AND (btrim(o.logo_light_url) <> '' OR btrim(o.logo_dark_url) <> '')
			AND EXISTS (
				SELECT 1 FROM sponsorships s
				JOIN sponsorships_conferences sc ON sc.sponsorship_id = s.id
				WHERE s.organization_id = o.id
					AND sc.conference_id::text = $1
					AND s.archived_at IS NULL
					AND lower(btrim(s.status)) IN ('paid', 'committed')
			)
		ORDER BY lower(o.name), o.id
	`, conferenceID, personIDs)
	if err != nil {
		return nil, fmt.Errorf("list conference attendee sponsors: %w", err)
	}
	defer rows.Close()
	result := make(map[string][]*types.Org)
	for rows.Next() {
		var personID string
		org := new(types.Org)
		if err := rows.Scan(&personID, &org.Ref, &org.Name, &org.LogoLight, &org.LogoDark); err != nil {
			return nil, fmt.Errorf("scan conference attendee sponsor: %w", err)
		}
		org.LogoLight = strings.TrimSpace(org.LogoLight)
		org.LogoDark = strings.TrimSpace(org.LogoDark)
		result[personID] = append(result[personID], org)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conference attendee sponsors: %w", err)
	}
	return result, nil
}
