package signer

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func TestIssuerSignsCompactEdDSAToken(t *testing.T) {
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	issuer, err := NewIssuer(base64.StdEncoding.EncodeToString(private))
	if err != nil {
		t.Fatal(err)
	}
	token, err := issuer.Sign(Claims{Issuer: "https://btcpp.dev", Audience: "https://bunker.btcpp.dev", TokenID: "token"})
	if err != nil || len(strings.Split(token, ".")) != 3 {
		t.Fatalf("token = %q, err = %v", token, err)
	}
}

func TestIssuerRejectsInvalidKey(t *testing.T) {
	if _, err := NewIssuer(base64.StdEncoding.EncodeToString(make([]byte, 31))); err == nil {
		t.Fatal("short key accepted")
	}
}
