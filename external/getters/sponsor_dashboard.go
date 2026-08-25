package getters

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
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
			coalesce(entitlements.participant_contact_access, false),
			coalesce(entitlements.participant_contact_export, false),
			coalesce(entitlements.can_manage_award_judges, false),
			coalesce(entitlements.can_edit_organization, false),
			coalesce(entitlements.created_at, sponsorships.created_at),
			coalesce(entitlements.updated_at, sponsorships.updated_at),
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
			   AND awards.archived_at IS NULL)
		FROM sponsorships
		JOIN sponsorships_conferences links ON links.sponsorship_id = sponsorships.id
		JOIN conferences ON conferences.id = links.conference_id
		LEFT JOIN sponsorship_entitlements entitlements
		  ON entitlements.sponsorship_id = sponsorships.id
		 AND entitlements.conference_id = conferences.id
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
		var conferenceID string
		if err := rows.Scan(
			&event.Sponsorship.Ref, &event.Sponsorship.Name,
			&event.Sponsorship.Level, &event.Sponsorship.Label,
			&event.Sponsorship.Status, &event.Sponsorship.IsVendor,
			&conferenceID,
			&event.Entitlement.TicketAllocation,
			&event.Entitlement.SponsorAwardLimit,
			&event.Entitlement.ParticipantContactAccess,
			&event.Entitlement.ParticipantContactExport,
			&event.Entitlement.CanManageAwardJudges,
			&event.Entitlement.CanEditOrganization,
			&event.Entitlement.CreatedAt, &event.Entitlement.UpdatedAt,
			&event.AwardCount, &event.WinnerCount,
		); err != nil {
			return nil, fmt.Errorf("scan sponsor dashboard event: %w", err)
		}
		event.Conference = confByID[conferenceID]
		event.Entitlement.SponsorshipID = event.Sponsorship.Ref
		event.Entitlement.ConferenceID = conferenceID
		if event.Conference != nil {
			event.Sponsorship.Confs = []*types.Conf{event.Conference}
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sponsor dashboard events: %w", err)
	}
	return out, nil
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
