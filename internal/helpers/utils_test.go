package helpers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func testAppContext(t *testing.T) *config.AppContext {
	t.Helper()
	key, err := types.DeriveHMACKey("test-secret")
	if err != nil {
		t.Fatalf("DeriveHMACKey: %s", err)
	}
	return &config.AppContext{
		Env: &types.EnvConfig{HMACKey: key},
	}
}

func TestEmailHMACRoundTrip(t *testing.T) {
	ctx := testAppContext(t)
	token := CreateEmailHMACTTL(ctx, "user@example.test", time.Minute)

	if !VerifyEmailHMAC(ctx, token, "user@example.test") {
		t.Fatal("expected token to verify")
	}
}

func TestEmailHMACRejectsWrongEmail(t *testing.T) {
	ctx := testAppContext(t)
	token := CreateEmailHMACTTL(ctx, "user@example.test", time.Minute)

	if VerifyEmailHMAC(ctx, token, "attacker@example.test") {
		t.Fatal("expected token to fail for a different email")
	}
}

func TestEmailHMACRejectsExpiredToken(t *testing.T) {
	ctx := testAppContext(t)
	token := CreateEmailHMACTTL(ctx, "user@example.test", -time.Second)

	if VerifyEmailHMAC(ctx, token, "user@example.test") {
		t.Fatal("expected expired token to fail")
	}
}

func TestEmailLinkExpiresAfterThirtyMinutes(t *testing.T) {
	ctx := testAppContext(t)
	ctx.Env.Host = "example.test"
	ctx.Env.Prod = true
	before := time.Now().UTC()

	link := EmailLink(ctx, "user@example.test", "/dashboard")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse EmailLink: %s", err)
	}
	encoded := u.Query().Get("hr")
	tokenBytes, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode EmailLink token: %s", err)
	}
	parts := strings.Split(string(tokenBytes), ".")
	if len(parts) != 3 {
		t.Fatalf("token parts = %d, want 3", len(parts))
	}
	expiresUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		t.Fatalf("parse EmailLink expiry: %s", err)
	}
	remaining := time.Unix(expiresUnix, 0).Sub(before)
	if remaining < EmailedLinkTTL-time.Second || remaining > EmailedLinkTTL+time.Second {
		t.Fatalf("EmailLink lifetime = %s, want %s", remaining, EmailedLinkTTL)
	}
}

func TestEmailHMACRejectsLegacyTokenShape(t *testing.T) {
	ctx := testAppContext(t)

	if VerifyEmailHMAC(ctx, strings.Repeat("a", 64), "user@example.test") {
		t.Fatal("expected mismatched legacy bare-hex token to fail")
	}
}

func TestEmailHMACAcceptsLegacyTokenUntilCutoff(t *testing.T) {
	ctx := testAppContext(t)
	token := createLegacyEmailTokenForTest(ctx, "user@example.test")
	now := legacyEmailTokenCutoff.Add(-time.Second)

	if !verifyEmailHMACAt(ctx, token, "user@example.test", now) {
		t.Fatal("expected legacy bare-hex token to verify before cutoff")
	}
}

func TestEmailHMACRejectsLegacyTokenAfterCutoff(t *testing.T) {
	ctx := testAppContext(t)
	token := createLegacyEmailTokenForTest(ctx, "user@example.test")
	now := legacyEmailTokenCutoff

	if verifyEmailHMACAt(ctx, token, "user@example.test", now) {
		t.Fatal("expected legacy bare-hex token to fail at cutoff")
	}
}

func createLegacyEmailTokenForTest(ctx *config.AppContext, email string) string {
	mac := hmac.New(sha256.New, ctx.Env.HMACKey[:])
	mac.Write([]byte(email))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestScopedHMACRoundTrip(t *testing.T) {
	ctx := testAppContext(t)
	token := CreateScopedHMAC(ctx, "media-render", "/media/imgs/example.png")

	if !VerifyScopedHMAC(ctx, "media-render", "/media/imgs/example.png", token) {
		t.Fatal("expected scoped token to verify")
	}
	if VerifyScopedHMAC(ctx, "different-purpose", "/media/imgs/example.png", token) {
		t.Fatal("expected scoped token to fail for a different purpose")
	}
}
