package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/alexedwards/scs/v2"
	"github.com/go-webauthn/webauthn/webauthn"
)

func TestNewWebAuthnUsesPublicOrigin(t *testing.T) {
	rp, err := newWebAuthn(&config.AppContext{Env: &types.EnvConfig{Prod: false, Host: "localhost", Port: "8888"}})
	if err != nil {
		t.Fatal(err)
	}
	if rp.Config.RPID != "localhost" || len(rp.Config.RPOrigins) != 1 || rp.Config.RPOrigins[0] != "http://localhost:8888" {
		t.Fatalf("WebAuthn config = %q / %v", rp.Config.RPID, rp.Config.RPOrigins)
	}
}

func TestPasskeySessionIsOneUse(t *testing.T) {
	manager := scs.New()
	requestContext, err := manager.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/passkey/login/verify", nil).WithContext(requestContext)
	ctx := &config.AppContext{Session: manager}
	session := &webauthn.SessionData{Challenge: "challenge", Expires: time.Now().Add(time.Minute)}
	if err := storePasskeySession(ctx, req, passkeyLoginSessionKey, session); err != nil {
		t.Fatal(err)
	}
	loaded, err := takePasskeySession(ctx, req, passkeyLoginSessionKey)
	if err != nil || loaded.Challenge != session.Challenge {
		t.Fatalf("loaded passkey session = %+v, %v", loaded, err)
	}
	if _, err := takePasskeySession(ctx, req, passkeyLoginSessionKey); err == nil {
		t.Fatal("passkey session was reusable")
	}
}
