package auth

import (
	"strings"
	"testing"
)

func TestPKCEChallengeMatchesRFC7636Example(t *testing.T) {
	challenge, ok := PKCEChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if !ok || challenge != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challenge = %q, valid = %v", challenge, ok)
	}
	for _, invalid := range []string{"short", strings.Repeat("a", 129), strings.Repeat("a", 42) + "!"} {
		if _, ok := PKCEChallenge(invalid); ok {
			t.Fatalf("accepted invalid verifier %q", invalid)
		}
	}
}

func TestOAuthOpaqueTokensRoundTripWithoutStoringPlaintext(t *testing.T) {
	plaintext, selector, digest, err := GenerateOAuthToken(OAuthAccessTokenVersion)
	if err != nil {
		t.Fatal(err)
	}
	parsedSelector, parsedDigest, err := ParseOAuthToken(plaintext, OAuthAccessTokenVersion)
	if err != nil || parsedSelector != selector || string(parsedDigest) != string(digest) {
		t.Fatalf("parsed selector=%q err=%v", parsedSelector, err)
	}
	if _, _, err := ParseOAuthToken(plaintext, OAuthRefreshTokenVersion); err == nil {
		t.Fatal("access token parsed as refresh token")
	}
}

func TestOAuthClientCredentialsSeparatePublicAndConfidentialClients(t *testing.T) {
	publicID, publicSecret, publicHash, err := GenerateOAuthClientCredentials(false)
	if err != nil || !strings.HasPrefix(publicID, "btcpp_client_") || publicSecret != "" || publicHash != nil {
		t.Fatalf("public credentials = %q %q %x, err=%v", publicID, publicSecret, publicHash, err)
	}
	confidentialID, secret, secretHash, err := GenerateOAuthClientCredentials(true)
	if err != nil || !strings.HasPrefix(confidentialID, "btcpp_client_") || !strings.HasPrefix(secret, "btcpp_cs.") || len(secretHash) != 32 {
		t.Fatalf("confidential credentials = %q %q %x, err=%v", confidentialID, secret, secretHash, err)
	}
}
