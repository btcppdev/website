package helpers

import (
	"testing"

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
