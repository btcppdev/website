package auth

import (
	"bytes"
	"testing"
)

func TestGenerateAndParseAPIToken(t *testing.T) {
	plaintext, selector, digest, err := GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	parsedSelector, parsedDigest, err := ParseAPIToken(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if parsedSelector != selector || !bytes.Equal(parsedDigest, digest) {
		t.Fatalf("parsed token = %q/%x, want %q/%x", parsedSelector, parsedDigest, selector, digest)
	}
	if plaintext == "" || bytes.Contains([]byte(plaintext), digest) {
		t.Fatal("plaintext token was empty or contained its binary digest")
	}
}

func TestParseAPITokenRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"", "btcpp_v2.a.b", "btcpp_v1.bad.extra.parts", "btcpp_v1.a.b"} {
		if _, _, err := ParseAPIToken(value); err == nil {
			t.Fatalf("ParseAPIToken(%q) succeeded", value)
		}
	}
}

func TestValidAPITokenScopes(t *testing.T) {
	if !ValidAPITokenScopes([]string{"profile:self:read", "talks:read"}) {
		t.Fatal("valid scopes rejected")
	}
	for _, scopes := range [][]string{nil, {"admin"}, {"profile:self:read", "profile:self:read"}, {"profile:read"}} {
		if ValidAPITokenScopes(scopes) {
			t.Fatalf("invalid scopes accepted: %v", scopes)
		}
	}
}
