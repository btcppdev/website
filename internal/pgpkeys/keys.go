// Package pgpkeys handles public OpenPGP certificates and proof of possession.
package pgpkeys

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
)

const MaxKeyBytes = 128 * 1024
const MaxSignatureBytes = 32 * 1024
const ChallengeLifetime = 30 * time.Minute

type PublicKey struct {
	Fingerprint string
	KeyID       string
	Armored     string
	Binary      []byte
	entity      *openpgp.Entity
}

// Parse accepts exactly one armored public certificate. Inspect all packets
// before parsing the entity so secret packets cannot be silently discarded.
func Parse(input string, now time.Time) (*PublicKey, error) {
	input = strings.TrimSpace(input)
	if len(input) > MaxKeyBytes || !strings.HasPrefix(input, "-----BEGIN PGP PUBLIC KEY BLOCK-----") || strings.Count(input, "-----BEGIN ") != 1 || !strings.HasSuffix(input, "-----END PGP PUBLIC KEY BLOCK-----") {
		return nil, errors.New("Paste one ASCII-armored public PGP key, up to 128 KiB. Never upload a private key.")
	}
	block, err := armor.Decode(strings.NewReader(input))
	if err != nil || block.Type != openpgp.PublicKeyType {
		return nil, errors.New("Invalid public PGP key armor.")
	}
	data, err := io.ReadAll(io.LimitReader(block.Body, MaxKeyBytes+1))
	if err != nil || len(data) > MaxKeyBytes {
		return nil, errors.New("Invalid or oversized public PGP key.")
	}
	packets := packet.NewReader(bytes.NewReader(data))
	primaries := 0
	for {
		p, err := packets.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("Unable to read the public PGP key packets.")
		}
		switch p := p.(type) {
		case *packet.PrivateKey:
			return nil, errors.New("Private keys are not accepted. Export only your public key.")
		case *packet.PublicKey:
			if !p.IsSubkey {
				primaries++
			}
		}
	}
	if primaries != 1 {
		return nil, errors.New("Add one public PGP key at a time.")
	}
	entities, err := openpgp.ReadKeyRing(bytes.NewReader(data))
	if err != nil || len(entities) != 1 {
		return nil, errors.New("Unable to read the public PGP key.")
	}
	e := entities[0]
	if _, ok := e.SigningKey(now, nil); !ok {
		return nil, errors.New("This key has no valid signing key, or is expired, revoked, or unsupported.")
	}
	var binary bytes.Buffer
	if err := e.Serialize(&binary); err != nil {
		return nil, err
	}
	armored, err := Armor(binary.Bytes())
	if err != nil {
		return nil, err
	}
	return &PublicKey{Fingerprint: strings.ToUpper(hex.EncodeToString(e.PrimaryKey.Fingerprint)), KeyID: fmt.Sprintf("%016X", e.PrimaryKey.KeyId), Armored: armored, Binary: binary.Bytes(), entity: e}, nil
}

func Armor(data []byte) (string, error) {
	var out bytes.Buffer
	// CRC24 is optional for v4 and forbidden for v6 public certificates.
	w, err := armor.EncodeWithChecksumOption(&out, openpgp.PublicKeyType, nil, false)
	if err != nil {
		return "", err
	}
	if _, err := w.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func NewChallenge(personID, fingerprint string, expires time.Time) (string, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return fmt.Sprintf("bitcoin++ PGP profile key verification\nAccount: %s\nFingerprint: %s\nExpires: %s\nNonce: %x\n\nI authorize bitcoin++ to publish this public key on my profile.\n", personID, fingerprint, expires.UTC().Format(time.RFC3339), nonce), nil
}

func (key *PublicKey) Verify(challenge, signature string, now time.Time) error {
	if len(signature) > MaxSignatureBytes {
		return errors.New("The signature is too large.")
	}
	cfg := &packet.Config{Time: func() time.Time { return now }}
	sig, signer, err := openpgp.VerifyArmoredDetachedSignature(openpgp.EntityList{key.entity}, strings.NewReader(challenge), strings.NewReader(signature), cfg)
	if err != nil || signer == nil || sig == nil {
		return errors.New("The signature did not verify. Sign the downloaded challenge with this key and paste its detached ASCII-armored signature.")
	}
	// Recheck current key validity, including the specific signing subkey.
	if sig.IssuerKeyId == nil {
		return errors.New("The signature must identify its signing key.")
	}
	if _, ok := key.entity.SigningKeyById(now, *sig.IssuerKeyId, cfg); !ok {
		return errors.New("The signing key is expired or revoked.")
	}
	return nil
}
