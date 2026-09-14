package pgpkeys

import (
	"bytes"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T, cfg *packet.Config) (*openpgp.Entity, string) {
	t.Helper()
	e, err := openpgp.NewEntity("Test User", "", "public@example.test", cfg)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := e.Serialize(&b); err != nil {
		t.Fatal(err)
	}
	armored, err := Armor(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return e, armored
}

func TestProofOfPossession(t *testing.T) {
	for _, v6 := range []bool{false, true} {
		t.Run(map[bool]string{false: "v4", true: "v6"}[v6], func(t *testing.T) {
			now := time.Now().Truncate(time.Second)
			cfg := &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Time: func() time.Time { return now }, V6Keys: v6}
			if v6 {
				cfg.Algorithm = packet.PubKeyAlgoEd25519
			}
			e, _ := testKey(t, cfg)
			if err := e.AddSigningSubkey(cfg); err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			if err := e.Serialize(&b); err != nil {
				t.Fatal(err)
			}
			armored, _ := Armor(b.Bytes())
			key, err := Parse(armored, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(key.KeyID) != 16 {
				t.Fatal(key.KeyID)
			}
			if v6 && key.KeyID != key.Fingerprint[:16] {
				t.Fatal("v6 ID must use leading 64 bits")
			}
			if !v6 && key.KeyID != key.Fingerprint[len(key.Fingerprint)-16:] {
				t.Fatal("v4 ID must use trailing 64 bits")
			}
			challenge, _ := NewChallenge("person-one", key.Fingerprint, now.Add(ChallengeLifetime))
			var sig bytes.Buffer
			if err := openpgp.ArmoredDetachSign(&sig, []*openpgp.Entity{e}, strings.NewReader(challenge), &openpgp.SignParams{Config: cfg}); err != nil {
				t.Fatal(err)
			}
			if err := key.Verify(challenge, sig.String(), now); err != nil {
				t.Fatal(err)
			}
			if err := key.Verify(challenge+"tampered", sig.String(), now); err == nil {
				t.Fatal("accepted changed challenge")
			}
			other, _ := testKey(t, cfg)
			sig.Reset()
			if err := openpgp.ArmoredDetachSign(&sig, []*openpgp.Entity{other}, strings.NewReader(challenge), &openpgp.SignParams{Config: cfg}); err != nil {
				t.Fatal(err)
			}
			if err := key.Verify(challenge, sig.String(), now); err == nil {
				t.Fatal("accepted different signing key")
			}
		})
	}
}

func TestRejectPrivateExpiredRevokedAndMultipleKeys(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	cfg := &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Time: func() time.Time { return now }}
	e, armored := testKey(t, cfg)
	var private bytes.Buffer
	if err := e.SerializePrivate(&private, cfg); err != nil {
		t.Fatal(err)
	}
	disguised, _ := Armor(private.Bytes())
	for _, input := range []string{disguised, armored + armored, "nonsense", strings.Repeat("a", MaxKeyBytes+1)} {
		if _, err := Parse(input, now); err == nil {
			t.Fatal("accepted invalid or private certificate")
		}
	}
	cfg.KeyLifetimeSecs = 60
	_, expires := testKey(t, cfg)
	if _, err := Parse(expires, now.Add(time.Hour)); err == nil {
		t.Fatal("accepted expired certificate")
	}
	if err := e.Revoke(packet.KeyRetired, "test", cfg); err != nil {
		t.Fatal(err)
	}
	var revoked bytes.Buffer
	if err := e.Serialize(&revoked); err != nil {
		t.Fatal(err)
	}
	revokedArmor, _ := Armor(revoked.Bytes())
	if _, err := Parse(revokedArmor, now); err == nil {
		t.Fatal("accepted revoked certificate")
	}
}

// Exercise the exact GnuPG command documented in profile settings, using only
// a generated fixture key and a disposable keyring.
func TestGnuPGDetachedSignature(t *testing.T) {
	gpg, err := exec.LookPath("gpg")
	if err != nil {
		t.Skip("GnuPG not installed")
	}
	cfg := &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA}
	e, armored := testKey(t, cfg)
	key, err := Parse(armored, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	var private bytes.Buffer
	if err := e.SerializePrivate(&private, cfg); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(gpg, "--homedir", home, "--batch", "--import")
	cmd.Stdin = &private
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("import fixture: %v %s", err, out)
	}
	defer func() {
		if gpgconf, err := exec.LookPath("gpgconf"); err == nil {
			exec.Command(gpgconf, "--homedir", home, "--kill", "gpg-agent").Run()
		}
	}()
	challenge, _ := NewChallenge("gnupg-fixture", key.Fingerprint, time.Now().Add(ChallengeLifetime))
	path := filepath.Join(home, "btcpp-pgp-challenge.txt")
	if err := os.WriteFile(path, []byte(challenge), 0600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(gpg, "--homedir", home, "--batch", "--pinentry-mode", "loopback", "--armor", "--local-user", key.Fingerprint, "--detach-sign", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sign fixture: %v %s", err, out)
	}
	signature, err := os.ReadFile(path + ".asc")
	if err != nil {
		t.Fatal(err)
	}
	if err := key.Verify(challenge, string(signature), time.Now()); err != nil {
		t.Fatal(err)
	}
}
