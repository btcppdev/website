package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginPreferenceCookieIsOnlyAMethod(t *testing.T) {
	for _, method := range []Method{MethodEmailLink, MethodPassword, MethodPasskey, MethodNostr, MethodGitHub, MethodDiscord, MethodGitLab, MethodMLH} {
		w := httptest.NewRecorder()
		RememberLoginMethod(w, method, true)
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Value != string(method) || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].MaxAge != 180*86400 {
			t.Fatalf("invalid preference cookie for %s", method)
		}
		r := httptest.NewRequest("GET", "/login", nil)
		r.AddCookie(cookies[0])
		if PreferredLoginMethod(r) != string(method) {
			t.Fatal("preference not read")
		}
	}
	for _, method := range []Method{"", "dev-login", "admin", "<script>", "email@example.test"} {
		w := httptest.NewRecorder()
		RememberLoginMethod(w, method, false)
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("invalid preference was written")
		}
		r := httptest.NewRequest("GET", "/login", nil)
		r.AddCookie(&http.Cookie{Name: loginPreferenceCookie, Value: string(method)})
		if PreferredLoginMethod(r) != "" {
			t.Fatal("invalid preference was accepted")
		}
	}
}
