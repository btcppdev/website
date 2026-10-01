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

	bitcoinPayoutChecks               *prometheus.Desc
	bitcoinPayoutDue                  *prometheus.Desc
	tickets                           *prometheus.Desc
	ticketCheckins                    *prometheus.Desc
	speakerApplications               *prometheus.Desc
	volunteerApplications             *prometheus.Desc
	recordingBroadcasts               *prometheus.Desc
	serviceFulfillmentJobs            *prometheus.Desc
	accountDeletionDeliveryJobs       *prometheus.Desc
	accountDeletionDeliveryOldest     *prometheus.Desc
	accountDeletionCancellationJobs   *prometheus.Desc
	accountDeletionCancellationOldest *prometheus.Desc
	serviceRefundJobs                 *prometheus.Desc
	serviceRefundTaxJobs              *prometheus.Desc
	serviceRefundTaxOldest            *prometheus.Desc
	serviceFulfillmentOldest          *prometheus.Desc
	serviceRefundOldest               *prometheus.Desc
	serviceReceiptJobs                *prometheus.Desc
	serviceReceiptOldest              *prometheus.Desc
	alertsCheckoutJobs                *prometheus.Desc
	alertsCheckoutDue                 *prometheus.Desc
	alertsCheckoutUnresolved          *prometheus.Desc
	collectionSuccess                 *prometheus.Desc
	lastSuccessfulRefresh             *prometheus.Desc
}

func newBusinessCollector(namespace string, loader BusinessMetricsLoader) *businessCollector {
	return &businessCollector{
		bitcoinPayoutChecks:               prometheus.NewDesc(namespace+"_bitcoin_payout_checks", "Latest verification state of recorded Bitcoin payouts.", []string{"state"}, nil),
		bitcoinPayoutDue:                  prometheus.NewDesc(namespace+"_bitcoin_payout_oldest_due_timestamp_seconds", "Earliest next verification time of recorded Bitcoin payouts; zero when none exist.", nil, nil),
		alertsCheckoutJobs:                prometheus.NewDesc(namespace+"_alerts_checkout_recovery_jobs", "Open Alerts checkouts by recovery state.", []string{"state"}, nil),
		alertsCheckoutDue:                 prometheus.NewDesc(namespace+"_alerts_checkout_oldest_due_timestamp_seconds", "Earliest scheduled reconciliation among open Alerts checkouts; zero when none exist.", nil, nil),
		alertsCheckoutUnresolved:          prometheus.NewDesc(namespace+"_alerts_checkout_oldest_unresolved_timestamp_seconds", "Creation time of the oldest unbound Alerts checkout; zero when none exist.", nil, nil),
		loader:                            loader,
		serviceRefundTaxJobs:              prometheus.NewDesc(namespace+"_service_refund_tax_jobs", "Current Bitcoin refund tax adjustments by state.", []string{"state"}, nil),
		serviceRefundTaxOldest:            prometheus.NewDesc(namespace+"_service_refund_tax_oldest_pending_timestamp_seconds", "Creation timestamp of oldest pending Bitcoin refund tax adjustment; zero when none are pending.", nil, nil),
		accountDeletionCancellationJobs:   prometheus.NewDesc(namespace+"_account_deletion_cancellation_jobs", "Current account deletion jobs by state.", []string{"state"}, nil),
		accountDeletionCancellationOldest: prometheus.NewDesc(namespace+"_account_deletion_cancellation_oldest_pending_timestamp_seconds", "Request timestamp of oldest pending account deletion job; zero when none are pending.", nil, nil),
		accountDeletionDeliveryJobs:       prometheus.NewDesc(namespace+"_account_deletion_delivery_jobs", "Current account deletion jobs by state.", []string{"state"}, nil),
		accountDeletionDeliveryOldest:     prometheus.NewDesc(namespace+"_account_deletion_delivery_oldest_pending_timestamp_seconds", "Request timestamp of oldest pending account deletion job; zero when none are pending.", nil, nil),
		serviceFulfillmentJobs:            prometheus.NewDesc(namespace+"_service_fulfillment_jobs", "Current external service fulfillment jobs by delivery state.", []string{"state"}, nil),
		serviceRefundJobs:                 prometheus.NewDesc(namespace+"_service_refund_jobs", "Current service refund adjustment jobs by delivery state.", []string{"state"}, nil),
		serviceFulfillmentOldest:          prometheus.NewDesc(namespace+"_service_fulfillment_oldest_pending_timestamp_seconds", "Creation timestamp of the oldest pending external service fulfillment; zero when none are pending.", nil, nil),
		serviceRefundOldest:               prometheus.NewDesc(namespace+"_service_refund_oldest_pending_timestamp_seconds", "Creation timestamp of the oldest pending service refund adjustment; zero when none are pending.", nil, nil),
		serviceReceiptJobs:                prometheus.NewDesc(namespace+"_service_receipt_jobs", "Current service receipt jobs by delivery state.", []string{"state"}, nil),
		serviceReceiptOldest:              prometheus.NewDesc(namespace+"_service_receipt_oldest_pending_timestamp_seconds", "Creation timestamp of the oldest pending service receipt; zero when none are pending.", nil, nil),
		tickets:                           prometheus.NewDesc(namespace+"_tickets", "Current ticket registrations by conference and state.", []string{"conference", "state"}, nil),
		ticketCheckins:                    prometheus.NewDesc(namespace+"_ticket_checkins", "Current non-revoked ticket check-ins by conference.", []string{"conference"}, nil),
		speakerApplications:               prometheus.NewDesc(namespace+"_speaker_applications", "Current speaker applications by conference and review state.", []string{"conference", "state"}, nil),
		volunteerApplications:             prometheus.NewDesc(namespace+"_volunteer_applications", "Current volunteer applications by conference and workflow state.", []string{"conference", "state"}, nil),
		recordingBroadcasts:               prometheus.NewDesc(namespace+"_recording_broadcasts", "Current recording broadcasts by conference and state.", []string{"conference", "state"}, nil),
		collectionSuccess:                 prometheus.NewDesc(namespace+"_business_metrics_collection_success", "Whether the latest business metric refresh succeeded.", nil, nil),
		lastSuccessfulRefresh:             prometheus.NewDesc(namespace+"_business_metrics_last_success_timestamp_seconds", "Unix timestamp of the latest successful business metric refresh.", nil, nil),
	}
}

func (c *businessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.bitcoinPayoutChecks
	ch <- c.bitcoinPayoutDue
	ch <- c.alertsCheckoutJobs
	ch <- c.alertsCheckoutDue
	ch <- c.alertsCheckoutUnresolved
	ch <- c.tickets
	ch <- c.ticketCheckins
	ch <- c.speakerApplications
	ch <- c.volunteerApplications
	ch <- c.recordingBroadcasts
	ch <- c.serviceFulfillmentJobs
	ch <- c.accountDeletionDeliveryJobs
	ch <- c.accountDeletionDeliveryOldest
	ch <- c.accountDeletionCancellationJobs
	ch <- c.accountDeletionCancellationOldest
	ch <- c.serviceRefundTaxJobs
	ch <- c.serviceRefundTaxOldest
	ch <- c.serviceRefundJobs
	ch <- c.serviceFulfillmentOldest
	ch <- c.serviceRefundOldest
	ch <- c.serviceReceiptJobs
	ch <- c.serviceReceiptOldest
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
		if count.Metric == "bitcoin_payout_checks" || count.Metric == "alerts_checkout_recovery_jobs" || count.Metric == "account_deletion_cancellation_jobs" || count.Metric == "account_deletion_delivery_jobs" || count.Metric == "service_receipt_jobs" || count.Metric == "service_fulfillment_jobs" || count.Metric == "service_refund_jobs" || count.Metric == "service_refund_tax_jobs" {
			labels = []string{count.State}
		}
		if count.Metric == "bitcoin_payout_oldest_due_timestamp_seconds" || count.Metric == "alerts_checkout_oldest_due_timestamp_seconds" || count.Metric == "alerts_checkout_oldest_unresolved_timestamp_seconds" || count.Metric == "account_deletion_cancellation_oldest_pending_timestamp_seconds" || count.Metric == "account_deletion_delivery_oldest_pending_timestamp_seconds" || count.Metric == "service_receipt_oldest_pending_timestamp_seconds" || count.Metric == "service_fulfillment_oldest_pending_timestamp_seconds" || count.Metric == "service_refund_oldest_pending_timestamp_seconds" || count.Metric == "service_refund_tax_oldest_pending_timestamp_seconds" {
			labels = nil
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
	case "bitcoin_payout_checks":
		return c.bitcoinPayoutChecks
	case "bitcoin_payout_oldest_due_timestamp_seconds":
		return c.bitcoinPayoutDue
	case "alerts_checkout_recovery_jobs":
		return c.alertsCheckoutJobs
	case "alerts_checkout_oldest_due_timestamp_seconds":
		return c.alertsCheckoutDue
	case "alerts_checkout_oldest_unresolved_timestamp_seconds":
		return c.alertsCheckoutUnresolved
	case "account_deletion_cancellation_jobs":
		return c.accountDeletionCancellationJobs
	case "account_deletion_cancellation_oldest_pending_timestamp_seconds":
		return c.accountDeletionCancellationOldest
	case "account_deletion_delivery_jobs":
		return c.accountDeletionDeliveryJobs
	case "account_deletion_delivery_oldest_pending_timestamp_seconds":
		return c.accountDeletionDeliveryOldest
	case "service_refund_tax_jobs":
		return c.serviceRefundTaxJobs
	case "service_refund_tax_oldest_pending_timestamp_seconds":
		return c.serviceRefundTaxOldest
	case "service_refund_jobs":
		return c.serviceRefundJobs
	case "service_refund_oldest_pending_timestamp_seconds":
		return c.serviceRefundOldest
	case "service_fulfillment_jobs":
		return c.serviceFulfillmentJobs
	case "service_fulfillment_oldest_pending_timestamp_seconds":
		return c.serviceFulfillmentOldest
	case "service_receipt_jobs":
		return c.serviceReceiptJobs
	case "service_receipt_oldest_pending_timestamp_seconds":
		return c.serviceReceiptOldest
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
