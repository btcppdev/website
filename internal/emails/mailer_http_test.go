package emails

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestMailerRequiresSuccessfulHTTPStatus(t *testing.T) {
	for _, status := range []int{200, 400, 429, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte(`{"success":true}`))
		}))
		app := &config.AppContext{Env: &types.EnvConfig{MailEndpoint: server.URL}}
		err := sendMailerReq(app, "/job", http.MethodPut, []byte(`{}`))
		server.Close()
		if (err == nil) != (status == 200) {
			t.Fatalf("status %d: error %v", status, err)
		}
	}
}

func TestMailerPreservesOnlyMatchingHTTPRejections(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		structured bool
	}{
		{"duplicate", 400, `{"success":false,"code":400,"error":"UNIQUE constraint failed: scheduled.idem_key"}`, true},
		{"conflict", 409, `{"success":false,"code":409,"error":"UNIQUE constraint failed: scheduled.idem_key"}`, true},
		{"server failure with duplicate body", 503, `{"success":false,"code":400,"error":"UNIQUE constraint failed: scheduled.idem_key"}`, false},
		{"contradictory success", 400, `{"success":true,"code":400}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			app := &config.AppContext{Env: &types.EnvConfig{MailEndpoint: server.URL}}
			err := sendMailerReq(app, "/job", http.MethodPut, []byte(`{}`))
			var rejection *mailerRequestError
			if err == nil || errors.As(err, &rejection) != tc.structured {
				t.Fatalf("unexpected rejection: %v", err)
			}
		})
	}
}
