package observability

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

func TestMetricsAuthentication(t *testing.T) {
	m := New("test_auth")
	h := m.Handler("secret")

	for _, test := range []struct {
		name   string
		token  string
		status int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "wrong", token: "wrong", status: http.StatusUnauthorized},
		{name: "valid", token: "secret", status: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if test.token != "" {
				req.Header.Set("Authorization", "Bearer "+test.token)
			}
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != test.status {
				t.Fatalf("status = %d, want %d", res.Code, test.status)
			}
		})
	}
}

func TestBusinessMetricsAreBoundedAndCached(t *testing.T) {
	loads := 0
	m := New("test_business", func() ([]types.BusinessMetricCount, error) {
		loads++
		return []types.BusinessMetricCount{
			{Metric: "tickets", Conference: "dev26", State: "active", Count: 42},
			{Metric: "ticket_checkins", Conference: "dev26", Count: 11},
			{Metric: "speaker_applications", Conference: "dev26", State: "pending", Count: 7},
			{Metric: "volunteer_applications", Conference: "dev26", State: "scheduled", Count: 5},
			{Metric: "recording_broadcasts", Conference: "dev26", State: "live", Count: 1},
			{Metric: "unknown", Conference: "private-id", State: "unexpected", Count: 99},
		}, nil
	})
	for i := 0; i < 2; i++ {
		families, err := m.registry.Gather()
		if err != nil {
			t.Fatal(err)
		}
		text := ""
		for _, family := range families {
			text += family.String()
		}
		text = strings.Join(strings.Fields(text), " ")
		for _, expected := range []string{
			`name:"test_business_tickets"`, `name:"conference" value:"dev26"`, `name:"state" value:"active"`,
			`name:"test_business_ticket_checkins"`, `name:"test_business_speaker_applications"`,
			`name:"test_business_volunteer_applications"`, `name:"test_business_recording_broadcasts"`,
			`name:"test_business_business_metrics_collection_success"`,
		} {
			if !strings.Contains(text, expected) {
				t.Fatalf("metrics omitted %q: %s", expected, text)
			}
		}
		if strings.Contains(text, "private-id") || strings.Contains(text, "unexpected") {
			t.Fatalf("unknown metric leaked labels: %s", text)
		}
	}
	if loads != 1 {
		t.Fatalf("business loader called %d times across immediate gathers, want 1", loads)
	}
}

func TestBusinessMetricsRetainLastSnapshotAfterRefreshFailure(t *testing.T) {
	loads := 0
	collector := newBusinessCollector("test_failure", func() ([]types.BusinessMetricCount, error) {
		loads++
		if loads > 1 {
			return nil, errors.New("database unavailable")
		}
		return []types.BusinessMetricCount{{Metric: "tickets", Conference: "dev26", State: "active", Count: 4}}, nil
	})
	now := time.Date(2026, 8, 25, 20, 0, 0, 0, time.UTC)
	counts, success, lastSuccessful := collector.snapshot(now)
	if !success || len(counts) != 1 || !lastSuccessful.Equal(now) {
		t.Fatalf("initial snapshot counts=%+v success=%t last=%s", counts, success, lastSuccessful)
	}
	counts, success, lastSuccessful = collector.snapshot(now.Add(businessRefreshInterval))
	if success || len(counts) != 1 || !lastSuccessful.Equal(now) {
		t.Fatalf("failed refresh counts=%+v success=%t last=%s", counts, success, lastSuccessful)
	}
	collector.snapshot(now.Add(businessRefreshInterval + businessFailureRetry - time.Second))
	if loads != 2 {
		t.Fatalf("loader called %d times before failure retry elapsed, want 2", loads)
	}
}

func TestDatabasePoolMetricsExposeCapacityAndContention(t *testing.T) {
	m := New("test_pool")
	m.RegisterDatabasePool(func() DatabasePoolStats {
		return DatabasePoolStats{
			AcquiredConnections:     3,
			IdleConnections:         2,
			ConstructingConnections: 1,
			TotalConnections:        6,
			MaxConnections:          10,
			AcquireCount:            12,
			EmptyAcquireCount:       4,
			CanceledAcquireCount:    1,
			AcquireDurationSeconds:  0.75,
			NewConnectionsCount:     7,
			LifetimeDestroyedCount:  2,
			IdleDestroyedCount:      3,
		}
	})
	families, err := m.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, family := range families {
		text += family.String()
	}
	text = strings.Join(strings.Fields(text), " ")
	for _, want := range []string{
		`name:"test_pool_db_pool_connections"`, `name:"state" value:"max"`, `gauge:{value:10}`,
		`name:"test_pool_db_pool_acquires_total"`, `name:"outcome" value:"canceled"`,
		`name:"test_pool_db_pool_acquire_duration_seconds_total"`, `counter:{value:0.75}`,
		`name:"test_pool_db_pool_new_connections_total"`, `name:"test_pool_db_pool_destroyed_connections_total"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("database pool metrics omitted %q: %s", want, text)
		}
	}
}

func TestDisabledMetricsAreNotFound(t *testing.T) {
	res := httptest.NewRecorder()
	New("test_disabled").Handler("").ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestMiddlewareUsesRouteTemplate(t *testing.T) {
	m := New("test_routes")
	router := mux.NewRouter()
	router.HandleFunc("/people/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	router.Use(m.Middleware)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/people/private-person-id", nil))
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusCreated)
	}

	families, err := m.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, family := range families {
		text += family.String()
	}
	text = strings.Join(strings.Fields(text), " ")
	if !strings.Contains(text, `name:"route" value:"/people/{id}"`) {
		t.Fatalf("route template missing from metrics: %s", text)
	}
	if strings.Contains(text, "private-person-id") {
		t.Fatalf("raw path leaked into metric labels: %s", text)
	}
}

func TestObserveHandlerPhaseUsesRequestMetrics(t *testing.T) {
	m := New("test_phases")
	router := mux.NewRouter()
	router.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		ObserveHandlerPhase(r.Context(), "/dashboard", "account_fetch", 0.125)
		w.WriteHeader(http.StatusOK)
	})
	router.Use(m.Middleware)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/dashboard", nil))

	families, err := m.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, family := range families {
		text += family.String()
	}
	text = strings.Join(strings.Fields(text), " ")
	for _, want := range []string{
		`name:"test_phases_handler_phase_duration_seconds"`,
		`name:"route" value:"/dashboard"`,
		`name:"phase" value:"account_fetch"`,
		`sample_count:1`,
		`sample_sum:0.125`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("phase metrics omitted %q: %s", want, text)
		}
	}
}
