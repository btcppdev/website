package emails

import (
	"io"
	"log"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
)

func TestMailDeliveryTargetRedirectsAndNamespacesDevelopmentEmail(t *testing.T) {
	ctx := &config.AppContext{
		Env:   &types.EnvConfig{Prod: false, DevEmailOverride: "Developer <developer@example.com>"},
		Infos: log.New(io.Discard, "", 0),
	}
	to, jobKey, err := mailDeliveryTarget(ctx, "speaker@example.com", "speaker-reminder")
	if err != nil {
		t.Fatal(err)
	}
	if to != "developer@example.com" {
		t.Fatalf("recipient = %q", to)
	}
	if !strings.HasPrefix(jobKey, "dev-") || !strings.HasSuffix(jobKey, "-speaker-reminder") {
		t.Fatalf("job key = %q", jobKey)
	}
}

func TestMailDeliveryTargetIgnoresOverrideInProduction(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Prod: true, DevEmailOverride: "developer@example.com"}}
	to, jobKey, err := mailDeliveryTarget(ctx, "speaker@example.com", "speaker-reminder")
	if err != nil {
		t.Fatal(err)
	}
	if to != "speaker@example.com" || jobKey != "speaker-reminder" {
		t.Fatalf("production target = (%q, %q)", to, jobKey)
	}
}

func TestMailDeliveryTargetRejectsInvalidDevelopmentOverride(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Prod: false, DevEmailOverride: "not-an-email"}}
	_, _, err := mailDeliveryTarget(ctx, "speaker@example.com", "job")
	if err == nil {
		t.Fatal("expected invalid DEV_EMAIL_OVERRIDE to fail")
	}
}

func TestNewsletterPreviewJobKeysAlwaysPunchThrough(t *testing.T) {
	letter := &mtypes.Letter{UID: 42, Title: "[TEST] Edited newsletter"}

	stable := newsletterMissiveJobKey("reader@example.com", letter, false)
	if again := newsletterMissiveJobKey("reader@example.com", letter, false); again != stable {
		t.Fatalf("production job key changed: %q != %q", stable, again)
	}

	first := newsletterMissiveJobKey("reader@example.com", letter, true)
	second := newsletterMissiveJobKey("reader@example.com", letter, true)
	if first == second {
		t.Fatalf("repeat preview reused idempotency key %q", first)
	}
	for _, key := range []string{first, second} {
		if !strings.HasPrefix(key, stable+"-test-") {
			t.Fatalf("preview key %q does not retain base missive identity %q", key, stable)
		}
	}
}
