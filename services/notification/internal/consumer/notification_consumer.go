package consumer

import (
	"backend/pkg/broker"
	appErrors "backend/pkg/errors"
	"backend/pkg/logger"
	"context"
	"encoding/json"
	stdErrors "errors"

	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/service"
	"go.uber.org/zap"
)

type NotificationConsumer struct {
	broker    broker.Broker
	service   service.NotificationService
	queueName string
}

func NewNotificationConsumer(brokerClient broker.Broker, notificationService service.NotificationService, queueName string) *NotificationConsumer {
	return &NotificationConsumer{
		broker:    brokerClient,
		service:   notificationService,
		queueName: queueName,
	}
}

func (c *NotificationConsumer) Start(ctx context.Context) error {
	return c.broker.QueueSubscribe(ctx, c.queueName, c.handleMessage)
}

func (c *NotificationConsumer) handleMessage(ctx context.Context, body []byte) error {
	var event dto.NotificationEvent
	if err := json.Unmarshal(body, &event); err != nil {
		logger.FromContext(ctx).Warn("ignore malformed notification event payload", zap.Error(err))
		return nil
	}

	err := c.service.ProcessEvent(ctx, event)
	if err == nil {
		return nil
	}

	var appErr *appErrors.AppError
	if stdErrors.As(err, &appErr) && appErr.Code < 500 {
		logger.FromContext(ctx).Warn("dropping invalid notification event",
			zap.String("event_id", event.EventID),
			zap.String("event_type", event.EventType),
			zap.Error(err),
		)
		return nil
	}

	logger.FromContext(ctx).Error("failed to process notification event",
		zap.String("event_id", event.EventID),
		zap.String("event_type", event.EventType),
		zap.Error(err),
	)
	return err
}
