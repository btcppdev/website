package getters

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func organizationInviteTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return fmt.Sprintf("%x", sum[:])
}

func CreateOrganizationMemberInvite(ctx *config.AppContext, organizationID, email, role, invitedByPersonID string, expiresAt time.Time) (string, *types.OrganizationMemberInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return "", nil, fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	email = strings.ToLower(strings.TrimSpace(email))
	role = strings.ToLower(strings.TrimSpace(role))
	if organizationID == "" || email == "" {
		return "", nil, fmt.Errorf("organization and email are required")
	}
	parsedEmail, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(parsedEmail.Address, email) {
		return "", nil, fmt.Errorf("a valid invitation email is required")
	}
	if role != OrganizationRoleManager && role != OrganizationRoleMember {
		return "", nil, fmt.Errorf("invite role must be manager or member")
	}
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(72 * time.Hour)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate organization invite: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return "", nil, fmt.Errorf("begin organization member invite: %w", err)
	}
	defer tx.Rollback(dbctx)
	var alreadyMember bool
	if err := tx.QueryRow(dbctx, `
		SELECT EXISTS (
			SELECT 1
			FROM organization_memberships memberships
			JOIN person_emails ON person_emails.person_id = memberships.person_id
			WHERE memberships.organization_id = $1::uuid
			  AND memberships.status = 'active'
			  AND person_emails.email = $2::citext
		)
	`, organizationID, email).Scan(&alreadyMember); err != nil {
		return "", nil, fmt.Errorf("check organization membership before invite: %w", err)
	}
	if alreadyMember {
		return "", nil, fmt.Errorf("that email already belongs to an active organization member")
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE organization_member_invites
		SET revoked_at = now()
		WHERE organization_id = $1::uuid AND email = $2::citext
		  AND accepted_at IS NULL AND revoked_at IS NULL
	`, organizationID, email); err != nil {
		return "", nil, fmt.Errorf("supersede organization member invites: %w", err)
	}
	invite := &types.OrganizationMemberInvite{
		OrganizationID:    organizationID,
		Email:             email,
		Role:              role,
		InvitedByPersonID: strings.TrimSpace(invitedByPersonID),
		ExpiresAt:         expiresAt,
	}
	err = tx.QueryRow(dbctx, `
		INSERT INTO organization_member_invites (
			organization_id, email, role, token_hash,
			invited_by_person_id, expires_at
		) VALUES ($1::uuid, $2::citext, $3, $4, NULLIF($5, '')::uuid, $6)
		RETURNING id::text, created_at
	`, organizationID, email, role, organizationInviteTokenHash(token),
		invite.InvitedByPersonID, expiresAt).Scan(&invite.ID, &invite.CreatedAt)
	if err != nil {
		return "", nil, fmt.Errorf("create organization member invite: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return "", nil, fmt.Errorf("commit organization member invite: %w", err)
	}
	return token, invite, nil
}

func GetOrganizationMemberInviteByToken(ctx *config.AppContext, token string) (*types.OrganizationMemberInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var invite types.OrganizationMemberInvite
	var acceptedAt, revokedAt pgtype.Timestamptz
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT invites.id::text, invites.organization_id::text,
			organizations.name, invites.email::text, invites.role,
			coalesce(invites.invited_by_person_id::text, ''),
			coalesce(invites.accepted_by_person_id::text, ''),
			invites.accepted_at, invites.revoked_at, invites.expires_at,
			invites.created_at
		FROM organization_member_invites invites
		JOIN organizations ON organizations.id = invites.organization_id
		WHERE invites.token_hash = $1
	`, organizationInviteTokenHash(token)).Scan(
		&invite.ID, &invite.OrganizationID, &invite.OrganizationName,
		&invite.Email, &invite.Role, &invite.InvitedByPersonID,
		&invite.AcceptedByPersonID, &acceptedAt, &revokedAt,
		&invite.ExpiresAt, &invite.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load organization member invite: %w", err)
	}
	if acceptedAt.Valid {
		value := acceptedAt.Time
		invite.AcceptedAt = &value
	}
	if revokedAt.Valid {
		value := revokedAt.Time
		invite.RevokedAt = &value
	}
	return &invite, nil
}

func AcceptOrganizationMemberInvite(ctx *config.AppContext, token, personID string) (*types.OrganizationMemberInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin accept organization invite: %w", err)
	}
	defer tx.Rollback(dbctx)

	var invite types.OrganizationMemberInvite
	var acceptedAt, revokedAt pgtype.Timestamptz
	err = tx.QueryRow(dbctx, `
		SELECT id::text, organization_id::text, email::text, role,
			expires_at, accepted_at, revoked_at
		FROM organization_member_invites
		WHERE token_hash = $1
		FOR UPDATE
	`, organizationInviteTokenHash(token)).Scan(
		&invite.ID, &invite.OrganizationID, &invite.Email, &invite.Role,
		&invite.ExpiresAt, &acceptedAt, &revokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("organization invitation not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load organization invitation: %w", err)
	}
	if acceptedAt.Valid {
		return nil, fmt.Errorf("organization invitation was already used")
	}
	if revokedAt.Valid || !invite.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("organization invitation is no longer valid")
	}
	var emailMatches bool
	if err := tx.QueryRow(dbctx, `
		SELECT EXISTS (
			SELECT 1 FROM person_emails
			WHERE person_id = $1::uuid AND email = $2::citext
				AND verified_at IS NOT NULL
		)
	`, personID, invite.Email).Scan(&emailMatches); err != nil {
		return nil, fmt.Errorf("verify organization invitation email: %w", err)
	}
	if !emailMatches {
		return nil, fmt.Errorf("the signed-in account does not have the invited verified email")
	}
	commandTag, err := tx.Exec(dbctx, `
		INSERT INTO organization_memberships (organization_id, person_id, role, status)
		VALUES ($1::uuid, $2::uuid, $3, 'active')
		ON CONFLICT (organization_id, person_id) DO UPDATE SET
			role = EXCLUDED.role, status = 'active'
		WHERE organization_memberships.status = 'removed'
	`, invite.OrganizationID, personID, invite.Role)
	if err != nil {
		return nil, fmt.Errorf("activate organization membership: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return nil, fmt.Errorf("that account is already an active organization member")
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE organization_member_invites
		SET accepted_by_person_id = $2::uuid, accepted_at = now()
		WHERE id = $1::uuid
	`, invite.ID, personID); err != nil {
		return nil, fmt.Errorf("consume organization invitation: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit organization invitation: %w", err)
	}
	return &invite, nil
}

const (
	OrganizationRoleOwner   = "owner"
	OrganizationRoleManager = "manager"
	OrganizationRoleMember  = "member"
)

const SponsorContactConsentPolicyVersion = "sponsor-contact-v1"

func HasActiveOrganizationMembership(ctx *config.AppContext, personID string) (bool, error) {
	if ctx == nil || ctx.DB == nil {
		return false, fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return false, nil
	}
	var exists bool
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT EXISTS (
			SELECT 1 FROM organization_memberships
			WHERE person_id = $1::uuid AND status = 'active'
		)
	`, personID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check active organization membership: %w", err)
	}
	return exists, nil
}

func ListOrganizationMembershipsForPerson(ctx *config.AppContext, personID string) ([]*types.OrganizationMembership, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT memberships.organization_id::text, memberships.person_id::text,
			memberships.role, memberships.status,
			coalesce(memberships.invited_by_person_id::text, ''),
			memberships.created_at, memberships.updated_at,
			organizations.id::text, organizations.name, organizations.tagline,
			organizations.logo_light_url, organizations.logo_dark_url,
			coalesce(organizations.email::text, ''), organizations.website_url,
			organizations.github_url, organizations.twitter_handle,
			organizations.nostr, organizations.matrix, organizations.linkedin_url,
			organizations.instagram_url, organizations.youtube_url,
			organizations.hiring
		FROM organization_memberships memberships
		JOIN organizations ON organizations.id = memberships.organization_id
		WHERE memberships.person_id = $1::uuid
			AND memberships.status = 'active'
		ORDER BY organizations.name
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list organization memberships for person %s: %w", personID, err)
	}
	defer rows.Close()

	var out []*types.OrganizationMembership
	for rows.Next() {
		membership := &types.OrganizationMembership{Organization: &types.Org{}}
		var twitterHandle string
		if err := rows.Scan(
			&membership.OrganizationID, &membership.PersonID, &membership.Role,
			&membership.Status, &membership.InvitedByPersonID,
			&membership.CreatedAt, &membership.UpdatedAt,
			&membership.Organization.Ref, &membership.Organization.Name,
			&membership.Organization.Tagline, &membership.Organization.LogoLight,
			&membership.Organization.LogoDark, &membership.Organization.Email,
			&membership.Organization.Website, &membership.Organization.Github,
			&twitterHandle, &membership.Organization.Nostr,
			&membership.Organization.Matrix, &membership.Organization.LinkedIn,
			&membership.Organization.Instagram, &membership.Organization.Youtube,
			&membership.Organization.Hiring,
		); err != nil {
			return nil, fmt.Errorf("scan organization membership: %w", err)
		}
		membership.Organization.Twitter = types.ParseTwitter(twitterHandle)
		out = append(out, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate organization memberships: %w", err)
	}
	return out, nil
}

func GetOrganizationMembership(ctx *config.AppContext, personID, organizationID string) (*types.OrganizationMembership, error) {
	memberships, err := ListOrganizationMembershipsForPerson(ctx, personID)
	if err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		if membership != nil && membership.OrganizationID == strings.TrimSpace(organizationID) {
			return membership, nil
		}
	}
	return nil, nil
}

func ListOrganizationMembers(ctx *config.AppContext, organizationID string) ([]*types.OrganizationMembership, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT memberships.organization_id::text, memberships.person_id::text,
			memberships.role, memberships.status,
			coalesce(memberships.invited_by_person_id::text, ''),
			memberships.created_at, memberships.updated_at,
			people.name, coalesce(primary_email.email::text, '')
		FROM organization_memberships memberships
		JOIN people ON people.id = memberships.person_id
		LEFT JOIN LATERAL (
			SELECT person_emails.email
			FROM person_emails
			WHERE person_emails.person_id = people.id
			ORDER BY person_emails.is_primary DESC, person_emails.verified_at DESC NULLS LAST
			LIMIT 1
		) primary_email ON true
		WHERE memberships.organization_id = $1::uuid
			AND memberships.status <> 'removed'
		ORDER BY CASE memberships.role WHEN 'owner' THEN 0 WHEN 'manager' THEN 1 ELSE 2 END,
			people.name
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization members %s: %w", organizationID, err)
	}
	defer rows.Close()
	var out []*types.OrganizationMembership
	for rows.Next() {
		membership := &types.OrganizationMembership{}
		if err := rows.Scan(
			&membership.OrganizationID, &membership.PersonID, &membership.Role,
			&membership.Status, &membership.InvitedByPersonID,
			&membership.CreatedAt, &membership.UpdatedAt,
			&membership.PersonName, &membership.PersonEmail,
		); err != nil {
			return nil, fmt.Errorf("scan organization member: %w", err)
		}
		out = append(out, membership)
	}
	return out, rows.Err()
}

func RemoveOrganizationMembership(ctx *config.AppContext, organizationID, actorPersonID, targetPersonID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	actorPersonID = strings.TrimSpace(actorPersonID)
	targetPersonID = strings.TrimSpace(targetPersonID)
	if organizationID == "" || actorPersonID == "" || targetPersonID == "" {
		return fmt.Errorf("organization, actor, and member are required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin organization membership removal: %w", err)
	}
	defer tx.Rollback(dbctx)

	rows, err := tx.Query(dbctx, `
		SELECT person_id::text, role
		FROM organization_memberships
		WHERE organization_id = $1::uuid AND status = 'active'
		FOR UPDATE
	`, organizationID)
	if err != nil {
		return fmt.Errorf("lock organization memberships: %w", err)
	}
	roles := make(map[string]string)
	owners := 0
	for rows.Next() {
		var personID, role string
		if err := rows.Scan(&personID, &role); err != nil {
			rows.Close()
			return fmt.Errorf("scan organization membership lock: %w", err)
		}
		roles[personID] = role
		if role == OrganizationRoleOwner {
			owners++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate organization membership lock: %w", err)
	}
	rows.Close()

	actorRole, actorActive := roles[actorPersonID]
	targetRole, targetActive := roles[targetPersonID]
	if !actorActive {
		return fmt.Errorf("you are not an active member of this organization")
	}
	if !targetActive {
		return fmt.Errorf("that person is not an active organization member")
	}
	removingSelf := actorPersonID == targetPersonID
	if !removingSelf {
		allowed := actorRole == OrganizationRoleOwner ||
			(actorRole == OrganizationRoleManager && targetRole == OrganizationRoleMember)
		if !allowed {
			return fmt.Errorf("your organization role cannot remove that member")
		}
	}
	if targetRole == OrganizationRoleOwner && owners <= 1 {
		return fmt.Errorf("the organization's last active owner cannot be removed")
	}
	commandTag, err := tx.Exec(dbctx, `
		UPDATE organization_memberships
		SET status = 'removed', updated_at = now()
		WHERE organization_id = $1::uuid AND person_id = $2::uuid
		  AND status = 'active'
	`, organizationID, targetPersonID)
	if err != nil {
		return fmt.Errorf("remove organization membership: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("organization membership was not removed")
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit organization membership removal: %w", err)
	}
	return nil
}

func ListSponsorDashboardEvents(ctx *config.AppContext, organizationID string) ([]*types.SponsorDashboardEvent, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT sponsorships.id::text, sponsorships.name, sponsorships.level,
			sponsorships.label, sponsorships.status, sponsorships.is_vendor,
			conferences.id::text,
			coalesce(entitlements.ticket_allocation, 0),
			coalesce(entitlements.sponsor_award_limit, 0),
			coalesce(entitlements.all_hackathon_submissions_access, false),
			coalesce(entitlements.automatic_submission_contact_access, false),
			coalesce(entitlements.participant_contact_access, false),
			coalesce(entitlements.participant_contact_export, false),
			coalesce(entitlements.can_edit_organization, false),
			coalesce(entitlements.created_at, sponsorships.created_at),
			coalesce(entitlements.updated_at, sponsorships.updated_at),
			coalesce(competitions.id::text, ''), coalesce(competitions.title, ''),
			(SELECT count(*) FROM awards
			 JOIN competitions ON competitions.id = awards.competition_id
			 WHERE competitions.conference_id = conferences.id
			   AND awards.sponsored_by_org_id = sponsorships.organization_id
			   AND awards.archived_at IS NULL),
			(SELECT count(DISTINCT project_awards.project_id) FROM project_awards
			 JOIN awards ON awards.id = project_awards.award_id
			 JOIN competitions ON competitions.id = awards.competition_id
			 WHERE competitions.conference_id = conferences.id
			   AND awards.sponsored_by_org_id = sponsorships.organization_id
				   AND awards.archived_at IS NULL),
			(SELECT coalesce(sum(issuances.quantity), 0)
			 FROM sponsor_ticket_issuances issuances
			 WHERE issuances.sponsorship_id = sponsorships.id
			   AND issuances.conference_id = conferences.id)
		FROM sponsorships
		JOIN sponsorships_conferences links ON links.sponsorship_id = sponsorships.id
		JOIN conferences ON conferences.id = links.conference_id
		LEFT JOIN sponsorship_entitlements entitlements
		  ON entitlements.sponsorship_id = sponsorships.id
		 AND entitlements.conference_id = conferences.id
		LEFT JOIN competitions ON competitions.conference_id = conferences.id
		WHERE sponsorships.organization_id = $1::uuid
		  AND sponsorships.archived_at IS NULL
		ORDER BY conferences.start_date DESC NULLS LAST, sponsorships.created_at DESC
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list sponsor dashboard events %s: %w", organizationID, err)
	}
	defer rows.Close()

	confs, err := ListConfs(ctx)
	if err != nil {
		return nil, err
	}
	confByID := make(map[string]*types.Conf, len(confs))
	for _, conf := range confs {
		if conf != nil {
			confByID[conf.Ref] = conf
		}
	}
	var out []*types.SponsorDashboardEvent
	for rows.Next() {
		event := &types.SponsorDashboardEvent{
			Sponsorship: &types.Sponsorship{Org: &types.Org{Ref: organizationID}},
			Entitlement: &types.SponsorshipEntitlement{},
		}
		var conferenceID, competitionID, competitionTitle string
		if err := rows.Scan(
			&event.Sponsorship.Ref, &event.Sponsorship.Name,
			&event.Sponsorship.Level, &event.Sponsorship.Label,
			&event.Sponsorship.Status, &event.Sponsorship.IsVendor,
			&conferenceID,
			&event.Entitlement.TicketAllocation,
			&event.Entitlement.SponsorAwardLimit,
			&event.Entitlement.AllHackathonSubmissions,
			&event.Entitlement.AutomaticSubmissionContactAccess,
			&event.Entitlement.ParticipantContactAccess,
			&event.Entitlement.ParticipantContactExport,
			&event.Entitlement.CanEditOrganization,
			&event.Entitlement.CreatedAt, &event.Entitlement.UpdatedAt,
			&competitionID, &competitionTitle,
			&event.AwardCount, &event.WinnerCount, &event.TicketsIssued,
		); err != nil {
			return nil, fmt.Errorf("scan sponsor dashboard event: %w", err)
		}
		event.Conference = confByID[conferenceID]
		event.Entitlement.SponsorshipID = event.Sponsorship.Ref
		event.Entitlement.ConferenceID = conferenceID
		if event.Conference != nil {
			event.Sponsorship.Confs = []*types.Conf{event.Conference}
		}
		if competitionID != "" {
			event.Competition = &types.HackathonCompetition{ID: competitionID, ConferenceID: conferenceID, Title: competitionTitle}
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sponsor dashboard events: %w", err)
	}
	return out, nil
}

func ListSponsorPrizeEntries(ctx *config.AppContext, organizationID string, includeContacts bool) ([]*types.SponsorPrizeEntry, error) {
	return listSponsorPrizeEntries(ctx, organizationID, includeContacts, false)
}

func ListSponsorPrizeEntriesForExport(ctx *config.AppContext, organizationID string) ([]*types.SponsorPrizeEntry, error) {
	return listSponsorPrizeEntries(ctx, organizationID, true, true)
}

func listSponsorPrizeEntries(ctx *config.AppContext, organizationID string, includeContacts, requireExportAccess bool) ([]*types.SponsorPrizeEntry, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	if organizationID == "" {
		return nil, fmt.Errorf("organization is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		WITH sponsor_access AS (
			SELECT competitions.id AS competition_id,
				competitions.title AS competition_title,
				conferences.id AS conference_id, conferences.tag AS conference_tag,
				coalesce(nullif(conferences.description, ''), conferences.tag) AS conference_title,
				conferences.start_date AS conference_start,
				bool_or(entitlements.all_hackathon_submissions_access
					AND lower(sponsorships.status) IN ('paid', 'committed')) AS all_submissions,
				bool_or(entitlements.automatic_submission_contact_access
					AND lower(sponsorships.status) IN ('paid', 'committed')) AS automatic_contact,
				bool_or(entitlements.participant_contact_access
					AND lower(sponsorships.status) IN ('paid', 'committed')) AS contact_access,
				bool_or(entitlements.participant_contact_export
					AND lower(sponsorships.status) IN ('paid', 'committed')) AS export_access
			FROM sponsorships
			JOIN sponsorships_conferences links ON links.sponsorship_id = sponsorships.id
			JOIN sponsorship_entitlements entitlements
			  ON entitlements.sponsorship_id = sponsorships.id
			 AND entitlements.conference_id = links.conference_id
			JOIN conferences ON conferences.id = links.conference_id
			JOIN competitions ON competitions.conference_id = conferences.id
			WHERE sponsorships.organization_id = $1::uuid
			  AND sponsorships.archived_at IS NULL
			GROUP BY competitions.id, competitions.title,
				conferences.id, conferences.tag, conferences.description, conferences.start_date
		), visible_entries AS (
			SELECT awards.id::text AS award_id, awards.title AS award_title,
				awards.status AS award_status, access.competition_id,
				access.competition_title, access.conference_id,
				access.conference_tag, access.conference_title, access.conference_start,
				projects.id AS project_id, projects.title AS project_title,
				projects.short_description, projects.image_url, projects.status AS project_status,
				projects.project_number, projects.github_url, projects.demo_url,
				opt_ins.opted_in_at AS entered_at,
				EXISTS (
					SELECT 1 FROM project_awards winners
					WHERE winners.project_id = projects.id AND winners.award_id = awards.id
				) AS winner,
				true AS sponsored_prize, access.automatic_contact, access.contact_access
			FROM sponsor_access access
			JOIN awards ON awards.competition_id = access.competition_id
			 AND awards.sponsored_by_org_id = $1::uuid
			 AND awards.archived_at IS NULL
			JOIN project_award_opt_ins opt_ins ON opt_ins.award_id = awards.id
			JOIN projects ON projects.id = opt_ins.project_id
			WHERE projects.status <> 'hidden'
			  AND (NOT $3::boolean OR (
				access.export_access
				AND projects.status IN ('submitted', 'advanced')
			  ))
			UNION ALL
			SELECT '' AS award_id, '' AS award_title, '' AS award_status,
				access.competition_id, access.competition_title,
				access.conference_id, access.conference_tag, access.conference_title,
				access.conference_start,
				projects.id, projects.title, projects.short_description,
				projects.image_url, projects.status, projects.project_number,
				projects.github_url, projects.demo_url,
				coalesce(projects.submitted_at, projects.updated_at),
				false AS winner, false AS sponsored_prize,
				access.automatic_contact, access.contact_access
			FROM sponsor_access access
			JOIN projects ON projects.competition_id = access.competition_id
			WHERE access.all_submissions
			  AND (NOT $3::boolean OR access.export_access)
			  AND projects.status IN ('submitted', 'advanced')
			  AND NOT EXISTS (
				SELECT 1
				FROM awards
				JOIN project_award_opt_ins opt_ins ON opt_ins.award_id = awards.id
				WHERE awards.competition_id = access.competition_id
				  AND awards.sponsored_by_org_id = $1::uuid
				  AND awards.archived_at IS NULL
				  AND opt_ins.project_id = projects.id
			  )
		)
		SELECT entries.award_id, entries.award_title, entries.award_status,
			entries.competition_id::text, entries.competition_title,
			entries.conference_id::text, entries.conference_tag, entries.conference_title,
			entries.project_id::text, entries.project_title, entries.short_description,
			entries.image_url, entries.project_status, entries.project_number,
			entries.github_url, entries.demo_url, entries.entered_at,
			entries.winner, entries.sponsored_prize, entries.automatic_contact,
			members.person_id::text, people.name, people.norm_photo_path,
			people.avail_to_hire,
			members.role,
			CASE WHEN $2::boolean AND (
				(entries.automatic_contact AND entries.project_status IN ('submitted', 'advanced'))
				OR (entries.contact_access
					AND (coalesce(consent.all_hackathon_sponsors, false)
						OR (entries.sponsored_prize AND coalesce(consent.entered_award_sponsors, false))))
				)
				THEN coalesce(contact.email, '') ELSE '' END,
			coalesce(consent.all_hackathon_sponsors, false),
			coalesce(consent.entered_award_sponsors, false)
		FROM visible_entries entries
		JOIN project_members members ON members.project_id = entries.project_id
		JOIN people ON people.id = members.person_id
		LEFT JOIN hackathon_sponsor_contact_consents consent
		  ON consent.competition_id = entries.competition_id
		 AND consent.person_id = members.person_id
		LEFT JOIN LATERAL (
			SELECT person_emails.email::text AS email
			FROM person_emails
			WHERE person_emails.person_id = members.person_id
			ORDER BY person_emails.is_primary DESC,
				person_emails.verified_at DESC NULLS LAST,
				person_emails.created_at, person_emails.id
			LIMIT 1
		) contact ON true
		ORDER BY entries.conference_start DESC NULLS LAST, entries.award_title,
			entries.project_number NULLS LAST, entries.project_title,
			CASE members.role WHEN 'owner' THEN 0 ELSE 1 END,
			members.created_at, members.person_id
	`, organizationID, includeContacts, requireExportAccess)
	if err != nil {
		return nil, fmt.Errorf("list sponsor prize entries: %w", err)
	}
	defer rows.Close()

	entries := make([]*types.SponsorPrizeEntry, 0)
	byKey := make(map[string]*types.SponsorPrizeEntry)
	for rows.Next() {
		entry := &types.SponsorPrizeEntry{}
		participant := &types.SponsorPrizeParticipant{}
		var projectNumber pgtype.Int4
		var allSponsors, enteredAwardSponsors bool
		if err := rows.Scan(
			&entry.AwardID, &entry.AwardTitle, &entry.AwardStatus,
			&entry.CompetitionID, &entry.CompetitionTitle,
			&entry.ConferenceID, &entry.ConferenceTag, &entry.ConferenceTitle,
			&entry.ProjectID, &entry.ProjectTitle, &entry.ProjectShortDescription,
			&entry.ProjectImageURL, &entry.ProjectStatus, &projectNumber,
			&entry.GitHubURL, &entry.DemoURL, &entry.OptedInAt, &entry.Winner,
			&entry.SponsoredPrize, &entry.AutomaticContact,
			&participant.PersonID, &participant.Name, &participant.Photo,
			&participant.AvailableToHire,
			&participant.Role, &participant.Email, &allSponsors, &enteredAwardSponsors,
		); err != nil {
			return nil, fmt.Errorf("scan sponsor prize entry: %w", err)
		}
		if projectNumber.Valid {
			n := int(projectNumber.Int32)
			entry.ProjectNumber = &n
		}
		if participant.Email != "" {
			if entry.AutomaticContact && (entry.ProjectStatus == ProjectStatusSubmitted || entry.ProjectStatus == ProjectStatusAdvanced) {
				participant.ConsentScope = "included_sponsorship"
			} else if allSponsors {
				participant.ConsentScope = "all_sponsors"
			} else if entry.SponsoredPrize && enteredAwardSponsors {
				participant.ConsentScope = "entered_award"
			}
		}
		key := entry.AwardID + "|" + entry.ProjectID
		stored := byKey[key]
		if stored == nil {
			stored = entry
			byKey[key] = stored
			entries = append(entries, stored)
		}
		stored.Participants = append(stored.Participants, participant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sponsor prize entries: %w", err)
	}
	return entries, nil
}

type SponsorAwardProposalInput struct {
	SponsorshipID       string
	ConferenceID        string
	CompetitionID       string
	OrganizationID      string
	SubmittedByPersonID string
	Title               string
	Description         string
	JudgingInstructions string
	MaxAwardees         int
	OptInRequired       bool
	FinalistsOnly       bool
	PrizeType           string
	PrizeTitle          string
	PrizeDescription    string
	PrizeValueText      string
}

func CreateSponsorAwardProposal(ctx *config.AppContext, in SponsorAwardProposalInput) (*types.SponsorAwardProposal, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	in.SponsorshipID = strings.TrimSpace(in.SponsorshipID)
	in.ConferenceID = strings.TrimSpace(in.ConferenceID)
	in.CompetitionID = strings.TrimSpace(in.CompetitionID)
	in.OrganizationID = strings.TrimSpace(in.OrganizationID)
	in.SubmittedByPersonID = strings.TrimSpace(in.SubmittedByPersonID)
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.JudgingInstructions = strings.TrimSpace(in.JudgingInstructions)
	in.PrizeType = strings.TrimSpace(in.PrizeType)
	in.PrizeTitle = strings.TrimSpace(in.PrizeTitle)
	in.PrizeDescription = strings.TrimSpace(in.PrizeDescription)
	in.PrizeValueText = strings.TrimSpace(in.PrizeValueText)
	if in.Title == "" || in.PrizeTitle == "" {
		return nil, fmt.Errorf("award and prize titles are required")
	}
	if in.MaxAwardees < 1 || in.MaxAwardees > 100 {
		return nil, fmt.Errorf("max awardees must be between 1 and 100")
	}
	switch in.PrizeType {
	case PrizeTypeSats, PrizeTypeInKind, PrizeTypeTickets, PrizeTypeTrophy:
	default:
		return nil, fmt.Errorf("unsupported prize type")
	}
	value, err := strconv.ParseInt(in.PrizeValueText, 10, 64)
	if err != nil || value <= 0 {
		return nil, fmt.Errorf("prize value must be a positive whole number of satoshis")
	}
	in.PrizeValueText = strconv.FormatInt(value, 10)

	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin sponsor award proposal: %w", err)
	}
	defer tx.Rollback(dbctx)
	var proposalLimit int
	if err := tx.QueryRow(dbctx, `
		SELECT entitlements.sponsor_award_limit
		FROM sponsorship_entitlements entitlements
		JOIN sponsorships ON sponsorships.id = entitlements.sponsorship_id
		JOIN competitions ON competitions.id = $3::uuid
		JOIN conferences ON conferences.id = entitlements.conference_id
		WHERE entitlements.sponsorship_id = $1::uuid
		  AND entitlements.conference_id = $2::uuid
		  AND competitions.conference_id = entitlements.conference_id
		  AND sponsorships.organization_id = $4::uuid
		  AND sponsorships.archived_at IS NULL
		  AND lower(sponsorships.status) IN ('paid', 'committed')
		  AND (conferences.end_date IS NULL OR conferences.end_date >= now())
		FOR UPDATE OF entitlements
	`, in.SponsorshipID, in.ConferenceID, in.CompetitionID, in.OrganizationID).Scan(&proposalLimit); err != nil {
		return nil, fmt.Errorf("this sponsorship cannot propose prizes for that hackathon")
	}
	var used int
	if err := tx.QueryRow(dbctx, `
		SELECT
		  (SELECT count(*) FROM awards
		   WHERE competition_id = $1::uuid AND sponsored_by_org_id = $2::uuid
		     AND archived_at IS NULL) +
		  (SELECT count(*) FROM sponsor_award_proposals
		   WHERE sponsorship_id = $3::uuid AND conference_id = $4::uuid
		     AND status = 'pending')
	`, in.CompetitionID, in.OrganizationID, in.SponsorshipID, in.ConferenceID).Scan(&used); err != nil {
		return nil, fmt.Errorf("count sponsor award proposals: %w", err)
	}
	if proposalLimit <= 0 || used >= proposalLimit {
		return nil, fmt.Errorf("this sponsorship has used its sponsor prize allowance")
	}
	proposal := &types.SponsorAwardProposal{
		SponsorshipID: in.SponsorshipID, ConferenceID: in.ConferenceID,
		CompetitionID: in.CompetitionID, OrganizationID: in.OrganizationID,
		SubmittedByPersonID: in.SubmittedByPersonID, Title: in.Title,
		Description: in.Description, JudgingInstructions: in.JudgingInstructions,
		MaxAwardees: in.MaxAwardees, OptInRequired: in.OptInRequired,
		FinalistsOnly: in.FinalistsOnly, PrizeType: in.PrizeType,
		PrizeTitle: in.PrizeTitle, PrizeDescription: in.PrizeDescription,
		PrizeValueText: in.PrizeValueText, Status: "pending",
	}
	if err := tx.QueryRow(dbctx, `
		INSERT INTO sponsor_award_proposals (
			sponsorship_id, conference_id, competition_id, submitted_by_person_id,
			title, description, judging_instructions, max_awardees,
			opt_in_required, finalists_only, prize_type, prize_title,
			prize_description, prize_value_text
		) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id::text, created_at, updated_at
	`, proposal.SponsorshipID, proposal.ConferenceID, proposal.CompetitionID,
		proposal.SubmittedByPersonID, proposal.Title, proposal.Description,
		proposal.JudgingInstructions, proposal.MaxAwardees, proposal.OptInRequired,
		proposal.FinalistsOnly, proposal.PrizeType, proposal.PrizeTitle,
		proposal.PrizeDescription, proposal.PrizeValueText).Scan(
		&proposal.ID, &proposal.CreatedAt, &proposal.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create sponsor award proposal: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit sponsor award proposal: %w", err)
	}
	return proposal, nil
}

func ListSponsorAwardProposalsForOrganization(ctx *config.AppContext, organizationID string) ([]*types.SponsorAwardProposal, error) {
	return listSponsorAwardProposals(ctx, `WHERE sponsorships.organization_id = $1::uuid`, strings.TrimSpace(organizationID))
}

func ListSponsorAwardProposalsForCompetition(ctx *config.AppContext, competitionID string) ([]*types.SponsorAwardProposal, error) {
	return listSponsorAwardProposals(ctx, `WHERE proposals.competition_id = $1::uuid`, strings.TrimSpace(competitionID))
}

func listSponsorAwardProposals(ctx *config.AppContext, where string, arg string) ([]*types.SponsorAwardProposal, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT proposals.id::text, proposals.sponsorship_id::text,
			proposals.conference_id::text, proposals.competition_id::text,
			sponsorships.organization_id::text, organizations.name,
			coalesce(proposals.submitted_by_person_id::text, ''), coalesce(submitter.name, ''),
			proposals.title, proposals.description, proposals.judging_instructions,
			proposals.max_awardees, proposals.opt_in_required, proposals.finalists_only,
			proposals.prize_type, proposals.prize_title, proposals.prize_description,
			proposals.prize_value_text, proposals.status, proposals.review_notes,
			coalesce(proposals.reviewed_by_person_id::text, ''), proposals.reviewed_at,
			coalesce(proposals.award_id::text, ''), proposals.created_at, proposals.updated_at
		FROM sponsor_award_proposals proposals
		JOIN sponsorships ON sponsorships.id = proposals.sponsorship_id
		JOIN organizations ON organizations.id = sponsorships.organization_id
		LEFT JOIN people submitter ON submitter.id = proposals.submitted_by_person_id
		`+where+`
		ORDER BY proposals.created_at DESC
	`, arg)
	if err != nil {
		return nil, fmt.Errorf("list sponsor award proposals: %w", err)
	}
	defer rows.Close()
	var out []*types.SponsorAwardProposal
	for rows.Next() {
		proposal := &types.SponsorAwardProposal{}
		var reviewedAt pgtype.Timestamptz
		if err := rows.Scan(&proposal.ID, &proposal.SponsorshipID, &proposal.ConferenceID,
			&proposal.CompetitionID, &proposal.OrganizationID, &proposal.OrganizationName,
			&proposal.SubmittedByPersonID, &proposal.SubmittedByName, &proposal.Title,
			&proposal.Description, &proposal.JudgingInstructions, &proposal.MaxAwardees,
			&proposal.OptInRequired, &proposal.FinalistsOnly, &proposal.PrizeType,
			&proposal.PrizeTitle, &proposal.PrizeDescription, &proposal.PrizeValueText,
			&proposal.Status, &proposal.ReviewNotes, &proposal.ReviewedByPersonID,
			&reviewedAt, &proposal.AwardID, &proposal.CreatedAt, &proposal.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan sponsor award proposal: %w", err)
		}
		if reviewedAt.Valid {
			value := reviewedAt.Time
			proposal.ReviewedAt = &value
		}
		out = append(out, proposal)
	}
	return out, rows.Err()
}

func ReviewSponsorAwardProposal(ctx *config.AppContext, proposalID, competitionID, reviewerPersonID, decision, notes string) (*types.SponsorAwardProposal, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "approved" && decision != "rejected" {
		return nil, fmt.Errorf("review decision must be approved or rejected")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin sponsor award review: %w", err)
	}
	defer tx.Rollback(dbctx)
	proposal := &types.SponsorAwardProposal{}
	if err := tx.QueryRow(dbctx, `
		SELECT proposals.id::text, proposals.sponsorship_id::text,
			proposals.conference_id::text, proposals.competition_id::text,
			sponsorships.organization_id::text, proposals.title, proposals.description,
			proposals.judging_instructions, proposals.max_awardees,
			proposals.opt_in_required, proposals.finalists_only,
			proposals.prize_type, proposals.prize_title,
			proposals.prize_description, proposals.prize_value_text, proposals.status
		FROM sponsor_award_proposals proposals
		JOIN sponsorships ON sponsorships.id = proposals.sponsorship_id
		WHERE proposals.id = $1::uuid AND proposals.competition_id = $2::uuid
		FOR UPDATE OF proposals
	`, strings.TrimSpace(proposalID), strings.TrimSpace(competitionID)).Scan(
		&proposal.ID, &proposal.SponsorshipID, &proposal.ConferenceID,
		&proposal.CompetitionID, &proposal.OrganizationID, &proposal.Title,
		&proposal.Description, &proposal.JudgingInstructions, &proposal.MaxAwardees,
		&proposal.OptInRequired, &proposal.FinalistsOnly, &proposal.PrizeType,
		&proposal.PrizeTitle, &proposal.PrizeDescription, &proposal.PrizeValueText,
		&proposal.Status); err != nil {
		return nil, fmt.Errorf("sponsor award proposal not found")
	}
	if proposal.Status != "pending" {
		return nil, fmt.Errorf("sponsor award proposal has already been reviewed")
	}
	if decision == "approved" {
		if err := tx.QueryRow(dbctx, `
			INSERT INTO awards (
				competition_id, sponsored_by_org_id, award_type, title, description,
				judging_instructions, max_awardees, opt_in_required, finalists_only, status
			) VALUES ($1::uuid, $2::uuid, 'challenge', $3, $4, $5, $6, $7, $8, 'available')
			RETURNING id::text
		`, proposal.CompetitionID, proposal.OrganizationID, proposal.Title,
			proposal.Description, proposal.JudgingInstructions, proposal.MaxAwardees,
			proposal.OptInRequired, proposal.FinalistsOnly).Scan(&proposal.AwardID); err != nil {
			return nil, fmt.Errorf("create approved sponsor award: %w", err)
		}
		if _, err := tx.Exec(dbctx, `
			INSERT INTO prizes (award_id, prize_type, title, description, value_text, status)
			VALUES ($1::uuid, $2, $3, $4, $5, 'available')
		`, proposal.AwardID, proposal.PrizeType, proposal.PrizeTitle,
			proposal.PrizeDescription, proposal.PrizeValueText); err != nil {
			return nil, fmt.Errorf("create approved sponsor prize: %w", err)
		}
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE sponsor_award_proposals SET
			status = $2, review_notes = $3, reviewed_by_person_id = $4::uuid,
			reviewed_at = now(), award_id = NULLIF($5, '')::uuid
		WHERE id = $1::uuid
	`, proposal.ID, decision, strings.TrimSpace(notes), strings.TrimSpace(reviewerPersonID), proposal.AwardID); err != nil {
		return nil, fmt.Errorf("finish sponsor award review: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit sponsor award review: %w", err)
	}
	proposal.Status = decision
	proposal.ReviewNotes = strings.TrimSpace(notes)
	proposal.ReviewedByPersonID = strings.TrimSpace(reviewerPersonID)
	return proposal, nil
}

func IssueSponsorTickets(ctx *config.AppContext, organizationID, sponsorshipID, conferenceID, issuedByPersonID, email string, quantity int) (*types.SponsorTicketIssuance, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(parsed.Address, email) {
		return nil, fmt.Errorf("a valid recipient email is required")
	}
	if quantity < 1 || quantity > 25 {
		return nil, fmt.Errorf("ticket quantity must be between 1 and 25")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin sponsor ticket issuance: %w", err)
	}
	defer tx.Rollback(dbctx)
	var allocation int
	var conferenceDescription, conferenceTag string
	if err := tx.QueryRow(dbctx, `
		SELECT entitlements.ticket_allocation, conferences.description, conferences.tag
		FROM sponsorship_entitlements entitlements
		JOIN sponsorships ON sponsorships.id = entitlements.sponsorship_id
		JOIN conferences ON conferences.id = entitlements.conference_id
		WHERE entitlements.sponsorship_id = $1::uuid
		  AND entitlements.conference_id = $2::uuid
		  AND sponsorships.organization_id = $3::uuid
		  AND sponsorships.archived_at IS NULL
		  AND lower(sponsorships.status) IN ('paid', 'committed')
		  AND (conferences.end_date IS NULL OR conferences.end_date >= now())
		FOR UPDATE OF entitlements
	`, sponsorshipID, conferenceID, organizationID).Scan(&allocation, &conferenceDescription, &conferenceTag); err != nil {
		return nil, fmt.Errorf("this sponsorship cannot issue tickets for that event")
	}
	var issued int
	if err := tx.QueryRow(dbctx, `
		SELECT coalesce(sum(quantity), 0) FROM sponsor_ticket_issuances
		WHERE sponsorship_id = $1::uuid AND conference_id = $2::uuid
	`, sponsorshipID, conferenceID).Scan(&issued); err != nil {
		return nil, fmt.Errorf("count sponsor tickets: %w", err)
	}
	if issued+quantity > allocation {
		remaining := allocation - issued
		if remaining < 0 {
			remaining = 0
		}
		return nil, fmt.Errorf("only %d sponsor ticket(s) remain", remaining)
	}
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate sponsor ticket batch: %w", err)
	}
	checkoutID := "sponsor-" + base64.RawURLEncoding.EncodeToString(raw)
	issuedAt := time.Now()
	for i := 0; i < quantity; i++ {
		refID := types.UniqueID(email, checkoutID, int32(i))
		if _, err := tx.Exec(dbctx, `
			INSERT INTO registrations (
				ref_id, checkout_id, conference_id, type, email, person_id,
				item_bought, amount_paid, currency, platform, registered_at, revoked
			) VALUES (
				$1, $2, $3::uuid, 'sponsor', $4::citext,
				(SELECT person_id FROM person_emails WHERE email = $4::citext),
				$5, 0, 'USD', 'sponsor', $6, false
			)
		`, refID, checkoutID, conferenceID, email, conferenceDescription, issuedAt); err != nil {
			return nil, fmt.Errorf("issue sponsor ticket: %w", err)
		}
	}
	issuance := &types.SponsorTicketIssuance{
		SponsorshipID: sponsorshipID, ConferenceID: conferenceID,
		ConferenceTag:    conferenceTag,
		IssuedByPersonID: issuedByPersonID, RecipientEmail: email,
		Quantity: quantity, CheckoutID: checkoutID, CreatedAt: issuedAt,
	}
	if err := tx.QueryRow(dbctx, `
		INSERT INTO sponsor_ticket_issuances (
			sponsorship_id, conference_id, issued_by_person_id,
			recipient_email, quantity, checkout_id, created_at
		) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::citext, $5, $6, $7)
		RETURNING id::text
	`, sponsorshipID, conferenceID, issuedByPersonID, email, quantity,
		checkoutID, issuedAt).Scan(&issuance.ID); err != nil {
		return nil, fmt.Errorf("record sponsor ticket issuance: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit sponsor ticket issuance: %w", err)
	}
	return issuance, nil
}

func ListSponsorTicketIssuances(ctx *config.AppContext, organizationID string) ([]*types.SponsorTicketIssuance, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT issuances.id::text, issuances.sponsorship_id::text,
			issuances.conference_id::text, coalesce(issuances.issued_by_person_id::text, ''),
			issuances.recipient_email::text, issuances.quantity,
			issuances.checkout_id, issuances.created_at
		FROM sponsor_ticket_issuances issuances
		JOIN sponsorships ON sponsorships.id = issuances.sponsorship_id
		WHERE sponsorships.organization_id = $1::uuid
		ORDER BY issuances.created_at DESC
	`, strings.TrimSpace(organizationID))
	if err != nil {
		return nil, fmt.Errorf("list sponsor ticket issuances: %w", err)
	}
	defer rows.Close()
	var out []*types.SponsorTicketIssuance
	for rows.Next() {
		issuance := &types.SponsorTicketIssuance{}
		if err := rows.Scan(&issuance.ID, &issuance.SponsorshipID,
			&issuance.ConferenceID, &issuance.IssuedByPersonID,
			&issuance.RecipientEmail, &issuance.Quantity,
			&issuance.CheckoutID, &issuance.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan sponsor ticket issuance: %w", err)
		}
		out = append(out, issuance)
	}
	return out, rows.Err()
}

func GetHackathonSponsorContactConsent(ctx *config.AppContext, competitionID, personID string) (*types.HackathonSponsorContactConsent, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	consent := &types.HackathonSponsorContactConsent{
		CompetitionID: strings.TrimSpace(competitionID),
		PersonID:      strings.TrimSpace(personID),
	}
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT all_hackathon_sponsors, entered_award_sponsors, created_at, updated_at
		FROM hackathon_sponsor_contact_consents
		WHERE competition_id = $1::uuid AND person_id = $2::uuid
	`, consent.CompetitionID, consent.PersonID).Scan(
		&consent.AllHackathonSponsors, &consent.EnteredAwardSponsors,
		&consent.CreatedAt, &consent.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return consent, nil
		}
		return nil, fmt.Errorf("load hackathon sponsor contact consent: %w", err)
	}
	return consent, nil
}

func SetHackathonSponsorContactConsent(ctx *config.AppContext, consent *types.HackathonSponsorContactConsent) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if consent == nil || strings.TrimSpace(consent.CompetitionID) == "" || strings.TrimSpace(consent.PersonID) == "" {
		return fmt.Errorf("competition and person are required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin hackathon sponsor contact consent: %w", err)
	}
	defer tx.Rollback(dbctx)
	_, err = tx.Exec(dbctx, `
		INSERT INTO hackathon_sponsor_contact_consents (
			competition_id, person_id, all_hackathon_sponsors, entered_award_sponsors
		) VALUES ($1::uuid, $2::uuid, $3, $4)
		ON CONFLICT (competition_id, person_id) DO UPDATE SET
			all_hackathon_sponsors = EXCLUDED.all_hackathon_sponsors,
			entered_award_sponsors = EXCLUDED.entered_award_sponsors
	`, strings.TrimSpace(consent.CompetitionID), strings.TrimSpace(consent.PersonID),
		consent.AllHackathonSponsors, consent.EnteredAwardSponsors)
	if err != nil {
		return fmt.Errorf("save hackathon sponsor contact consent: %w", err)
	}
	if _, err := tx.Exec(dbctx, `
		INSERT INTO hackathon_sponsor_contact_consent_events (
			competition_id, person_id, all_hackathon_sponsors,
			entered_award_sponsors, policy_version, source
		) VALUES ($1::uuid, $2::uuid, $3, $4, $5, 'web')
	`, strings.TrimSpace(consent.CompetitionID), strings.TrimSpace(consent.PersonID),
		consent.AllHackathonSponsors, consent.EnteredAwardSponsors,
		SponsorContactConsentPolicyVersion); err != nil {
		return fmt.Errorf("record hackathon sponsor contact consent event: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit hackathon sponsor contact consent: %w", err)
	}
	return nil
}

// UpdateOrganizationPublicDetails intentionally excludes internal notes and
// sponsorship fields from sponsor self-service edits.
func UpdateOrganizationPublicDetails(ctx *config.AppContext, org *types.Org) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if org == nil || strings.TrimSpace(org.Ref) == "" || strings.TrimSpace(org.Name) == "" {
		return fmt.Errorf("organization id and name are required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE organizations SET
			name = $2, tagline = $3, logo_light_url = $4, logo_dark_url = $5,
			email = NULLIF($6, '')::citext, website_url = $7,
			linkedin_url = $8, instagram_url = $9, youtube_url = $10,
			github_url = $11, twitter_handle = $12, nostr = $13,
			matrix = $14, hiring = $15
		WHERE id = $1::uuid
	`, org.Ref, strings.TrimSpace(org.Name), strings.TrimSpace(org.Tagline),
		strings.TrimSpace(org.LogoLight), strings.TrimSpace(org.LogoDark),
		strings.TrimSpace(org.Email), strings.TrimSpace(org.Website),
		strings.TrimSpace(org.LinkedIn), strings.TrimSpace(org.Instagram),
		strings.TrimSpace(org.Youtube), strings.TrimSpace(org.Github),
		strings.TrimPrefix(strings.TrimSpace(org.Twitter.Handle), "@"),
		strings.TrimSpace(org.Nostr), strings.TrimSpace(org.Matrix), org.Hiring)
	if err != nil {
		return fmt.Errorf("update organization public details %s: %w", org.Ref, err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("organization %s not found", org.Ref)
	}
	return nil
}

func RecordSponsorAuditEvent(ctx *config.AppContext, organizationID, sponsorshipID, conferenceID, actorPersonID, action, targetType, targetID string, details map[string]any) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if details == nil {
		details = map[string]any{}
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode sponsor audit details: %w", err)
	}
	_, err = ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO sponsor_audit_events (
			organization_id, sponsorship_id, conference_id, actor_person_id,
			action, target_type, target_id, details
		) VALUES (
			$1::uuid, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid,
			NULLIF($4, '')::uuid, $5, $6, $7, $8::jsonb
		)
	`, strings.TrimSpace(organizationID), strings.TrimSpace(sponsorshipID),
		strings.TrimSpace(conferenceID), strings.TrimSpace(actorPersonID),
		strings.TrimSpace(action), strings.TrimSpace(targetType),
		strings.TrimSpace(targetID), string(raw))
	if err != nil {
		return fmt.Errorf("record sponsor audit event: %w", err)
	}
	return nil
}
