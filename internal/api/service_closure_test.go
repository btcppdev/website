package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"github.com/gorilla/mux"
)

func TestServiceClosurePublicProjection(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"alerts", "insider", "none", "past", "unknown", "database error", "wrong service", "invalid time"} {
		t.Run(scenario, func(t *testing.T) {
			service := "alerts"
			if scenario == "insider" || scenario == "unknown" {
				service = scenario
			}
			at := now.Add(time.Hour)
			if scenario == "past" {
				at = now.Add(-time.Hour)
			}
			calls := 0
			s := &server{now: func() time.Time { return now }, loadServiceClosure: func(ctx context.Context, got string) (*getters.ServiceClosurePlan, error) {
				calls++
				if got != service || ctx == nil {
					t.Fatal("untrusted service")
				}
				if scenario == "none" {
					return nil, nil
				}
				if scenario == "database error" {
					return nil, errors.New("private database details")
				}
				result := &getters.ServiceClosurePlan{Service: got, ClosesAt: at, ActorID: "private-admin", Reason: "private-review-reason", RecordedAt: now}
				if scenario == "wrong service" {
					result.Service = "insider"
				}
				if scenario == "invalid time" {
					result.ClosesAt = time.Time{}
				}
				return result, nil
			}}
			router := mux.NewRouter()
			s.register(router.PathPrefix("/api/v1").Subrouter())
			req := httptest.NewRequest("GET", "/api/v1/services/"+service+"/closure?service=other&at=2000-01-01", nil)
			req.Header.Set("If-None-Match", "*")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			expected := 200
			if scenario == "unknown" {
				expected = 404
			}
			if scenario == "database error" || scenario == "wrong service" || scenario == "invalid time" {
				expected = 500
			}
			if response.Code != expected {
				t.Fatal(response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private-") || strings.Contains(response.Body.String(), "private database") {
				t.Fatal("private audit leaked")
			}
			if scenario == "unknown" {
				if calls != 0 {
					t.Fatal("unknown service queried")
				}
				return
			}
			if expected != 200 {
				return
			}
			if calls != 1 || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") || response.Header().Get("ETag") != "" {
				t.Fatal("stale status caching")
			}
			var body struct {
				Data struct {
					Service  string     `json:"service"`
					AsOf     time.Time  `json:"asOf"`
					ClosesAt *time.Time `json:"closesAt"`
				}
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Data.Service != service || !body.Data.AsOf.Equal(now) {
				t.Fatal(body)
			}
			if scenario == "none" {
				if body.Data.ClosesAt != nil {
					t.Fatal(body)
				}
			} else if body.Data.ClosesAt == nil || !body.Data.ClosesAt.Equal(at) {
				t.Fatal(body)
			}
		})
	}
}
