package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func certificate(t *testing.T, name string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, c, c, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestPruneAndVerify(t *testing.T) {
	dir := t.TempDir()
	good := certificate(t, "Unrelated retained root")
	if err := os.WriteFile(filepath.Join(dir, "good.crt"), good, 0644); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"emSign ECC Root CA - G3", "emSign Root TLS CA - G3", "AAA Certificate Services"} {
		path := filepath.Join(dir, string(rune('a'+i))+".crt")
		if err := os.WriteFile(path, certificate(t, name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if run([]string{"verify", dir}) == nil {
		t.Fatal("unsafe store accepted")
	}
	if err := run([]string{"prune", dir}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"prune", dir}); err != nil {
		t.Fatal("pruning is not idempotent", err)
	}
	if err := run([]string{"verify", dir}); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatal("unrelated trust changed", err)
	}
	bad := certificate(t, "AAA Certificate Services")
	if err := os.WriteFile(filepath.Join(dir, "ca-certificates.crt"), append(good, bad...), 0644); err != nil {
		t.Fatal(err)
	}
	if run([]string{"verify", dir}) == nil {
		t.Fatal("blocked root in concatenated bundle accepted")
	}
}

func TestTrustGuardFailsClosed(t *testing.T) {
	if run([]string{"verify", t.TempDir()}) == nil {
		t.Fatal("empty store accepted")
	}
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "blocked.pem")
	if err := os.WriteFile(target, certificate(t, "emSign ECC Root CA - G3"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "12345678.0")); err != nil {
		t.Fatal(err)
	}
	if run([]string{"verify", dir}) == nil {
		t.Fatal("blocked root via hashed symlink accepted")
	}
}
