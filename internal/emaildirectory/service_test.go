package emaildirectory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func serviceStore(t *testing.T) Store {
	t.Helper()
	p := directoryTestPool(t)
	if err := applyDirectoryMigration(t, p); err != nil {
		t.Fatal(err)
	}
	_, err := p.Exec(context.Background(), `CREATE TABLE people(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),name text,norm_photo_path text,is_deleted_account boolean NOT NULL DEFAULT false); CREATE TABLE person_email_conflicts(email citext); ALTER TABLE person_emails ADD COLUMN is_primary boolean NOT NULL DEFAULT false; ALTER TABLE person_emails ADD COLUMN verified_at timestamptz;`)
	if err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile("../../db/migrations/130_email_preference_service.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(context.Background(), string(sql)); err != nil {
		t.Fatal(err)
	}
	return Store{DB: p}
}
func TestSharedPreferenceCASAndReplay(t *testing.T) {
	s := serviceStore(t)
	ctx := context.Background()
	c, err := s.Resolve(ctx, "Person+tag@example.test", "insider")
	if err != nil {
		t.Fatal(err)
	}
	change := []Change{{Newsletter: "insider.deep-dives", Subscribed: true}}
	if err = s.Change(ctx, c.ID, "insider", "initial-request-0001", change, false); err != nil {
		t.Fatal(err)
	}
	if err = s.Change(ctx, c.ID, "insider", "optout-request-0001", []Change{{Newsletter: "insider.deep-dives", ExpectedVersion: 1}}, false); err != nil {
		t.Fatal(err)
	}
	if err = s.Change(ctx, c.ID, "insider", "initial-request-0001", change, false); err != nil {
		t.Fatal(err)
	}
	if err = s.Change(ctx, c.ID, "insider", "stale-request-0001", change, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write: %v", err)
	}
	if err = s.Change(ctx, c.ID, "insider", "import-request-0001", change, true); err != nil {
		t.Fatal(err)
	}
	c, err = s.Get(ctx, c.ID, "insider")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Memberships) != 1 || c.Memberships[0].Status != "unsubscribed" || c.Memberships[0].Version != 2 {
		t.Fatalf("replay restored consent: %+v", c)
	}
	if err = s.Change(ctx, c.ID, "alerts", "cross-service-0001", change, false); !errors.Is(err, ErrInput) {
		t.Fatalf("cross-service scope accepted: %v", err)
	}
	var events int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM email_preference_events WHERE contact_id=$1`, c.ID).Scan(&events); err != nil || events != 2 {
		t.Fatalf("events %d: %v", events, err)
	}
}
func TestPasswordlessPreferenceFlow(t *testing.T) {
	s := serviceStore(t)
	ctx := context.Background()
	c, err := s.Resolve(ctx, "reader@example.test", "insider")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Change(ctx, c.ID, "insider", "import-request-0002", []Change{{Newsletter: "insider.deep-dives", Subscribed: true}}, true); err != nil {
		t.Fatal(err)
	}
	var link string
	sent := make(chan string, 1)
	h := &Handler{Store: s, Origin: "https://btcpp.example", Tokens: map[string]string{"insider": strings.Repeat("i", 32), "alerts": strings.Repeat("a", 32)}, SendLink: func(_ context.Context, email, value string) error { sent <- value; return nil }}
	handler := h.Routes()
	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	// Knowing a contact UUID must not make its address public. Neither an
	// arbitrary cookie nor the service name supplies authentication.
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/internal/email-directory/insider/contacts/" + c.ID},
		{"GET", "/api/internal/email-directory/alerts/contacts/" + c.ID},
		{"POST", "/api/internal/email-directory/insider/resolve"},
		{"POST", "/api/internal/email-directory/insider/verified-account"},
		{"PUT", "/api/internal/email-directory/insider/contacts/" + c.ID},
		{"GET", "/email-preferences/manage?contact_id=" + c.ID},
	} {
		for _, cookie := range []*http.Cookie{nil, {Name: "email_preferences", Value: c.ID}} {
			response := request(endpoint.method, endpoint.path, "", cookie)
			if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), c.Email) {
				t.Fatalf("unauthenticated contact exposure at %s: status %d", endpoint.path, response.Code)
			}
			if response.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("contact response may be cached")
			}
		}
	}
	known := request("POST", "/email-preferences", "email=reader%40example.test", nil)
	unknown := request("POST", "/email-preferences", "email=nobody%40example.test", nil)
	select {
	case link = <-sent:
	case <-time.After(time.Second):
		t.Fatal("management email not queued")
	}
	if known.Code != 200 || known.Body.String() != unknown.Body.String() || link == "" {
		t.Fatal("enumerating response or missing link")
	}
	u, _ := url.Parse(link)
	raw := u.Query().Get("token")
	// GET previews cannot consume the token or change subscriptions.
	preview := request("GET", u.RequestURI(), "", nil)
	if preview.Code != 200 || strings.Contains(preview.Body.String(), "reader@example.test") {
		t.Fatal("GET revealed preferences")
	}
	opened := request("POST", "/email-preferences/open", url.Values{"token": {raw}}.Encode(), nil)
	if opened.Code != 303 {
		t.Fatalf("open %d %s", opened.Code, opened.Body.String())
	}
	cookies := opened.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatal("unsafe session cookie")
	}
	if replay := request("POST", "/email-preferences/open", url.Values{"token": {raw}}.Encode(), nil); replay.Code != 410 {
		t.Fatal("link reused")
	}
	cookie := cookies[0]
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)
	_, csrf, err := h.session(r)
	if err != nil {
		t.Fatal(err)
	}
	page := request("GET", "/email-preferences/manage", "", cookie)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "insider.deep-dives") {
		t.Fatal("missing scoped preferences")
	}
	form := url.Values{"csrf": {csrf}, "key": {"browser-optout-0001"}, "service": {"insider"}, "newsletter": {"insider.deep-dives"}, "version": {"1"}, "subscribed": {"false"}}
	if result := request("POST", "/email-preferences/manage", form.Encode(), nil); result.Code != 401 {
		t.Fatal("unauthenticated mutation")
	}
	form.Set("csrf", "bad")
	if result := request("POST", "/email-preferences/manage", form.Encode(), cookie); result.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	form.Set("csrf", csrf)
	if result := request("POST", "/email-preferences/manage", form.Encode(), cookie); result.Code != 303 {
		t.Fatalf("save %d %s", result.Code, result.Body.String())
	}
	contact, err := s.Get(ctx, c.ID, "insider")
	if err != nil || contact.Memberships[0].Status != "unsubscribed" {
		t.Fatal("unsubscribe not recorded")
	}
	// A service credential cannot read another service's endpoint.
	req := httptest.NewRequest("GET", "/api/internal/email-directory/alerts/contacts/"+c.ID, nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("i", 32))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("cross-service token accepted")
	}
	req = httptest.NewRequest("GET", "/api/internal/email-directory/alerts/contacts/"+c.ID, nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var out Contact
	if err = json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Memberships) != 0 {
		t.Fatal("cross-service preferences leaked")
	}
}

func TestVerifiedSignupCreatesOrLinksOneAccount(t *testing.T) {
	s := serviceStore(t)
	ctx := context.Background()
	a, err := s.EnsureVerifiedAccount(ctx, "new@example.test", "insider", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Created || a.PersonID == "" {
		t.Fatal("signup did not create account")
	}
	again, err := s.EnsureVerifiedAccount(ctx, "NEW@example.test", "insider", strings.Repeat("b", 64))
	if err != nil || again.PersonID != a.PersonID || again.Created {
		t.Fatalf("duplicate account: %+v %v", again, err)
	}
	if _, err = s.EnsureVerifiedAccount(ctx, "other@example.test", "insider", strings.Repeat("a", 64)); !errors.Is(err, ErrConflict) {
		t.Fatalf("proof rebound: %v", err)
	}
	if _, err = s.EnsureVerifiedAccount(ctx, "new@example.test", "alerts", strings.Repeat("c", 64)); !errors.Is(err, ErrInput) {
		t.Fatal("unexpected service can provision accounts")
	}
	c, err := s.Get(ctx, a.ContactID, "")
	if err != nil || len(c.Memberships) != 0 {
		t.Fatal("account creation implied newsletter consent")
	}
	var photo string
	if err = s.DB.QueryRow(ctx, `SELECT norm_photo_path FROM people WHERE id=$1`, a.PersonID).Scan(&photo); err != nil || photo != "default.avif" {
		t.Fatal("default avatar missing")
	}
	if _, err = s.DB.Exec(ctx, `DELETE FROM people WHERE id=$1`, a.PersonID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.EnsureVerifiedAccount(ctx, "new@example.test", "insider", strings.Repeat("a", 64)); !errors.Is(err, ErrConflict) {
		t.Fatal("old proof restored a deleted account")
	}

}
