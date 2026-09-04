package observability

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type dbPoolCollector struct {
	pool             *pgxpool.Pool
	connections      *prometheus.Desc
	acquires         *prometheus.Desc
	emptyAcquires    *prometheus.Desc
	canceledAcquires *prometheus.Desc
	acquireDuration  *prometheus.Desc
	emptyAcquireWait *prometheus.Desc
	newConnections   *prometheus.Desc
}

func newDBPoolCollector(namespace string, pool *pgxpool.Pool) *dbPoolCollector {
	return &dbPoolCollector{
		pool: pool,
		connections: prometheus.NewDesc(
			namespace+"_db_pool_connections",
			"PostgreSQL pool connections by state.",
			[]string{"state"}, nil,
		),
		acquires: prometheus.NewDesc(
			namespace+"_db_pool_acquires_total",
			"Successful PostgreSQL pool acquisitions.", nil, nil,
		),
		emptyAcquires: prometheus.NewDesc(
			namespace+"_db_pool_empty_acquires_total",
			"Successful acquisitions that had to wait for a connection.", nil, nil,
		),
		canceledAcquires: prometheus.NewDesc(
			namespace+"_db_pool_canceled_acquires_total",
			"PostgreSQL pool acquisitions canceled while waiting.", nil, nil,
		),
		acquireDuration: prometheus.NewDesc(
			namespace+"_db_pool_acquire_duration_seconds_total",
			"Cumulative time spent acquiring PostgreSQL connections.", nil, nil,
		),
		emptyAcquireWait: prometheus.NewDesc(
			namespace+"_db_pool_empty_acquire_wait_seconds_total",
			"Cumulative time successful acquisitions waited for an available connection.", nil, nil,
		),
		newConnections: prometheus.NewDesc(
			namespace+"_db_pool_new_connections_total",
			"PostgreSQL connections created by the pool.", nil, nil,
		),
	}
}

func (c *dbPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.connections
	ch <- c.acquires
	ch <- c.emptyAcquires
	ch <- c.canceledAcquires
	ch <- c.acquireDuration
	ch <- c.emptyAcquireWait
	ch <- c.newConnections
}

func (c *dbPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.pool.Stat()
	for state, value := range map[string]int32{
		"acquired":     stats.AcquiredConns(),
		"idle":         stats.IdleConns(),
		"constructing": stats.ConstructingConns(),
		"total":        stats.TotalConns(),
		"max":          stats.MaxConns(),
	} {
		ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(value), state)
	}
	ch <- prometheus.MustNewConstMetric(c.acquires, prometheus.CounterValue, float64(stats.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.emptyAcquires, prometheus.CounterValue, float64(stats.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.canceledAcquires, prometheus.CounterValue, float64(stats.CanceledAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.acquireDuration, prometheus.CounterValue, stats.AcquireDuration().Seconds())
	ch <- prometheus.MustNewConstMetric(c.emptyAcquireWait, prometheus.CounterValue, stats.EmptyAcquireWaitTime().Seconds())
	ch <- prometheus.MustNewConstMetric(c.newConnections, prometheus.CounterValue, float64(stats.NewConnsCount()))
}
