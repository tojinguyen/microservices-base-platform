package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	notifmetrics "github.com/tojinguyen/notification/internal/metrics"
)

// PrometheusMiddleware records HTTP request count, duration, and in-flight gauge for the
// notification API. Mount it after gin.Recovery() so panics are counted correctly.
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		start := time.Now()
		notifmetrics.HTTPRequestsInFlight.Inc()
		c.Next()
		notifmetrics.HTTPRequestsInFlight.Dec()

		// Use FullPath() to group by route pattern (e.g. /api/v1/users/:id) and avoid
		// cardinality explosion from user IDs or other dynamic path segments.
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}

		notifmetrics.HTTPRequestsTotal.WithLabelValues(
			c.Request.Method,
			path,
			strconv.Itoa(c.Writer.Status()),
		).Inc()
		notifmetrics.HTTPRequestDuration.WithLabelValues(c.Request.Method, path).
			Observe(time.Since(start).Seconds())
	}
}
