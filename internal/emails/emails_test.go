package emails

import (
	"encoding/json"
	htmltemplate "html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	texttemplate "text/template"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
)

func TestMailDeliveryTargetRedirectsAndNamespacesDevelopmentEmail(t *testing.T) {
	ctx := &config.AppContext{
		Env:   &types.EnvConfig{Prod: false, DevEmailOverride: "Developer <developer@example.com>"},
		Infos: log.New(io.Discard, "", 0),
	}
	to, jobKey, err := mailDeliveryTarget(ctx, "speaker@example.com", "speaker-reminder")
	if err != nil {
		t.Fatal(err)
	}
	if to != "developer@example.com" {
		t.Fatalf("recipient = %q", to)
	}
	if !strings.HasPrefix(jobKey, "dev-") || !strings.HasSuffix(jobKey, "-speaker-reminder") {
		t.Fatalf("job key = %q", jobKey)
	}
}

func TestMailDeliveryTargetIgnoresOverrideInProduction(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Prod: true, DevEmailOverride: "developer@example.com"}}
	to, jobKey, err := mailDeliveryTarget(ctx, "speaker@example.com", "speaker-reminder")
	if err != nil {
		t.Fatal(err)
	}
	if to != "speaker@example.com" || jobKey != "speaker-reminder" {
		t.Fatalf("production target = (%q, %q)", to, jobKey)
	}
}

func TestMailDeliveryTargetRejectsInvalidDevelopmentOverride(t *testing.T) {
	ctx := &config.AppContext{Env: &types.EnvConfig{Prod: false, DevEmailOverride: "not-an-email"}}
	_, _, err := mailDeliveryTarget(ctx, "speaker@example.com", "job")
	if err == nil {
		t.Fatal("expected invalid DEV_EMAIL_OVERRIDE to fail")
	}
}

func TestNewsletterPreviewJobKeysAlwaysPunchThrough(t *testing.T) {
	letter := &mtypes.Letter{UID: 42, Title: "[TEST] Edited newsletter"}

	stable := newsletterMissiveJobKey("reader@example.com", letter, false)
	if again := newsletterMissiveJobKey("reader@example.com", letter, false); again != stable {
		t.Fatalf("production job key changed: %q != %q", stable, again)
	}

	first := newsletterMissiveJobKey("reader@example.com", letter, true)
	second := newsletterMissiveJobKey("reader@example.com", letter, true)
	if first == second {
		t.Fatalf("repeat preview reused idempotency key %q", first)
	}
	for _, key := range []string{first, second} {
		if !strings.HasPrefix(key, stable+"-test-") {
			t.Fatalf("preview key %q does not retain base missive identity %q", key, stable)
		}
	}
}

func TestSendWeeklyNewsletterDraftReviewIncludesDirectEditorLink(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/job" {
			t.Errorf("mailer request = %s %s", r.Method, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode mailer request: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"code":200}`))
	}))
	defer server.Close()

	ctx := &config.AppContext{
		Env: &types.EnvConfig{
			Prod:         true,
			Host:         "btcpp.dev",
			MailEndpoint: server.URL,
			MailerSecret: "test-secret",
		},
		Infos:         log.New(io.Discard, "", 0),
		EmailCache:    make(map[string]*texttemplate.Template),
		TemplateCache: htmltemplate.Must(htmltemplate.New("root").Parse(`{{ define "emails/rebrand.tmpl" }}<html><body><main>{{ .Content }}</main></body></html>{{ end }}`)),
	}
	letter := &mtypes.Letter{
		UID: 77, Title: "bitcoin++ weekly — August 11, 2026",
		OnlyFor: mtypes.OnlyForTemplated, SendAt: "2026-08-11T10:00:00-05:00",
		Markdown: "The actual rendered weekly newsletter draft.",
	}
	if err := SendWeeklyNewsletterDraftReview(ctx, letter); err != nil {
		t.Fatalf("SendWeeklyNewsletterDraftReview: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("production review requests = %d, want 1", len(requests))
	}
	payload, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatal(err)
	}
	got := string(payload)
	for _, want := range []string{"inbox@btcpp.dev", "weekly-draft-review-77", "https://btcpp.dev/admin/missives/77", "View and edit draft", "The actual rendered weekly newsletter draft."} {
		if !strings.Contains(got, want) {
			t.Errorf("review email payload missing %q: %s", want, got)
		}
	}

	ctx.Env.Prod = false
	ctx.Env.Host = "localhost"
	ctx.Env.Port = "8080"
	ctx.Env.DevEmailOverride = "developer@example.com"
	if err := SendWeeklyNewsletterDraftReviewTest(ctx, letter); err != nil {
		t.Fatalf("first test review: %v", err)
	}
	if err := SendWeeklyNewsletterDraftReviewTest(ctx, letter); err != nil {
		t.Fatalf("second test review: %v", err)
	}
	if len(requests) != 3 {
		t.Fatalf("all review requests = %d, want 3", len(requests))
	}
	firstTest, _ := json.Marshal(requests[1])
	secondTest, _ := json.Marshal(requests[2])
	for i, testPayload := range [][]byte{firstTest, secondTest} {
		body := string(testPayload)
		if !strings.Contains(body, "developer@example.com") || !strings.Contains(body, "-test-") {
			t.Errorf("test review %d was not uniquely redirected: %s", i+1, body)
		}
	}
	if string(firstTest) == string(secondTest) {
		t.Fatal("repeat test reviews reused the same mailer request")
	}
}
