package getters

import (
	"fmt"
	"sort"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func ConferenceEmailRecipients(ctx *config.AppContext, occurrence *types.ConferenceEmailOccurrence) ([]*types.ConferenceEmailRecipient, error) {
	if occurrence == nil {
		return nil, fmt.Errorf("conference email occurrence is required")
	}
	switch occurrence.Audience {
	case "attendees":
		return conferenceAttendeeRecipients(ctx, occurrence.ConferenceID)
	case "speakers":
		return conferenceSpeakerRecipients(ctx, occurrence.ConferenceID, occurrence.TargetKey)
	case "volunteers":
		return conferenceVolunteerRecipients(ctx, occurrence.ConferenceID, occurrence.ConferenceTag)
	default:
		return nil, fmt.Errorf("unsupported conference email audience %q", occurrence.Audience)
	}
}

func conferenceAttendeeRecipients(ctx *config.AppContext, confID string) ([]*types.ConferenceEmailRecipient, error) {
	registrations, err := FetchRegistrations(ctx, confID)
	if err != nil {
		return nil, err
	}
	byEmail := make(map[string]*types.ConferenceEmailRecipient)
	for _, registration := range registrations {
		if registration == nil || registration.Revoked {
			continue
		}
		email := strings.ToLower(strings.TrimSpace(registration.Email))
		if email == "" {
			continue
		}
		recipient := byEmail[email]
		if recipient == nil {
			recipient = &types.ConferenceEmailRecipient{Key: "attendee:" + email, Email: email}
			byEmail[email] = recipient
		}
		recipient.Registrations = append(recipient.Registrations, registration)
	}
	return sortedConferenceRecipients(byEmail), nil
}

func conferenceSpeakerRecipients(ctx *config.AppContext, confID, targetSpeakerConfID string) ([]*types.ConferenceEmailRecipient, error) {
	query := `
		SELECT sc.id::text, p.name, pe.email::text
		FROM speaker_confs sc
		JOIN people p ON p.id = sc.speaker_id
		JOIN LATERAL (
			SELECT email FROM person_emails
			WHERE person_id = p.id
			ORDER BY is_primary DESC, created_at, id LIMIT 1
		) pe ON true
		JOIN speaker_confs_conferences scc ON scc.speaker_conf_id = sc.id
		WHERE scc.conference_id = $1::uuid AND sc.accepted_at IS NOT NULL`
	args := []any{confID}
	if strings.TrimSpace(targetSpeakerConfID) != "" {
		query += " AND sc.id = $2::uuid"
		args = append(args, targetSpeakerConfID)
	}
	query += " ORDER BY lower(p.name), sc.id"
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conference email speakers: %w", err)
	}
	defer rows.Close()
	byEmail := make(map[string]*types.ConferenceEmailRecipient)
	for rows.Next() {
		var speakerConfID, name, email string
		if err := rows.Scan(&speakerConfID, &name, &email); err != nil {
			return nil, fmt.Errorf("scan conference email speaker: %w", err)
		}
		normalized := strings.ToLower(strings.TrimSpace(email))
		if normalized == "" {
			continue
		}
		if existing := byEmail[normalized]; existing != nil {
			if existing.SpeakerConfID == "" {
				existing.SpeakerConfID = speakerConfID
			}
			continue
		}
		byEmail[normalized] = &types.ConferenceEmailRecipient{
			Key: "speaker:" + speakerConfID, Email: normalized, Name: name, SpeakerConfID: speakerConfID,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sortedConferenceRecipients(byEmail), nil
}

func conferenceVolunteerRecipients(ctx *config.AppContext, confID, confTag string) ([]*types.ConferenceEmailRecipient, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT DISTINCT v.id::text, p.name, pe.email::text
		FROM volunteers v
		JOIN people p ON p.id = v.person_id
		JOIN LATERAL (
			SELECT email FROM person_emails
			WHERE person_id = p.id
			ORDER BY is_primary DESC, created_at, id LIMIT 1
		) pe ON true
		JOIN volunteers_conferences vc ON vc.volunteer_id = v.id
		WHERE vc.conference_id = $1::uuid AND vc.kind = 'schedule_for'
			AND (
				lower(v.status) = 'scheduled'
				OR EXISTS (
					SELECT 1 FROM work_shifts_volunteers wsv
					JOIN work_shifts ws ON ws.id = wsv.shift_id
					WHERE wsv.volunteer_id = v.id AND ws.conference_id = $1::uuid
				)
			)
	`, confID)
	if err != nil {
		return nil, fmt.Errorf("list scheduled conference volunteers: %w", err)
	}
	byEmail := make(map[string]*types.ConferenceEmailRecipient)
	for rows.Next() {
		var volunteerID, name, email string
		if err := rows.Scan(&volunteerID, &name, &email); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan scheduled conference volunteer: %w", err)
		}
		email = strings.ToLower(strings.TrimSpace(email))
		if email != "" {
			byEmail[email] = &types.ConferenceEmailRecipient{Key: "volunteer:" + volunteerID, Email: email, Name: name, VolunteerID: volunteerID}
		}
	}
	rows.Close()
	roles := []string{
		confTag + "-volcoord", confTag + "-staff", confTag + "-admin",
		"global-volcoord", "global-staff", "global-admin",
	}
	people, err := ListSpeakersWithAnyRole(ctx, roles)
	if err != nil {
		return nil, fmt.Errorf("list conference email staff: %w", err)
	}
	for _, person := range people {
		if person == nil {
			continue
		}
		email := strings.ToLower(strings.TrimSpace(person.Email))
		if email == "" {
			continue
		}
		if existing := byEmail[email]; existing != nil {
			continue
		}
		byEmail[email] = &types.ConferenceEmailRecipient{Key: "staff:" + person.ID, Email: email, Name: person.Name}
	}
	return sortedConferenceRecipients(byEmail), nil
}

func sortedConferenceRecipients(byEmail map[string]*types.ConferenceEmailRecipient) []*types.ConferenceEmailRecipient {
	out := make([]*types.ConferenceEmailRecipient, 0, len(byEmail))
	for _, recipient := range byEmail {
		out = append(out, recipient)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out
}
