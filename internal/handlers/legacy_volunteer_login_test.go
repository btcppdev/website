package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacyVolunteerFindShiftUsesUnifiedLogin(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/volunteers/findshift", nil)

	redirectVolunteerFindShiftLogin(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if got := recorder.Header().Get("Location"); got != "/login?next=%2Fvols%2Fshift" {
		t.Fatalf("Location = %q", got)
	}
}
