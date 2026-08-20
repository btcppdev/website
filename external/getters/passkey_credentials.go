package getters

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
)

var ErrPasskeyCredentialLinked = errors.New("passkey is linked to another person")

func ListPersonPasskeyCredentials(ctx *config.AppContext, personID string) ([]*types.PersonPasskeyCredential, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, person_id::text, credential_id, credential,
			display_name, created_at, last_used_at, updated_at
		FROM person_passkey_credentials
		WHERE person_id = $1::uuid
		ORDER BY created_at, id
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list passkeys: %w", err)
	}
	defer rows.Close()
	var credentials []*types.PersonPasskeyCredential
	for rows.Next() {
		credential := &types.PersonPasskeyCredential{}
		var encrypted []byte
		if err := rows.Scan(&credential.ID, &credential.PersonID, &credential.CredentialID,
			&encrypted, &credential.DisplayName, &credential.CreatedAt,
			&credential.LastUsedAt, &credential.UpdatedAt); err != nil {
			return nil, err
		}
		credential.CredentialJSON, err = decryptPasskeyCredential(ctx, encrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt passkey %s: %w", credential.ID, err)
		}
		credentials = append(credentials, credential)
	}
	return credentials, rows.Err()
}

func CreatePersonPasskeyCredential(ctx *config.AppContext, personID, displayName string, credential *webauthn.Credential) (*types.PersonPasskeyCredential, error) {
	if credential == nil || len(credential.ID) == 0 {
		return nil, errors.New("passkey credential is required")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = "Passkey"
	}
	if len([]rune(displayName)) > 80 {
		return nil, errors.New("passkey name must be no more than 80 characters")
	}
	encoded, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}
	encrypted, err := encryptPasskeyCredential(ctx, encoded)
	if err != nil {
		return nil, err
	}
	stored := &types.PersonPasskeyCredential{}
	var storedEncrypted []byte
	err = ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO person_passkey_credentials (person_id, credential_id, credential, display_name)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (credential_id) DO UPDATE SET
			credential = EXCLUDED.credential,
			display_name = EXCLUDED.display_name
		WHERE person_passkey_credentials.person_id = EXCLUDED.person_id
		RETURNING id::text, person_id::text, credential_id, credential,
			display_name, created_at, last_used_at, updated_at
	`, personID, credential.ID, encrypted, displayName).Scan(&stored.ID, &stored.PersonID,
		&stored.CredentialID, &storedEncrypted, &stored.DisplayName,
		&stored.CreatedAt, &stored.LastUsedAt, &stored.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPasskeyCredentialLinked
	}
	if err != nil {
		return nil, fmt.Errorf("store passkey: %w", err)
	}
	stored.CredentialJSON, err = decryptPasskeyCredential(ctx, storedEncrypted)
	if err != nil {
		return nil, err
	}
	return stored, nil
}

func UpdatePersonPasskeyCredentialUse(ctx *config.AppContext, personID string, credential *webauthn.Credential) error {
	if credential == nil || len(credential.ID) == 0 {
		return errors.New("passkey credential is required")
	}
	encoded, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	encrypted, err := encryptPasskeyCredential(ctx, encoded)
	if err != nil {
		return err
	}
	command, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE person_passkey_credentials
		SET credential = $3, last_used_at = now()
		WHERE person_id = $1::uuid AND credential_id = $2
	`, personID, credential.ID, encrypted)
	if err != nil {
		return fmt.Errorf("update passkey use: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("passkey is no longer linked to that person")
	}
	return nil
}

func FindPasskeyOwner(ctx *config.AppContext, credentialID []byte) (string, error) {
	var personID string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT person_id::text FROM person_passkey_credentials WHERE credential_id = $1
	`, credentialID).Scan(&personID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find passkey owner: %w", err)
	}
	return personID, nil
}

func UnlinkPersonPasskeyCredential(ctx *config.AppContext, personID, credentialID string) (*types.PersonPasskeyCredential, error) {
	credential := &types.PersonPasskeyCredential{}
	var encrypted []byte
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		DELETE FROM person_passkey_credentials
		WHERE id = $1::uuid AND person_id = $2::uuid
		RETURNING id::text, person_id::text, credential_id, credential,
			display_name, created_at, last_used_at, updated_at
	`, credentialID, personID).Scan(&credential.ID, &credential.PersonID,
		&credential.CredentialID, &encrypted, &credential.DisplayName,
		&credential.CreatedAt, &credential.LastUsedAt, &credential.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unlink passkey: %w", err)
	}
	credential.CredentialJSON, err = decryptPasskeyCredential(ctx, encrypted)
	if err != nil {
		return nil, err
	}
	return credential, nil
}

func encryptPasskeyCredential(ctx *config.AppContext, plaintext []byte) ([]byte, error) {
	gcm, err := passkeyCredentialCipher(ctx)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, []byte("btcpp-passkey-credential-v1")), nil
}

func decryptPasskeyCredential(ctx *config.AppContext, encrypted []byte) ([]byte, error) {
	gcm, err := passkeyCredentialCipher(ctx)
	if err != nil {
		return nil, err
	}
	if len(encrypted) <= gcm.NonceSize() {
		return nil, errors.New("encrypted passkey credential is truncated")
	}
	nonce, ciphertext := encrypted[:gcm.NonceSize()], encrypted[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, []byte("btcpp-passkey-credential-v1"))
}

func passkeyCredentialCipher(ctx *config.AppContext) (cipher.AEAD, error) {
	if ctx == nil || ctx.Env == nil {
		return nil, errors.New("application secret is unavailable")
	}
	keyMaterial := append([]byte("btcpp-passkey-encryption-v1:"), ctx.Env.HMACKey[:]...)
	key := sha256.Sum256(keyMaterial)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
