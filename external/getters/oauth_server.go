package getters

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

func GetOAuthClient(ctx *config.AppContext, clientID string) (*types.OAuthClient, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var client types.OAuthClient
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, client_id, name, redirect_uris, allowed_scopes,
			token_endpoint_auth_method, coalesce(created_by_person_id::text, ''),
			created_at, revoked_at, client_secret_hash
		FROM oauth_clients WHERE client_id = $1
	`, strings.TrimSpace(clientID)).Scan(
		&client.ID, &client.ClientID, &client.Name, &client.RedirectURIs,
		&client.AllowedScopes, &client.TokenEndpointAuthMethod,
		&client.CreatedByPersonID, &client.CreatedAt, &client.RevokedAt,
		&client.ClientSecretHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get OAuth client: %w", err)
	}
	if client.RevokedAt != nil {
		return nil, nil
	}
	return &client, nil
}

func ListOAuthClients(ctx *config.AppContext) ([]*types.OAuthClient, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, client_id, name, redirect_uris, allowed_scopes,
			token_endpoint_auth_method, coalesce(created_by_person_id::text, ''),
			created_at, revoked_at, client_secret_hash
		FROM oauth_clients ORDER BY created_at DESC, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list OAuth clients: %w", err)
	}
	defer rows.Close()
	var clients []*types.OAuthClient
	for rows.Next() {
		var client types.OAuthClient
		if err := rows.Scan(&client.ID, &client.ClientID, &client.Name, &client.RedirectURIs,
			&client.AllowedScopes, &client.TokenEndpointAuthMethod, &client.CreatedByPersonID,
			&client.CreatedAt, &client.RevokedAt, &client.ClientSecretHash); err != nil {
			return nil, fmt.Errorf("scan OAuth client: %w", err)
		}
		clients = append(clients, &client)
	}
	return clients, rows.Err()
}

func CreateOAuthClient(ctx *config.AppContext, client *types.OAuthClient) error {
	if ctx == nil || ctx.DB == nil || client == nil {
		return fmt.Errorf("OAuth client data is required")
	}
	return ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO oauth_clients (
			client_id, client_secret_hash, name, redirect_uris, allowed_scopes,
			token_endpoint_auth_method, created_by_person_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7::uuid)
		RETURNING id::text, created_at
	`, client.ClientID, client.ClientSecretHash, client.Name, client.RedirectURIs,
		client.AllowedScopes, client.TokenEndpointAuthMethod, client.CreatedByPersonID,
	).Scan(&client.ID, &client.CreatedAt)
}

func RevokeOAuthClient(ctx *config.AppContext, clientID string) error {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin OAuth client revocation: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if _, err := tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_clients SET revoked_at = coalesce(revoked_at, now()) WHERE id = $1::uuid`, clientID); err != nil {
		return fmt.Errorf("revoke OAuth client: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_access_tokens SET revoked_at = coalesce(revoked_at, now()) WHERE client_id = $1::uuid`, clientID); err != nil {
		return fmt.Errorf("revoke OAuth client access tokens: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_refresh_tokens SET revoked_at = coalesce(revoked_at, now()) WHERE client_id = $1::uuid`, clientID); err != nil {
		return fmt.Errorf("revoke OAuth client refresh tokens: %w", err)
	}
	return tx.Commit(ctx.DatabaseContext())
}

func StoreOAuthAuthorizationCode(ctx *config.AppContext, codeHash []byte, code *types.OAuthAuthorizationCode) error {
	if ctx == nil || ctx.DB == nil || code == nil || len(codeHash) == 0 {
		return fmt.Errorf("OAuth authorization code data is required")
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO oauth_authorization_codes (
			code_hash, client_id, person_id, redirect_uri, scopes,
			code_challenge, code_challenge_method, expires_at
		) VALUES ($1, $2::uuid, $3::uuid, $4, $5, $6, 'S256', $7)
	`, codeHash, code.ClientDBID, code.PersonID, code.RedirectURI,
		code.Scopes, code.CodeChallenge, code.ExpiresAt)
	if err != nil {
		return fmt.Errorf("store OAuth authorization code: %w", err)
	}
	return nil
}

func UpsertOAuthConsent(ctx *config.AppContext, personID, clientDBID string, scopes []string) error {
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO oauth_consents (person_id, client_id, scopes)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (person_id, client_id) DO UPDATE SET
			scopes = EXCLUDED.scopes, updated_at = now(), revoked_at = NULL
	`, personID, clientDBID, scopes)
	if err != nil {
		return fmt.Errorf("store OAuth consent: %w", err)
	}
	return nil
}

func ListPersonOAuthConsents(ctx *config.AppContext, personID string) ([]*types.OAuthConsent, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT client.id::text, client.client_id, client.name, consent.scopes,
			consent.granted_at, consent.updated_at
		FROM oauth_consents consent
		JOIN oauth_clients client ON client.id = consent.client_id
		WHERE consent.person_id = $1::uuid AND consent.revoked_at IS NULL
			AND client.revoked_at IS NULL
		ORDER BY consent.updated_at DESC, client.name
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list OAuth consents: %w", err)
	}
	defer rows.Close()
	var out []*types.OAuthConsent
	for rows.Next() {
		var consent types.OAuthConsent
		if err := rows.Scan(&consent.ClientDBID, &consent.ClientID, &consent.ClientName,
			&consent.Scopes, &consent.GrantedAt, &consent.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan OAuth consent: %w", err)
		}
		out = append(out, &consent)
	}
	return out, rows.Err()
}

func RevokePersonOAuthConsent(ctx *config.AppContext, personID, clientDBID string) error {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin OAuth consent revocation: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	if _, err := tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_consents SET revoked_at = coalesce(revoked_at, now()), updated_at = now() WHERE person_id = $1::uuid AND client_id = $2::uuid`, personID, clientDBID); err != nil {
		return fmt.Errorf("revoke OAuth consent: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_access_tokens SET revoked_at = coalesce(revoked_at, now()) WHERE person_id = $1::uuid AND client_id = $2::uuid`, personID, clientDBID); err != nil {
		return fmt.Errorf("revoke consent access tokens: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_refresh_tokens SET revoked_at = coalesce(revoked_at, now()) WHERE person_id = $1::uuid AND client_id = $2::uuid`, personID, clientDBID); err != nil {
		return fmt.Errorf("revoke consent refresh tokens: %w", err)
	}
	return tx.Commit(ctx.DatabaseContext())
}

type OAuthTokenIssue struct {
	AccessSelector string
	AccessHash     []byte
	RefreshHash    []byte
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
}

type OAuthTokenGrant struct {
	AccessTokenID   string
	PersonID        string
	ClientDBID      string
	Scopes          []string
	AccessExpiresAt time.Time
	RefreshTokenID  string
}

func RedeemOAuthAuthorizationCode(ctx *config.AppContext, codeHash []byte, clientDBID, redirectURI, verifierChallenge string, issue OAuthTokenIssue) (*OAuthTokenGrant, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, fmt.Errorf("begin OAuth code exchange: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var personID, storedClientID, storedRedirect, storedChallenge string
	var scopes []string
	var expiresAt time.Time
	err = tx.QueryRow(ctx.DatabaseContext(), `
		UPDATE oauth_authorization_codes SET consumed_at = now()
		WHERE code_hash = $1 AND consumed_at IS NULL AND expires_at > now()
		RETURNING person_id::text, client_id::text, redirect_uri, scopes, code_challenge, expires_at
	`, codeHash).Scan(&personID, &storedClientID, &storedRedirect, &scopes, &storedChallenge, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consume OAuth authorization code: %w", err)
	}
	if storedClientID != clientDBID || storedRedirect != redirectURI || !bytes.Equal([]byte(storedChallenge), []byte(verifierChallenge)) {
		return nil, nil
	}
	grant := &OAuthTokenGrant{PersonID: personID, ClientDBID: storedClientID, Scopes: scopes}
	familyID := ""
	if len(issue.RefreshHash) > 0 && oauthScopePresent(scopes, "offline_access") {
		if err := tx.QueryRow(ctx.DatabaseContext(), `SELECT gen_random_uuid()::text`).Scan(&familyID); err != nil {
			return nil, fmt.Errorf("create OAuth refresh family: %w", err)
		}
	}
	err = tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO oauth_access_tokens (selector, token_hash, client_id, person_id, scopes, refresh_family_id, expires_at)
		VALUES ($1, $2, $3::uuid, $4::uuid, $5, NULLIF($6, '')::uuid, now() + $7::interval)
		RETURNING id::text, expires_at
	`, issue.AccessSelector, issue.AccessHash, storedClientID, personID, scopes, familyID, durationInterval(issue.AccessTTL)).Scan(&grant.AccessTokenID, &grant.AccessExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("issue OAuth access token: %w", err)
	}
	if familyID != "" {
		err = tx.QueryRow(ctx.DatabaseContext(), `
			INSERT INTO oauth_refresh_tokens (token_hash, family_id, client_id, person_id, scopes, expires_at)
			VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5, now() + $6::interval)
			RETURNING id::text
		`, issue.RefreshHash, familyID, storedClientID, personID, scopes, durationInterval(issue.RefreshTTL)).Scan(&grant.RefreshTokenID)
		if err != nil {
			return nil, fmt.Errorf("issue OAuth refresh token: %w", err)
		}
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, fmt.Errorf("commit OAuth code exchange: %w", err)
	}
	return grant, nil
}

func FindActiveOAuthAccessToken(ctx *config.AppContext, selector string) (*types.OAuthAccessToken, []byte, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, nil, fmt.Errorf("database is not configured")
	}
	var token types.OAuthAccessToken
	var hash []byte
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT token.id::text, token.client_id::text, client.client_id,
			token.person_id::text, token.scopes, token.created_at, token.expires_at,
			token.last_used_at, token.token_hash
		FROM oauth_access_tokens token
		JOIN oauth_clients client ON client.id = token.client_id
		WHERE token.selector = $1 AND token.revoked_at IS NULL
			AND token.expires_at > now() AND client.revoked_at IS NULL
	`, selector).Scan(&token.ID, &token.ClientDBID, &token.ClientID, &token.PersonID,
		&token.Scopes, &token.CreatedAt, &token.ExpiresAt, &token.LastUsedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("find OAuth access token: %w", err)
	}
	return &token, hash, nil
}

func MarkOAuthAccessTokenUsed(ctx *config.AppContext, id string) error {
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE oauth_access_tokens SET last_used_at = now() WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("mark OAuth access token used: %w", err)
	}
	return nil
}

func RotateOAuthRefreshToken(ctx *config.AppContext, refreshHash []byte, clientDBID string, issue OAuthTokenIssue) (*OAuthTokenGrant, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, fmt.Errorf("begin OAuth refresh: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var oldID, familyID, storedClientID, personID string
	var scopes []string
	var expiresAt time.Time
	var consumedAt, revokedAt *time.Time
	err = tx.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, family_id::text, client_id::text, person_id::text,
			scopes, expires_at, consumed_at, revoked_at
		FROM oauth_refresh_tokens WHERE token_hash = $1 FOR UPDATE
	`, refreshHash).Scan(&oldID, &familyID, &storedClientID, &personID, &scopes, &expiresAt, &consumedAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load OAuth refresh token: %w", err)
	}
	if consumedAt != nil {
		_, _ = tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_refresh_tokens SET revoked_at = coalesce(revoked_at, now()) WHERE family_id = $1::uuid`, familyID)
		_, _ = tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_access_tokens SET revoked_at = coalesce(revoked_at, now()) WHERE refresh_family_id = $1::uuid`, familyID)
		_ = tx.Commit(ctx.DatabaseContext())
		return nil, nil
	}
	if revokedAt != nil || expiresAt.Before(time.Now()) || storedClientID != clientDBID {
		return nil, nil
	}
	grant := &OAuthTokenGrant{PersonID: personID, ClientDBID: storedClientID, Scopes: scopes}
	err = tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO oauth_access_tokens (selector, token_hash, client_id, person_id, scopes, refresh_family_id, expires_at)
		VALUES ($1, $2, $3::uuid, $4::uuid, $5, $6::uuid, now() + $7::interval)
		RETURNING id::text, expires_at
	`, issue.AccessSelector, issue.AccessHash, storedClientID, personID, scopes, familyID, durationInterval(issue.AccessTTL)).Scan(&grant.AccessTokenID, &grant.AccessExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("refresh OAuth access token: %w", err)
	}
	err = tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO oauth_refresh_tokens (token_hash, family_id, client_id, person_id, scopes, expires_at)
		VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5, now() + $6::interval)
		RETURNING id::text
	`, issue.RefreshHash, familyID, storedClientID, personID, scopes, durationInterval(issue.RefreshTTL)).Scan(&grant.RefreshTokenID)
	if err != nil {
		return nil, fmt.Errorf("rotate OAuth refresh token: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_refresh_tokens SET consumed_at = now(), replaced_by_id = $2::uuid WHERE id = $1::uuid`, oldID, grant.RefreshTokenID); err != nil {
		return nil, fmt.Errorf("consume OAuth refresh token: %w", err)
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, fmt.Errorf("commit OAuth refresh: %w", err)
	}
	return grant, nil
}

func RevokeOAuthAccessToken(ctx *config.AppContext, selector string, tokenHash []byte, clientDBID string) error {
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE oauth_access_tokens SET revoked_at = coalesce(revoked_at, now())
		WHERE selector = $1 AND token_hash = $2 AND client_id = $3::uuid
	`, selector, tokenHash, clientDBID)
	if err != nil {
		return fmt.Errorf("revoke OAuth access token: %w", err)
	}
	return nil
}

func RevokeOAuthRefreshToken(ctx *config.AppContext, tokenHash []byte, clientDBID string) error {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin refresh token revocation: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var familyID string
	err = tx.QueryRow(ctx.DatabaseContext(), `SELECT family_id::text FROM oauth_refresh_tokens WHERE token_hash = $1 AND client_id = $2::uuid`, tokenHash, clientDBID).Scan(&familyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find refresh token family: %w", err)
	}
	_, err = tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_refresh_tokens SET revoked_at = coalesce(revoked_at, now()) WHERE family_id = $1::uuid`, familyID)
	if err != nil {
		return fmt.Errorf("revoke OAuth refresh token: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `UPDATE oauth_access_tokens SET revoked_at = coalesce(revoked_at, now()) WHERE refresh_family_id = $1::uuid`, familyID); err != nil {
		return fmt.Errorf("revoke OAuth refresh family access tokens: %w", err)
	}
	return tx.Commit(ctx.DatabaseContext())
}

func CleanupOAuthServerState(ctx *config.AppContext) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	statements := []string{
		`DELETE FROM oauth_authorization_codes WHERE expires_at < now() - interval '1 day' OR consumed_at < now() - interval '1 day'`,
		`DELETE FROM oauth_access_tokens WHERE expires_at < now() - interval '7 days' OR revoked_at < now() - interval '7 days'`,
		`DELETE FROM oauth_refresh_tokens WHERE expires_at < now() - interval '30 days' OR revoked_at < now() - interval '30 days'`,
		`DELETE FROM oauth_consents WHERE revoked_at < now() - interval '90 days'`,
	}
	for _, statement := range statements {
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), statement); err != nil {
			return fmt.Errorf("clean OAuth server state: %w", err)
		}
	}
	return nil
}

func durationInterval(value time.Duration) string {
	return fmt.Sprintf("%f seconds", value.Seconds())
}

func oauthScopePresent(scopes []string, wanted string) bool {
	for _, scope := range scopes {
		if scope == wanted {
			return true
		}
	}
	return false
}
