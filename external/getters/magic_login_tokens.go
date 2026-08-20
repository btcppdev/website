package getters

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"

	"github.com/jackc/pgx/v5"
)

const MagicLoginTokenTTL = 72 * time.Hour

var ErrMagicLoginTokenInvalid = errors.New("magic login token is invalid, expired, or already used")

func CreateMagicLoginToken(ctx *config.AppContext, email, next string) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", errors.New("database is not configured")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	next = strings.TrimSpace(next)
	if email == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "", errors.New("valid email and relative destination are required")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate magic login token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO magic_login_tokens (email, token_hash, next_path, expires_at)
		VALUES ($1::citext, $2, $3, now() + $4::interval)
	`, email, hash[:], next, postgresInterval(MagicLoginTokenTTL))
	if err != nil {
		return "", fmt.Errorf("create magic login token: %w", err)
	}
	return token, nil
}

func ConsumeMagicLoginToken(ctx *config.AppContext, token string) (email, next string, err error) {
	if ctx == nil || ctx.DB == nil {
		return "", "", errors.New("database is not configured")
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(token)))
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
		UPDATE magic_login_tokens
		SET consumed_at = now()
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
		RETURNING email::text, next_path
	`, hash[:]).Scan(&email, &next)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrMagicLoginTokenInvalid
	}
	if err != nil {
		return "", "", fmt.Errorf("consume magic login token: %w", err)
	}
	return email, next, nil
}

func MagicLoginTokenValid(ctx *config.AppContext, token string) (bool, error) {
	_, _, valid, _, err := LookupMagicLoginToken(ctx, token)
	return valid, err
}

// LookupMagicLoginToken inspects a token without consuming it. found remains
// true for expired and consumed tokens so a handler can offer to send a fresh
// link to the same mailbox without exposing that address in the browser.
func LookupMagicLoginToken(ctx *config.AppContext, token string) (email, next string, valid, found bool, err error) {
	if ctx == nil || ctx.DB == nil {
		return "", "", false, false, errors.New("database is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", false, false, nil
	}
	hash := sha256.Sum256([]byte(token))
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT email::text, next_path, consumed_at IS NULL AND expires_at > now()
		FROM magic_login_tokens
		WHERE token_hash = $1
	`, hash[:]).Scan(&email, &next, &valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, false, nil
	}
	if err != nil {
		return "", "", false, false, fmt.Errorf("lookup magic login token: %w", err)
	}
	return email, next, valid, true, nil
}
