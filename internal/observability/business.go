package observability

import (
	"sync"
	"time"

	"btcpp-web/internal/types"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	businessRefreshInterval = 2 * time.Minute
	businessFailureRetry    = 30 * time.Second
)

type BusinessMetricsLoader func() ([]types.BusinessMetricCount, error)

type businessCollector struct {
	mu             sync.Mutex
	loader         BusinessMetricsLoader
	nextRefresh    time.Time
	counts         []types.BusinessMetricCount
	lastSuccessful time.Time
	lastSuccess    bool

	tickets               *prometheus.Desc
	ticketCheckins        *prometheus.Desc
	speakerApplications   *prometheus.Desc
	volunteerApplications *prometheus.Desc
	recordingBroadcasts   *prometheus.Desc
	collectionSuccess     *prometheus.Desc
	lastSuccessfulRefresh *prometheus.Desc
}

func newBusinessCollector(namespace string, loader BusinessMetricsLoader) *businessCollector {
	return &businessCollector{
		loader:                loader,
		tickets:               prometheus.NewDesc(namespace+"_tickets", "Current ticket registrations by conference and state.", []string{"conference", "state"}, nil),
		ticketCheckins:        prometheus.NewDesc(namespace+"_ticket_checkins", "Current non-revoked ticket check-ins by conference.", []string{"conference"}, nil),
		speakerApplications:   prometheus.NewDesc(namespace+"_speaker_applications", "Current speaker applications by conference and review state.", []string{"conference", "state"}, nil),
		volunteerApplications: prometheus.NewDesc(namespace+"_volunteer_applications", "Current volunteer applications by conference and workflow state.", []string{"conference", "state"}, nil),
		recordingBroadcasts:   prometheus.NewDesc(namespace+"_recording_broadcasts", "Current recording broadcasts by conference and state.", []string{"conference", "state"}, nil),
		collectionSuccess:     prometheus.NewDesc(namespace+"_business_metrics_collection_success", "Whether the latest business metric refresh succeeded.", nil, nil),
		lastSuccessfulRefresh: prometheus.NewDesc(namespace+"_business_metrics_last_success_timestamp_seconds", "Unix timestamp of the latest successful business metric refresh.", nil, nil),
	}
}

func (c *businessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.tickets
	ch <- c.ticketCheckins
	ch <- c.speakerApplications
	ch <- c.volunteerApplications
	ch <- c.recordingBroadcasts
	ch <- c.collectionSuccess
	ch <- c.lastSuccessfulRefresh
}

func (c *businessCollector) Collect(ch chan<- prometheus.Metric) {
	counts, success, lastSuccessful := c.snapshot(time.Now())
	for _, count := range counts {
		desc := c.metricDesc(count.Metric)
		if desc == nil {
			continue
		}
		labels := []string{count.Conference, count.State}
		if count.Metric == "ticket_checkins" {
			labels = []string{count.Conference}
		}
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, count.Count, labels...)
	}
	successValue := 0.0
	if success {
		successValue = 1
	}
	ch <- prometheus.MustNewConstMetric(c.collectionSuccess, prometheus.GaugeValue, successValue)
	if !lastSuccessful.IsZero() {
		ch <- prometheus.MustNewConstMetric(c.lastSuccessfulRefresh, prometheus.GaugeValue, float64(lastSuccessful.Unix()))
	}
}

func (c *businessCollector) metricDesc(metric string) *prometheus.Desc {
	switch metric {
	case "tickets":
		return c.tickets
	case "ticket_checkins":
		return c.ticketCheckins
	case "speaker_applications":
		return c.speakerApplications
	case "volunteer_applications":
		return c.volunteerApplications
	case "recording_broadcasts":
		return c.recordingBroadcasts
	default:
		return nil
	}
}

func (c *businessCollector) snapshot(now time.Time) ([]types.BusinessMetricCount, bool, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !now.Before(c.nextRefresh) {
		counts, err := c.loader()
		if err == nil {
			c.counts = append(c.counts[:0], counts...)
			c.lastSuccessful = now
			c.lastSuccess = true
			c.nextRefresh = now.Add(businessRefreshInterval)
		} else {
			c.lastSuccess = false
			c.nextRefresh = now.Add(businessFailureRetry)
		}
	}
	return append([]types.BusinessMetricCount(nil), c.counts...), c.lastSuccess, c.lastSuccessful
}
