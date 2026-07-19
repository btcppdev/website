package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestVerifyEasyshipSignature(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	secret := "webh_test_secret"
	token := signEasyshipTestJWT(secret, fmt.Sprintf(`{"easyship_company_id":"company","exp":%d}`, now.Add(time.Minute).Unix()))
	if err := verifyEasyshipSignature(token, secret, now); err != nil {
		t.Fatalf("valid signature rejected: %s", err)
	}
	if err := verifyEasyshipSignature(token, "wrong-secret", now); err == nil {
		t.Fatal("signature with wrong secret was accepted")
	}
}

func TestVerifyEasyshipSignatureRejectsExpiryAndAlgorithm(t *testing.T) {
	now := time.Unix(1_720_000_000, 0)
	expired := signEasyshipTestJWT("webh_test", fmt.Sprintf(`{"exp":%d}`, now.Add(-time.Second).Unix()))
	if err := verifyEasyshipSignature(expired, "webh_test", now); err == nil {
		t.Fatal("expired signature was accepted")
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{}`))
	if err := verifyEasyshipSignature(header+"."+claims+".", "webh_test", now); err == nil {
		t.Fatal("non-HS256 signature was accepted")
	}
}

func TestEasyshipCallbackRejectsInvalidSignatureBeforeDatabase(t *testing.T) {
	ctx := &config.AppContext{
		Env: &types.EnvConfig{Easyship: types.EasyshipConfig{WebhookSecret: "webh_test"}},
		Err: log.New(io.Discard, "", 0),
	}
	req := httptest.NewRequest(http.MethodPost, "/callbacks/easyship", strings.NewReader(`{"event_type":"shipment.label.created"}`))
	req.Header.Set("X-EASYSHIP-SIGNATURE", "invalid")
	recorder := httptest.NewRecorder()
	EasyshipCallback(recorder, req, ctx)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func signEasyshipTestJWT(secret, claimsJSON string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(claimsJSON))
	input := header + "." + claims
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
