package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestAccountEntitlementsAuthorization(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, scope          string
		authenticated, empty bool
		status               int
	}{
		{"anonymous", "", false, false, 401},
		{"profile scope", "profile:self:read", true, false, 403},
		{"newsletter scope", "newsletters:self:read", true, false, 403},
		{"entitlements scope", "entitlements:self:read", true, false, 200},
		{"empty access", "entitlements:self:read", true, true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := &server{now: func() time.Time { return now },
				authenticateToken: func(string) (*auth.BearerGrant, error) {
					return &auth.BearerGrant{PersonID: "owner", Scopes: []string{tc.scope}}, nil
				},
				loadPerson: func(id string) (*types.Speaker, error) { return &types.Speaker{ID: id}, nil },
				listEntitlements: func(id string, at time.Time) ([]getters.ServiceEntitlement, error) {
					calls++
					if id != "owner" || !at.Equal(now) {
						t.Fatalf("untrusted identity/time: %s %v", id, at)
					}
					if tc.empty {
						return nil, nil
					}
					return []getters.ServiceEntitlement{{ID: "grant", ProductCode: "insider.article", ResourceID: "publication-123", Quantity: 1}}, nil
				},
			}
			router := mux.NewRouter()
			s.register(router.PathPrefix("/api/v1").Subrouter())
			r := httptest.NewRequest("GET", "/api/v1/me/entitlements?person_id=someone-else&at=2000-01-01", nil)
			if tc.authenticated {
				r.Header.Set("Authorization", "Bearer test")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.status != 200 {
				if calls != 0 {
					t.Fatal("unauthorized lookup")
				}
				return
			}
			if calls != 1 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("missing private account lookup")
			}
			var body struct {
				Data struct {
					AsOf         time.Time                    `json:"asOf"`
					Entitlements []getters.ServiceEntitlement `json:"entitlements"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if !body.Data.AsOf.Equal(now) || body.Data.Entitlements == nil {
				t.Fatalf("invalid response: %s", w.Body.String())
			}
			if !tc.empty && (len(body.Data.Entitlements) != 1 || body.Data.Entitlements[0].ResourceID != "publication-123") {
				t.Fatal("lost publication access")
			}
		})
	}
}
