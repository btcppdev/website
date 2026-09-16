package api

import (
	"context"
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
				listAccountAccess: func(ctx context.Context, id string, at time.Time) (*getters.AccountAccessSnapshot, error) {
					calls++
					if id != "owner" || !at.Equal(now) {
						t.Fatalf("untrusted identity/time: %s %v", id, at)
					}
					if tc.empty {
						return &getters.AccountAccessSnapshot{AccountID: id, AsOf: at, Entitlements: []getters.ServiceEntitlement{}, Renewals: []getters.AlertsRenewalAccess{}}, nil
					}
					return &getters.AccountAccessSnapshot{AccountID: id, AsOf: at, Entitlements: []getters.ServiceEntitlement{{ID: "grant", ProductCode: "insider.article", ResourceID: "publication-123", Quantity: 1}}, Renewals: []getters.AlertsRenewalAccess{{RenewalID: "in_renewal", State: "failed", Revision: 1, DueAt: now, PaidSlots: 2, RenewalURL: "https://btcpp.dev/services/alerts/renew/in_renewal"}}}, nil
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
					AccountID    string                        `json:"accountId"`
					AsOf         time.Time                     `json:"asOf"`
					Renewals     []getters.AlertsRenewalAccess `json:"renewals"`
					Entitlements []getters.ServiceEntitlement  `json:"entitlements"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Data.AccountID != "owner" || !body.Data.AsOf.Equal(now) || body.Data.Entitlements == nil {
				t.Fatalf("invalid response: %s", w.Body.String())
			}
			if body.Data.Renewals == nil || (!tc.empty && (len(body.Data.Renewals) != 1 || body.Data.Renewals[0].RenewalID != "in_renewal")) {
				t.Fatal("missing renewal evidence")
			}
			if !tc.empty && (len(body.Data.Entitlements) != 1 || body.Data.Entitlements[0].ResourceID != "publication-123") {
				t.Fatal("lost publication access")
			}
		})
	}
}
