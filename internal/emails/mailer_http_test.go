package emails

import (
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
