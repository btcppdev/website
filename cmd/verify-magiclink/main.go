// verify-magiclink verifies a magic-link URL's HMAC against the local
// HMACSecret. Use to debug "this link 401s" / "this link landed me on
// dashboard-login" reports — distinguishes between a malformed link
// (bad base64, wrong byte length) and a signature mismatch (right
// shape, wrong secret or wrong email).
//
// Usage:
//
//	go run ./cmd/verify-magiclink -url 'http://localhost:8888/auth?em=...&hr=...'
//
// Reads .env from the cwd to pull HMAC_SECRET. No network
// calls. No Notion writes.
package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

func main() {
	rawURL := flag.String("url", "", "magic-link URL to verify (required)")
	flag.Parse()
	if *rawURL == "" {
		log.Fatal("required: -url")
	}

	if err := envconfig.LoadDotEnv(".env"); err != nil && !os.IsNotExist(err) {
		log.Fatal(err)
	}
	hmacSecret := os.Getenv("HMAC_SECRET")
	if hmacSecret == "" {
		log.Fatal(".env or environment is missing HMAC_SECRET — refusing to verify against a zero-byte key")
	}

	u, err := url.Parse(*rawURL)
	if err != nil {
		log.Fatalf("parse url: %s", err)
	}
	em := u.Query().Get("em")
	hr := u.Query().Get("hr")
	if em == "" || hr == "" {
		log.Fatalf("URL is missing em or hr query param (em=%q hr=%q)", em, hr)
	}

	emailBytes, err := base64.RawURLEncoding.DecodeString(em)
	if err != nil {
		log.Fatalf("decode em (base64url): %s", err)
	}
	tokenBytes, err := base64.RawURLEncoding.DecodeString(hr)
	if err != nil {
		log.Fatalf("decode hr (base64url): %s", err)
	}
	email := string(emailBytes)
	token := string(tokenBytes)

	key, err := types.DeriveHMACKey(hmacSecret)
	if err != nil {
		log.Fatal(err)
	}
	ctx := &config.AppContext{
		Env: &types.EnvConfig{HMACKey: key},
	}

	fmt.Printf("Decoded email: %q\n", email)
	fmt.Printf("Supplied token (%d chars): %s\n", len(token), token)

	if helpers.VerifyEmailHMAC(ctx, token, email) {
		fmt.Println("\nMATCH — the link is valid for this HMACSecret.")
		os.Exit(0)
	}

	fmt.Println("\nMISMATCH — the link will fail VerifyEmailHMAC.")
	switch {
	case !strings.HasPrefix(token, "v1."):
		fmt.Println("The token is not the current v1 expiring-token format.")
	default:
		fmt.Println("Most likely: the link expired, was minted against a different HMACSecret")
		fmt.Println("(prod vs local .env mismatch, or the secret rotated since the link was generated).")
	}
	os.Exit(1)
}
