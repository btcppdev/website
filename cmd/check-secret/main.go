package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"flag"
	"log"
	"strings"
)

const unsanitizedPullSecretSHA256 = "726ccfe5d51bf42158b328539b235a3537f5502e1f4a553e01161db970a02233"

func main() {
	value := flag.String("value", "", "secret value to verify")
	flag.Parse()

	if err := verifySecret(*value); err != nil {
		log.Fatal(err)
	}
}

func verifySecret(value string) error {
	if value == "" {
		return errf("secret value is required")
	}
	return matchesSHA256(value, unsanitizedPullSecretSHA256)
}

func matchesSHA256(value string, sumHex string) error {
	sumHex = strings.ToLower(strings.TrimSpace(sumHex))
	if sumHex == "" {
		return errf("sha256 digest is required")
	}
	want, err := hex.DecodeString(sumHex)
	if err != nil || len(want) != sha256.Size {
		return errf("sha256 digest must be 64 hex characters")
	}
	got := sha256.Sum256([]byte(value))
	if subtle.ConstantTimeCompare(got[:], want) != 1 {
		return errf("secret does not match sha256 digest")
	}
	return nil
}

type secretError string

func (e secretError) Error() string { return string(e) }

func errf(msg string) error { return secretError(msg) }
