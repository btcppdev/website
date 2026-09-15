package getters

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
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

var ErrOrganizationMemberInvitePending = errors.New("an unexpired invitation is already pending for that email")

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
	var pendingInvite types.OrganizationMemberInvite
	err = tx.QueryRow(dbctx, `
		SELECT id::text, organization_id::text, email::text, role,
			coalesce(invited_by_person_id::text, ''), expires_at, created_at
		FROM organization_member_invites
		WHERE organization_id = $1::uuid AND email = $2::citext
		  AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
	`, organizationID, email).Scan(
		&pendingInvite.ID, &pendingInvite.OrganizationID, &pendingInvite.Email,
		&pendingInvite.Role, &pendingInvite.InvitedByPersonID,
		&pendingInvite.ExpiresAt, &pendingInvite.CreatedAt,
	)
	if err == nil {
		return "", &pendingInvite, ErrOrganizationMemberInvitePending
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", nil, fmt.Errorf("check pending organization member invite: %w", err)
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

// ReplaceOrganizationMemberInvite explicitly rotates a pending invitation.
// Raw tokens are never stored, so replacing is the only safe way to provide a
// link again after its one-time display.
func ReplaceOrganizationMemberInvite(ctx *config.AppContext, organizationID, inviteID, invitedByPersonID string, expiresAt time.Time) (string, *types.OrganizationMemberInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return "", nil, fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	inviteID = strings.TrimSpace(inviteID)
	invitedByPersonID = strings.TrimSpace(invitedByPersonID)
	if organizationID == "" || inviteID == "" {
		return "", nil, fmt.Errorf("organization and invitation are required")
	}
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(72 * time.Hour)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate replacement organization invite: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return "", nil, fmt.Errorf("begin replacement organization invite: %w", err)
	}
	defer tx.Rollback(dbctx)

	invite := &types.OrganizationMemberInvite{
		OrganizationID: organizationID, InvitedByPersonID: invitedByPersonID,
		ExpiresAt: expiresAt,
	}
	if err := tx.QueryRow(dbctx, `
		SELECT email::text, role
		FROM organization_member_invites
		WHERE id = $1::uuid AND organization_id = $2::uuid
		  AND accepted_at IS NULL AND revoked_at IS NULL
		FOR UPDATE
	`, inviteID, organizationID).Scan(&invite.Email, &invite.Role); err != nil {
		return "", nil, fmt.Errorf("pending organization invitation not found")
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE organization_member_invites SET revoked_at = now()
		WHERE id = $1::uuid
	`, inviteID); err != nil {
		return "", nil, fmt.Errorf("revoke previous organization invite: %w", err)
	}
	if err := tx.QueryRow(dbctx, `
		INSERT INTO organization_member_invites (
			organization_id, email, role, token_hash,
			invited_by_person_id, expires_at
		) VALUES ($1::uuid, $2::citext, $3, $4, NULLIF($5, '')::uuid, $6)
		RETURNING id::text, created_at
	`, organizationID, invite.Email, invite.Role, organizationInviteTokenHash(token),
		invitedByPersonID, expiresAt).Scan(&invite.ID, &invite.CreatedAt); err != nil {
		return "", nil, fmt.Errorf("create replacement organization invite: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return "", nil, fmt.Errorf("commit replacement organization invite: %w", err)
	}
	return token, invite, nil
}

func ListPendingOrganizationMemberInvites(ctx *config.AppContext, organizationID string) ([]*types.OrganizationMemberInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	if organizationID == "" {
		return nil, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, organization_id::text, email::text, role,
			coalesce(invited_by_person_id::text, ''), expires_at, created_at
		FROM organization_member_invites
		WHERE organization_id = $1::uuid
		  AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list pending organization member invites %s: %w", organizationID, err)
	}
	defer rows.Close()
	var out []*types.OrganizationMemberInvite
	for rows.Next() {
		invite := &types.OrganizationMemberInvite{}
		if err := rows.Scan(
			&invite.ID, &invite.OrganizationID, &invite.Email, &invite.Role,
			&invite.InvitedByPersonID, &invite.ExpiresAt, &invite.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending organization member invite: %w", err)
		}
		out = append(out, invite)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending organization member invites: %w", err)
	}
	return out, nil
}

// ListPendingOrganizationMemberInvitesForPerson returns live invitations sent
// to any verified email on the person's account. Using verified emails here
// keeps dashboard discovery subject to the same ownership proof as acceptance.
func ListPendingOrganizationMemberInvitesForPerson(ctx *config.AppContext, personID string) ([]*types.OrganizationMemberInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT invites.id::text, invites.organization_id::text,
			organizations.name, invites.email::text, invites.role,
			coalesce(invites.invited_by_person_id::text, ''),
			invites.expires_at, invites.created_at
		FROM organization_member_invites invites
		JOIN organizations ON organizations.id = invites.organization_id
		WHERE invites.accepted_at IS NULL
		  AND invites.revoked_at IS NULL
		  AND invites.expires_at > now()
		  AND EXISTS (
			SELECT 1 FROM person_emails
			WHERE person_emails.person_id = $1::uuid
			  AND person_emails.email = invites.email
			  AND person_emails.verified_at IS NOT NULL
		  )
		ORDER BY invites.created_at DESC
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list pending organization member invites for person %s: %w", personID, err)
	}
	defer rows.Close()
	var out []*types.OrganizationMemberInvite
	for rows.Next() {
		invite := &types.OrganizationMemberInvite{}
		if err := rows.Scan(
			&invite.ID, &invite.OrganizationID, &invite.OrganizationName,
			&invite.Email, &invite.Role, &invite.InvitedByPersonID,
			&invite.ExpiresAt, &invite.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending organization member invite for person: %w", err)
		}
		out = append(out, invite)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending organization member invites for person: %w", err)
	}
	return out, nil
}

func CountPendingOrganizationMemberInvitesForPerson(ctx *config.AppContext, personID string) (int, error) {
	if ctx == nil || ctx.DB == nil {
		return 0, fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return 0, nil
	}
	var count int
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT count(*)
		FROM organization_member_invites invites
		WHERE invites.accepted_at IS NULL
		  AND invites.revoked_at IS NULL
		  AND invites.expires_at > now()
		  AND EXISTS (
			SELECT 1 FROM person_emails
			WHERE person_emails.person_id = $1::uuid
			  AND person_emails.email = invites.email
			  AND person_emails.verified_at IS NOT NULL
		  )
	`, personID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending organization member invites for person %s: %w", personID, err)
	}
	return count, nil
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
	return acceptOrganizationMemberInvite(ctx, organizationInviteTokenHash(token), personID, false)
}

// AcceptOrganizationMemberInviteByID supports acceptance from the signed-in
// dashboard. The transaction still proves the invite email is a verified email
// on personID, so knowing an invitation UUID alone grants no access.
func AcceptOrganizationMemberInviteByID(ctx *config.AppContext, inviteID, personID string) (*types.OrganizationMemberInvite, error) {
	return acceptOrganizationMemberInvite(ctx, strings.TrimSpace(inviteID), personID, true)
}

func acceptOrganizationMemberInvite(ctx *config.AppContext, lookup, personID string, lookupByID bool) (*types.OrganizationMemberInvite, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	lookup = strings.TrimSpace(lookup)
	personID = strings.TrimSpace(personID)
	if lookup == "" || personID == "" {
		return nil, fmt.Errorf("organization invitation and person are required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin accept organization invite: %w", err)
	}
	defer tx.Rollback(dbctx)

	var invite types.OrganizationMemberInvite
	var acceptedAt, revokedAt pgtype.Timestamptz
	query := `
		SELECT id::text, organization_id::text, email::text, role,
			expires_at, accepted_at, revoked_at
		FROM organization_member_invites
		WHERE token_hash = $1
		FOR UPDATE
	`
	if lookupByID {
		query = `
			SELECT id::text, organization_id::text, email::text, role,
				expires_at, accepted_at, revoked_at
			FROM organization_member_invites
			WHERE id = $1::uuid
			FOR UPDATE
		`
	}
	err = tx.QueryRow(dbctx, query, lookup).Scan(
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

func ListSponsoredOrganizationIDsForPerson(ctx *config.AppContext, personID string) ([]string, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT DISTINCT memberships.organization_id::text
		FROM organization_memberships memberships
		JOIN sponsorships
		  ON sponsorships.organization_id = memberships.organization_id
		 AND sponsorships.archived_at IS NULL
		JOIN sponsorships_conferences
		  ON sponsorships_conferences.sponsorship_id = sponsorships.id
		WHERE memberships.person_id = $1::uuid
		  AND memberships.status = 'active'
		  AND memberships.role IN ('owner', 'manager')
		ORDER BY memberships.organization_id::text
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list sponsored organizations for person %s: %w", personID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var organizationID string
		if err := rows.Scan(&organizationID); err != nil {
			return nil, fmt.Errorf("scan sponsored organization: %w", err)
		}
		out = append(out, organizationID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sponsored organizations: %w", err)
	}
	return out, nil
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
			organizations.id::text, organizations.public_slug, organizations.name, organizations.tagline,
			organizations.logo_light_url, organizations.logo_dark_url,
			coalesce(organizations.email::text, ''), organizations.website_url,
			organizations.github_url, organizations.twitter_handle,
			organizations.nostr, organizations.matrix, organizations.linkedin_url,
			organizations.instagram_url, organizations.youtube_url,
			organizations.hiring, organizations.membership_policy
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
			&membership.Organization.Ref, &membership.Organization.Slug, &membership.Organization.Name,
			&membership.Organization.Tagline, &membership.Organization.LogoLight,
			&membership.Organization.LogoDark, &membership.Organization.Email,
			&membership.Organization.Website, &membership.Organization.Github,
			&twitterHandle, &membership.Organization.Nostr,
			&membership.Organization.Matrix, &membership.Organization.LinkedIn,
			&membership.Organization.Instagram, &membership.Organization.Youtube,
			&membership.Organization.Hiring, &membership.Organization.MembershipPolicy,
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
			people.name, coalesce(primary_email.email::text, ''),
			coalesce(nostr.pubkey_hex, '')
		FROM organization_memberships memberships
		JOIN people ON people.id = memberships.person_id
		LEFT JOIN LATERAL (
			SELECT person_emails.email
			FROM person_emails
			WHERE person_emails.person_id = people.id
			ORDER BY person_emails.is_primary DESC, person_emails.verified_at DESC NULLS LAST
			LIMIT 1
		) primary_email ON true
		LEFT JOIN LATERAL (
			SELECT person_nostr_credentials.pubkey_hex FROM person_nostr_credentials
			WHERE person_nostr_credentials.person_id = people.id
				AND person_nostr_credentials.verified_at IS NOT NULL
			ORDER BY person_nostr_credentials.verified_at DESC,
				person_nostr_credentials.linked_at DESC
			LIMIT 1
		) nostr ON true
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
			&membership.PersonName, &membership.PersonEmail, &membership.PersonNostr,
		); err != nil {
			return nil, fmt.Errorf("scan organization member: %w", err)
		}
		out = append(out, membership)
	}
	return out, rows.Err()
}

func AddOrganizationMembershipAsAdmin(ctx *config.AppContext, organizationID, personID, role, addedByPersonID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	personID = strings.TrimSpace(personID)
	role = strings.ToLower(strings.TrimSpace(role))
	addedByPersonID = strings.TrimSpace(addedByPersonID)
	if organizationID == "" || personID == "" {
		return fmt.Errorf("organization and person are required")
	}
	if !validOrganizationRole(role, true) {
		return fmt.Errorf("organization role must be owner, manager, or member")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin add organization membership: %w", err)
	}
	defer tx.Rollback(dbctx)
	commandTag, err := tx.Exec(dbctx, `
		INSERT INTO organization_memberships (
			organization_id, person_id, role, status, invited_by_person_id
		) VALUES ($1::uuid, $2::uuid, $3, 'active', NULLIF($4, '')::uuid)
		ON CONFLICT (organization_id, person_id) DO UPDATE SET
			role = EXCLUDED.role, status = 'active',
			invited_by_person_id = EXCLUDED.invited_by_person_id
		WHERE organization_memberships.status = 'removed'
	`, organizationID, personID, role, addedByPersonID)
	if err != nil {
		return fmt.Errorf("add organization membership: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("that person is already an active organization member")
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE organization_member_invites invites
		SET revoked_at = now()
		WHERE invites.organization_id = $1::uuid
		  AND invites.accepted_at IS NULL AND invites.revoked_at IS NULL
		  AND EXISTS (
			SELECT 1 FROM person_emails
			WHERE person_emails.person_id = $2::uuid
			  AND person_emails.email = invites.email
		  )
	`, organizationID, personID); err != nil {
		return fmt.Errorf("revoke obsolete organization invitations: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit add organization membership: %w", err)
	}
	return nil
}

func UpdateOrganizationMembershipRoleAsAdmin(ctx *config.AppContext, organizationID, personID, role string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	personID = strings.TrimSpace(personID)
	role = strings.ToLower(strings.TrimSpace(role))
	if organizationID == "" || personID == "" {
		return fmt.Errorf("organization and person are required")
	}
	if !validOrganizationRole(role, true) {
		return fmt.Errorf("organization role must be owner, manager, or member")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin organization membership role update: %w", err)
	}
	defer tx.Rollback(dbctx)

	roles, owners, err := lockedActiveOrganizationRoles(dbctx, tx, organizationID)
	if err != nil {
		return err
	}
	currentRole, active := roles[personID]
	if !active {
		return fmt.Errorf("that person is not an active organization member")
	}
	if currentRole == OrganizationRoleOwner && role != OrganizationRoleOwner && owners <= 1 {
		return fmt.Errorf("the organization's last active owner cannot be demoted")
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE organization_memberships SET role = $3, updated_at = now()
		WHERE organization_id = $1::uuid AND person_id = $2::uuid AND status = 'active'
	`, organizationID, personID, role); err != nil {
		return fmt.Errorf("update organization membership role: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit organization membership role update: %w", err)
	}
	return nil
}

// UpdateOrganizationMembershipRole applies an owner-authorized role change.
// Keeping this check in the transaction prevents a concurrent demotion from
// bypassing the last-owner invariant.
func UpdateOrganizationMembershipRole(ctx *config.AppContext, organizationID, actorPersonID, targetPersonID, role string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	actorPersonID = strings.TrimSpace(actorPersonID)
	targetPersonID = strings.TrimSpace(targetPersonID)
	role = strings.ToLower(strings.TrimSpace(role))
	if organizationID == "" || actorPersonID == "" || targetPersonID == "" {
		return fmt.Errorf("organization, actor, and member are required")
	}
	if !validOrganizationRole(role, true) {
		return fmt.Errorf("organization role must be owner, manager, or member")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin organization role update: %w", err)
	}
	defer tx.Rollback(dbctx)
	roles, owners, err := lockedActiveOrganizationRoles(dbctx, tx, organizationID)
	if err != nil {
		return err
	}
	if roles[actorPersonID] != OrganizationRoleOwner {
		return fmt.Errorf("only organization owners can change member roles")
	}
	currentRole, active := roles[targetPersonID]
	if !active {
		return fmt.Errorf("that person is not an active organization member")
	}
	if currentRole == OrganizationRoleOwner && role != OrganizationRoleOwner && owners <= 1 {
		return fmt.Errorf("the organization's last active owner cannot be demoted")
	}
	commandTag, err := tx.Exec(dbctx, `
		UPDATE organization_memberships
		SET role = $3, updated_at = now()
		WHERE organization_id = $1::uuid AND person_id = $2::uuid
		  AND status = 'active'
	`, organizationID, targetPersonID, role)
	if err != nil {
		return fmt.Errorf("update organization member role: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("organization member role was not updated")
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit organization role update: %w", err)
	}
	return nil
}

func UpdateOrganizationMemberInviteRoleAsAdmin(ctx *config.AppContext, organizationID, inviteID, role string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	inviteID = strings.TrimSpace(inviteID)
	role = strings.ToLower(strings.TrimSpace(role))
	if organizationID == "" || inviteID == "" {
		return fmt.Errorf("organization and invitation are required")
	}
	if !validOrganizationRole(role, false) {
		return fmt.Errorf("invitation role must be manager or member")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE organization_member_invites SET role = $3
		WHERE id = $1::uuid AND organization_id = $2::uuid
		  AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
	`, inviteID, organizationID, role)
	if err != nil {
		return fmt.Errorf("update organization invitation role: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("pending organization invitation not found")
	}
	return nil
}

func RevokeOrganizationMemberInviteAsAdmin(ctx *config.AppContext, organizationID, inviteID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	inviteID = strings.TrimSpace(inviteID)
	if organizationID == "" || inviteID == "" {
		return fmt.Errorf("organization and invitation are required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE organization_member_invites SET revoked_at = now()
		WHERE id = $1::uuid AND organization_id = $2::uuid
		  AND accepted_at IS NULL AND revoked_at IS NULL
	`, inviteID, organizationID)
	if err != nil {
		return fmt.Errorf("revoke organization invitation: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("pending organization invitation not found")
	}
	return nil
}

func validOrganizationRole(role string, allowOwner bool) bool {
	return role == OrganizationRoleManager || role == OrganizationRoleMember || (allowOwner && role == OrganizationRoleOwner)
}

func lockedActiveOrganizationRoles(dbctx context.Context, tx pgx.Tx, organizationID string) (map[string]string, int, error) {
	rows, err := tx.Query(dbctx, `
		SELECT person_id::text, role
		FROM organization_memberships
		WHERE organization_id = $1::uuid AND status = 'active'
		FOR UPDATE
	`, organizationID)
	if err != nil {
		return nil, 0, fmt.Errorf("lock organization memberships: %w", err)
	}
	defer rows.Close()
	roles := make(map[string]string)
	owners := 0
	for rows.Next() {
		var lockedPersonID, lockedRole string
		if err := rows.Scan(&lockedPersonID, &lockedRole); err != nil {
			return nil, 0, fmt.Errorf("scan organization membership lock: %w", err)
		}
		roles[lockedPersonID] = lockedRole
		if lockedRole == OrganizationRoleOwner {
			owners++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate organization memberships: %w", err)
	}
	return roles, owners, nil
}

func RemoveOrganizationMembership(ctx *config.AppContext, organizationID, actorPersonID, targetPersonID string) error {
	return removeOrganizationMembership(ctx, organizationID, actorPersonID, targetPersonID, false)
}

func RemoveOrganizationMembershipAsAdmin(ctx *config.AppContext, organizationID, targetPersonID string) error {
	return removeOrganizationMembership(ctx, organizationID, "", targetPersonID, true)
}

func removeOrganizationMembership(ctx *config.AppContext, organizationID, actorPersonID, targetPersonID string, admin bool) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	actorPersonID = strings.TrimSpace(actorPersonID)
	targetPersonID = strings.TrimSpace(targetPersonID)
	if organizationID == "" || (!admin && actorPersonID == "") || targetPersonID == "" {
		return fmt.Errorf("organization, actor, and member are required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin organization membership removal: %w", err)
	}
	defer tx.Rollback(dbctx)

	roles, owners, err := lockedActiveOrganizationRoles(dbctx, tx, organizationID)
	if err != nil {
		return err
	}

	actorRole, actorActive := roles[actorPersonID]
	targetRole, targetActive := roles[targetPersonID]
	if !admin && !actorActive {
		return fmt.Errorf("you are not an active member of this organization")
	}
	if !targetActive {
		return fmt.Errorf("that person is not an active organization member")
	}
	removingSelf := !admin && actorPersonID == targetPersonID
	if !admin && !removingSelf {
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
			competitions.hacking_starts_at,
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
		var hackingStartsAt pgtype.Timestamptz
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
			&competitionID, &competitionTitle, &hackingStartsAt,
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
			if hackingStartsAt.Valid {
				value := hackingStartsAt.Time
				event.Competition.HackingStartsAt = &value
			}
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sponsor dashboard events: %w", err)
	}
	return out, nil
}

// ListSponsorSpeakerApplications returns proposals for sponsored conferences
// when at least one attached speaker is an active member of the organization.
// The grouping prevents a co-speaker proposal from appearing more than once.
func ListSponsorSpeakerApplications(ctx *config.AppContext, organizationID string) ([]*types.SponsorSpeakerApplication, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	if organizationID == "" {
		return nil, fmt.Errorf("organization is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		WITH sponsored_conferences AS (
			SELECT DISTINCT links.conference_id
			FROM sponsorships
			JOIN sponsorships_conferences links ON links.sponsorship_id = sponsorships.id
			WHERE sponsorships.organization_id = $1::uuid
			  AND sponsorships.archived_at IS NULL
		)
		SELECT proposals.id::text, proposals.conference_id::text,
			proposals.title, proposals.talk_type, proposals.status,
			proposals.desired_duration_min, proposals.created_at,
			array_agg(DISTINCT people.name ORDER BY people.name)
		FROM sponsored_conferences
		JOIN conferences ON conferences.id = sponsored_conferences.conference_id
		JOIN proposals ON proposals.conference_id = sponsored_conferences.conference_id
		JOIN proposals_speaker_confs proposal_speakers
		  ON proposal_speakers.proposal_id = proposals.id
		JOIN speaker_confs ON speaker_confs.id = proposal_speakers.speaker_conf_id
		JOIN organization_memberships memberships
		  ON memberships.person_id = speaker_confs.speaker_id
		 AND memberships.organization_id = $1::uuid
		 AND memberships.status = 'active'
		JOIN people ON people.id = memberships.person_id
		WHERE conferences.end_date IS NULL OR conferences.end_date >= now()
		GROUP BY proposals.id, proposals.conference_id, proposals.title,
			proposals.talk_type, proposals.status, proposals.desired_duration_min,
			proposals.created_at, conferences.start_date
		ORDER BY conferences.start_date, proposals.created_at, lower(proposals.title), proposals.id
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list sponsor speaker applications: %w", err)
	}
	defer rows.Close()

	var applications []*types.SponsorSpeakerApplication
	for rows.Next() {
		application := &types.SponsorSpeakerApplication{}
		if err := rows.Scan(
			&application.ProposalID, &application.ConferenceID,
			&application.Title, &application.TalkType, &application.Status,
			&application.DesiredMinutes, &application.SubmittedAt,
			&application.MemberNames,
		); err != nil {
			return nil, fmt.Errorf("scan sponsor speaker application: %w", err)
		}
		applications = append(applications, application)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sponsor speaker applications: %w", err)
	}
	return applications, nil
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
				EXISTS (
					SELECT 1
					FROM project_awards podium_wins
					JOIN awards podium_awards ON podium_awards.id = podium_wins.award_id
					WHERE podium_wins.project_id = projects.id
					  AND podium_awards.competition_id = access.competition_id
					  AND podium_awards.archived_at IS NULL
					  AND podium_awards.award_type = 'normal'
					  AND podium_awards.award_rank BETWEEN 1 AND 3
				) AS general_podium_winner,
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
				false AS winner,
				EXISTS (
					SELECT 1
					FROM project_awards podium_wins
					JOIN awards podium_awards ON podium_awards.id = podium_wins.award_id
					WHERE podium_wins.project_id = projects.id
					  AND podium_awards.competition_id = access.competition_id
					  AND podium_awards.archived_at IS NULL
					  AND podium_awards.award_type = 'normal'
					  AND podium_awards.award_rank BETWEEN 1 AND 3
				) AS general_podium_winner,
				false AS sponsored_prize,
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
			entries.winner, entries.general_podium_winner,
			entries.sponsored_prize, entries.automatic_contact,
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
			&entry.GeneralPodiumWinner,
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

func normalizeSponsorAwardProposalInput(in SponsorAwardProposalInput) (SponsorAwardProposalInput, error) {
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
	if in.Title == "" {
		return SponsorAwardProposalInput{}, fmt.Errorf("challenge name is required")
	}
	if in.MaxAwardees < 1 || in.MaxAwardees > 100 {
		return SponsorAwardProposalInput{}, fmt.Errorf("max awardees must be between 1 and 100")
	}
	switch in.PrizeType {
	case PrizeTypeSats, PrizeTypeInKind:
	default:
		return SponsorAwardProposalInput{}, fmt.Errorf("sponsors can offer only Bitcoin or a physical prize; ticket prizes must be added by hackathon administrators")
	}
	if in.PrizeType == PrizeTypeInKind && in.PrizeTitle == "" {
		return SponsorAwardProposalInput{}, fmt.Errorf("describe the physical prize each winner will receive")
	}
	if in.PrizeType == PrizeTypeSats || in.PrizeValueText != "" {
		value, err := strconv.ParseInt(in.PrizeValueText, 10, 64)
		if err != nil || value <= 0 {
			return SponsorAwardProposalInput{}, fmt.Errorf("prize amount or estimated value must be a positive whole number of satoshis")
		}
		in.PrizeValueText = strconv.FormatInt(value, 10)
		if in.PrizeType == PrizeTypeSats && in.PrizeTitle == "" {
			in.PrizeTitle = in.PrizeValueText + " sats per winner"
		}
	}
	return in, nil
}

func CreateSponsorAwardProposal(ctx *config.AppContext, in SponsorAwardProposalInput) (*types.SponsorAwardProposal, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var err error
	in, err = normalizeSponsorAwardProposalInput(in)
	if err != nil {
		return nil, err
	}

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
		  AND (coalesce(competitions.hacking_starts_at, conferences.start_date) IS NULL
		       OR coalesce(competitions.hacking_starts_at, conferences.start_date) > now())
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

// UpdateSponsorAwardProposal updates both proposals awaiting review and the
// live award/prize created from an approved proposal. The database timing
// check is authoritative so a stale dashboard cannot edit after hacking starts.
func UpdateSponsorAwardProposal(ctx *config.AppContext, proposalID, organizationID string, in SponsorAwardProposalInput) (*types.SponsorAwardProposal, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	proposalID = strings.TrimSpace(proposalID)
	organizationID = strings.TrimSpace(organizationID)
	var err error
	in, err = normalizeSponsorAwardProposalInput(in)
	if err != nil {
		return nil, err
	}
	if proposalID == "" || organizationID == "" {
		return nil, fmt.Errorf("challenge and organization are required")
	}

	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin sponsor challenge update: %w", err)
	}
	defer tx.Rollback(dbctx)

	proposal := &types.SponsorAwardProposal{}
	var awardID, prizeID string
	var hackingStartsAt, conferenceStartsAt pgtype.Timestamptz
	if err := tx.QueryRow(dbctx, `
		SELECT proposals.id::text, proposals.sponsorship_id::text,
			proposals.conference_id::text, proposals.competition_id::text,
			sponsorships.organization_id::text, proposals.status,
			coalesce(proposals.award_id::text, ''),
			coalesce(primary_prize.id::text, ''),
			competitions.hacking_starts_at, conferences.start_date
		FROM sponsor_award_proposals proposals
		JOIN sponsorships ON sponsorships.id = proposals.sponsorship_id
		JOIN competitions ON competitions.id = proposals.competition_id
		JOIN conferences ON conferences.id = proposals.conference_id
		LEFT JOIN LATERAL (
			SELECT prizes.id FROM prizes
			WHERE prizes.award_id = proposals.award_id
			ORDER BY prizes.created_at, prizes.id LIMIT 1
		) primary_prize ON true
		WHERE proposals.id = $1::uuid
		  AND sponsorships.organization_id = $2::uuid
		FOR UPDATE OF proposals
	`, proposalID, organizationID).Scan(
		&proposal.ID, &proposal.SponsorshipID, &proposal.ConferenceID,
		&proposal.CompetitionID, &proposal.OrganizationID, &proposal.Status,
		&awardID, &prizeID, &hackingStartsAt, &conferenceStartsAt,
	); err != nil {
		return nil, fmt.Errorf("sponsor challenge not found")
	}
	if proposal.Status != "pending" && proposal.Status != "approved" {
		return nil, fmt.Errorf("only pending or approved challenges can be edited")
	}
	startsAt := conferenceStartsAt
	if hackingStartsAt.Valid {
		startsAt = hackingStartsAt
	}
	if startsAt.Valid && !startsAt.Time.After(time.Now()) {
		return nil, fmt.Errorf("this challenge is read-only because the hackathon has started")
	}

	if _, err := tx.Exec(dbctx, `
		UPDATE sponsor_award_proposals SET
			title = $2, description = $3, judging_instructions = $4,
			max_awardees = $5, opt_in_required = $6, finalists_only = $7,
			prize_type = $8, prize_title = $9, prize_description = $10,
			prize_value_text = $11
		WHERE id = $1::uuid
	`, proposal.ID, in.Title, in.Description, in.JudgingInstructions,
		in.MaxAwardees, in.OptInRequired, in.FinalistsOnly, in.PrizeType,
		in.PrizeTitle, in.PrizeDescription, in.PrizeValueText); err != nil {
		return nil, fmt.Errorf("update sponsor challenge proposal: %w", err)
	}
	if proposal.Status == "approved" {
		if awardID == "" || prizeID == "" {
			return nil, fmt.Errorf("approved challenge is missing its award or prize")
		}
		commandTag, err := tx.Exec(dbctx, `
			UPDATE awards SET title = $3, description = $4, judging_instructions = $5,
				max_awardees = $6, opt_in_required = $7, finalists_only = $8
			WHERE id = $1::uuid AND competition_id = $2::uuid
			  AND sponsored_by_org_id = $9::uuid AND archived_at IS NULL
		`, awardID, proposal.CompetitionID, in.Title, in.Description,
			in.JudgingInstructions, in.MaxAwardees, in.OptInRequired,
			in.FinalistsOnly, organizationID)
		if err != nil {
			return nil, fmt.Errorf("update approved sponsor challenge: %w", err)
		}
		if commandTag.RowsAffected() != 1 {
			return nil, fmt.Errorf("approved sponsor challenge is no longer available")
		}
		if !in.OptInRequired {
			if _, err := tx.Exec(dbctx, `DELETE FROM project_award_opt_ins WHERE award_id = $1::uuid`, awardID); err != nil {
				return nil, fmt.Errorf("clear sponsor challenge opt-ins: %w", err)
			}
		}
		commandTag, err = tx.Exec(dbctx, `
			UPDATE prizes SET prize_type = $3, title = $4, description = $5, value_text = $6
			WHERE id = $1::uuid AND award_id = $2::uuid
		`, prizeID, awardID, in.PrizeType, in.PrizeTitle,
			in.PrizeDescription, in.PrizeValueText)
		if err != nil {
			return nil, fmt.Errorf("update approved sponsor challenge prize: %w", err)
		}
		if commandTag.RowsAffected() != 1 {
			return nil, fmt.Errorf("approved sponsor challenge prize is no longer available")
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit sponsor challenge update: %w", err)
	}
	proposal.Title = in.Title
	proposal.Description = in.Description
	proposal.JudgingInstructions = in.JudgingInstructions
	proposal.MaxAwardees = in.MaxAwardees
	proposal.OptInRequired = in.OptInRequired
	proposal.FinalistsOnly = in.FinalistsOnly
	proposal.PrizeType = in.PrizeType
	proposal.PrizeTitle = in.PrizeTitle
	proposal.PrizeDescription = in.PrizeDescription
	proposal.PrizeValueText = in.PrizeValueText
	proposal.AwardID = awardID
	proposal.PrizeID = prizeID
	return proposal, nil
}

func ListSponsorAwardProposalsForOrganization(ctx *config.AppContext, organizationID string) ([]*types.SponsorAwardProposal, error) {
	return listSponsorAwardProposals(ctx, `WHERE sponsorships.organization_id = $1::uuid`, strings.TrimSpace(organizationID))
}

// ListSponsorChallengesForOrganization includes both challenges submitted
// through the sponsor proposal workflow and older/organizer-created awards.
// Organizer-created awards have no sponsor proposal to update and are exposed
// as read-only records in the sponsor workspace.
func ListSponsorChallengesForOrganization(ctx *config.AppContext, organizationID string) ([]*types.SponsorAwardProposal, error) {
	organizationID = strings.TrimSpace(organizationID)
	proposals, err := ListSponsorAwardProposalsForOrganization(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	legacy, err := listOrganizerManagedSponsorAwards(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	proposals = append(proposals, legacy...)
	sort.SliceStable(proposals, func(i, j int) bool {
		return proposals[i].CreatedAt.After(proposals[j].CreatedAt)
	})
	return proposals, nil
}

func listOrganizerManagedSponsorAwards(ctx *config.AppContext, organizationID string) ([]*types.SponsorAwardProposal, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT awards.id::text, awards.competition_id::text,
			organizations.id::text, organizations.name,
			awards.title, awards.description, awards.judging_instructions,
			awards.max_awardees, awards.opt_in_required, awards.finalists_only,
			awards.status, awards.created_at, awards.updated_at,
			coalesce(primary_prize.id::text, ''), coalesce(primary_prize.prize_type, ''),
			coalesce(primary_prize.title, ''), coalesce(primary_prize.description, ''),
			coalesce(primary_prize.value_text, ''),
			conferences.id::text,
			coalesce(nullif(conferences.description, ''), conferences.tag),
			competitions.title,
			coalesce(competitions.hacking_starts_at, conferences.start_date)
		FROM awards
		JOIN organizations ON organizations.id = awards.sponsored_by_org_id
		JOIN competitions ON competitions.id = awards.competition_id
		JOIN conferences ON conferences.id = competitions.conference_id
		LEFT JOIN LATERAL (
			SELECT prizes.id, prizes.prize_type, prizes.title, prizes.description,
				prizes.value_text
			FROM prizes
			WHERE prizes.award_id = awards.id
			ORDER BY prizes.created_at, prizes.id
			LIMIT 1
		) primary_prize ON true
		WHERE awards.sponsored_by_org_id = $1::uuid
		  AND awards.archived_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM sponsor_award_proposals proposals
			WHERE proposals.award_id = awards.id
		  )
		  AND EXISTS (
			SELECT 1
			FROM sponsorships
			JOIN sponsorships_conferences links ON links.sponsorship_id = sponsorships.id
			WHERE sponsorships.organization_id = awards.sponsored_by_org_id
			  AND links.conference_id = conferences.id
			  AND sponsorships.archived_at IS NULL
		  )
		ORDER BY awards.created_at DESC, awards.id
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organizer-managed sponsor awards: %w", err)
	}
	defer rows.Close()
	var out []*types.SponsorAwardProposal
	for rows.Next() {
		challenge := &types.SponsorAwardProposal{OrganizerManaged: true}
		var editableUntil pgtype.Timestamptz
		if err := rows.Scan(
			&challenge.AwardID, &challenge.CompetitionID,
			&challenge.OrganizationID, &challenge.OrganizationName,
			&challenge.Title, &challenge.Description, &challenge.JudgingInstructions,
			&challenge.MaxAwardees, &challenge.OptInRequired, &challenge.FinalistsOnly,
			&challenge.Status, &challenge.CreatedAt, &challenge.UpdatedAt,
			&challenge.PrizeID, &challenge.PrizeType, &challenge.PrizeTitle,
			&challenge.PrizeDescription, &challenge.PrizeValueText,
			&challenge.ConferenceID, &challenge.ConferenceTitle,
			&challenge.CompetitionTitle, &editableUntil,
		); err != nil {
			return nil, fmt.Errorf("scan organizer-managed sponsor award: %w", err)
		}
		if editableUntil.Valid {
			value := editableUntil.Time
			challenge.EditableUntil = &value
		}
		out = append(out, challenge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate organizer-managed sponsor awards: %w", err)
	}
	return out, nil
}

func ListSponsorAwardProposalsForCompetition(ctx *config.AppContext, competitionID string) ([]*types.SponsorAwardProposal, error) {
	return listSponsorAwardProposals(ctx, `WHERE proposals.competition_id = $1::uuid`, strings.TrimSpace(competitionID))
}

func GetSponsorAwardProposal(ctx *config.AppContext, proposalID string) (*types.SponsorAwardProposal, error) {
	proposals, err := listSponsorAwardProposals(ctx, `WHERE proposals.id = $1::uuid`, strings.TrimSpace(proposalID))
	if err != nil {
		return nil, err
	}
	if len(proposals) == 0 {
		return nil, fmt.Errorf("sponsor award proposal not found")
	}
	return proposals[0], nil
}

// ListSponsorResultNotificationRecipients returns the managers of every
// organization entitled to see the finalized competition: sponsors with full
// submission access and organizations that issued an award are both included.
func ListSponsorResultNotificationRecipients(ctx *config.AppContext, competitionID string) ([]*types.SponsorOrganizationRecipient, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		WITH eligible_organizations AS (
			SELECT sponsorships.organization_id
			FROM competitions
			JOIN sponsorships_conferences links ON links.conference_id = competitions.conference_id
			JOIN sponsorships ON sponsorships.id = links.sponsorship_id
			JOIN sponsorship_entitlements entitlements
			  ON entitlements.sponsorship_id = sponsorships.id
			 AND entitlements.conference_id = competitions.conference_id
			WHERE competitions.id = $1::uuid
			  AND sponsorships.archived_at IS NULL
			  AND lower(sponsorships.status) IN ('paid', 'committed')
			  AND entitlements.all_hackathon_submissions_access
			UNION
			SELECT awards.sponsored_by_org_id
			FROM awards
			WHERE awards.competition_id = $1::uuid
			  AND awards.sponsored_by_org_id IS NOT NULL
			  AND awards.archived_at IS NULL
		)
		SELECT organizations.id::text, organizations.name,
			memberships.person_id::text, people.name, contact.email::text
		FROM eligible_organizations eligible
		JOIN organizations ON organizations.id = eligible.organization_id
		JOIN organization_memberships memberships
		  ON memberships.organization_id = eligible.organization_id
		JOIN people ON people.id = memberships.person_id
		JOIN LATERAL (
			SELECT email FROM person_emails
			WHERE person_id = people.id AND verified_at IS NOT NULL
			ORDER BY is_primary DESC, verified_at DESC, created_at, id LIMIT 1
		) contact ON true
		WHERE memberships.status = 'active'
		  AND memberships.role IN ('owner', 'manager')
		ORDER BY organizations.name, lower(contact.email::text), memberships.person_id
	`, strings.TrimSpace(competitionID))
	if err != nil {
		return nil, fmt.Errorf("list sponsor result notification recipients: %w", err)
	}
	defer rows.Close()
	var recipients []*types.SponsorOrganizationRecipient
	for rows.Next() {
		recipient := &types.SponsorOrganizationRecipient{}
		if err := rows.Scan(&recipient.OrganizationID, &recipient.OrganizationName,
			&recipient.PersonID, &recipient.Name, &recipient.Email); err != nil {
			return nil, fmt.Errorf("scan sponsor result notification recipient: %w", err)
		}
		recipients = append(recipients, recipient)
	}
	return recipients, rows.Err()
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
			coalesce(proposals.award_id::text, ''), proposals.created_at, proposals.updated_at,
			coalesce(primary_prize.id::text, ''),
			coalesce(nullif(conferences.description, ''), conferences.tag), competitions.title,
			coalesce(competitions.hacking_starts_at, conferences.start_date)
		FROM sponsor_award_proposals proposals
		JOIN sponsorships ON sponsorships.id = proposals.sponsorship_id
		JOIN organizations ON organizations.id = sponsorships.organization_id
		JOIN competitions ON competitions.id = proposals.competition_id
		JOIN conferences ON conferences.id = proposals.conference_id
		LEFT JOIN people submitter ON submitter.id = proposals.submitted_by_person_id
		LEFT JOIN LATERAL (
			SELECT prizes.id FROM prizes
			WHERE prizes.award_id = proposals.award_id
			ORDER BY prizes.created_at, prizes.id LIMIT 1
		) primary_prize ON true
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
		var reviewedAt, editableUntil pgtype.Timestamptz
		if err := rows.Scan(&proposal.ID, &proposal.SponsorshipID, &proposal.ConferenceID,
			&proposal.CompetitionID, &proposal.OrganizationID, &proposal.OrganizationName,
			&proposal.SubmittedByPersonID, &proposal.SubmittedByName, &proposal.Title,
			&proposal.Description, &proposal.JudgingInstructions, &proposal.MaxAwardees,
			&proposal.OptInRequired, &proposal.FinalistsOnly, &proposal.PrizeType,
			&proposal.PrizeTitle, &proposal.PrizeDescription, &proposal.PrizeValueText,
			&proposal.Status, &proposal.ReviewNotes, &proposal.ReviewedByPersonID,
			&reviewedAt, &proposal.AwardID, &proposal.CreatedAt, &proposal.UpdatedAt,
			&proposal.PrizeID, &proposal.ConferenceTitle, &proposal.CompetitionTitle,
			&editableUntil); err != nil {
			return nil, fmt.Errorf("scan sponsor award proposal: %w", err)
		}
		if reviewedAt.Valid {
			value := reviewedAt.Time
			proposal.ReviewedAt = &value
		}
		if editableUntil.Valid {
			value := editableUntil.Time
			proposal.EditableUntil = &value
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
	return GetSponsorAwardProposal(ctx, proposal.ID)
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
