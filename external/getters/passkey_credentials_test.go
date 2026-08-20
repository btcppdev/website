package getters

import (
	"bytes"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestPasskeyCredentialEncryption(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{HMACKey: [32]byte{1, 2, 3, 4}}}
	plaintext := []byte(`{"id":"credential"}`)
	first, err := encryptPasskeyCredential(ctx, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	second, err := encryptPasskeyCredential(ctx, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) || bytes.Contains(first, plaintext) {
		t.Fatal("passkey credential encryption was deterministic or exposed plaintext")
	}
	decrypted, err := decryptPasskeyCredential(ctx, first)
	if err != nil || !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted passkey = %q, %v", decrypted, err)
	}
	wrongKey := &config.AppContext{Env: &types.EnvConfig{HMACKey: [32]byte{9, 8, 7, 6}}}
	if _, err := decryptPasskeyCredential(wrongKey, first); err == nil {
		t.Fatal("passkey credential decrypted with the wrong key")
	}
}
