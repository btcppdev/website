package emails

import (
	"io"
	"log"
	"strings"
	"testing"

	"btcpp-web/internal/config"
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
