package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestConferenceBroadcastAuthorizationAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name, role, scope, body string
		status                  int
	}{
		{"global admin", "global-admin", "recordings:write", `{"state":"live","hls_url":"https://stream.example/live.m3u8","title":"Day 3"}`, 200},
		{"conference admin", "toronto-admin", "recordings:write", `{"state":"ended"}`, 200},
		{"other conference", "vienna-admin", "recordings:write", `{"state":"ended"}`, 403},
		{"missing scope", "global-admin", "identity:self:read", `{"state":"ended"}`, 403},
		{"missing HLS", "global-admin", "recordings:write", `{"state":"live"}`, 422},
		{"unsafe URL", "global-admin", "recordings:write", `{"state":"live","hls_url":"javascript:alert(1)"}`, 422},
		{"bad state", "global-admin", "recordings:write", `{"state":"oops"}`, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			person := &types.Speaker{ID: "person-1", Roles: []string{tc.role}}
			called := false
			s := &server{
				source: &fakeSource{conferences: []*types.Conf{{Ref: "conference-1", Tag: "toronto"}}}, now: time.Now,
				authenticateToken: func(string) (*auth.BearerGrant, error) {
					return &auth.BearerGrant{PersonID: person.ID, Scopes: []string{tc.scope}, Kind: "personal_access_token"}, nil
				},
				loadPerson:              func(string) (*types.Speaker, error) { return person, nil },
				loadConferenceBroadcast: func(string) (*types.ConferenceBroadcast, error) { return nil, nil },
				upsertConferenceBroadcast: func(id string, update getters.ConferenceBroadcastUpdate) (*types.ConferenceBroadcast, error) {
					called = true
					if id != "conference-1" {
						t.Errorf("conference=%s", id)
					}
					return &types.ConferenceBroadcast{ConferenceID: id, Title: update.Title, RecordingBroadcast: types.RecordingBroadcast{State: update.State, HLSURL: update.HLSURL, HeartbeatAt: &update.Now}}, nil
				},
			}
			router := mux.NewRouter()
			s.register(router.PathPrefix("/api/v1").Subrouter())
			r := httptest.NewRequest(http.MethodPut, "/api/v1/conferences/toronto/broadcast", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer test")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if called != (tc.status == 200) {
				t.Fatalf("mutation called=%v", called)
			}
		})
	}
}
