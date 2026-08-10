package emails

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
)

func TestSendCancelMissiveRequestUsesMailerMissiveEndpoint(t *testing.T) {
	var method, path, missive string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		var body struct {
			Missive string `json:"missive"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode cancellation request: %v", err)
		}
		missive = body.Missive
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"code":200}`))
	}))
	defer server.Close()

	ctx := &config.AppContext{
		Env:   &types.EnvConfig{MailEndpoint: server.URL, MailerSecret: "test-secret"},
		Infos: log.New(io.Discard, "", 0),
	}
	if err := SendCancelMissiveRequest(ctx, &mtypes.Letter{UID: 42}); err != nil {
		t.Fatalf("SendCancelMissiveRequest: %v", err)
	}
	if method != http.MethodDelete || path != "/missive" || missive != "MISS-42" {
		t.Fatalf("cancellation request = %s %s missive=%q", method, path, missive)
	}
}
