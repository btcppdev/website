package getters

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

var ErrOAuthIdentityLinked = errors.New("oauth identity is linked to another person")

func FindOAuthIdentity(ctx *config.AppContext, provider, subject string) (*types.PersonOAuthIdentity, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	subject = strings.TrimSpace(subject)
	var identity types.PersonOAuthIdentity
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, person_id::text, provider, provider_subject,
			provider_username, coalesce(provider_email::text, ''),
			provider_email_verified, avatar_url, linked_at, last_login_at, updated_at
		FROM person_oauth_identities
		WHERE provider = $1 AND provider_subject = $2
	`, provider, subject).Scan(
		&identity.ID, &identity.PersonID, &identity.Provider, &identity.Subject,
		&identity.Username, &identity.Email, &identity.EmailVerified,
		&identity.AvatarURL, &identity.LinkedAt, &identity.LastLoginAt, &identity.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find oauth identity: %w", err)
	}
	return &identity, nil
}

func ListPersonOAuthIdentities(ctx *config.AppContext, personID string) ([]*types.PersonOAuthIdentity, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, person_id::text, provider, provider_subject,
			provider_username, coalesce(provider_email::text, ''),
			provider_email_verified, avatar_url, linked_at, last_login_at, updated_at
		FROM person_oauth_identities
		WHERE person_id = $1::uuid
		ORDER BY provider, linked_at, id
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list person oauth identities: %w", err)
	}
	defer rows.Close()

	var identities []*types.PersonOAuthIdentity
	for rows.Next() {
		var identity types.PersonOAuthIdentity
		if err := rows.Scan(
			&identity.ID, &identity.PersonID, &identity.Provider, &identity.Subject,
			&identity.Username, &identity.Email, &identity.EmailVerified,
			&identity.AvatarURL, &identity.LinkedAt, &identity.LastLoginAt, &identity.UpdatedAt,
		); err != nil {
			return nil, err
		}
		identities = append(identities, &identity)
	}
	return identities, rows.Err()
}

func LinkOAuthIdentity(ctx *config.AppContext, personID string, identity *types.PersonOAuthIdentity) (*types.PersonOAuthIdentity, error) {
	if identity == nil {
		return nil, errors.New("oauth identity is required")
	}
	provider := strings.ToLower(strings.TrimSpace(identity.Provider))
	subject := strings.TrimSpace(identity.Subject)
	if strings.TrimSpace(personID) == "" || provider == "" || subject == "" {
		return nil, errors.New("person, provider, and subject are required")
	}

	var linked types.PersonOAuthIdentity
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO person_oauth_identities (
			person_id, provider, provider_subject, provider_username,
			provider_email, provider_email_verified, avatar_url
		)
		VALUES ($1::uuid, $2, $3, $4, NULLIF($5, '')::citext, $6, $7)
		ON CONFLICT (provider, provider_subject) DO UPDATE SET
			provider_username = EXCLUDED.provider_username,
			provider_email = EXCLUDED.provider_email,
			provider_email_verified = EXCLUDED.provider_email_verified,
			avatar_url = EXCLUDED.avatar_url,
			updated_at = now()
		WHERE person_oauth_identities.person_id = EXCLUDED.person_id
		RETURNING id::text, person_id::text, provider, provider_subject,
			provider_username, coalesce(provider_email::text, ''),
			provider_email_verified, avatar_url, linked_at, last_login_at, updated_at
	`, personID, provider, subject, strings.TrimSpace(identity.Username),
		strings.ToLower(strings.TrimSpace(identity.Email)), identity.EmailVerified,
		strings.TrimSpace(identity.AvatarURL)).Scan(
		&linked.ID, &linked.PersonID, &linked.Provider, &linked.Subject,
		&linked.Username, &linked.Email, &linked.EmailVerified, &linked.AvatarURL,
		&linked.LinkedAt, &linked.LastLoginAt, &linked.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOAuthIdentityLinked
	}
	if err != nil {
		return nil, fmt.Errorf("link oauth identity: %w", err)
	}
	return &linked, nil
}

func MarkOAuthIdentityLogin(ctx *config.AppContext, identityID string) error {
	command, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE person_oauth_identities
		SET last_login_at = now(), updated_at = now()
		WHERE id = $1::uuid
	`, identityID)
	if err != nil {
		return fmt.Errorf("mark oauth identity login: %w", err)
	}
	if command.RowsAffected() == 0 {
		return fmt.Errorf("oauth identity not found")
	}
	return nil
}

func UnlinkOAuthIdentity(ctx *config.AppContext, personID, provider, identityID string) (*types.PersonOAuthIdentity, error) {
	var identity types.PersonOAuthIdentity
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		DELETE FROM person_oauth_identities
		WHERE id = $1::uuid AND person_id = $2::uuid AND provider = $3
		RETURNING id::text, person_id::text, provider, provider_subject,
			provider_username, coalesce(provider_email::text, ''),
			provider_email_verified, avatar_url, linked_at, last_login_at, updated_at
	`, identityID, personID, strings.ToLower(strings.TrimSpace(provider))).Scan(
		&identity.ID, &identity.PersonID, &identity.Provider, &identity.Subject,
		&identity.Username, &identity.Email, &identity.EmailVerified,
		&identity.AvatarURL, &identity.LinkedAt, &identity.LastLoginAt, &identity.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unlink oauth identity: %w", err)
	}
	return &identity, nil
}

func RecordAuthAuditEvent(ctx *config.AppContext, event *types.AuthAuditEvent) error {
	if event == nil || strings.TrimSpace(event.Event) == "" {
		return errors.New("auth audit event is required")
	}
	metadata := event.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal auth audit metadata: %w", err)
	}
	_, err = ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO auth_audit_events (
			person_id, method, event, remote_address, user_agent, metadata
		)
		VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6::jsonb)
	`, strings.TrimSpace(event.PersonID), strings.TrimSpace(event.Method),
		strings.TrimSpace(event.Event), strings.TrimSpace(event.RemoteAddress),
		strings.TrimSpace(event.UserAgent), metadataJSON)
	if err != nil {
		return fmt.Errorf("record auth audit event: %w", err)
	}
	return nil
}
