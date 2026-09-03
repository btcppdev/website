package observability

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"github.com/felixge/httpsnoop"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	namespace    string
	registry     *prometheus.Registry
	requests     *prometheus.CounterVec
	duration     *prometheus.HistogramVec
	responseSize *prometheus.HistogramVec
	inflight     *prometheus.GaugeVec
	phases       *prometheus.HistogramVec
}

type metricsContextKey struct{}

func New(namespace string, businessLoaders ...BusinessMetricsLoader) *Metrics {
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "HTTP requests completed by route, method, and status code.",
	}, []string{"route", "method", "code"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request duration by route and method.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"route", "method"})
	responseSize := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "response_size_bytes",
		Help:      "HTTP response body size by route and method.",
		Buckets:   []float64{512, 1024, 4 << 10, 16 << 10, 64 << 10, 256 << 10, 1 << 20, 4 << 20, 16 << 20},
	}, []string{"route", "method"})
	inflight := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "requests_in_flight",
		Help:      "HTTP requests currently being served by method.",
	}, []string{"method"})
	phases := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "handler",
		Name:      "phase_duration_seconds",
		Help:      "Server-side handler phase duration by route and phase.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15},
	}, []string{"route", "phase"})

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		requests,
		duration,
		responseSize,
		inflight,
		phases,
	)
	if len(businessLoaders) > 0 && businessLoaders[0] != nil {
		registry.MustRegister(newBusinessCollector(namespace, businessLoaders[0]))
	}
	return &Metrics{namespace: namespace, registry: registry, requests: requests, duration: duration, responseSize: responseSize, inflight: inflight, phases: phases}
}

func (m *Metrics) RegisterDatabasePool(loader DatabasePoolStatsLoader) {
	if m == nil || loader == nil {
		return
	}
	m.registry.MustRegister(newDatabasePoolCollector(m.namespace, loader))
}

func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		method := normalizedMethod(r.Method)
		m.inflight.WithLabelValues(method).Inc()
		defer m.inflight.WithLabelValues(method).Dec()

		r = r.WithContext(context.WithValue(r.Context(), metricsContextKey{}, m))
		captured := httpsnoop.CaptureMetrics(next, w, r)
		route := "unmatched"
		if current := mux.CurrentRoute(r); current != nil {
			if template, err := current.GetPathTemplate(); err == nil && template != "" {
				route = template
			}
		}
		m.requests.WithLabelValues(route, method, strconv.Itoa(captured.Code)).Inc()
		m.duration.WithLabelValues(route, method).Observe(captured.Duration.Seconds())
		m.responseSize.WithLabelValues(route, method).Observe(float64(captured.Written))
	})
}

// ObserveHandlerPhase records a bounded, named phase within a request. Route
// and phase must be fixed call-site values rather than user-controlled input.
func ObserveHandlerPhase(ctx context.Context, route, phase string, elapsedSeconds float64) {
	metrics, _ := ctx.Value(metricsContextKey{}).(*Metrics)
	if metrics == nil || elapsedSeconds < 0 {
		return
	}
	metrics.phases.WithLabelValues(route, phase).Observe(elapsedSeconds)
}

func (m *Metrics) Handler(token string) http.Handler {
	metrics := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			http.NotFound(w, r)
			return
		}
		provided, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		metrics.ServeHTTP(w, r)
	})
}

func normalizedMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}
