package emaildirectory

import (
	"bytes"
	"strings"
	"testing"
)

func TestPreferencesFormsPreservePrivateManagementFlow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   page
		fields []string
	}{
		{"request", page{}, []string{`action="/email-preferences"`, `type="email"`}},
		{"open", page{Token: "private-token"}, []string{`action="/email-preferences/open"`, `name="token" value="private-token"`}},
		{"manage", page{CSRF: "csrf-proof", Key: "request-key", Contact: &Contact{Email: "reader@example.test", Memberships: []Membership{{Service: "alerts", Newsletter: "releases", Status: "subscribed", Version: 7, DeliveryMode: "immediate"}}}}, []string{`action="/email-preferences/manage"`, `name="csrf" value="csrf-proof"`, `name="version" value="7"`, `name="service" value="alerts"`, `name="newsletter" value="releases"`, `name="subscribed" value="false"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := preferencesPage.Execute(&out, tc.data); err != nil {
				t.Fatal(err)
			}
			for _, field := range tc.fields {
				if !strings.Contains(out.String(), field) {
					t.Fatalf("missing %s", field)
				}
			}
		})
	}
	var out bytes.Buffer
	if err := preferencesPage.Execute(&out, page{Notice: "<script>alert(1)</script>"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "<script>") {
		t.Fatal("notice rendered as executable markup")
	}
}
