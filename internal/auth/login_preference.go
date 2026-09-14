package auth

import "net/http"

const loginPreferenceCookie = "btcpp_login_method"

func validLoginPreference(method Method) bool {
	switch method {
	case MethodEmailLink, MethodPassword, MethodPasskey, MethodNostr, MethodGitHub, MethodDiscord, MethodGitLab, MethodMLH:
		return true
	}
	return false
}

// RememberLoginMethod is called only after successful authentication. The cookie
// stores a display preference, never identity, email, credentials or auth state.
func RememberLoginMethod(w http.ResponseWriter, method Method, secure bool) {
	if validLoginPreference(method) {
		http.SetCookie(w, &http.Cookie{Name: loginPreferenceCookie, Value: string(method), Path: "/", MaxAge: 180 * 86400, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	}
}

func PreferredLoginMethod(r *http.Request) string {
	cookie, err := r.Cookie(loginPreferenceCookie)
	if err == nil && validLoginPreference(Method(cookie.Value)) {
		return cookie.Value
	}
	return ""
}
