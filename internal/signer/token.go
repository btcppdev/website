package signer

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type Claims struct {
	Issuer          string `json:"iss"`
	Audience        string `json:"aud"`
	Subject         string `json:"sub"`
	Tenant          string `json:"tenant"`
	TenantID        string `json:"tenant_id"`
	Role            string `json:"role"`
	Action          string `json:"action"`
	EventHash       string `json:"event_hash,omitempty"`
	Target          string `json:"target,omitempty"`
	AuthMethod      string `json:"auth_method"`
	AuthenticatedAt int64  `json:"auth_time"`
	IssuedAt        int64  `json:"iat"`
	ExpiresAt       int64  `json:"exp"`
	TokenID         string `json:"jti"`
}

type Issuer struct{ privateKey ed25519.PrivateKey }

func NewIssuer(encodedPrivateKey string) (*Issuer, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedPrivateKey))
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("BTCPP_SIGNER_AUTH_PRIVATE_KEY must be a base64-encoded Ed25519 private key")
	}
	private := make(ed25519.PrivateKey, len(key))
	copy(private, key)
	clear(key)
	return &Issuer{privateKey: private}, nil
}

func (i *Issuer) Sign(claims Claims) (string, error) {
	if i == nil || len(i.privateKey) != ed25519.PrivateKeySize {
		return "", errors.New("signer authorization issuer is not configured")
	}
	header, _ := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := rawURL(header) + "." + rawURL(payload)
	return unsigned + "." + rawURL(ed25519.Sign(i.privateKey, []byte(unsigned))), nil
}

func rawURL(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }
