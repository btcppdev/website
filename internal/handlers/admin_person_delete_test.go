package handlers

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"github.com/alexedwards/scs/v2"
)

func TestAccountDeletionAuthorizationAndCSRF(t *testing.T) {
	for _, role := range []string{"", "nairobi-admin", "global-speaker"} {
		for _, method := range []string{"GET", "POST"} {
			w := httptest.NewRecorder()
			serveAdminPersonDelete(w, httptest.NewRequest(method, "/admin/people/delete", nil), &config.AppContext{}, &auth.Identity{Roles: auth.ParseRoles([]string{role})})
			if w.Code != 403 {
				t.Fatalf("%s %s allowed: %d", role, method, w.Code)
			}
		}
	}
	session := scs.New()
	req := httptest.NewRequest(http.MethodPost, "/admin/people/delete", strings.NewReader(url.Values{"csrf": {"wrong"}, "confirmation": {"DELETE"}, "confirm_delete": {"yes"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx, err := session.Load(req.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(ctx)
	session.Put(ctx, authMethodsCSRFKey, "expected-token")
	w := httptest.NewRecorder()
	serveAdminPersonDelete(w, req, &config.AppContext{Session: session}, &auth.Identity{Roles: auth.ParseRoles([]string{"global-admin"})})
	if w.Code != 403 {
		t.Fatalf("bad CSRF reached database: %d", w.Code)
	}
}

func TestAccountDeletionTemplate(t *testing.T) {
	raw, err := os.ReadFile("../../templates/admin/person_delete.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tm, err := template.New("delete").Parse(`{{define "admin_favicon"}}{{end}}` + string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = tm.Execute(&out, &AdminPersonDeletePage{CSRF: "test-token", Preview: &getters.PersonDeletionPreview{ID: "id", Name: "<script>alert(1)</script>", Talks: 3, Projects: 2}})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"test-token", "3 talk proposals", "2 hackathon projects", "Permanently delete account", "cannot be undone"} {
		if !strings.Contains(out.String(), s) {
			t.Fatalf("missing %s", s)
		}
	}
	if strings.Contains(out.String(), "<script>alert(1)</script>") {
		t.Fatal("profile name not escaped")
	}
}
