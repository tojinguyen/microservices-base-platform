package worker

import (
	"context"
	"encoding/json"
	"time"

	"backend/pkg/broker"
	"backend/pkg/logger"
	"backend/pkg/trace"

	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/repository"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

type OutboxWorker interface {
	Start(ctx context.Context)
}

type outboxWorker struct {
	outboxRepo repository.OutboxRepository
	broker     broker.Broker
	cfg        *config.Config
}

func NewOutboxWorker(outboxRepo repository.OutboxRepository, broker broker.Broker, cfg *config.Config) OutboxWorker {
	return &outboxWorker{
		outboxRepo: outboxRepo,
		broker:     broker,
		cfg:        cfg,
	}
}

func (w *outboxWorker) Start(ctx context.Context) {
	log := logger.L()
	interval := time.Duration(w.cfg.Worker.OutboxInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Info("Outbox worker started", zap.Duration("interval", interval))

	for {
		select {
		case <-ctx.Done():
			log.Info("Outbox worker stopping")
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *outboxWorker) processBatch(ctx context.Context) {
	log := logger.L()

	events, err := w.outboxRepo.FetchPending(ctx, w.cfg.Worker.OutboxBatchSize)
	if err != nil {
		log.Error("outbox: failed to fetch pending events", zap.Error(err))
		return
	}

	for _, event := range events {
		w.publishEvent(ctx, event)
	}
}

func (w *outboxWorker) publishEvent(ctx context.Context, event *domain.OutboxEvent) {
	log := logger.L()

	// Giải mã payload để lấy trace_context và khôi phục chuỗi trace từ HTTP request gốc
	var payloadMap map[string]json.RawMessage
	traceCtx := ctx
	if err := json.Unmarshal(event.Payload, &payloadMap); err == nil {
		if rawTraceCtx, ok := payloadMap["trace_context"]; ok {
			var traceMap map[string]string
			if err := json.Unmarshal(rawTraceCtx, &traceMap); err == nil && len(traceMap) > 0 {
				traceCtx = trace.ExtractMap(ctx, traceMap)
			}
		}
	}

	// Tạo child span cho bước publish - trace sẽ tiếp tục từ context đã khôi phục phía trên
	tracer := otel.Tracer("notification-service")
	traceCtx, span := tracer.Start(traceCtx, "outbox.publish")
	defer span.End()

	if err := w.broker.Publish(traceCtx, w.cfg.Queue.Exchange, event.RoutingKey, event.Payload); err != nil {
		log.Error("outbox: failed to publish event",
			zap.String("event_id", event.ID.String()),
			zap.String("aggregate_id", event.AggregateID.String()),
			zap.String("routing_key", event.RoutingKey),
			zap.Int("retry_count", event.RetryCount),
			zap.Error(err),
		)

		if event.RetryCount+1 >= w.cfg.Worker.OutboxMaxRetries {
			if markErr := w.outboxRepo.MarkFailed(ctx, event.ID, err.Error()); markErr != nil {
				log.Error("outbox: failed to mark event as failed", zap.String("event_id", event.ID.String()), zap.Error(markErr))
			}
			return
		}

		if retryErr := w.outboxRepo.IncrementRetry(ctx, event.ID, err.Error()); retryErr != nil {
			log.Error("outbox: failed to increment retry", zap.String("event_id", event.ID.String()), zap.Error(retryErr))
		}
		return
	}

	if err := w.outboxRepo.MarkPublished(ctx, event.ID); err != nil {
		// Event was published to broker but we couldn't mark it — worker will re-publish on next
		// poll tick. Consumer deduplication (Redis SETNX on event_id) handles the duplicate.
		log.Error("outbox: published to broker but failed to mark as published",
			zap.String("event_id", event.ID.String()),
			zap.Error(err),
		)
		return
	}

	log.Info("outbox: event published",
		zap.String("event_id", event.ID.String()),
		zap.String("aggregate_id", event.AggregateID.String()),
		zap.String("routing_key", event.RoutingKey),
	)
}
