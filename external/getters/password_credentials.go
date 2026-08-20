package getters

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

var ErrPasswordResetTokenInvalid = errors.New("password reset token is invalid or expired")

func GetPersonPasswordCredential(ctx *config.AppContext, personID string) (*types.PersonPasswordCredential, error) {
	credential := &types.PersonPasswordCredential{}
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT person_id::text, password_hash, failed_attempts, locked_until,
			created_at, password_changed_at, updated_at
		FROM person_password_credentials
		WHERE person_id = $1::uuid
	`, personID).Scan(&credential.PersonID, &credential.PasswordHash, &credential.FailedAttempts,
		&credential.LockedUntil, &credential.CreatedAt, &credential.PasswordChangedAt, &credential.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get password credential: %w", err)
	}
	return credential, nil
}

// SetPersonPassword adds or replaces a password. Adding the first password
// preserves existing sessions; replacing one advances the session version.
func SetPersonPassword(ctx *config.AppContext, personID, passwordHash string) (version int64, replaced bool, err error) {
	queryCtx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(queryCtx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(queryCtx)
	inserted, err := tx.Exec(queryCtx, `
		INSERT INTO person_password_credentials (person_id, password_hash)
		VALUES ($1::uuid, $2)
		ON CONFLICT (person_id) DO NOTHING
	`, personID, strings.TrimSpace(passwordHash))
	if err != nil {
		return 0, false, fmt.Errorf("set password: %w", err)
	}
	replaced = inserted.RowsAffected() == 0
	if replaced {
		if _, err := tx.Exec(queryCtx, `
			UPDATE person_password_credentials
			SET password_hash = $2, failed_attempts = 0,
				locked_until = NULL, password_changed_at = now()
			WHERE person_id = $1::uuid
		`, personID, strings.TrimSpace(passwordHash)); err != nil {
			return 0, false, fmt.Errorf("replace password: %w", err)
		}
		version, err = incrementSessionVersion(queryCtx, tx, personID)
	} else {
		version, err = ensureSessionVersion(queryCtx, tx, personID)
	}
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(queryCtx); err != nil {
		return 0, false, err
	}
	return version, replaced, nil
}

func RecordPasswordFailure(ctx *config.AppContext, personID string) error {
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE person_password_credentials
		SET failed_attempts = CASE
				WHEN locked_until > now() THEN failed_attempts
				WHEN locked_until IS NOT NULL THEN 1
				ELSE failed_attempts + 1
			END,
			locked_until = CASE
				WHEN locked_until > now() THEN locked_until
				WHEN locked_until IS NOT NULL THEN NULL
				WHEN failed_attempts + 1 >= 10 THEN now() + interval '15 minutes'
				ELSE NULL
			END
		WHERE person_id = $1::uuid
	`, personID)
	return err
}

func RecordPasswordSuccess(ctx *config.AppContext, personID string) error {
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE person_password_credentials
		SET failed_attempts = 0, locked_until = NULL
		WHERE person_id = $1::uuid
	`, personID)
	return err
}

func CreatePasswordResetToken(ctx *config.AppContext, personID, requestedEmail string, ttl time.Duration) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO password_reset_tokens (person_id, requested_email, token_hash, expires_at)
		VALUES ($1::uuid, $2::citext, $3, now() + $4::interval)
	`, personID, strings.ToLower(strings.TrimSpace(requestedEmail)), hash[:], postgresInterval(ttl))
	if err != nil {
		return "", fmt.Errorf("create password reset token: %w", err)
	}
	return token, nil
}

func PasswordResetTokenValid(ctx *config.AppContext, token string) (bool, error) {
	hash := sha256.Sum256([]byte(strings.TrimSpace(token)))
	var valid bool
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT EXISTS (
			SELECT 1 FROM password_reset_tokens
			WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
		)
	`, hash[:]).Scan(&valid)
	return valid, err
}

func ConsumePasswordResetToken(ctx *config.AppContext, token, passwordHash string) (string, int64, error) {
	queryCtx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(queryCtx)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback(queryCtx)
	hash := sha256.Sum256([]byte(strings.TrimSpace(token)))
	var personID string
	err = tx.QueryRow(queryCtx, `
		SELECT person_id::text
		FROM password_reset_tokens
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
		FOR UPDATE
	`, hash[:]).Scan(&personID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrPasswordResetTokenInvalid
	}
	if err != nil {
		return "", 0, err
	}
	if _, err := tx.Exec(queryCtx, `
		INSERT INTO person_password_credentials (person_id, password_hash)
		VALUES ($1::uuid, $2)
		ON CONFLICT (person_id) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			failed_attempts = 0, locked_until = NULL, password_changed_at = now()
	`, personID, passwordHash); err != nil {
		return "", 0, err
	}
	if _, err := tx.Exec(queryCtx, `
		UPDATE password_reset_tokens SET consumed_at = now()
		WHERE person_id = $1::uuid AND consumed_at IS NULL
	`, personID); err != nil {
		return "", 0, err
	}
	version, err := incrementSessionVersion(queryCtx, tx, personID)
	if err != nil {
		return "", 0, err
	}
	if err := tx.Commit(queryCtx); err != nil {
		return "", 0, err
	}
	return personID, version, nil
}

func PersonSessionVersion(ctx *config.AppContext, personID string) (int64, error) {
	var version int64
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		WITH inserted AS (
			INSERT INTO person_auth_security (person_id) VALUES ($1::uuid)
			ON CONFLICT (person_id) DO NOTHING
			RETURNING session_version
		)
		SELECT session_version FROM inserted
		UNION ALL
		SELECT session_version FROM person_auth_security WHERE person_id = $1::uuid
		LIMIT 1
	`, personID).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("person session version: %w", err)
	}
	return version, nil
}

// RevokePersonSessions advances the account-wide session version. Callers
// refresh only the browser that authorized the credential change; every other
// session becomes invalid on its next request.
func RevokePersonSessions(ctx *config.AppContext, personID string) (int64, error) {
	var version int64
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO person_auth_security (person_id, session_version) VALUES ($1::uuid, 2)
		ON CONFLICT (person_id) DO UPDATE
		SET session_version = person_auth_security.session_version + 1
		RETURNING session_version
	`, strings.TrimSpace(personID)).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("revoke person sessions: %w", err)
	}
	return version, nil
}

func incrementSessionVersion(queryCtx context.Context, tx pgx.Tx, personID string) (int64, error) {
	var version int64
	err := tx.QueryRow(queryCtx, `
		INSERT INTO person_auth_security (person_id, session_version) VALUES ($1::uuid, 2)
		ON CONFLICT (person_id) DO UPDATE SET session_version = person_auth_security.session_version + 1
		RETURNING session_version
	`, personID).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("increment session version: %w", err)
	}
	return version, nil
}

func ensureSessionVersion(queryCtx context.Context, tx pgx.Tx, personID string) (int64, error) {
	var version int64
	err := tx.QueryRow(queryCtx, `
		WITH inserted AS (
			INSERT INTO person_auth_security (person_id) VALUES ($1::uuid)
			ON CONFLICT (person_id) DO NOTHING
			RETURNING session_version
		)
		SELECT session_version FROM inserted
		UNION ALL
		SELECT session_version FROM person_auth_security WHERE person_id = $1::uuid
		LIMIT 1
	`, personID).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("ensure session version: %w", err)
	}
	return version, nil
}

func postgresInterval(duration time.Duration) string {
	return fmt.Sprintf("%f seconds", duration.Seconds())
}
