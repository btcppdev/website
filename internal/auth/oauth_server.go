package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
)

const (
	OAuthAccessTokenVersion   = "btcpp_oat"
	OAuthRefreshTokenVersion  = "btcpp_ort"
	OAuthAuthorizationCodeTTL = 10 * time.Minute
	OAuthAccessTokenTTL       = time.Hour
	OAuthRefreshTokenTTL      = 30 * 24 * time.Hour
)

type BearerGrant struct {
	TokenID  string
	PersonID string
	ClientID string
	Scopes   []string
	Kind     string
}

func GenerateOAuthAuthorizationCode() (string, []byte, error) {
	value, err := randomURLToken(32)
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256([]byte(value))
	return value, digest[:], nil
}

func GenerateOAuthClientCredentials(confidential bool) (clientID, secret string, secretHash []byte, err error) {
	randomID, err := randomURLToken(18)
	if err != nil {
		return "", "", nil, err
	}
	clientID = "btcpp_client_" + randomID
	if !confidential {
		return clientID, "", nil, nil
	}
	randomSecret, err := randomURLToken(32)
	if err != nil {
		return "", "", nil, err
	}
	secret = "btcpp_cs." + randomSecret
	digest := sha256.Sum256([]byte(secret))
	return clientID, secret, digest[:], nil
}

func GenerateOAuthToken(version string) (plaintext, selector string, digest []byte, err error) {
	selector, err = randomURLToken(12)
	if err != nil {
		return "", "", nil, err
	}
	secret, err := randomURLToken(32)
	if err != nil {
		return "", "", nil, err
	}
	plaintext = version + "." + selector + "." + secret
	hash := sha256.Sum256([]byte(secret))
	return plaintext, selector, hash[:], nil
}

func ParseOAuthToken(raw, version string) (string, []byte, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 3 || parts[0] != version {
		return "", nil, errors.New("invalid OAuth token")
	}
	selectorBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(selectorBytes) != 12 {
		return "", nil, errors.New("invalid OAuth token")
	}
	secretBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(secretBytes) != 32 {
		return "", nil, errors.New("invalid OAuth token")
	}
	digest := sha256.Sum256([]byte(parts[2]))
	return parts[1], digest[:], nil
}

func PKCEChallenge(verifier string) (string, bool) {
	verifier = strings.TrimSpace(verifier)
	if len(verifier) < 43 || len(verifier) > 128 {
		return "", false
	}
	for _, char := range verifier {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("-._~", char)) {
			return "", false
		}
	}
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:]), true
}

func ValidPKCEChallenge(challenge string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(challenge))
	return err == nil && len(decoded) == sha256.Size && len(strings.TrimSpace(challenge)) == 43
}

func AuthenticateBearerToken(ctx *config.AppContext, raw string) (*BearerGrant, error) {
	if strings.HasPrefix(strings.TrimSpace(raw), APITokenVersion+".") {
		token, err := AuthenticateAPIToken(ctx, raw)
		if err != nil || token == nil {
			return nil, err
		}
		return &BearerGrant{TokenID: token.ID, PersonID: token.PersonID, Scopes: token.Scopes, Kind: "personal_access_token"}, nil
	}
	selector, presented, err := ParseOAuthToken(raw, OAuthAccessTokenVersion)
	if err != nil {
		return nil, nil
	}
	token, expected, err := getters.FindActiveOAuthAccessToken(ctx, selector)
	if err != nil || token == nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(presented, expected) != 1 {
		return nil, nil
	}
	if err := getters.MarkOAuthAccessTokenUsed(ctx, token.ID); err != nil {
		return nil, err
	}
	return &BearerGrant{TokenID: token.ID, PersonID: token.PersonID, ClientID: token.ClientID, Scopes: token.Scopes, Kind: "oauth_access_token"}, nil
}

func randomURLToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
