package auth

import (
	"bytes"
	"testing"
)

func TestPasskeyUserIDUsesRawUUID(t *testing.T) {
	user := &PasskeyUser{PersonID: "00000000-0000-4000-8000-000000000123", Name: "Test User", Email: "test@example.com"}
	if got := user.WebAuthnID(); len(got) != 16 || !bytes.Equal(got, []byte{0, 0, 0, 0, 0, 0, 0x40, 0, 0x80, 0, 0, 0, 0, 0, 1, 0x23}) {
		t.Fatalf("WebAuthnID() = %x", got)
	}
	if user.WebAuthnName() != "test@example.com" || user.WebAuthnDisplayName() != "Test User" {
		t.Fatalf("passkey names = %q / %q", user.WebAuthnName(), user.WebAuthnDisplayName())
	}
}
