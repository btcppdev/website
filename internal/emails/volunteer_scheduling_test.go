package emails

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"text/template"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
	mailer "github.com/base58btc/mailer/mail"
)

func TestVolunteerOnboardingTemplateShiftTypeTitle(t *testing.T) {
	ctx := &config.AppContext{EmailCache: make(map[string]*template.Template)}
	data := &VolShifts{Volunteer: &types.Volunteer{WorkShifts: []*types.WorkShift{
		{Name: "Morning shift", Type: &types.JobType{Title: "Registration"}},
		{Name: "Setup"},
		nil,
	}}}
	letter := &mtypes.Letter{UID: 42, Markdown: `{{range .Volunteer.WorkShifts}}[{{.TypeTitle}}]{{end}}`}
	var out bytes.Buffer
	if err := executeMissiveTemplate(ctx, letter, &out, data); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[Registration][Setup][]" {
		t.Fatalf("rendered %q", out.String())
	}
}

func TestSendMailRequestAlreadyQueued(t *testing.T) {
	for _, tc := range []struct {
		name         string
		code         int
		message, key string
		wantErr      bool
	}{
		{"existing job", 400, "UNIQUE constraint failed: scheduled.idem_key", "orientation-1", false},
		{"conflict response", 409, "UNIQUE constraint failed: scheduled.idem_key", "orientation-1", false},
		{"other constraint", 400, "UNIQUE constraint failed: subscribers.email", "orientation-1", true},
		{"validation failure", 400, "invalid recipient", "orientation-1", true},
		{"server failure", 500, "UNIQUE constraint failed: scheduled.idem_key", "orientation-1", true},
		{"no stable job key", 400, "UNIQUE constraint failed: scheduled.idem_key", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != "PUT" || r.URL.Path != "/job" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				var sent mailer.MailRequest
				if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
					t.Error(err)
				}
				if sent.JobKey != tc.key {
					t.Errorf("changed key %q", sent.JobKey)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.code)
				json.NewEncoder(w).Encode(mailer.ReturnVal{Success: false, Code: tc.code, Message: tc.message})
			}))
			defer server.Close()
			ctx := &config.AppContext{Env: &types.EnvConfig{Prod: true, MailEndpoint: server.URL, MailerSecret: "test"}, Infos: log.New(io.Discard, "", 0)}
			err := SendMailRequest(ctx, &mailer.MailRequest{ToAddr: "volunteer@example.test", JobKey: tc.key})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, want error %v", err, tc.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("lost rejection detail: %v", err)
			}
			if requests != 1 {
				t.Fatalf("made %d requests; duplicate must not resend", requests)
			}
		})
	}
}
