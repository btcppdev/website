package observability

import (
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
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inflight *prometheus.GaugeVec
}

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
	inflight := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "requests_in_flight",
		Help:      "HTTP requests currently being served by method.",
	}, []string{"method"})

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		requests,
		duration,
		inflight,
	)
	if len(businessLoaders) > 0 && businessLoaders[0] != nil {
		registry.MustRegister(newBusinessCollector(namespace, businessLoaders[0]))
	}
	return &Metrics{registry: registry, requests: requests, duration: duration, inflight: inflight}
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

		captured := httpsnoop.CaptureMetrics(next, w, r)
		route := "unmatched"
		if current := mux.CurrentRoute(r); current != nil {
			if template, err := current.GetPathTemplate(); err == nil && template != "" {
				route = template
			}
		}
		m.requests.WithLabelValues(route, method, strconv.Itoa(captured.Code)).Inc()
		m.duration.WithLabelValues(route, method).Observe(captured.Duration.Seconds())
	})
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
