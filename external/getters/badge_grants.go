package getters

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	BadgeGrantStateGranted       = "granted"
	BadgeGrantStateReady         = "ready_to_issue"
	BadgeGrantStateIssued        = "issued"
	BadgeGrantStateAccepted      = "accepted"
	BadgeGrantStateRevoked       = "revoked"
	BadgeGrantStateDeliveryError = "delivery_error"
	BadgeGrantStateCanceled      = "canceled"
	BadgeGrantStateCorrected     = "corrected"
)

var ErrBadgeGrantConflict = errors.New("an active grant already exists for this person and badge")

type OrganizationBadgeGrantInput struct {
	OrganizationID, RecipientPersonID, CreatedByPersonID                                         string
	IssuerPubkey, BadgeIdentifier, BadgeName, BadgeDescription, BadgeImageURL, SubjectProfileURL string
}

func CreateOrganizationBadgeGrant(ctx *config.AppContext, input OrganizationBadgeGrantInput) (*types.OrganizationBadgeGrant, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	input.OrganizationID = strings.TrimSpace(input.OrganizationID)
	input.RecipientPersonID = strings.TrimSpace(input.RecipientPersonID)
	input.CreatedByPersonID = strings.TrimSpace(input.CreatedByPersonID)
	input.IssuerPubkey = strings.ToLower(strings.TrimSpace(input.IssuerPubkey))
	input.BadgeIdentifier = strings.TrimSpace(input.BadgeIdentifier)
	input.BadgeName = strings.TrimSpace(input.BadgeName)
	input.BadgeDescription = strings.TrimSpace(input.BadgeDescription)
	input.BadgeImageURL = strings.TrimSpace(input.BadgeImageURL)
	input.SubjectProfileURL = strings.TrimSpace(input.SubjectProfileURL)
	if input.OrganizationID == "" || input.RecipientPersonID == "" || input.CreatedByPersonID == "" || len(input.IssuerPubkey) != 64 || input.BadgeIdentifier == "" || input.BadgeName == "" || !validBadgeCredentialURL(input.BadgeImageURL) || !validBadgeCredentialURL(input.SubjectProfileURL) {
		return nil, fmt.Errorf("organization, recipient, issuer, badge, artwork, and public subject profile are required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin badge grant: %w", err)
	}
	defer tx.Rollback(dbctx)
	var allowed bool
	if err := tx.QueryRow(dbctx, `SELECT EXISTS (SELECT 1 FROM organization_memberships WHERE organization_id=$1::uuid AND person_id=$2::uuid AND status='active' AND role IN ('owner','manager'))`, input.OrganizationID, input.CreatedByPersonID).Scan(&allowed); err != nil {
		return nil, fmt.Errorf("check badge grant manager: %w", err)
	}
	if !allowed {
		return nil, fmt.Errorf("organization owner or manager access is required")
	}
	var recipientName, recipientPubkey string
	if err := tx.QueryRow(dbctx, `
		SELECT people.name, coalesce(credentials.pubkey_hex, '')
		FROM people
		LEFT JOIN person_nostr_credentials credentials ON credentials.person_id=people.id AND credentials.verified_at IS NOT NULL
		WHERE people.id=$1::uuid`, input.RecipientPersonID).Scan(&recipientName, &recipientPubkey); errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("recipient profile was not found")
	} else if err != nil {
		return nil, fmt.Errorf("load badge grant recipient: %w", err)
	}
	state := BadgeGrantStateGranted
	var readyAt *time.Time
	if recipientPubkey != "" {
		state = BadgeGrantStateReady
		now := time.Now().UTC()
		readyAt = &now
	}
	grant := &types.OrganizationBadgeGrant{}
	err = tx.QueryRow(dbctx, `
		INSERT INTO organization_badge_grants (
			organization_id, recipient_person_id, created_by_person_id, issuer_pubkey,
			badge_identifier, badge_name, badge_description, badge_image_url,
			subject_profile_url, recipient_pubkey, state, ready_at
		) VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id::text, granted_at, updated_at`,
		input.OrganizationID, input.RecipientPersonID, input.CreatedByPersonID, input.IssuerPubkey,
		input.BadgeIdentifier, input.BadgeName, input.BadgeDescription, input.BadgeImageURL,
		input.SubjectProfileURL, recipientPubkey, state, readyAt).Scan(&grant.ID, &grant.GrantedAt, &grant.UpdatedAt)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.ConstraintName == "organization_badge_grants_active_unique_idx" {
			return nil, ErrBadgeGrantConflict
		}
		return nil, fmt.Errorf("create badge grant: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit badge grant: %w", err)
	}
	grant.OrganizationID, grant.RecipientPersonID, grant.RecipientName, grant.CreatedByPersonID = input.OrganizationID, input.RecipientPersonID, recipientName, input.CreatedByPersonID
	grant.IssuerPubkey, grant.BadgeIdentifier, grant.BadgeName, grant.BadgeDescription = input.IssuerPubkey, input.BadgeIdentifier, input.BadgeName, input.BadgeDescription
	grant.BadgeImageURL, grant.SubjectProfileURL, grant.RecipientPubkey, grant.State, grant.ReadyAt = input.BadgeImageURL, input.SubjectProfileURL, recipientPubkey, state, readyAt
	return grant, nil
}

func validBadgeCredentialURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// PromotePendingBadgeGrants binds durable Bitcoin++ grants to a newly verified
// Nostr key. Rows are locked before the state change so concurrent requests
// cannot both report the same transition as new.
func PromotePendingBadgeGrants(ctx *config.AppContext, personID string) ([]*types.OrganizationBadgeGrant, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin badge grant promotion: %w", err)
	}
	defer tx.Rollback(dbctx)
	rows, err := tx.Query(dbctx, `
		SELECT grants.id::text
		FROM organization_badge_grants grants
		JOIN person_nostr_credentials credentials
		  ON credentials.person_id=grants.recipient_person_id AND credentials.verified_at IS NOT NULL
		WHERE grants.state IN ('granted','ready_to_issue','delivery_error')
		  AND ($1='' OR grants.recipient_person_id=NULLIF($1,'')::uuid)
		  AND (grants.recipient_pubkey<>credentials.pubkey_hex OR grants.state<>'ready_to_issue')
		ORDER BY grants.granted_at, grants.id
		FOR UPDATE OF grants`, strings.TrimSpace(personID))
	if err != nil {
		return nil, fmt.Errorf("lock badge grants for promotion: %w", err)
	}
	var grantIDs []string
	for rows.Next() {
		var grantID string
		if err := rows.Scan(&grantID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan badge grant promotion: %w", err)
		}
		grantIDs = append(grantIDs, grantID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate badge grant promotions: %w", err)
	}
	rows.Close()
	for _, grantID := range grantIDs {
		if _, err := tx.Exec(dbctx, `
			UPDATE organization_badge_grants grants
			SET recipient_pubkey=credentials.pubkey_hex, state='ready_to_issue',
				ready_at=now(), delivery_error=''
			FROM person_nostr_credentials credentials
			WHERE grants.id=$1::uuid AND credentials.person_id=grants.recipient_person_id
			  AND credentials.verified_at IS NOT NULL`, grantID); err != nil {
			return nil, fmt.Errorf("promote badge grant %s: %w", grantID, err)
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit badge grant promotion: %w", err)
	}
	promoted := make([]*types.OrganizationBadgeGrant, 0, len(grantIDs))
	for _, grantID := range grantIDs {
		grant, err := GetBadgeGrant(ctx, grantID)
		if err != nil {
			return nil, fmt.Errorf("load promoted badge grant %s: %w", grantID, err)
		}
		if grant != nil {
			promoted = append(promoted, grant)
		}
	}
	return promoted, nil
}

func refreshPendingBadgeGrants(ctx *config.AppContext, personID string) error {
	if _, err := PromotePendingBadgeGrants(ctx, personID); err != nil {
		return err
	}
	dbctx := ctx.DatabaseContext()
	if _, err := ctx.DB.Exec(dbctx, `
		UPDATE organization_badge_grants grants
		SET recipient_pubkey='', state='granted', ready_at=NULL, delivery_error=''
		WHERE grants.state IN ('ready_to_issue','delivery_error')
		  AND ($1='' OR grants.recipient_person_id=NULLIF($1,'')::uuid)
		  AND NOT EXISTS (SELECT 1 FROM person_nostr_credentials credentials WHERE credentials.person_id=grants.recipient_person_id AND credentials.verified_at IS NOT NULL)`, personID); err != nil {
		return fmt.Errorf("demote badge grants without verified Nostr keys: %w", err)
	}
	return nil
}

func ListOrganizationBadgeGrants(ctx *config.AppContext, organizationID string) ([]*types.OrganizationBadgeGrant, error) {
	if err := refreshPendingBadgeGrants(ctx, ""); err != nil {
		return nil, err
	}
	return queryBadgeGrants(ctx, `WHERE grants.organization_id=$1::uuid`, strings.TrimSpace(organizationID))
}

func ListPersonBadgeGrants(ctx *config.AppContext, personID string) ([]*types.OrganizationBadgeGrant, error) {
	personID = strings.TrimSpace(personID)
	if err := refreshPendingBadgeGrants(ctx, personID); err != nil {
		return nil, err
	}
	return queryBadgeGrants(ctx, `WHERE grants.recipient_person_id=$1::uuid AND grants.state NOT IN ('canceled','corrected')`, personID)
}

func queryBadgeGrants(ctx *config.AppContext, where string, arg any) ([]*types.OrganizationBadgeGrant, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT grants.id::text, grants.organization_id::text, organizations.name,
			grants.recipient_person_id::text, people.name, coalesce(grants.created_by_person_id::text,''),
			grants.issuer_pubkey, grants.badge_identifier, grants.badge_name, grants.badge_description,
			grants.badge_image_url, grants.subject_profile_url, grants.recipient_pubkey, grants.state,
			grants.award_event_id, grants.acceptance_event_id, grants.revocation_event_id,
			grants.revocation_reason, grants.delivery_error,
			coalesce(grants.corrected_by_grant_id::text,''), grants.granted_at, grants.ready_at,
			grants.issued_at, grants.accepted_at, grants.revoked_at, grants.canceled_at, grants.updated_at
		FROM organization_badge_grants grants
		JOIN organizations ON organizations.id=grants.organization_id
		JOIN people ON people.id=grants.recipient_person_id
		`+where+` ORDER BY grants.granted_at DESC, grants.id`, arg)
	if err != nil {
		return nil, fmt.Errorf("list badge grants: %w", err)
	}
	defer rows.Close()
	var out []*types.OrganizationBadgeGrant
	for rows.Next() {
		grant := &types.OrganizationBadgeGrant{}
		if err := rows.Scan(&grant.ID, &grant.OrganizationID, &grant.OrganizationName, &grant.RecipientPersonID, &grant.RecipientName, &grant.CreatedByPersonID, &grant.IssuerPubkey, &grant.BadgeIdentifier, &grant.BadgeName, &grant.BadgeDescription, &grant.BadgeImageURL, &grant.SubjectProfileURL, &grant.RecipientPubkey, &grant.State, &grant.AwardEventID, &grant.AcceptanceEventID, &grant.RevocationEventID, &grant.RevocationReason, &grant.DeliveryError, &grant.CorrectedByGrantID, &grant.GrantedAt, &grant.ReadyAt, &grant.IssuedAt, &grant.AcceptedAt, &grant.RevokedAt, &grant.CanceledAt, &grant.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan badge grant: %w", err)
		}
		out = append(out, grant)
	}
	return out, rows.Err()
}

func CancelOrganizationBadgeGrant(ctx *config.AppContext, organizationID, grantID, actorPersonID string) error {
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE organization_badge_grants grants SET state='canceled', canceled_at=now()
		WHERE grants.id=$1::uuid AND grants.organization_id=$2::uuid AND grants.state IN ('granted','ready_to_issue')
		  AND EXISTS (SELECT 1 FROM organization_memberships membership WHERE membership.organization_id=grants.organization_id AND membership.person_id=$3::uuid AND membership.status='active' AND membership.role IN ('owner','manager'))`, grantID, organizationID, actorPersonID)
	if err != nil {
		return fmt.Errorf("cancel badge grant: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("active badge grant was not found")
	}
	return nil
}

func GetBadgeGrant(ctx *config.AppContext, grantID string) (*types.OrganizationBadgeGrant, error) {
	items, err := queryBadgeGrants(ctx, `WHERE grants.id=$1::uuid`, strings.TrimSpace(grantID))
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func MarkBadgeGrantIssued(ctx *config.AppContext, grantID, eventID string, issuedAt time.Time) error {
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE organization_badge_grants SET state='issued', award_event_id=$2, issued_at=$3, delivery_error='' WHERE id=$1::uuid AND state IN ('ready_to_issue','delivery_error')`, grantID, eventID, issuedAt)
	if err != nil {
		return fmt.Errorf("mark badge grant issued: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("badge grant is not ready to issue")
	}
	return nil
}

func MarkBadgeGrantAccepted(ctx *config.AppContext, grantID, eventID string, acceptedAt time.Time) error {
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE organization_badge_grants SET state='accepted', acceptance_event_id=$2, accepted_at=$3 WHERE id=$1::uuid AND state='issued'`, grantID, eventID, acceptedAt)
	if err != nil {
		return fmt.Errorf("mark badge grant accepted: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("badge grant is not issued")
	}
	return nil
}

func MarkBadgeGrantRevoked(ctx *config.AppContext, grantID, eventID, reason string, revokedAt time.Time) error {
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE organization_badge_grants SET state='revoked', revocation_event_id=$2, revocation_reason=$3, revoked_at=$4 WHERE id=$1::uuid AND state IN ('issued','accepted')`, grantID, eventID, strings.TrimSpace(reason), revokedAt)
	if err != nil {
		return fmt.Errorf("mark badge grant revoked: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("badge grant is not issued")
	}
	return nil
}
