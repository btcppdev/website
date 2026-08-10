package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"btcpp-web/external/getters"
)

func TestSelfServicePersonMergeDecisionsOnlyReviewProfileFields(t *testing.T) {
	preview := &getters.PersonMergePreview{}
	for _, spec := range getters.PersonMergeFieldSpecs {
		canonical := any("main-" + spec.Key)
		source := any("second-" + spec.Key)
		if spec.Kind == "bool" {
			canonical = false
			source = true
		}
		if strings.HasPrefix(spec.Key, "tax_form_") {
			canonical = ""
		}
		preview.Fields = append(preview.Fields, getters.PersonMergeField{
			Spec: spec, Canonical: canonical, Source: source,
		})
	}

	form := url.Values{}
	for key := range selfServicePersonMergeFieldKeys {
		form.Set("choice_"+key, "canonical")
	}
	form.Set("choice_phone", "source")
	req := httptest.NewRequest("POST", "/account/merge/confirm", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	decisions, err := parseSelfServicePersonMergeDecisions(req, preview)
	if err != nil {
		t.Fatalf("parse decisions: %v", err)
	}
	if got := decisions["phone"]; got.Choice != "source" || got.Value != "second-phone" {
		t.Fatalf("phone decision = %+v", got)
	}
	if got := decisions["name"]; got.Choice != "canonical" || got.Value != "main-name" {
		t.Fatalf("name decision = %+v", got)
	}
	for _, key := range []string{"tax_form_type", "tax_form_object", "tax_form_name", "tax_form_uploaded"} {
		if got := decisions[key]; got.Choice != "source" || got.Value != "second-"+key {
			t.Fatalf("%s decision = %+v", key, got)
		}
	}
}

func TestSelfServicePersonMergeFieldsHideTaxDocumentMetadata(t *testing.T) {
	preview := &getters.PersonMergePreview{}
	for _, spec := range getters.PersonMergeFieldSpecs {
		preview.Fields = append(preview.Fields, getters.PersonMergeField{Spec: spec, Source: "second-" + spec.Key})
	}
	fields := selfServicePersonMergeFields(preview)
	if len(fields) == 0 {
		t.Fatal("expected profile fields")
	}
	for _, field := range fields {
		if strings.HasPrefix(field.Key, "tax_form_") {
			t.Fatalf("tax document field %q exposed in self-service review", field.Key)
		}
	}
}

func TestPersonEmailAdditionVerificationIsNotLoginCopy(t *testing.T) {
	markdown := personEmailAdditionVerificationMarkdown("primary@example.com", "second@example.com", "https://btcpp.dev/dashboard/emails/verify?token=test")
	for _, want := range []string{
		"# Add This Email to Your bitcoin++ Account",
		"primary@example.com",
		"second@example.com",
		"[Add This Email](button#https://btcpp.dev/dashboard/emails/verify?token=test)",
		"do not click the link",
		"ignore or delete this message",
		"expires in 30 minutes",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("verification email missing %q", want)
		}
	}
	if strings.Contains(strings.ToLower(markdown), "log in") || strings.Contains(strings.ToLower(markdown), "sign in") {
		t.Fatal("email addition verification should not use login copy")
	}
}
