package handlers

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/ics"
	"btcpp-web/internal/types"
)

func TestNewVolunteerReceivesAlreadyStampedShift(t *testing.T) {
	var requests []string
	mailer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/job" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, string(body))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"success":true,"code":200}`)
	}))
	defer mailer.Close()
	ctx := &config.AppContext{
		Env:   &types.EnvConfig{Prod: true, Host: "btcpp.dev", MailEndpoint: mailer.URL, MailerSecret: "test"},
		Infos: log.New(io.Discard, "", 0), Err: log.New(io.Discard, "", 0),
		TemplateCache: template.Must(template.New("root").Parse(`{{define "emails/rebrand.tmpl"}}<html>{{.Content}}</html>{{end}}`)),
	}
	start := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	conf := &types.Conf{Ref: "conf", Tag: "test", Desc: "Test", Timezone: "UTC"}
	shift := &types.WorkShift{Ref: "shift", Name: "Registration", ShiftTime: &types.Times{Start: start, End: &end}}
	shift.CalNotif = ics.CalNotif{UID: ics.NewUID("shift", shift.Ref), Sequence: 2, HashHex: ics.ContentHash(start, end, conf.Tag, shift.Name)}.String()
	// The old hash-gated path must reproduce the missed invitation without any send.
	if err := DispatchShiftICS(ctx, shift, conf, []ics.Attendee{{Email: "second@example.com"}}, kindRequest, false); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 {
		t.Fatal("expected unchanged shared stamp to suppress old path")
	}
	err := dispatchVolunteerShiftInvite(ctx, shift, conf, ics.Attendee{Email: "second@example.com", Name: "Second Volunteer"})
	if len(requests) != 1 || !strings.Contains(requests[0], "second@example.com") {
		t.Fatalf("new recipient did not receive calendar attachment: %v", requests)
	}
	var payload struct {
		Attachments []string `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(requests[0]), &payload); err != nil || len(payload.Attachments) != 1 || payload.Attachments[0] == "" {
		t.Fatalf("missing encoded calendar attachment: %v", err)
	}
	// No database is configured in this test: delivery succeeds, then stamp
	// persistence fails. The caller must be told about that partial failure.
	if err == nil || !strings.Contains(err.Error(), "calendar state save failed") {
		t.Fatalf("missing calendar persistence error: %v", err)
	}
	conf.OrientCalNotif = ics.CalNotif{UID: ics.NewUID("orient", conf.Tag), Sequence: 3, HashHex: ics.ContentHash(start, end, conf.Tag, "Volunteer Orientation: "+conf.Desc)}.String()
	err = dispatchOrientICS(ctx, conf, ics.Attendee{Email: "second@example.com"}, start, end, "https://example.com/orientation", kindCancel)
	if len(requests) != 2 || !strings.Contains(requests[1], "CANCEL") || !strings.Contains(requests[1], "second@example.com") {
		t.Fatalf("orientation cancellation was not queued: %v", requests)
	}
	if err == nil {
		t.Fatal("orientation stamp failure was hidden")
	}

}
