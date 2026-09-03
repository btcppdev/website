package observability

import "github.com/prometheus/client_golang/prometheus"

type DatabasePoolStats struct {
	AcquiredConnections     int32
	IdleConnections         int32
	ConstructingConnections int32
	TotalConnections        int32
	MaxConnections          int32
	AcquireCount            int64
	EmptyAcquireCount       int64
	CanceledAcquireCount    int64
	AcquireDurationSeconds  float64
	NewConnectionsCount     int64
	LifetimeDestroyedCount  int64
	IdleDestroyedCount      int64
}

type DatabasePoolStatsLoader func() DatabasePoolStats

type databasePoolCollector struct {
	loader          DatabasePoolStatsLoader
	connections     *prometheus.Desc
	acquires        *prometheus.Desc
	acquireDuration *prometheus.Desc
	newConnections  *prometheus.Desc
	destroyed       *prometheus.Desc
}

func newDatabasePoolCollector(namespace string, loader DatabasePoolStatsLoader) *databasePoolCollector {
	return &databasePoolCollector{
		loader:          loader,
		connections:     prometheus.NewDesc(namespace+"_db_pool_connections", "Current PostgreSQL pool connections by state.", []string{"state"}, nil),
		acquires:        prometheus.NewDesc(namespace+"_db_pool_acquires_total", "Cumulative PostgreSQL pool acquisition events by outcome.", []string{"outcome"}, nil),
		acquireDuration: prometheus.NewDesc(namespace+"_db_pool_acquire_duration_seconds_total", "Cumulative time spent acquiring PostgreSQL connections.", nil, nil),
		newConnections:  prometheus.NewDesc(namespace+"_db_pool_new_connections_total", "Cumulative PostgreSQL connections created by the pool.", nil, nil),
		destroyed:       prometheus.NewDesc(namespace+"_db_pool_destroyed_connections_total", "Cumulative PostgreSQL connections destroyed by reason.", []string{"reason"}, nil),
	}
}

func (c *databasePoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.connections
	ch <- c.acquires
	ch <- c.acquireDuration
	ch <- c.newConnections
	ch <- c.destroyed
}

func (c *databasePoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.loader()
	for state, value := range map[string]int32{
		"acquired":     stats.AcquiredConnections,
		"idle":         stats.IdleConnections,
		"constructing": stats.ConstructingConnections,
		"total":        stats.TotalConnections,
		"max":          stats.MaxConnections,
	} {
		ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(value), state)
	}
	for outcome, value := range map[string]int64{
		"successful": stats.AcquireCount,
		"empty":      stats.EmptyAcquireCount,
		"canceled":   stats.CanceledAcquireCount,
	} {
		ch <- prometheus.MustNewConstMetric(c.acquires, prometheus.CounterValue, float64(value), outcome)
	}
	ch <- prometheus.MustNewConstMetric(c.acquireDuration, prometheus.CounterValue, stats.AcquireDurationSeconds)
	ch <- prometheus.MustNewConstMetric(c.newConnections, prometheus.CounterValue, float64(stats.NewConnectionsCount))
	ch <- prometheus.MustNewConstMetric(c.destroyed, prometheus.CounterValue, float64(stats.LifetimeDestroyedCount), "max_lifetime")
	ch <- prometheus.MustNewConstMetric(c.destroyed, prometheus.CounterValue, float64(stats.IdleDestroyedCount), "max_idle")
}
