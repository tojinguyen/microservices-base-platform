package worker

import (
	"context"
	"encoding/json"

	"backend/pkg/broker"
	"backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"go.uber.org/zap"
)

type DLQWorker struct {
	repo   repository.DLQRepository
	broker broker.Broker
	cfg    *config.Config
}

func NewDLQWorker(repo repository.DLQRepository, broker broker.Broker, cfg *config.Config) *DLQWorker {
	return &DLQWorker{repo: repo, broker: broker, cfg: cfg}
}

func (w *DLQWorker) Start(ctx context.Context) {
	log := logger.L()
	log.Info("DLQ worker started")

	// Subscribe to email.dlq
	go func() {
		dlqExchange := w.cfg.Queue.Exchange + ".dlq"
		opts := broker.QueueOptions{Prefetch: 1}
		if err := w.broker.QueueSubscribeWithOptions(ctx, "email.dlq", dlqExchange, "email.dlq", w.HandleEmailDLQ, opts); err != nil {
			log.Error("Failed to subscribe to email.dlq queue", zap.Error(err))
		}
	}()

	// Subscribe to webhook.dlq
	go func() {
		dlqExchange := w.cfg.Queue.Exchange + ".dlq"
		opts := broker.QueueOptions{Prefetch: 1}
		if err := w.broker.QueueSubscribeWithOptions(ctx, "webhook.dlq", dlqExchange, "webhook.dlq", w.HandleWebhookDLQ, opts); err != nil {
			log.Error("Failed to subscribe to webhook.dlq queue", zap.Error(err))
		}
	}()

	<-ctx.Done()
	log.Info("DLQ worker stopping")
}

func (w *DLQWorker) HandleEmailDLQ(ctx context.Context, body []byte) error {
	return w.saveToDLQ(ctx, "email", body)
}

func (w *DLQWorker) HandleWebhookDLQ(ctx context.Context, body []byte) error {
	return w.saveToDLQ(ctx, "webhook", body)
}

func (w *DLQWorker) saveToDLQ(ctx context.Context, queueName string, body []byte) error {
	log := logger.L()

	var task dto.NotificationTask
	var notiIDPtr *uuid.UUID

	if err := json.Unmarshal(body, &task); err == nil && task.NotificationID != "" {
		if uID, parseErr := uuid.Parse(task.NotificationID); parseErr == nil {
			notiIDPtr = &uID
		}
	}

	dlqMsg := &domain.DLQMessage{
		NotificationID: notiIDPtr,
		QueueName:      queueName,
		Payload:        json.RawMessage(body),
		ErrorMessage:   "Failed after max retries or invalid template payload",
		Status:         domain.DLQStatusPending,
	}

	if err := w.repo.Save(ctx, dlqMsg); err != nil {
		log.Error("Failed to save DLQ message to database", zap.Error(err))
		return err // Returns error to requeue back to RabbitMQ DLQ
	}

	log.Info("Successfully saved message to DLQ database",
		zap.String("queue", queueName),
		zap.Any("noti_id", task.NotificationID))
	return nil // ACKs the message to release it from RabbitMQ DLQ
}
