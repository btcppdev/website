package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestPublishingStreamsAuthorizationAndValidation(t *testing.T) {
	person := &types.Speaker{ID: "test", Roles: []string{"berlin26-admin"}}
	s := &server{now: time.Now, authenticateToken: func(string) (*auth.BearerGrant, error) {
		return &auth.BearerGrant{PersonID: person.ID, Scopes: []string{"recordings:write"}, Kind: "personal_access_token"}, nil
	}, loadPerson: func(string) (*types.Speaker, error) { return person, nil }}
	router := mux.NewRouter()
	s.register(router.PathPrefix("/api/v1").Subrouter())
	for _, test := range []struct {
		role, body string
		want       int
	}{{"berlin26-admin", `{"streams":[]}`, 403}, {"global-admin", `{"streams":[{"id":"a","title":"Test","state":"bad"}]}`, 422}, {"global-admin", `{"streams":[{"id":"a","title":"One","state":"scheduled"},{"id":"a","title":"Two","state":"scheduled"}]}`, 422}, {"global-admin", `{"streams":[]}`, 503}} {
		person.Roles = []string{test.role}
		req := httptest.NewRequest(http.MethodPut, "/api/v1/publishing/streams", strings.NewReader(test.body))
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != test.want {
			t.Fatalf("%s: %d %s", test.role, w.Code, w.Body.String())
		}
	}
}
