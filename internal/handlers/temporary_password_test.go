package handlers

import (
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"github.com/alexedwards/scs/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTemporaryPasswordAdminAuthorization(t *testing.T) {
	for _, role := range []string{"", "seoul-admin", "seoul-volcoord", "global-speaker"} {
		w := httptest.NewRecorder()
		serveAdminTemporaryPassword(w, httptest.NewRequest("POST", "/admin/people/temporary-password", nil), &config.AppContext{}, &auth.Identity{Roles: auth.ParseRoles([]string{role})})
		if w.Code != 403 {
			t.Fatalf("%s allowed: %d", role, w.Code)
		}
	}
}

func TestTemporaryPasswordRestrictedSession(t *testing.T) {
	session := scs.New()
	req := httptest.NewRequest("GET", "/login", nil)
	q, err := session.Load(req.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(q)
	app := &config.AppContext{Session: session}
	session.Put(q, auth.SessionPersonIDKey, "previous-person")
	session.Put(q, auth.SessionEmailKey, "previous@example.test")
	if err := beginTemporaryPasswordChange(app, req, "new-person", "proof", "https://evil.example"); err != nil {
		t.Fatal(err)
	}
	if session.GetString(q, auth.SessionPersonIDKey) != "" || session.GetString(q, auth.SessionEmailKey) != "" {
		t.Fatal("temporary login retained authenticated identity")
	}
	id, err := auth.Resolve(req, app)
	if err != nil || id != nil {
		t.Fatal("temporary proof grants account access", id, err)
	}
	if session.GetString(q, temporaryNextKey) != "/dashboard" {
		t.Fatal("unsafe next URL retained")
	}
	if session.GetString(q, temporaryPersonKey) != "new-person" {
		t.Fatal("missing password-change proof")
	}
	session.Put(q, temporaryUntilKey, time.Now().Add(-time.Minute).Unix())
	w := httptest.NewRecorder()
	ChangeTemporaryPassword(w, req, app)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatal("expired proof accepted", w.Code)
	}
}

func TestTemporaryPasswordCSRF(t *testing.T) {
	session := scs.New()
	req := httptest.NewRequest("POST", "/admin/people/temporary-password", strings.NewReader(url.Values{"csrf": {"wrong"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	q, err := session.Load(req.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(q)
	session.Put(q, authMethodsCSRFKey, "expected")
	w := httptest.NewRecorder()
	serveAdminTemporaryPassword(w, req, &config.AppContext{Session: session}, &auth.Identity{Roles: auth.ParseRoles([]string{"global-admin"})})
	if w.Code != 403 {
		t.Fatal("invalid csrf reached database", w.Code)
	}
	session.Put(q, temporaryPersonKey, "person")
	session.Put(q, temporaryHashKey, "proof")
	session.Put(q, temporaryUntilKey, time.Now().Add(time.Minute).Unix())
	w = httptest.NewRecorder()
	ChangeTemporaryPassword(w, req, &config.AppContext{Session: session})
	if w.Code != 403 {
		t.Fatal("invalid change csrf reached database", w.Code)
	}
}

func TestTemporaryPasswordSessionPersistence(t *testing.T) {
	session := scs.New()
	app := &config.AppContext{Session: session}
	login := session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := beginTemporaryPasswordChange(app, r, "person", "proof", "/dashboard/sponsor"); err != nil {
			t.Fatal(err)
		}
		http.Redirect(w, r, "/change-temporary-password", http.StatusSeeOther)
	}))
	w := httptest.NewRecorder()
	login.ServeHTTP(w, httptest.NewRequest("POST", "/login/password", nil))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("persist session: status %d: %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("missing session cookie")
	}
	next := httptest.NewRequest("GET", "/change-temporary-password", nil)
	for _, cookie := range cookies {
		next.AddCookie(cookie)
	}
	session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if session.GetString(r.Context(), temporaryPersonKey) != "person" || session.GetString(r.Context(), temporaryHashKey) != "proof" {
			t.Fatal("password proof did not survive session storage")
		}
		if session.GetInt64(r.Context(), temporaryUntilKey) <= time.Now().Unix() {
			t.Fatal("expiry did not survive session storage")
		}
		if session.GetString(r.Context(), temporaryNextKey) != "/dashboard/sponsor" {
			t.Fatal("return path lost")
		}
		if id, err := auth.Resolve(r, app); err != nil || id != nil {
			t.Fatalf("restricted session gained identity: %v %v", id, err)
		}
	})).ServeHTTP(httptest.NewRecorder(), next)
}
