package emails

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestTrialEmailEndpointRemoved(t *testing.T) {
	router := mux.NewRouter()
	// No app context: retired routes must never reach database or mail code.
	RegisterEndpoints(router, nil)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodHead} {
		for _, path := range []string{"/trial-email", "/trial-email?conf=atx25&email=recipient@example.com"} {
			t.Run(method+path, func(t *testing.T) {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
				if response.Code != http.StatusNotFound {
					t.Fatalf("status = %d; want 404", response.Code)
				}
			})
		}
	}
}
