package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"fmt"
	"strings"
)

// ListConferenceCommittee includes accepted steering members, independently of
// ticket purchases or talks. Affiliations need an active membership, not a sponsorship.
func ListConferenceCommittee(ctx *config.AppContext, confID string) ([]*types.ConferenceCommitteeMember, error) {
	if strings.TrimSpace(confID) == "" {
		return nil, nil
	}
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
  SELECT p.id::text, p.name, p.norm_photo_path, p.company, p.org_logo_path, m.track,
   coalesce(o.id::text,''), coalesce(o.name,''), coalesce(o.logo_light_url,''), coalesce(o.logo_dark_url,'')
  FROM event_team_members m
  JOIN people p ON p.id=m.person_id
  LEFT JOIN organization_memberships om ON om.person_id=p.id AND om.status='active'
  LEFT JOIN organizations o ON o.id=om.organization_id
  WHERE m.conference_id=$1::uuid AND m.role='steering' AND m.accepted_at IS NOT NULL
   AND NOT p.is_deleted_account
  ORDER BY lower(p.name), p.id, lower(o.name), o.id
 `, confID)
	if err != nil {
		return nil, fmt.Errorf("list steering committee: %w", err)
	}
	defer rows.Close()
	var members []*types.ConferenceCommitteeMember
	byPerson := map[string]*types.ConferenceCommitteeMember{}
	for rows.Next() {
		person := new(types.Speaker)
		org := new(types.Org)
		var track string
		if err := rows.Scan(&person.ID, &person.Name, &person.Photo, &person.Company, &person.OrgLogo, &track, &org.Ref, &org.Name, &org.LogoLight, &org.LogoDark); err != nil {
			return nil, fmt.Errorf("scan steering committee: %w", err)
		}
		member := byPerson[person.ID]
		if member == nil {
			member = &types.ConferenceCommitteeMember{Speaker: person, Track: strings.TrimSpace(track)}
			byPerson[person.ID] = member
			members = append(members, member)
		}
		if org.Ref != "" {
			org.LogoLight = strings.TrimSpace(org.LogoLight)
			org.LogoDark = strings.TrimSpace(org.LogoDark)
			member.Organizations = append(member.Organizations, org)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate steering committee: %w", err)
	}
	return members, nil
}

func normalizeSteeringTrack(track string) (string, error) {
	track = strings.TrimSpace(track)
	if len([]rune(track)) > 120 {
		return "", fmt.Errorf("use 120 characters or fewer for the track name")
	}
	return track, nil
}

// UpdateEventSteeringTrack cannot change team roles or another event's members.
func UpdateEventSteeringTrack(ctx *config.AppContext, confID, memberID, track string) error {
	track, err := normalizeSteeringTrack(track)
	if err != nil {
		return err
	}
	result, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE event_team_members SET track=$3 WHERE conference_id=$1::uuid AND id=$2::uuid AND role='steering'`, confID, memberID, track)
	if err != nil {
		return fmt.Errorf("update steering track: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("steering member not found in this event")
	}
	return nil
}
