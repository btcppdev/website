package envconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsMailerOffWhenDotEnvExists(t *testing.T) {
	t.Setenv("MAILER_OFF", "")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("PORT=8888\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !env.MailOff {
		t.Fatal("expected .env-backed config to default MailOff=true")
	}
	if env.MailerJobEnabled {
		t.Fatal("expected .env-backed config to default the background mailer job off")
	}
}

func TestLoadDoesNotDefaultMailerOffWhenDotEnvMissing(t *testing.T) {
	t.Setenv("MAILER_OFF", "")
	env, err := Load(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if env.MailOff {
		t.Fatal("expected env-only config to default MailOff=false")
	}
	if !env.MailerJobEnabled {
		t.Fatal("expected env-only production config to default the background mailer job on")
	}
}

func TestLoadRespectsExplicitMailerOff(t *testing.T) {
	t.Setenv("MAILER_OFF", "")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("MAILER_OFF=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if env.MailOff {
		t.Fatal("expected explicit MAILER_OFF=false to override .env default")
	}
}

func TestLoadAllowsEmailWithoutBackgroundMailerJob(t *testing.T) {
	t.Setenv("MAILER_OFF", "false")
	t.Setenv("MAILER_JOB_ENABLED", "")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("PORT=8888\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if env.MailOff {
		t.Fatal("expected request-driven email to be enabled")
	}
	if env.MailerJobEnabled {
		t.Fatal("expected the development background mailer job to remain disabled")
	}
}

func TestLoadCanExplicitlyEnableBackgroundMailerJob(t *testing.T) {
	t.Setenv("MAILER_JOB_ENABLED", "true")
	env, err := Load(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !env.MailerJobEnabled {
		t.Fatal("expected explicit MAILER_JOB_ENABLED=true to be honored")
	}
}

func TestYouTubeUpdatesDefaultFromProductionMode(t *testing.T) {
	t.Setenv("YOUTUBE_UPDATES_ENABLED", "")
	t.Setenv("PROD", "false")
	dev, err := Load(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if dev.YouTube.UpdatesEnabled {
		t.Fatal("expected YouTube updates to default off when PROD=false")
	}

	t.Setenv("PROD", "true")
	prod, err := Load(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !prod.YouTube.UpdatesEnabled {
		t.Fatal("expected YouTube updates to default on when PROD=true")
	}
}

func TestYouTubeUpdatesCanBeEnabledInDevelopment(t *testing.T) {
	t.Setenv("PROD", "false")
	t.Setenv("YOUTUBE_UPDATES_ENABLED", "true")
	env, err := Load(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !env.YouTube.UpdatesEnabled {
		t.Fatal("expected explicit YOUTUBE_UPDATES_ENABLED=true to be honored")
	}
}

func TestLoadReadsLocalExternal(t *testing.T) {
	t.Setenv("LOCAL_EXTERNAL", "")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("PROD=false\nLOCAL_EXTERNAL=https://example.ngrok.app\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if env.LocalExternal != "https://example.ngrok.app" {
		t.Fatalf("LocalExternal = %q", env.LocalExternal)
	}
	if env.GetURI() != "https://example.ngrok.app" {
		t.Fatalf("GetURI() = %q", env.GetURI())
	}
}

func TestLoadReadsDevEmailOverride(t *testing.T) {
	t.Setenv("DEV_EMAIL_OVERRIDE", "")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("DEV_EMAIL_OVERRIDE=developer@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if env.DevEmailOverride != "developer@example.com" {
		t.Fatalf("DevEmailOverride = %q", env.DevEmailOverride)
	}
}

func TestLoadUsesCurrentEasyshipDefaults(t *testing.T) {
	t.Setenv("EASYSHIP_ENDPOINT", "")
	t.Setenv("EASYSHIP_API_VERSION", "")
	env, err := Load(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if env.Easyship.Endpoint != "https://public-api.easyship.com" {
		t.Fatalf("Easyship endpoint = %q", env.Easyship.Endpoint)
	}
	if env.Easyship.APIVersion != "2024-09" {
		t.Fatalf("Easyship API version = %q", env.Easyship.APIVersion)
	}
}

func TestLoadReadsGitHubAuthConfig(t *testing.T) {
	t.Setenv("AUTH_GITHUB_CLIENT_ID", "github-client")
	t.Setenv("AUTH_GITHUB_CLIENT_SECRET", "github-secret")
	env, err := Load(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if env.OAuth.GitHub.ClientID != "github-client" || env.OAuth.GitHub.ClientSecret != "github-secret" {
		t.Fatalf("GitHub OAuth config = %+v", env.OAuth.GitHub)
	}
}
