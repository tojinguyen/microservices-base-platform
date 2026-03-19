package logger

import (
	"context"

	"go.uber.org/zap"
)

type ctxKey string

const (
	traceIDKey   ctxKey = "trace_id"
	requestIDKey ctxKey = "request_id"
)

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

func FromContext(ctx context.Context) *zap.Logger {
	l := log

	if traceID, ok := ctx.Value(traceIDKey).(string); ok {
		l = l.With(zap.String("trace_id", traceID))
	}

	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		l = l.With(zap.String("request_id", requestID))
	}

	return l
}
