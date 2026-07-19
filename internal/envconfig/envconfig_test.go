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
