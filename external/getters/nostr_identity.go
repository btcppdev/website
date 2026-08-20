package getters

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nbd-wtf/go-nostr/nip19"
)

var ErrNostrPubkeyConflict = errors.New("nostr public key belongs to another person")
var ErrNostrCredentialLinked = errors.New("person already has a nostr public key")

func ListPersonNostrCredentials(ctx *config.AppContext, personID string) ([]*types.PersonNostrCredential, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, person_id::text, coalesce(pubkey_hex, ''),
			coalesce(legacy_value, ''), verified_at, linked_at, last_login_at, updated_at
		FROM person_nostr_credentials
		WHERE person_id = $1::uuid
		ORDER BY linked_at, id
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list person Nostr credentials: %w", err)
	}
	defer rows.Close()
	var credentials []*types.PersonNostrCredential
	for rows.Next() {
		credential := &types.PersonNostrCredential{}
		if err := rows.Scan(&credential.ID, &credential.PersonID, &credential.PubkeyHex,
			&credential.LegacyValue, &credential.VerifiedAt, &credential.LinkedAt,
			&credential.LastLoginAt, &credential.UpdatedAt); err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, rows.Err()
}

// FindNostrCredentialByPubkey only accepts an explicitly verified credential.
// Public profile metadata and legacy snapshots never grant sign-in access.
func FindNostrCredentialByPubkey(ctx *config.AppContext, pubkey string) (*types.PersonNostrCredential, error) {
	pubkey, err := NormalizeNostrPubkey(pubkey)
	if err != nil {
		return nil, err
	}
	credential := &types.PersonNostrCredential{}
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, person_id::text, coalesce(pubkey_hex, ''),
			coalesce(legacy_value, ''), verified_at, linked_at, last_login_at, updated_at
		FROM person_nostr_credentials
		WHERE pubkey_hex = $1 AND verified_at IS NOT NULL
	`, pubkey).Scan(&credential.ID, &credential.PersonID, &credential.PubkeyHex,
		&credential.LegacyValue, &credential.VerifiedAt, &credential.LinkedAt,
		&credential.LastLoginAt, &credential.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find Nostr credential: %w", err)
	}
	return credential, nil
}

func FindPersonByNostrPubkey(ctx *config.AppContext, pubkey string) (*types.Speaker, error) {
	credential, err := FindNostrCredentialByPubkey(ctx, pubkey)
	if err != nil || credential == nil {
		return nil, err
	}
	return FetchSpeakerByID(ctx, credential.PersonID)
}

// VerifyNostrCredential records successful use of an already verified key.
func VerifyNostrCredential(ctx *config.AppContext, credentialID, personID, pubkey string) error {
	pubkey, err := NormalizeNostrPubkey(pubkey)
	if err != nil {
		return err
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return fmt.Errorf("begin Nostr credential verification: %w", err)
	}
	defer tx.Rollback(dbctx)
	command, err := tx.Exec(dbctx, `
		UPDATE person_nostr_credentials
		SET last_login_at = now()
		WHERE id = $1::uuid AND person_id = $2::uuid
			AND pubkey_hex = $3 AND verified_at IS NOT NULL
	`, credentialID, personID, pubkey)
	if err != nil {
		if isNostrPersonSlotViolation(err) {
			return ErrNostrCredentialLinked
		}
		return fmt.Errorf("verify Nostr credential: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("Nostr credential is no longer attached to that person")
	}
	if err := setPersonNostrProfile(dbctx, tx, personID, pubkey); err != nil {
		return err
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit Nostr credential verification: %w", err)
	}
	return nil
}

func LinkNostrCredential(ctx *config.AppContext, personID, pubkey string) (*types.PersonNostrCredential, error) {
	pubkey, err := NormalizeNostrPubkey(pubkey)
	if err != nil {
		return nil, err
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin Nostr credential link: %w", err)
	}
	defer tx.Rollback(dbctx)
	credential := &types.PersonNostrCredential{}
	err = tx.QueryRow(dbctx, `
		INSERT INTO person_nostr_credentials (person_id, pubkey_hex, verified_at, last_login_at)
		VALUES ($1::uuid, $2, now(), now())
		ON CONFLICT (pubkey_hex) DO UPDATE SET
			verified_at = coalesce(person_nostr_credentials.verified_at, now()),
			last_login_at = now()
		WHERE person_nostr_credentials.person_id = EXCLUDED.person_id
		RETURNING id::text, person_id::text, pubkey_hex, coalesce(legacy_value, ''),
			verified_at, linked_at, last_login_at, updated_at
	`, personID, pubkey).Scan(&credential.ID, &credential.PersonID, &credential.PubkeyHex,
		&credential.LegacyValue, &credential.VerifiedAt, &credential.LinkedAt,
		&credential.LastLoginAt, &credential.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNostrPubkeyConflict
	}
	if isNostrPersonSlotViolation(err) {
		return nil, ErrNostrCredentialLinked
	}
	if err != nil {
		return nil, fmt.Errorf("link Nostr credential: %w", err)
	}
	if err := setPersonNostrProfile(dbctx, tx, personID, pubkey); err != nil {
		return nil, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit Nostr credential link: %w", err)
	}
	return credential, nil
}

func UnlinkNostrCredential(ctx *config.AppContext, personID, credentialID string) (*types.PersonNostrCredential, error) {
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return nil, fmt.Errorf("begin Nostr credential unlink: %w", err)
	}
	defer tx.Rollback(dbctx)
	credential := &types.PersonNostrCredential{}
	err = tx.QueryRow(dbctx, `
		DELETE FROM person_nostr_credentials
		WHERE id = $1::uuid AND person_id = $2::uuid
		RETURNING id::text, person_id::text, coalesce(pubkey_hex, ''),
			coalesce(legacy_value, ''), verified_at, linked_at, last_login_at, updated_at
	`, credentialID, personID).Scan(&credential.ID, &credential.PersonID, &credential.PubkeyHex,
		&credential.LegacyValue, &credential.VerifiedAt, &credential.LinkedAt,
		&credential.LastLoginAt, &credential.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unlink Nostr credential: %w", err)
	}
	if _, err := tx.Exec(dbctx, `UPDATE people SET nostr = '' WHERE id = $1::uuid`, personID); err != nil {
		return nil, fmt.Errorf("clear person Nostr profile: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, fmt.Errorf("commit Nostr credential unlink: %w", err)
	}
	return credential, nil
}

type nostrProfileWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func setPersonNostrProfile(dbctx context.Context, db nostrProfileWriter, personID, pubkey string) error {
	npub := NostrPubkeyDisplay(pubkey)
	if _, err := db.Exec(dbctx, `UPDATE people SET nostr = $2 WHERE id = $1::uuid`, personID, npub); err != nil {
		return fmt.Errorf("synchronize person Nostr profile: %w", err)
	}
	return nil
}

func isNostrPersonSlotViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.ConstraintName == "person_nostr_credentials_person_unique"
}

// VerifiedNostrProfileValue returns the canonical npub for a person's linked,
// signature-verified credential. Legacy profile-only values are not treated as
// authentication credentials.
func VerifiedNostrProfileValue(ctx *config.AppContext, personID string) (string, error) {
	var pubkey string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT pubkey_hex
		FROM person_nostr_credentials
		WHERE person_id = $1::uuid AND pubkey_hex IS NOT NULL AND verified_at IS NOT NULL
	`, personID).Scan(&pubkey)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load verified Nostr profile key: %w", err)
	}
	return NostrPubkeyDisplay(pubkey), nil
}

// ReconcileNostrCredentialProfiles repairs profiles linked before npub
// synchronization was introduced. It is idempotent and runs after migrations.
func ReconcileNostrCredentialProfiles(ctx *config.AppContext) (int, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT credential.person_id::text, credential.pubkey_hex, person.nostr
		FROM person_nostr_credentials credential
		JOIN people person ON person.id = credential.person_id
		WHERE credential.pubkey_hex IS NOT NULL AND credential.verified_at IS NOT NULL
		ORDER BY credential.person_id
	`)
	if err != nil {
		return 0, fmt.Errorf("list verified Nostr profile keys: %w", err)
	}
	type profileKey struct{ personID, pubkey, current string }
	var keys []profileKey
	for rows.Next() {
		var key profileKey
		if err := rows.Scan(&key.personID, &key.pubkey, &key.current); err != nil {
			rows.Close()
			return 0, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	updated := 0
	for _, key := range keys {
		npub := NostrPubkeyDisplay(key.pubkey)
		if key.current == npub {
			continue
		}
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE people SET nostr = $2 WHERE id = $1::uuid`, key.personID, npub); err != nil {
			return updated, fmt.Errorf("reconcile person Nostr profile: %w", err)
		}
		updated++
	}
	return updated, nil
}

func NormalizeNostrPubkey(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "nostr:")
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = strings.Trim(strings.TrimSpace(parsed.Path), "/")
		if slash := strings.LastIndex(value, "/"); slash >= 0 {
			value = value[slash+1:]
		}
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return strings.ToLower(value), nil
	}
	prefix, decoded, err := nip19.Decode(value)
	if err != nil || prefix != "npub" {
		return "", errors.New("Nostr public key must be an npub or 32-byte hex key")
	}
	pubkey, ok := decoded.(string)
	if !ok {
		return "", errors.New("invalid npub payload")
	}
	if raw, err := hex.DecodeString(pubkey); err != nil || len(raw) != 32 {
		return "", errors.New("invalid npub public key")
	}
	return strings.ToLower(pubkey), nil
}

func NostrPubkeyDisplay(value string) string {
	pubkey, err := NormalizeNostrPubkey(value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	npub, err := nip19.EncodePublicKey(pubkey)
	if err != nil {
		return pubkey
	}
	return npub
}
