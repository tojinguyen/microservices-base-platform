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
	"gorm.io/gorm"
)

type OutboxWorker interface {
	Start(ctx context.Context)
}

type outboxWorker struct {
	db               *gorm.DB
	outboxRepo       repository.OutboxRepository
	notificationRepo repository.NotificationRepository
	broker           broker.Broker
	cfg              *config.Config
}

func NewOutboxWorker(db *gorm.DB, outboxRepo repository.OutboxRepository, notificationRepo repository.NotificationRepository, broker broker.Broker, cfg *config.Config) OutboxWorker {
	return &outboxWorker{
		db:               db,
		outboxRepo:       outboxRepo,
		notificationRepo: notificationRepo,
		broker:           broker,
		cfg:              cfg,
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

	events, err := w.outboxRepo.ClaimPendingBatch(ctx, w.cfg.Worker.OutboxBatchSize)
	if err != nil {
		log.Error("outbox: failed to claim pending events", zap.Error(err))
		return
	}

	for _, event := range events {
		w.publishEvent(ctx, event)
	}
}

func (w *outboxWorker) publishEvent(ctx context.Context, event *domain.OutboxEvent) {
	log := logger.L()

	// Khôi phục chuỗi trace từ column trace_context riêng biệt.
	// trace_context được lưu lúc tạo outbox event, giúp tiếp nối distributed trace
	// qua async boundary (DB Outbox → RabbitMQ) kể cả khi service đã restart.
	traceCtx := ctx
	if len(event.TraceContext) > 0 {
		traceCtx = trace.ExtractMap(ctx, event.TraceContext)
	}

	// Tạo child span — trace sẽ tiếp tục từ context đã khôi phục phía trên
	tracer := otel.Tracer("notification-service")
	traceCtx, span := tracer.Start(traceCtx, "outbox.publish")
	defer span.End()

	// Publish payload thuần (NotificationTask) — broker sẽ tự inject trace vào AMQP headers.
	// Dùng json.RawMessage để tránh broker.Publish marshal lại []byte thành base64 string.
	if err := w.broker.Publish(traceCtx, w.cfg.Queue.Exchange, event.RoutingKey, json.RawMessage(event.Payload)); err != nil {
		log.Error("outbox: failed to publish event",
			zap.String("event_id", event.ID.String()),
			zap.String("aggregate_id", event.AggregateID.String()),
			zap.String("routing_key", event.RoutingKey),
			zap.Int("retry_count", event.RetryCount),
			zap.Error(err),
		)

		if event.RetryCount+1 >= w.cfg.Worker.OutboxMaxRetries {
			errTx := w.db.Transaction(func(tx *gorm.DB) error {
				outboxRepoTx := repository.NewOutboxRepository(tx)
				if markErr := outboxRepoTx.MarkFailed(ctx, event.ID, err.Error()); markErr != nil {
					return markErr
				}
				notificationRepoTx := repository.NewNotificationRepository(tx)
				if updateErr := notificationRepoTx.UpdateDeliveryStatus(ctx, event.AggregateID, domain.NotificationStatusFailed, "outbox publish failed: "+err.Error(), nil, nil); updateErr != nil {
					return updateErr
				}
				return nil
			})
			if errTx != nil {
				log.Error("outbox: failed to mark event and notification as failed in transaction", zap.String("event_id", event.ID.String()), zap.Error(errTx))
			}
			return
		}

		if retryErr := w.outboxRepo.IncrementRetry(ctx, event.ID, err.Error()); retryErr != nil {
			log.Error("outbox: failed to increment retry", zap.String("event_id", event.ID.String()), zap.Error(retryErr))
		}
		return
	}

	if err := w.outboxRepo.MarkPublished(ctx, event.ID); err != nil {
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

