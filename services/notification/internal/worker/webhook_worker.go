package worker

import (
	"context"
	"encoding/json"

	"backend/pkg/broker"
	"backend/pkg/logger"

	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/service"
	"go.uber.org/zap"
)

type WebhookWorker struct {
	service service.NotificationService
	broker  broker.Broker
	cfg     *config.Config
}

func NewWebhookWorker(svc service.NotificationService, broker broker.Broker, cfg *config.Config) *WebhookWorker {
	return &WebhookWorker{
		service: svc,
		broker:  broker,
		cfg:     cfg,
	}
}

func (w *WebhookWorker) Start(ctx context.Context) {
	log := logger.L()
	log.Info("Webhook worker started")

	// Subscribe to mailpit webhook queue with DLQ options
	opts := broker.QueueOptions{
		Prefetch: 1,
		QueueArgs: map[string]interface{}{
			"x-dead-letter-exchange":    w.cfg.Queue.Exchange + ".dlq",
			"x-dead-letter-routing-key": "webhook.dlq",
		},
	}
	err := w.broker.QueueSubscribeWithOptions(ctx, w.cfg.Queue.WebhookMailpit, w.cfg.Queue.Exchange, w.cfg.Queue.WebhookMailpit, w.HandleMessage, opts)
	if err != nil {
		log.Error("Failed to subscribe to webhook queue", zap.Error(err))
		return
	}

	<-ctx.Done()
	log.Info("Webhook worker stopping")
}

func (w *WebhookWorker) HandleMessage(ctx context.Context, body []byte) error {
	log := logger.L()
	var webhook dto.MailpitWebhook
	if err := json.Unmarshal(body, &webhook); err != nil {
		log.Error("Failed to unmarshal webhook payload", zap.Error(err))
		return broker.ErrRejectToDLQ // Reject to DLQ immediately on malformed JSON
	}

	log.Info("Processing Mailpit webhook", zap.String("mailpit_id", webhook.ID))

	if err := w.service.HandleMailpitWebhook(ctx, webhook); err != nil {
		log.Error("Failed to process Mailpit webhook", zap.String("mailpit_id", webhook.ID), zap.Error(err))
		return err // Requeue to retry on database/internal errors
	}

	return nil
}
