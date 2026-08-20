package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

const APITokenVersion = types.APITokenVersion

func GenerateAPIToken() (plaintext, selector string, digest []byte, err error) {
	selectorBytes := make([]byte, 12)
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(selectorBytes); err != nil {
		return "", "", nil, err
	}
	if _, err = rand.Read(secretBytes); err != nil {
		return "", "", nil, err
	}
	selector = base64.RawURLEncoding.EncodeToString(selectorBytes)
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	plaintext = APITokenVersion + "." + selector + "." + secret
	hash := sha256.Sum256([]byte(secret))
	return plaintext, selector, hash[:], nil
}

func ParseAPIToken(raw string) (selector string, digest []byte, err error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 3 || parts[0] != APITokenVersion {
		return "", nil, errors.New("invalid API token")
	}
	if selectorBytes, err := base64.RawURLEncoding.DecodeString(parts[1]); err != nil || len(selectorBytes) != 12 {
		return "", nil, errors.New("invalid API token")
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(secret) != 32 {
		return "", nil, errors.New("invalid API token")
	}
	hash := sha256.Sum256([]byte(parts[2]))
	return parts[1], hash[:], nil
}

func ValidAPITokenScopes(scopes []string) bool {
	return types.ValidAPITokenScopes(scopes)
}

// AuthenticateAPIToken validates a personal access token and returns the
// person-owned credential. API middleware must still enforce endpoint scopes
// and load current person roles; possessing a token is not authorization by
// itself.
func AuthenticateAPIToken(ctx *config.AppContext, raw string) (*types.PersonAPIToken, error) {
	selector, presentedHash, err := ParseAPIToken(raw)
	if err != nil {
		return nil, nil
	}
	credential, err := getters.FindActiveAPIToken(ctx, selector)
	if err != nil || credential == nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(presentedHash, credential.TokenHash) != 1 {
		return nil, nil
	}
	if err := getters.MarkPersonAPITokenUsed(ctx, credential.Token.ID); err != nil {
		return nil, err
	}
	return credential.Token, nil
}
