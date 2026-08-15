package types

import (
	"strings"
	"testing"
)

func TestDeriveHMACKeyRejectsWeakSecrets(t *testing.T) {
	for _, secret := range []string{"", "   "} {
		if _, err := DeriveHMACKey(secret); err == nil {
			t.Fatalf("DeriveHMACKey(%q) returned nil error", secret)
		}
	}
}

func TestDeriveHMACKeyAcceptsExistingSecret(t *testing.T) {
	if _, err := DeriveHMACKey("existing-prod-secret"); err != nil {
		t.Fatalf("DeriveHMACKey returned error: %s", err)
	}
}

func TestEnvConfigValidateRequiresProdSecrets(t *testing.T) {
	env := &EnvConfig{
		Port:      "8080",
		Host:      "https://example.test",
		MailerJob: 60,
		Prod:      true,
	}

	err := env.Validate()
	if err == nil {
		t.Fatal("Validate returned nil error")
	}
	for _, want := range []string{
		"MAILER_SECRET",
		"MAILER_ENDPOINT",
		"STRIPE_KEY",
		"STRIPE_END_SECRET",
		"OPENNODE_KEY",
		"OPENNODE_ENDPOINT",
		"REGISTRY_PIN",
		"EASYSHIP_API_KEY",
		"EASYSHIP_WEBHOOK_SECRET",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate error %q missing %s", err, want)
		}
	}
}

func TestEnvConfigValidateAllowsCompleteProdConfig(t *testing.T) {
	env := &EnvConfig{
		Port:              "8080",
		Host:              "https://example.test",
		MailerJob:         60,
		MailerSecret:      "mailer",
		MailEndpoint:      "https://mailer.example.test",
		StripeKey:         "stripe",
		StripeEndpointSec: "stripe-webhook",
		RegistryPin:       "pin",
		OpenNode: OpenNodeConfig{
			Key:      "opennode",
			Endpoint: "https://opennode.example.test",
		},
		Easyship: EasyshipConfig{
			APIKey: "easyship", WebhookSecret: "webh_easyship",
		},
		Prod: true,
	}

	if err := env.Validate(); err != nil {
		t.Fatalf("Validate returned error: %s", err)
	}
}

func TestEnvConfigValidateOnlyRequiresMailerIntervalWhenJobEnabled(t *testing.T) {
	env := &EnvConfig{Port: "8080", Host: "http://localhost:8080", MailOff: false}
	if err := env.Validate(); err != nil {
		t.Fatalf("disabled background mailer job should not require an interval: %s", err)
	}

	env.MailerJobEnabled = true
	if err := env.Validate(); err == nil || !strings.Contains(err.Error(), "MAILER_JOB_SEC") {
		t.Fatalf("enabled background mailer job with no interval returned %v", err)
	}
}

func TestEnvConfigApplyDefaultsSeparatesXProfileObjects(t *testing.T) {
	staging := &EnvConfig{}
	staging.ApplyDefaults()
	if got, want := staging.Recordings.X.ProfileObject, "private/social/x-chrome-profile-staging.tgz.enc"; got != want {
		t.Fatalf("staging X profile object = %q, want %q", got, want)
	}

	prod := &EnvConfig{Prod: true}
	prod.ApplyDefaults()
	if got, want := prod.Recordings.X.ProfileObject, "private/social/x-chrome-profile-prod.tgz.enc"; got != want {
		t.Fatalf("prod X profile object = %q, want %q", got, want)
	}
}

func TestEnvConfigValidateRejectsPartialOAuthConfig(t *testing.T) {
	for _, test := range []struct {
		name string
		set  func(*EnvConfig, OAuthProviderConfig)
	}{
		{"GITHUB", func(env *EnvConfig, config OAuthProviderConfig) { env.OAuth.GitHub = config }},
		{"DISCORD", func(env *EnvConfig, config OAuthProviderConfig) { env.OAuth.Discord = config }},
		{"GITLAB", func(env *EnvConfig, config OAuthProviderConfig) { env.OAuth.GitLab = config }},
		{"MLH", func(env *EnvConfig, config OAuthProviderConfig) { env.OAuth.MLH = config }},
	} {
		base := EnvConfig{Port: "8888", Host: "localhost"}
		test.set(&base, OAuthProviderConfig{ClientID: "client"})
		if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "AUTH_"+test.name+"_CLIENT_ID") {
			t.Fatalf("partial %s client config returned %v", test.name, err)
		}
		test.set(&base, OAuthProviderConfig{ClientID: "client", ClientSecret: "secret"})
		if err := base.Validate(); err != nil {
			t.Fatalf("complete %s config returned %v", test.name, err)
		}
	}
}
