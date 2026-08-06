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

const PersonEmailVerificationTTL = 30 * time.Minute

func ListPersonEmails(ctx *config.AppContext, personID string) ([]*types.PersonEmail, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, person_id::text, email::text, is_primary, verified_at,
			coalesce(origin_merge_event_id::text, ''), EXISTS (
				SELECT 1 FROM person_merge_events merge
				WHERE merge.id = person_emails.origin_merge_event_id
					AND merge.status = 'merged' AND merge.undo_expires_at > now()
			), created_at, updated_at
		FROM person_emails
		WHERE person_id = $1::uuid
		ORDER BY is_primary DESC, lower(email::text), id
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list person emails: %w", err)
	}
	defer rows.Close()
	var out []*types.PersonEmail
	for rows.Next() {
		var email types.PersonEmail
		if err := rows.Scan(&email.ID, &email.PersonID, &email.Email, &email.IsPrimary,
			&email.VerifiedAt, &email.OriginMergeEventID, &email.RemovalLocked,
			&email.CreatedAt, &email.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan person email: %w", err)
		}
		out = append(out, &email)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate person emails: %w", err)
	}
	return out, nil
}

func GetPrimaryPersonEmail(ctx *config.AppContext, personID string) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return "", nil
	}
	var email string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT email::text
		FROM person_emails
		WHERE person_id = $1::uuid
		ORDER BY is_primary DESC, created_at, id
		LIMIT 1
	`, personID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get primary person email: %w", err)
	}
	return email, nil
}

func ResolvePersonByEmail(ctx *config.AppContext, rawEmail string) (*types.PersonEmailResolution, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	if email == "" {
		return &types.PersonEmailResolution{}, nil
	}
	resolution := &types.PersonEmailResolution{Email: email}
	var alias types.PersonEmail
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, person_id::text, email::text, is_primary, verified_at,
			coalesce(origin_merge_event_id::text, ''), created_at, updated_at
		FROM person_emails
		WHERE email = $1::citext
	`, email).Scan(&alias.ID, &alias.PersonID, &alias.Email, &alias.IsPrimary,
		&alias.VerifiedAt, &alias.OriginMergeEventID, &alias.CreatedAt, &alias.UpdatedAt)
	if err == nil {
		person, fetchErr := FetchSpeakerByID(ctx, alias.PersonID)
		if fetchErr != nil {
			return nil, fetchErr
		}
		resolution.Alias = &alias
		resolution.Person = person
		return resolution, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("resolve person email: %w", err)
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT person_id::text
		FROM person_email_conflicts
		WHERE email = $1::citext
		ORDER BY person_id
	`, email)
	if err != nil {
		return nil, fmt.Errorf("query person email conflicts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var personID string
		if err := rows.Scan(&personID); err != nil {
			return nil, fmt.Errorf("scan person email conflict: %w", err)
		}
		resolution.ConflictPersonIDs = append(resolution.ConflictPersonIDs, personID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate person email conflicts: %w", err)
	}
	return resolution, nil
}

func ListPersonEmailConflicts(ctx *config.AppContext) ([]*types.PersonEmailConflict, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT conflict.email::text, conflict.person_id::text, person.name, conflict.detected_at
		FROM person_email_conflicts conflict
		JOIN people person ON person.id = conflict.person_id
		ORDER BY lower(conflict.email::text), lower(person.name), conflict.person_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list person email conflicts: %w", err)
	}
	defer rows.Close()
	var out []*types.PersonEmailConflict
	for rows.Next() {
		var conflict types.PersonEmailConflict
		if err := rows.Scan(&conflict.Email, &conflict.PersonID, &conflict.PersonName, &conflict.DetectedAt); err != nil {
			return nil, fmt.Errorf("scan person email conflict: %w", err)
		}
		out = append(out, &conflict)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate person email conflicts: %w", err)
	}
	return out, nil
}

func LinkUnownedRecordsByEmail(ctx *config.AppContext, personID, rawEmail string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	if personID == "" || email == "" {
		return fmt.Errorf("person and email are required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin historical identity link: %w", err)
	}
	defer tx.Rollback(dbctx)
	if err := linkUnownedRecordsByEmailTx(dbctx, tx, personID, email); err != nil {
		return err
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit historical identity link: %w", err)
	}
	return nil
}

func linkUnownedRecordsByEmailTx(dbctx context.Context, tx pgx.Tx, personID, email string) error {
	updates := []struct {
		label string
		sql   string
	}{
		{"registrations", `UPDATE registrations SET person_id = $1::uuid WHERE person_id IS NULL AND email = $2::citext`},
		{"volunteers", `UPDATE volunteers SET person_id = $1::uuid WHERE person_id IS NULL AND email = $2::citext`},
		{"shop orders", `UPDATE shop_orders SET buyer_person_id = $1::uuid WHERE buyer_person_id IS NULL AND buyer_email = $2::citext`},
		{"discounts", `UPDATE discounts SET affiliate_person_id = $1::uuid WHERE affiliate_person_id IS NULL AND affiliate_email = $2::citext`},
		{"affiliate usages", `UPDATE affiliate_usages SET affiliate_person_id = $1::uuid WHERE affiliate_person_id IS NULL AND affiliate_email = $2::citext`},
		{"satellite events", `UPDATE satellite_events SET submitter_person_id = $1::uuid WHERE submitter_person_id IS NULL AND submitter_email = $2::citext`},
	}
	for _, update := range updates {
		if _, err := tx.Exec(dbctx, update.sql, personID, email); err != nil {
			return fmt.Errorf("link %s by verified email: %w", update.label, err)
		}
	}
	return nil
}

func MarkPersonEmailVerified(ctx *config.AppContext, personID, rawEmail string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	if personID == "" || email == "" {
		return fmt.Errorf("person and email are required")
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE person_emails
		SET verified_at = coalesce(verified_at, now()), updated_at = now()
		WHERE person_id = $1::uuid AND email = $2::citext
	`, personID, email)
	if err != nil {
		return fmt.Errorf("verify person email: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("email is not attached to person")
	}
	return nil
}

// ResolveCanonicalPersonID follows an active merge mapping so existing
// sessions for a merged source person continue as the canonical person.
func ResolveCanonicalPersonID(ctx *config.AppContext, rawPersonID string) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	personID := strings.TrimSpace(rawPersonID)
	if personID == "" {
		return "", nil
	}
	seen := make(map[string]bool)
	for range 16 {
		if seen[personID] {
			return "", fmt.Errorf("person merge cycle detected at %s", personID)
		}
		seen[personID] = true
		var canonicalID string
		err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
			SELECT canonical_person_id::text
			FROM person_merge_events
			WHERE source_person_id = $1::uuid AND status = 'merged'
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`, personID).Scan(&canonicalID)
		if errors.Is(err, pgx.ErrNoRows) {
			return personID, nil
		}
		if err != nil {
			return "", fmt.Errorf("resolve canonical person: %w", err)
		}
		personID = canonicalID
	}
	return "", fmt.Errorf("person merge chain is too deep")
}

func CreatePersonEmailVerification(ctx *config.AppContext, personID, rawEmail string, makePrimary bool) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	if personID == "" || email == "" {
		return "", fmt.Errorf("person and email are required")
	}
	resolution, err := ResolvePersonByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	if resolution.IsConflict() {
		return "", fmt.Errorf("email belongs to unresolved duplicate profiles")
	}
	if resolution.Alias != nil {
		if resolution.Alias.PersonID == personID {
			return "", fmt.Errorf("email is already attached to this account")
		}
		return "", fmt.Errorf("email is already attached to another account")
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return "", fmt.Errorf("generate email verification token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	tokenHash := sha256.Sum256([]byte(token))
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return "", fmt.Errorf("begin email verification: %w", err)
	}
	defer tx.Rollback(dbctx)
	if _, err := tx.Exec(dbctx, `
		DELETE FROM person_email_verifications
		WHERE person_id = $1::uuid AND email = $2::citext AND consumed_at IS NULL
	`, personID, email); err != nil {
		return "", fmt.Errorf("replace pending email verification: %w", err)
	}
	if _, err := tx.Exec(dbctx, `
		INSERT INTO person_email_verifications (
			person_id, email, token_hash, make_primary, expires_at
		) VALUES ($1::uuid, $2::citext, $3, $4, $5)
	`, personID, email, tokenHash[:], makePrimary, time.Now().UTC().Add(PersonEmailVerificationTTL)); err != nil {
		return "", fmt.Errorf("create email verification: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return "", fmt.Errorf("commit email verification: %w", err)
	}
	return token, nil
}

func ConsumePersonEmailVerification(ctx *config.AppContext, token string) (string, string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", "", fmt.Errorf("database is not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", fmt.Errorf("verification token is required")
	}
	tokenHash := sha256.Sum256([]byte(token))
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return "", "", fmt.Errorf("begin email verification: %w", err)
	}
	defer tx.Rollback(dbctx)
	var personID, email string
	var makePrimary bool
	err = tx.QueryRow(dbctx, `
		SELECT person_id::text, email::text, make_primary
		FROM person_email_verifications
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
		FOR UPDATE
	`, tokenHash[:]).Scan(&personID, &email, &makePrimary)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("verification link is invalid, expired, or already used")
	}
	if err != nil {
		return "", "", fmt.Errorf("load email verification: %w", err)
	}
	var ownerID string
	err = tx.QueryRow(dbctx, `SELECT person_id::text FROM person_emails WHERE email = $1::citext`, email).Scan(&ownerID)
	if err == nil && ownerID != personID {
		return "", "", fmt.Errorf("email was attached to another account before verification completed")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("check verified email owner: %w", err)
	}
	if makePrimary {
		if _, err := tx.Exec(dbctx, `UPDATE person_emails SET is_primary = false WHERE person_id = $1::uuid`, personID); err != nil {
			return "", "", fmt.Errorf("clear primary email: %w", err)
		}
	}
	tag, err := tx.Exec(dbctx, `
		INSERT INTO person_emails (person_id, email, is_primary, verified_at)
		VALUES (
			$1::uuid, $2::citext,
			$3 OR NOT EXISTS (SELECT 1 FROM person_emails WHERE person_id = $1::uuid AND is_primary),
			now()
		)
		ON CONFLICT (email) DO UPDATE
		SET verified_at = coalesce(person_emails.verified_at, now()),
			is_primary = CASE WHEN EXCLUDED.person_id = person_emails.person_id THEN EXCLUDED.is_primary ELSE person_emails.is_primary END,
			updated_at = now()
		WHERE EXCLUDED.person_id = person_emails.person_id
	`, personID, email, makePrimary)
	if err != nil {
		return "", "", fmt.Errorf("attach verified email: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", "", fmt.Errorf("email was attached to another account before verification completed")
	}
	if err := linkUnownedRecordsByEmailTx(dbctx, tx, personID, email); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE person_email_verifications SET consumed_at = now() WHERE token_hash = $1
	`, tokenHash[:]); err != nil {
		return "", "", fmt.Errorf("consume email verification: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return "", "", fmt.Errorf("commit email verification: %w", err)
	}
	return personID, email, nil
}

func SetPrimaryPersonEmail(ctx *config.AppContext, personID, rawEmail string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin primary email update: %w", err)
	}
	defer tx.Rollback(dbctx)
	tag, err := tx.Exec(dbctx, `
		UPDATE person_emails
		SET is_primary = (email = $2::citext), updated_at = now()
		WHERE person_id = $1::uuid
	`, personID, email)
	if err != nil {
		return fmt.Errorf("set primary email: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("email is not attached to this account")
	}
	var selected bool
	if err := tx.QueryRow(dbctx, `
		SELECT EXISTS (
			SELECT 1 FROM person_emails
			WHERE person_id = $1::uuid AND email = $2::citext AND is_primary
		)
	`, personID, email).Scan(&selected); err != nil {
		return fmt.Errorf("check primary email: %w", err)
	}
	if !selected {
		return fmt.Errorf("email is not attached to this account")
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit primary email update: %w", err)
	}
	return nil
}

func RemovePersonEmail(ctx *config.AppContext, personID, rawEmail string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	personID = strings.TrimSpace(personID)
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin email removal: %w", err)
	}
	defer tx.Rollback(dbctx)
	var count int
	if err := tx.QueryRow(dbctx, `
		SELECT count(*) FROM (
			SELECT id FROM person_emails WHERE person_id = $1::uuid FOR UPDATE
		) locked
	`, personID).Scan(&count); err != nil {
		return fmt.Errorf("count person emails: %w", err)
	}
	if count <= 1 {
		return fmt.Errorf("an account must keep at least one email")
	}
	var isPrimary, locked bool
	err = tx.QueryRow(dbctx, `
		SELECT email.is_primary, EXISTS (
			SELECT 1 FROM person_merge_events merge
			WHERE merge.id = email.origin_merge_event_id
				AND merge.status = 'merged' AND merge.undo_expires_at > now()
		)
		FROM person_emails email
		WHERE email.person_id = $1::uuid AND email.email = $2::citext
		FOR UPDATE
	`, personID, email).Scan(&isPrimary, &locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("email is not attached to this account")
	}
	if err != nil {
		return fmt.Errorf("load person email: %w", err)
	}
	if locked {
		return fmt.Errorf("email is locked until its profile merge can no longer be undone")
	}
	if _, err := tx.Exec(dbctx, `DELETE FROM person_emails WHERE person_id = $1::uuid AND email = $2::citext`, personID, email); err != nil {
		return fmt.Errorf("remove person email: %w", err)
	}
	if isPrimary {
		if _, err := tx.Exec(dbctx, `
			UPDATE person_emails SET is_primary = true, updated_at = now()
			WHERE id = (
				SELECT id FROM person_emails WHERE person_id = $1::uuid ORDER BY created_at, id LIMIT 1
			)
		`, personID); err != nil {
			return fmt.Errorf("choose replacement primary email: %w", err)
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit email removal: %w", err)
	}
	return nil
}
