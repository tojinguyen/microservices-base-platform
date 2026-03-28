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

		// 1. Generate or retrieve Trace ID from Header (if passed from another system)
		traceID := c.GetHeader("X-Trace-ID")
		if traceID == "" {
			traceID = uuid.NewString()
		}

		// 2. Store Trace ID in Go's context.Context
		// (so logger.FromContext(ctx) can extract it)
		ctx := WithTraceID(c.Request.Context(), traceID)
		c.Request = c.Request.WithContext(ctx)

		// 3. Attach Trace ID to the response Header so the Frontend can log it if needed
		c.Writer.Header().Set("X-Trace-ID", traceID)

		// 4. Proceed to the next handlers
		c.Next()

		// 5. After the request is processed, log the result
		end := time.Now()
		latency := end.Sub(start)

		if len(c.Errors) > 0 {
			// If there are errors during Gin's processing
			for _, e := range c.Errors.Errors() {
				FromContext(ctx).Error("gin_error", zap.String("error", e))
			}
		} else {
			// Log normal request
			FromContext(ctx).Info("http_request",
				zap.Int("status", c.Writer.Status()),
				zap.String("method", c.Request.Method),
				zap.String("path", path),
				zap.String("query", query),
				zap.String("ip", c.ClientIP()),
				zap.String("user_agent", c.Request.UserAgent()),
				zap.Duration("latency", latency),
				zap.String("trace_id", traceID),
			)
		}
	}
}
