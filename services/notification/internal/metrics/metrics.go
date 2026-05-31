package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP API metrics (api mode only)

	// HTTPRequestsTotal — PromQL: sum(rate(notification_http_requests_total[5m])) by (method, path, status)
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "notification",
			Name:      "http_requests_total",
			Help:      "Total HTTP requests by method, path, and status code.",
		},
		[]string{"method", "path", "status"},
	)

	// HTTPRequestDuration — PromQL: histogram_quantile(0.99, rate(notification_http_request_duration_seconds_bucket[5m]))
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "notification",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.2, 0.5, 1.0, 2.0, 5.0},
		},
		[]string{"method", "path"},
	)

	// HTTPRequestsInFlight — PromQL: notification_http_requests_in_flight
	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "notification",
			Name:      "http_requests_in_flight",
			Help:      "Number of HTTP requests currently being processed.",
		},
	)

	// Email worker metrics

	// EmailsProcessedTotal — labels: type=regular|campaign, status=success|failed|rejected|retried|circuit_open
	// PromQL: sum(rate(notification_emails_processed_total[5m])) by (type, status)
	EmailsProcessedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "notification",
			Name:      "emails_processed_total",
			Help:      "Total emails processed by type and status.",
		},
		[]string{"type", "status"},
	)

	// EmailSendDuration — PromQL: histogram_quantile(0.99, rate(notification_email_send_duration_seconds_bucket[5m]))
	EmailSendDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "notification",
			Name:      "email_send_duration_seconds",
			Help:      "SMTP send duration in seconds.",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
		[]string{"type"},
	)

	// Campaign worker metrics

	// CampaignBatchesDispatched — PromQL: rate(notification_campaign_batches_dispatched_total[5m])
	CampaignBatchesDispatched = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "notification",
			Name:      "campaign_batches_dispatched_total",
			Help:      "Total campaign batches published to RabbitMQ.",
		},
	)

	// CampaignUsersDispatched — PromQL: rate(notification_campaign_users_dispatched_total[5m])
	CampaignUsersDispatched = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "notification",
			Name:      "campaign_users_dispatched_total",
			Help:      "Total individual users dispatched across all campaign batches.",
		},
	)

	// CampaignBackoffs — PromQL: rate(notification_campaign_backoffs_total[5m])
	// Spikes here = campaign.email queue is full; scale worker-email to drain it.
	CampaignBackoffs = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "notification",
			Name:      "campaign_backoffs_total",
			Help:      "Total backoff events triggered when campaign.email queue is full.",
		},
	)

	// CampaignDispatchDuration — PromQL: histogram_quantile(0.5, rate(notification_campaign_dispatch_duration_seconds_bucket[10m]))
	CampaignDispatchDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "notification",
			Name:      "campaign_dispatch_duration_seconds",
			Help:      "Total wall-clock time to fully dispatch a campaign.",
			Buckets:   []float64{1, 5, 15, 30, 60, 120, 300, 600},
		},
	)
)
