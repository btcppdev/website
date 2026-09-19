package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInsiderSubscriptionBoundary(t *testing.T) {
	app := &config.AppContext{Env: &types.EnvConfig{InsiderServiceToken: "private-test-token"}}
	for _, tc := range []struct {
		name, method, token, body string
		cancel                    bool
		want                      int
	}{
		{"auth first", "POST", "", `{}`, false, 401},
		{"wrong method", "GET", "private-test-token", `{}`, false, 405},
		{"unknown input", "POST", "private-test-token", `{"extra":true}`, false, 400},
		{"trailing JSON", "POST", "private-test-token", `{} {}`, false, 400},
		{"cross-product", "POST", "private-test-token", `{"person_id":"00000000-0000-0000-0000-000000000001","checkout_id":"00000000-0000-0000-0000-000000000002","product_code":"alerts.repository-slots"}`, true, 422},
		{"missing consent", "POST", "private-test-token", `{"person_id":"00000000-0000-0000-0000-000000000001","checkout_id":"00000000-0000-0000-0000-000000000002","product_code":"insider.membership.monthly"}`, true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/internal/insider/subscriptions/status", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+tc.token)
			out := httptest.NewRecorder()
			serveInsiderSubscription(out, req, app, tc.cancel, nil)
			if out.Code != tc.want || out.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal(out.Code, out.Body.String())
			}
		})
	}
}
