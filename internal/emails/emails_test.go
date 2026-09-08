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

func TestOrganizationApplicationEmailsIncludeReviewAndDecisionLinks(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		Env:           &types.EnvConfig{Prod: true, Host: "btcpp.dev", MailEndpoint: server.URL, MailerSecret: "test-secret"},
		Infos:         log.New(io.Discard, "", 0),
		EmailCache:    make(map[string]*texttemplate.Template),
		TemplateCache: htmltemplate.Must(htmltemplate.New("root").Parse(`{{ define "emails/tmp.tmpl" }}<html><body><main>{{ .Content }}</main></body></html>{{ end }}`)),
	}
	application := &types.OrganizationApplication{
		ID: "application-id", ApplicantEmail: "applicant@example.com",
		Name: "Nairobi BitDevs", Status: "pending",
	}
	if err := SendOrganizationApplicationAdminNotice(ctx, application, "admin@example.com", "https://btcpp.dev/admin/org-applications/application-id"); err != nil {
		t.Fatalf("SendOrganizationApplicationAdminNotice: %v", err)
	}
	application.Status = "approved"
	application.ReviewNote = "Welcome aboard."
	if err := SendOrganizationApplicationDecision(ctx, application, "https://btcpp.dev/dashboard/orgs/org-id"); err != nil {
		t.Fatalf("SendOrganizationApplicationDecision: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("organization application email requests = %d, want 2", len(requests))
	}
	adminPayload, _ := json.Marshal(requests[0])
	for _, want := range []string{"admin@example.com", "Nairobi BitDevs", "https://btcpp.dev/admin/org-applications/application-id", "Review, edit, approve, or deny"} {
		if !strings.Contains(string(adminPayload), want) {
			t.Errorf("administrator email missing %q: %s", want, adminPayload)
		}
	}
	applicantPayload, _ := json.Marshal(requests[1])
	for _, want := range []string{"applicant@example.com", "Your organization was approved", "Welcome aboard.", "https://btcpp.dev/dashboard/orgs/org-id"} {
		if !strings.Contains(string(applicantPayload), want) {
			t.Errorf("applicant email missing %q: %s", want, applicantPayload)
		}
	}
}

func TestOrganizationMembershipAndSponsorNotificationEmails(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		Env:           &types.EnvConfig{Prod: true, Host: "btcpp.dev", MailEndpoint: server.URL, MailerSecret: "test-secret"},
		Infos:         log.New(io.Discard, "", 0),
		EmailCache:    make(map[string]*texttemplate.Template),
		TemplateCache: htmltemplate.Must(htmltemplate.New("root").Parse(`{{ define "emails/tmp.tmpl" }}<html><body><main>{{ .Content }}</main></body></html>{{ end }}`)),
	}
	request := &types.OrganizationMembershipRequest{
		ID: "request-id", OrganizationID: "org-id", OrganizationName: "Nairobi BitDevs",
		PersonID: "person-id", PersonName: "Amina", PersonEmail: "amina@example.com", Message: "I organize meetups", Status: "pending",
	}
	manager := &types.NotificationRecipient{PersonID: "manager-id", Name: "Manager", Email: "manager@example.com"}
	if err := SendOrganizationMembershipRequestReceipt(ctx, request, "https://btcpp.dev/dashboard/orgs", false); err != nil {
		t.Fatal(err)
	}
	if err := SendOrganizationMembershipRequestManagerNotice(ctx, request, manager, "https://btcpp.dev/dashboard/orgs/org-id#requests", false); err != nil {
		t.Fatal(err)
	}
	request.Status = "approved"
	request.ReviewNote = "Welcome!"
	if err := SendOrganizationMembershipDecision(ctx, request, "https://btcpp.dev/dashboard/orgs"); err != nil {
		t.Fatal(err)
	}
	application := &types.OrganizationApplication{ID: "application-id", Name: "Nairobi BitDevs", ApplicantEmail: "amina@example.com"}
	if err := SendOrganizationApplicationReceipt(ctx, application, "https://btcpp.dev/dashboard/orgs"); err != nil {
		t.Fatal(err)
	}
	proposal := &types.SponsorAwardProposal{ID: "proposal-id", OrganizationID: "org-id", OrganizationName: "Spiral", Title: "Best Lightning Tool", CompetitionTitle: "Austin Hackathon", Status: "approved", ReviewNotes: "Looks great"}
	if err := SendSponsorAwardProposalAdminNotice(ctx, proposal, "admin@example.com", "https://btcpp.dev/austin/admin/hackathon/awards#sponsor-proposals"); err != nil {
		t.Fatal(err)
	}
	if err := SendSponsorAwardProposalDecision(ctx, proposal, manager, "https://btcpp.dev/dashboard/sponsor/org-id#challenges"); err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{Tag: "austin", Desc: "bitcoin++ Austin"}
	competition := &types.HackathonCompetition{ID: "competition-id", Title: "Austin Hackathon"}
	sponsorManager := &types.SponsorOrganizationRecipient{OrganizationID: "org-id", OrganizationName: "Spiral", PersonID: "manager-id", Email: "manager@example.com"}
	if err := SendSponsorResultsNotice(ctx, conf, competition, sponsorManager, "https://btcpp.dev/austin/hackathon#awards", "https://btcpp.dev/dashboard/sponsor/org-id/projects"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 7 {
		t.Fatalf("notification email requests = %d, want 7", len(requests))
	}
	payload, _ := json.Marshal(requests)
	got := string(payload)
	for _, want := range []string{
		"Membership request received", "I organize meetups", "Welcome!",
		"Organization application received", "Review sponsor prize proposal",
		"Sponsor prize proposal approved", "View public results", "View sponsor projects",
		"sponsor-award-proposal-proposal-id-approved-manager-",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("notification payloads missing %q: %s", want, got)
		}
	}
}
