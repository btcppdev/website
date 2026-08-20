package getters

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

type APITokenAuthentication struct {
	Token     *types.PersonAPIToken
	TokenHash []byte
}

func ListPersonAPITokens(ctx *config.AppContext, personID string) ([]*types.PersonAPIToken, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, person_id::text, name, token_selector, scopes,
			created_at, last_used_at, expires_at, revoked_at, updated_at
		FROM person_api_tokens
		WHERE person_id = $1::uuid
		ORDER BY created_at DESC, id
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list API tokens: %w", err)
	}
	defer rows.Close()
	var tokens []*types.PersonAPIToken
	for rows.Next() {
		token := &types.PersonAPIToken{}
		var selector string
		if err := rows.Scan(&token.ID, &token.PersonID, &token.Name, &selector,
			&token.Scopes, &token.CreatedAt, &token.LastUsedAt, &token.ExpiresAt,
			&token.RevokedAt, &token.UpdatedAt); err != nil {
			return nil, err
		}
		token.Prefix = types.APITokenVersion + "." + selector
		token.Expired = !token.ExpiresAt.After(time.Now())
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func CreatePersonAPIToken(ctx *config.AppContext, personID, name, selector string, tokenHash []byte, scopes []string, expiresAt time.Time) (*types.PersonAPIToken, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return nil, errors.New("token name must be between 1 and 80 characters")
	}
	if selector == "" || len(tokenHash) != 32 || !types.ValidAPITokenScopes(scopes) {
		return nil, errors.New("invalid API token credential")
	}
	if !expiresAt.After(time.Now()) {
		return nil, errors.New("API token expiry must be in the future")
	}
	token := &types.PersonAPIToken{}
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO person_api_tokens (person_id, name, token_selector, token_hash, scopes, expires_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)
		RETURNING id::text, person_id::text, name, scopes, created_at,
			last_used_at, expires_at, revoked_at, updated_at
	`, personID, name, selector, tokenHash, scopes, expiresAt).Scan(
		&token.ID, &token.PersonID, &token.Name, &token.Scopes, &token.CreatedAt,
		&token.LastUsedAt, &token.ExpiresAt, &token.RevokedAt, &token.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create API token: %w", err)
	}
	token.Prefix = types.APITokenVersion + "." + selector
	token.Expired = !token.ExpiresAt.After(time.Now())
	return token, nil
}

func RevokePersonAPIToken(ctx *config.AppContext, personID, tokenID string) (*types.PersonAPIToken, error) {
	token := &types.PersonAPIToken{}
	var selector string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		UPDATE person_api_tokens
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1::uuid AND person_id = $2::uuid
		RETURNING id::text, person_id::text, name, token_selector, scopes,
			created_at, last_used_at, expires_at, revoked_at, updated_at
	`, tokenID, personID).Scan(&token.ID, &token.PersonID, &token.Name, &selector,
		&token.Scopes, &token.CreatedAt, &token.LastUsedAt, &token.ExpiresAt,
		&token.RevokedAt, &token.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("revoke API token: %w", err)
	}
	token.Prefix = types.APITokenVersion + "." + selector
	token.Expired = !token.ExpiresAt.After(time.Now())
	return token, nil
}

func FindActiveAPIToken(ctx *config.AppContext, selector string) (*APITokenAuthentication, error) {
	token := &types.PersonAPIToken{}
	var tokenHash []byte
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, person_id::text, name, scopes, token_hash,
			created_at, last_used_at, expires_at, revoked_at, updated_at
		FROM person_api_tokens
		WHERE token_selector = $1 AND revoked_at IS NULL AND expires_at > now()
	`, selector).Scan(&token.ID, &token.PersonID, &token.Name, &token.Scopes,
		&tokenHash, &token.CreatedAt, &token.LastUsedAt, &token.ExpiresAt,
		&token.RevokedAt, &token.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find API token: %w", err)
	}
	token.Prefix = types.APITokenVersion + "." + selector
	token.Expired = false
	return &APITokenAuthentication{Token: token, TokenHash: tokenHash}, nil
}

// MarkPersonAPITokenUsed records coarse-grained activity without turning every
// API request into a database write.
func MarkPersonAPITokenUsed(ctx *config.AppContext, tokenID string) error {
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE person_api_tokens
		SET last_used_at = now()
		WHERE id = $1::uuid
			AND revoked_at IS NULL
			AND expires_at > now()
			AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes')
	`, tokenID)
	if err != nil {
		return fmt.Errorf("mark API token used: %w", err)
	}
	return nil
}
