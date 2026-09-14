package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
)

func TestServiceOpenNodeCallback(t *testing.T) {
	key, id := "local-callback-key", uuid.NewString()
	for _, scenario := range []string{"valid", "unpaid", "invalid-signature", "duplicate-id", "query-only", "oversized", "wrong-method", "wrong-content-type", "unconfigured", "provider-failure", "needs-review"} {
		t.Run(scenario, func(t *testing.T) {
			app := &config.AppContext{Env: &types.EnvConfig{OpenNode: types.OpenNodeConfig{Key: key}}}
			form := url.Values{"id": {id}, "hashed_order": {computeHash(key, id)}, "status": {"paid"}, "price": {"0"}, "order_id": {"forged-purchase"}}
			method, contentType, target := http.MethodPost, "application/x-www-form-urlencoded", "/callback/services/opennode"
			want, callsWant := http.StatusNoContent, 1
			var resultErr error
			switch scenario {
			case "invalid-signature":
				form.Set("hashed_order", "invalid")
				want, callsWant = 401, 0
			case "duplicate-id":
				form.Add("id", id)
				want, callsWant = 401, 0
			case "query-only":
				target += "?" + form.Encode()
				form = url.Values{}
				want, callsWant = 401, 0
			case "oversized":
				form.Set("padding", strings.Repeat("x", 17<<10))
				want, callsWant = 400, 0
			case "wrong-method":
				method = http.MethodGet
				want, callsWant = 405, 0
			case "wrong-content-type":
				contentType = "application/json"
				want, callsWant = 415, 0
			case "unconfigured":
				app.Env.OpenNode.Key = ""
				want, callsWant = 503, 0
			case "provider-failure":
				resultErr = errors.New("timeout")
				want = 503
			case "needs-review":
				resultErr = getters.ErrServiceFulfillment
				want = 202
			}
			req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", contentType)
			w := httptest.NewRecorder()
			calls := 0
			serviceOpenNodeCallback(w, req, app, func(_ context.Context, gotApp *config.AppContext, gotID string) (bool, error) {
				calls++
				if gotID != id || gotApp != app {
					t.Fatal("incorrect verification request")
				}
				return scenario != "unpaid", resultErr
			})
			if w.Code != want || calls != callsWant {
				t.Fatalf("status=%d calls=%d, want %d/%d", w.Code, calls, want, callsWant)
			}
		})
	}
}
