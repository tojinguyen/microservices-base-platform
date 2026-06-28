package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// httpRequestsTotal đếm tổng số HTTP request theo method, path, và status code
	// Ví dụ PromQL: sum(rate(identity_http_requests_total[5m])) by (method, path, status)
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "identity",
			Name:      "http_requests_total",
			Help:      "Tổng số HTTP request đã nhận theo method, path, và status code.",
		},
		[]string{"method", "path", "status"},
	)

	// httpRequestDuration đo thời gian xử lý mỗi request (histogram)
	// Ví dụ PromQL: histogram_quantile(0.99, rate(identity_http_request_duration_seconds_bucket[5m]))
	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "identity",
			Name:      "http_request_duration_seconds",
			Help:      "Thời gian xử lý HTTP request tính bằng giây.",
			// Buckets: 10ms, 50ms, 100ms, 200ms, 500ms, 1s, 2s, 5s
			Buckets: []float64{0.01, 0.05, 0.1, 0.2, 0.5, 1.0, 2.0, 5.0},
		},
		[]string{"method", "path"},
	)

	// httpRequestsInFlight đo số lượng request đang xử lý đồng thời
	// Ví dụ PromQL: identity_http_requests_in_flight
	httpRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "identity",
			Name:      "http_requests_in_flight",
			Help:      "Số lượng HTTP request đang được xử lý tại thời điểm hiện tại.",
		},
	)
)

// PrometheusMiddleware là Gin middleware thu thập metrics cho mỗi HTTP request.
// Cách dùng: r.Use(middleware.PrometheusMiddleware())
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Bỏ qua chính endpoint /metrics để tránh vòng lặp đo lường
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		start := time.Now()
		httpRequestsInFlight.Inc()

		// Xử lý request
		c.Next()

		// Ghi nhận sau khi request hoàn thành
		httpRequestsInFlight.Dec()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		// Dùng FullPath() để nhóm các route cùng pattern (ví dụ: /api/v1/profile/:id thay vì /api/v1/profile/123)
		// Điều này giúp tránh "cardinality explosion" - tình huống mỗi user ID tạo ra một time series riêng
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}

		httpRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}
