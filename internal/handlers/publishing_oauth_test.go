package handlers

import (
	"encoding/base64"
	"net/url"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestPublishingOAuthCallbackUsesConfiguredSite(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  types.EnvConfig
		want string
	}{
		{"production", types.EnvConfig{Prod: true, Host: "btcpp.dev", Port: "8080"}, "https://btcpp.dev/admin/publishing/x-callback"},
		{"staging", types.EnvConfig{Prod: true, Host: "staging.example.test"}, "https://staging.example.test/admin/publishing/x-callback"},
		{"local", types.EnvConfig{Host: "127.0.0.1", Port: "8138"}, "http://127.0.0.1:8138/admin/publishing/x-callback"},
		{"tunnel", types.EnvConfig{Host: "127.0.0.1", Port: "8138", LocalExternal: "https://preview.example.test/"}, "https://preview.example.test/admin/publishing/x-callback"},
		{"override", types.EnvConfig{Prod: true, Host: "btcpp.dev", Publishing: types.PublishingConfig{XRedirectURL: " https://override.example.test/admin/publishing/x-callback "}}, "https://override.example.test/admin/publishing/x-callback"},
		{"blank override", types.EnvConfig{Prod: true, Host: "btcpp.dev", Publishing: types.PublishingConfig{XRedirectURL: " \t "}}, "https://btcpp.dev/admin/publishing/x-callback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &config.AppContext{Env: &tc.env}
			oauth := publishingOAuth(app)
			if oauth.RedirectURL != tc.want {
				t.Fatalf("callback=%q; want %q", oauth.RedirectURL, tc.want)
			}
			authURL, err := url.Parse(oauth.AuthCodeURL("test-state"))
			if err != nil {
				t.Fatal(err)
			}
			if got := authURL.Query().Get("redirect_uri"); got != tc.want {
				t.Fatalf("authorization redirect_uri=%q; want %q", got, tc.want)
			}
		})
	}
}

func TestPublishingXConfiguredDoesNotRequireRedirectOverride(t *testing.T) {
	env := &types.EnvConfig{Prod: true, Host: "btcpp.dev", Publishing: types.PublishingConfig{XClientID: "client", XClientSecret: "secret"}}
	env.Recordings.EncryptionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	app := &config.AppContext{Env: env}
	if !publishingXConfigured(app) {
		t.Fatal("valid client credentials with default callback should enable connecting")
	}
	for _, field := range []*string{&env.Publishing.XClientID, &env.Publishing.XClientSecret, &env.Recordings.EncryptionKey} {
		old := *field
		*field = ""
		if publishingXConfigured(app) {
			t.Fatal("missing credentials or encryption key should disable connecting")
		}
		*field = old
	}
}
