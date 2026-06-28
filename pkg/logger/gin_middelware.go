package logger

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		traceID := c.GetHeader("X-Trace-ID")
		if traceID == "" {
			traceID = uuid.NewString()
		}

		ctx := WithTraceID(c.Request.Context(), traceID)
		c.Request = c.Request.WithContext(ctx)
		c.Writer.Header().Set("X-Trace-ID", traceID)

		c.Next()

		latency := time.Since(start)
		// Đổi ra ms (số thập phân) để Grafana dễ đọc (VD: 15.8ms)
		latencyMS := float64(latency.Microseconds()) / 1000.0

		// Dùng FromContext(ctx) thì KHÔNG CẦN truyền lại trace_id nữa
		logger := FromContext(ctx)

		if len(c.Errors) > 0 {
			for _, e := range c.Errors.Errors() {
				logger.Error("gin_error", zap.String("error", e))
			}
		} else {
			logger.Info("http_request",
				zap.Int("status", c.Writer.Status()),
				zap.String("method", c.Request.Method),
				zap.String("path", path),
				zap.String("query", query),
				zap.String("ip", c.ClientIP()),
				zap.String("user_agent", c.Request.UserAgent()),
				zap.Float64("latency_ms", latencyMS), // Đã đổi tên thành latency_ms
			)
		}
	}
}
