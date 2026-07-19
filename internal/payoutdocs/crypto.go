package payoutdocs

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
)

var envelopeMagic = []byte("BTCPP-TAX-v1\x00")

func Encrypt(secret string, plaintext []byte) ([]byte, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, fmt.Errorf("TAX_FORM_ENCRYPTION_KEY is not configured")
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("create tax form nonce: %w", err)
	}
	out := append([]byte{}, envelopeMagic...)
	out = append(out, nonce...)
	out = gcm.Seal(out, nonce, plaintext, envelopeMagic)
	return out, nil
}

func Decrypt(secret string, envelope []byte) ([]byte, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, fmt.Errorf("TAX_FORM_ENCRYPTION_KEY is not configured")
	}
	if len(envelope) < len(envelopeMagic) || string(envelope[:len(envelopeMagic)]) != string(envelopeMagic) {
		return nil, fmt.Errorf("unrecognized encrypted tax form")
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	rest := envelope[len(envelopeMagic):]
	if len(rest) < gcm.NonceSize() {
		return nil, fmt.Errorf("truncated encrypted tax form")
	}
	nonce, ciphertext := rest[:gcm.NonceSize()], rest[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, envelopeMagic)
	if err != nil {
		return nil, fmt.Errorf("decrypt tax form: %w", err)
	}
	return plaintext, nil
}
