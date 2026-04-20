package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/smtp"

	"backend/pkg/broker"
	"backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"go.uber.org/zap"
)

type EmailWorker struct {
	repo   repository.NotificationRepository
	broker broker.Broker
	cfg    *config.Config
}

func NewEmailWorker(repo repository.NotificationRepository, broker broker.Broker, cfg *config.Config) *EmailWorker {
	return &EmailWorker{
		repo:   repo,
		broker: broker,
		cfg:    cfg,
	}
}

func (w *EmailWorker) Start(ctx context.Context) {
	log := logger.L()
	log.Info("Email worker started")

	err := w.broker.QueueSubscribe(ctx, "email", w.cfg.Queue.Exchange, string(domain.ChannelEmail), w.HandleMessage)
	if err != nil {
		log.Error("Failed to subscribe to email queue", zap.Error(err))
		return
	}

	<-ctx.Done()
	log.Info("Email worker stopping")
}

func (w *EmailWorker) HandleMessage(ctx context.Context, body []byte) error {
	log := logger.L()
	var task dto.NotificationTask
	if err := json.Unmarshal(body, &task); err != nil {
		log.Error("Failed to unmarshal notification task", zap.Error(err))
		return err
	}

	log.Info("Sending email",
		zap.String("notification_id", task.NotificationID),
		zap.String("recipient", task.Recipient),
	)

	err := w.sendEmail(task.NotificationID, task.Recipient, task.Data["subject"], task.Data["content"])

	notificationID, _ := uuid.Parse(task.NotificationID)

	if err != nil {
		log.Error("Failed to send email", zap.Error(err))
		w.repo.UpdateDeliveryStatus(ctx, notificationID, domain.NotificationStatusFailed, err.Error(), nil)
		return err
	}

	err = w.repo.UpdateDeliveryStatus(ctx, notificationID, domain.NotificationStatusDelivering, "Waiting for webhook confirmation", nil)
	if err != nil {
		log.Error("Failed to update notification status to delivering", zap.Error(err))
	}

	log.Info("Email sent successfully", zap.String("notification_id", task.NotificationID))
	return nil
}

func (w *EmailWorker) sendEmail(id, to, subject, body string) error {
	smtpCfg := w.cfg.SMTP
	auth := smtp.PlainAuth("", smtpCfg.Username, smtpCfg.Password, smtpCfg.Host)

	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"X-Notification-ID: %s\r\n"+
		"\r\n"+
		"%s\r\n", to, subject, id, body))

	addr := fmt.Sprintf("%s:%d", smtpCfg.Host, smtpCfg.Port)

	if smtpCfg.Username == "" {
		return smtp.SendMail(addr, nil, smtpCfg.From, []string{to}, msg)
	}

	return smtp.SendMail(addr, auth, smtpCfg.From, []string{to}, msg)
}
