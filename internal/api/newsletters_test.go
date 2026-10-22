package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestNewsletterAuthorizationAndChoices(t *testing.T) {
	for _, tc := range []struct {
		name, method, scope, body, key string
		authenticated                  bool
		storeErr                       error
		status, calls                  int
	}{
		{name: "anonymous", method: "GET", status: 401},
		{name: "profile scope is insufficient", method: "GET", authenticated: true, scope: "profile:self:read", status: 403},
		{name: "write does not imply read", method: "GET", authenticated: true, scope: "newsletters:self:write", status: 403},
		{name: "read", method: "GET", authenticated: true, scope: "newsletters:self:read", status: 200, calls: 1},
		{name: "read cannot write", method: "PUT", authenticated: true, scope: "newsletters:self:read", status: 403},
		{name: "missing choice", method: "PUT", authenticated: true, scope: "newsletters:self:write", body: `{"email":"me@example.com","newsletter":"newsletter"}`, key: "request-key-0001", status: 422},
		{name: "missing key", method: "PUT", authenticated: true, scope: "newsletters:self:write", body: `{"email":"me@example.com","newsletter":"newsletter","subscribed":true}`, status: 422},
		{name: "cannot select another account", method: "PUT", authenticated: true, scope: "newsletters:self:write", body: `{"person_id":"someone-else","email":"me@example.com","newsletter":"newsletter","subscribed":true}`, key: "request-key-0001", status: 400},
		{name: "subscribe", method: "PUT", authenticated: true, scope: "newsletters:self:write", body: `{"email":"me@example.com","newsletter":"newsletter","subscribed":true}`, key: "request-key-0001", status: 200, calls: 1},
		{name: "unsubscribe is explicit false", method: "PUT", authenticated: true, scope: "newsletters:self:write", body: `{"email":"me@example.com","newsletter":"newsletter","subscribed":false}`, key: "request-key-0001", status: 200, calls: 1},
		{name: "unverified email", method: "PUT", authenticated: true, scope: "newsletters:self:write", body: `{"email":"me@example.com","newsletter":"newsletter","subscribed":true}`, key: "request-key-0001", storeErr: getters.ErrNewsletterEmail, status: 403, calls: 1},
		{name: "key conflict", method: "PUT", authenticated: true, scope: "newsletters:self:write", body: `{"email":"me@example.com","newsletter":"newsletter","subscribed":true}`, key: "request-key-0001", storeErr: getters.ErrNewsletterConflict, status: 409, calls: 1},
		{name: "invalid newsletter", method: "PUT", authenticated: true, scope: "newsletters:self:write", body: `{"email":"me@example.com","newsletter":"other","subscribed":true}`, key: "request-key-0001", storeErr: getters.ErrNewsletterInput, status: 422, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := &server{now: time.Now,
				authenticateToken: func(string) (*auth.BearerGrant, error) {
					return &auth.BearerGrant{PersonID: "owner", ClientID: "alerts", Scopes: []string{tc.scope}}, nil
				},
				loadPerson: func(id string) (*types.Speaker, error) {
					if id != "owner" {
						t.Fatalf("account = %s", id)
					}
					return &types.Speaker{ID: id}, nil
				},
				listNewsletters: func(id string) ([]getters.NewsletterPreference, error) {
					calls++
					if id != "owner" {
						t.Fatalf("account = %s", id)
					}
					return []getters.NewsletterPreference{{Email: "me@example.com", Newsletter: "newsletter", Subscribed: true}}, nil
				},
				setNewsletter: func(id, client, key string, p getters.NewsletterPreference) error {
					calls++
					if id != "owner" || client != "alerts" || key != tc.key || p.Email != "me@example.com" {
						t.Fatalf("unexpected preference identity: %s %s %s %+v", id, client, key, p)
					}
					if p.Subscribed != strings.Contains(tc.body, `"subscribed":true`) {
						t.Fatalf("choice lost: %+v", p)
					}
					return tc.storeErr
				},
			}
			router := mux.NewRouter()
			s.register(router.PathPrefix("/api/v1").Subrouter())
			req := httptest.NewRequest(tc.method, "/api/v1/me/newsletters", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", tc.key)
			if tc.authenticated {
				req.Header.Set("Authorization", "Bearer test")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status || calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s; want status=%d calls=%d", rec.Code, calls, rec.Body.String(), tc.status, tc.calls)
			}
			if rec.Code == http.StatusOK && !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("private response must not be cached")
			}
			if tc.method == "GET" && rec.Code == 200 && !strings.Contains(rec.Body.String(), `"newsletter":"newsletter"`) {
				t.Fatalf("preferences missing: %s", rec.Body.String())
			}
		})
	}
}
