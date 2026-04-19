package worker

import (
	"context"
	"time"

	"backend/pkg/broker"
	"backend/pkg/logger"

	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"go.uber.org/zap"
)

type NotificationWorker interface {
	Start(ctx context.Context)
}

type notificationWorker struct {
	repo   repository.NotificationRepository
	broker broker.Broker
	cfg    *config.Config
}

func NewNotificationWorker(repo repository.NotificationRepository, broker broker.Broker, cfg *config.Config) NotificationWorker {
	return &notificationWorker{
		repo:   repo,
		broker: broker,
		cfg:    cfg,
	}
}

func (w *notificationWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(w.cfg.Worker.Interval) * time.Second)
	defer ticker.Stop()

	log := logger.L()
	log.Info("Notification worker started")

	for {
		select {
		case <-ctx.Done():
			log.Info("Notification worker stopping")
			return
		case <-ticker.C:
			w.processPendingNotifications(ctx)
		}
	}
}

func (w *notificationWorker) processPendingNotifications(ctx context.Context) {
	log := logger.L()
	notifications, err := w.repo.GetPending(ctx, w.cfg.Worker.BatchSize)
	if err != nil {
		log.Error("Failed to get pending notifications", zap.Error(err))
		return
	}

	for _, noti := range notifications {
		task := dto.NotificationTask{
			NotificationID: noti.Id.String(),
			UserID:         noti.UserID,
			EventType:      noti.EventType,
			Recipient:      noti.Recipient,
			Channel:        noti.Channel,
			Data: map[string]string{
				"subject": noti.Subject,
				"content": noti.Content,
			},
		}

		routingKey := string(noti.Channel)
		err := w.broker.Publish(ctx, w.cfg.Queue.Exchange, routingKey, task)
		if err != nil {
			log.Error("failed to publish notification", zap.String("id", noti.Id.String()), zap.Error(err))
			continue
		}

		err = w.repo.UpdateDeliveryStatus(ctx, noti.Id, domain.NotificationStatusProcessing, "", nil)
		if err != nil {
			log.Error("failed to update notification status", zap.String("id", noti.Id.String()), zap.Error(err))
		}
	}
}
