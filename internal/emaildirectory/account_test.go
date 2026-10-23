package emaildirectory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAccountPreferenceOwnershipAndCSRF(t *testing.T) {
	s := serviceStore(t)
	ctx := context.Background()
	var person string
	if err := s.DB.QueryRow(ctx, `INSERT INTO people(name) VALUES('Reader') RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	own, err := s.Resolve(ctx, "own@example.test", "alerts")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Resolve(ctx, "other@example.test", "alerts")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.Resolve(ctx, "pending@example.test", "alerts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO person_emails(person_id,email,is_primary,verified_at) VALUES($1,'own@example.test',true,now()),($1,'pending@example.test',false,NULL)`, person); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*Contact{own, other, pending} {
		if err = s.Change(ctx, c.ID, "alerts", "initial-preference-"+c.ID, []Change{{Newsletter: "releases", Subscribed: true}}, false); err != nil {
			t.Fatal(err)
		}
	}
	signedIn := true
	h := &Handler{Store: s, Origin: "https://btcpp.test", Account: func(*http.Request) (string, string, error) {
		if !signedIn {
			return "", "", nil
		}
		return person, "csrf-proof", nil
	}}
	routes := h.Routes()
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", h.Origin)
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		return w
	}
	w := request("GET", "/email-preferences", nil)
	if w.Code != 303 || w.Header().Get("Location") != "/dashboard/email-subscriptions" {
		t.Fatal(w.Code, w.Header())
	}
	w = request("GET", "/email-preferences?token=private-link", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "/email-preferences/open") {
		t.Fatal("private link overridden")
	}
	w = request("GET", "/dashboard/email-subscriptions", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), own.Email) || strings.Contains(w.Body.String(), other.Email) || strings.Contains(w.Body.String(), pending.Email) {
		t.Fatal("incorrect account visibility", w.Code, w.Body.String())
	}
	for _, c := range []*Contact{other, pending} {
		if w = request("GET", "/dashboard/email-subscriptions?contact="+c.ID, nil); w.Code != 404 {
			t.Fatal("foreign/unverified contact visible")
		}
	}
	form := url.Values{"contact": {own.ID}, "csrf": {"wrong"}, "service": {"alerts"}, "newsletter": {"releases"}, "version": {"1"}, "subscribed": {"false"}, "key": {"account-preference-request"}}
	if w = request("POST", "/dashboard/email-subscriptions", form); w.Code != 403 {
		t.Fatal("CSRF accepted", w.Code)
	}
	form.Set("csrf", "csrf-proof")
	form.Set("contact", other.ID)
	if w = request("POST", "/dashboard/email-subscriptions", form); w.Code != 404 {
		t.Fatal("foreign update accepted", w.Code)
	}
	form.Set("contact", own.ID)
	if w = request("POST", "/dashboard/email-subscriptions", form); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	updated, err := s.Get(ctx, own.ID, "alerts")
	if err != nil || updated.Memberships[0].Status != "unsubscribed" {
		t.Fatal("preference not saved", err)
	}
	signedIn = false
	if w = request("GET", "/email-preferences", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Email me a private link") {
		t.Fatal("public flow missing")
	}
	if w = request("POST", "/dashboard/email-subscriptions", form); w.Code != 401 {
		t.Fatal("anonymous update accepted")
	}
}
