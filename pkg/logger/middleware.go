package logger

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func MiddleWare(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		traceID := uuid.NewString()
		ctx := WithTraceID(r.Context(), traceID)

		next.ServeHTTP(w, r.WithContext(ctx))

		duration := time.Since(start)

		FromContext(ctx).Info("http_request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Duration("duration", duration),
		)
	})
}
