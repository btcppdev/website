package handlers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"btcpp-web/internal/auth"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Generate an unrelated credential with the reported YubiKey response shape:
// cross-platform, USB, resident credential, ES256, and no attestation statement.
// No account holder's credential ID, public key, or challenge is stored here.
func passkeyRegistrationResponse(t *testing.T, challenge, origin, rpID string, flags byte) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := cbor.Marshal(map[int]any{
		1: 2, 3: -7, -1: 1,
		-2: key.X.FillBytes(make([]byte, 32)),
		-3: key.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 48)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(rpID))
	data := append(hash[:], flags)
	data = binary.BigEndian.AppendUint32(data, 3)
	data = append(data, make([]byte, 16)...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(id)))
	data = append(data, id...)
	data = append(data, publicKey...)
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": data})
	if err != nil {
		t.Fatal(err)
	}
	client, err := json.Marshal(map[string]any{"type": "webauthn.create", "challenge": challenge, "origin": origin, "crossOrigin": false})
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	raw, err := json.Marshal(map[string]any{
		"id": encode(id), "rawId": encode(id), "type": "public-key", "authenticatorAttachment": "cross-platform",
		"clientExtensionResults": map[string]any{"credProps": map[string]bool{"rk": true}},
		"response":               map[string]any{"clientDataJSON": encode(client), "attestationObject": encode(attestation), "transports": []string{"usb"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPasskeyRegistrationUserVerification(t *testing.T) {
	rp, err := webauthn.New(&webauthn.Config{RPID: "btcpp.dev", RPDisplayName: "bitcoin++", RPOrigins: []string{"https://btcpp.dev"}})
	if err != nil {
		t.Fatal(err)
	}
	user := &auth.PasskeyUser{PersonID: "00000000-0000-4000-8000-000000000123", Name: "Test"}
	for _, tc := range []struct {
		name, origin, rpID, reason string
		flags                      byte
		wrongChallenge             bool
	}{
		{name: "touch without verification", flags: 0x41, reason: "user_verification_required"},
		{name: "PIN verified key", flags: 0x45},
		{name: "wrong origin", flags: 0x45, origin: "https://other.example", reason: "verification_error"},
		{name: "wrong relying party", flags: 0x45, rpID: "other.example", reason: "verification_error"},
		{name: "wrong challenge without UV", flags: 0x41, wrongChallenge: true, reason: "verification_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, session, err := rp.BeginRegistration(user,
				webauthn.WithAuthenticatorSelection(passkeyAuthenticatorSelection()),
				webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
			)
			if err != nil {
				t.Fatal(err)
			}
			selection := options.Response.AuthenticatorSelection
			if selection.UserVerification != protocol.VerificationRequired || session.UserVerification != protocol.VerificationRequired || selection.ResidentKey != protocol.ResidentKeyRequirementRequired || selection.RequireResidentKey == nil || !*selection.RequireResidentKey {
				t.Fatal("browser and verifier must require a discoverable, user-verified credential")
			}
			origin, rpID, challenge := tc.origin, tc.rpID, session.Challenge
			if origin == "" {
				origin = "https://btcpp.dev"
			}
			if rpID == "" {
				rpID = "btcpp.dev"
			}
			if tc.wrongChallenge {
				challenge = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
			}
			request := httptest.NewRequest(http.MethodPost, "/auth/passkey/register/verify", strings.NewReader(passkeyRegistrationResponse(t, challenge, origin, rpID, tc.flags)))
			credential, err := rp.FinishRegistration(user, *session, request)
			if tc.reason == "" {
				if err != nil || credential == nil || !credential.Flags.UserVerified {
					t.Fatal("verified security key rejected", err)
				}
				return
			}
			if err == nil || credential != nil {
				t.Fatal("invalid registration accepted")
			}
			reason, message := passkeyRegistrationFailure(err)
			if reason != tc.reason {
				t.Fatalf("reason = %q, want %q (%v)", reason, tc.reason, err)
			}
			if strings.Contains(message, "FIDO2 PIN") != (tc.reason == "user_verification_required") {
				t.Fatal("incorrect PIN guidance", message)
			}
		})
	}
}

func TestPasskeyRegistrationErrorDoesNotExposeDetails(t *testing.T) {
	for _, err := range []error{
		errors.New("private-challenge-and-credential-data"),
		protocol.ErrVerification.WithInfo("private-challenge-and-credential-data"),
	} {
		reason, message := passkeyRegistrationFailure(err)
		if strings.Contains(reason+message, "private-challenge") || strings.Contains(message, "FIDO2 PIN") {
			t.Fatal("raw or misleading failure exposed", reason, message)
		}
	}
	err := fmt.Errorf("wrapped: %w", protocol.ErrVerification.WithInfo("User verification required but flag not set by authenticator"))
	if reason, _ := passkeyRegistrationFailure(err); reason != "user_verification_required" {
		t.Fatal("wrapped verifier error lost", reason)
	}
}
