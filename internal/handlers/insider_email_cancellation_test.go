package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInsiderTransactionalCancellation(t *testing.T) {
	token := strings.Repeat("s", 32)
	calls := 0
	status := 200
	reply := `{"success":true,"code":200}`
	mailer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request map[string]string
		if json.NewDecoder(r.Body).Decode(&request) != nil || len(request) != 2 || request["scope"] != "job" || request["key"] != "insider-transactional:fixture-job" || r.Method != "PUT" || r.URL.Path != "/suppression" {
			t.Error("unsafe suppression request", request, r.Method, r.URL.Path)
		}
		stamp := r.Header.Get("X-Base58-Timestamp")
		sum := sha256.Sum256([]byte("mailer-secret" + stamp + "/suppressionPUT"))
		if stamp == "" || r.Header.Get("Authorization") != hex.EncodeToString(sum[:]) {
			t.Error("incorrect mailer authentication")
		}
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
	defer mailer.Close()
	app := &config.AppContext{Env: &types.EnvConfig{InsiderServiceToken: token, MailEndpoint: mailer.URL, MailerSecret: "mailer-secret"}}
	send := func(body, key, auth string) int {
		r := httptest.NewRequest("POST", "/api/internal/insider/transactional-emails/cancel", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+auth)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		CancelInsiderTransactionalEmail(w, r, app)
		return w.Code
	}
	body := `{"schema_version":1,"idempotency_key":"fixture-job"}`
	for _, test := range []struct {
		body, key, auth string
		want            int
	}{
		{body, "fixture-job", "wrong", 401}, {body, "wrong", token, 400}, {body + ` {}`, "fixture-job", token, 400}, {`{"schema_version":1,"idempotency_key":"fixture-job","scope":"subscription"}`, "fixture-job", token, 400},
	} {
		if got := send(test.body, test.key, test.auth); got != test.want {
			t.Fatal(got, test.want)
		}
	}
	if calls != 0 {
		t.Fatal("invalid request reached mailer")
	}
	for _, code := range []int{503, 202, 200, 200} {
		status = code
		want := 503
		if code == 200 {
			want = 204
		}
		if got := send(body, "fixture-job", token); got != want {
			t.Fatal(code, got, want)
		}
	}
	for _, invalid := range []string{`{"success":false,"code":200}`, `{"success":true,"code":202}`, `{"success":true,"code":200} {}`, `garbage`} {
		reply = invalid
		if got := send(body, "fixture-job", token); got != 503 {
			t.Fatal("invalid ack accepted", invalid, got)
		}
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, mailer.URL+"/suppression", 307) }))
	defer redirect.Close()
	app.Env.MailEndpoint = redirect.URL
	before := calls
	if got := send(body, "fixture-job", token); got != 503 || calls != before {
		t.Fatal("redirect followed", got, calls, before)
	}
}
