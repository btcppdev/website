package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
)

func TestAuthStatusUsesCachedAccountPhoto(t *testing.T) {
	session := scs.New()
	sessionContext, err := session.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	session.Put(sessionContext, auth.SessionEmailKey, "person@example.test")
	session.Put(sessionContext, auth.SessionPersonIDKey, "person-id")
	session.Put(sessionContext, sessionAccountPhotoPersonKey, "person-id")
	session.Put(sessionContext, sessionAccountPhotoURLKey, "https://cdn.example.test/person.jpg")

	request := httptest.NewRequest("GET", "/auth/status", nil).WithContext(sessionContext)
	response := httptest.NewRecorder()
	AuthStatus(response, request, &config.AppContext{Session: session})

	if response.Code != 200 {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Authenticated bool   `json:"authenticated"`
		PhotoURL      string `json:"photoUrl"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Authenticated || body.PhotoURL != "https://cdn.example.test/person.jpg" {
		t.Fatalf("response = %+v, want cached authenticated account", body)
	}
}

func TestDashboardRequestIdentityAcceptsSession(t *testing.T) {
	session := scs.New()
	sessionContext, err := session.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	session.Put(sessionContext, auth.SessionEmailKey, "person@example.test")
	app := &config.AppContext{
		Env:     &types.EnvConfig{},
		Session: session,
	}
	request := httptest.NewRequest("GET", "/dashboard/speaker", nil).WithContext(sessionContext)

	email, encodedEmail, encodedHMAC, ok := dashboardRequestIdentity(httptest.NewRecorder(), request, app)
	if !ok || email != "person@example.test" {
		t.Fatalf("identity = %q, ok = %t", email, ok)
	}
	decodedEmail, err := base64.RawURLEncoding.DecodeString(encodedEmail)
	if err != nil {
		t.Fatalf("decode email: %v", err)
	}
	if string(decodedEmail) != email {
		t.Fatalf("decoded email = %q, want %q", decodedEmail, email)
	}
	decodedHMAC, err := base64.RawURLEncoding.DecodeString(encodedHMAC)
	if err != nil {
		t.Fatalf("decode HMAC: %v", err)
	}
	if !helpers.VerifyEmailHMAC(app, string(decodedHMAC), email) {
		t.Fatal("session-derived HMAC did not validate")
	}
}
