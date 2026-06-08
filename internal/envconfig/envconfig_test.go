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
