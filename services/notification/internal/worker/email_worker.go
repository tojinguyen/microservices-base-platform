package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/smtp"
	"time"

	"backend/pkg/broker"
	"backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

type EmailWorker struct {
	repo           repository.NotificationRepository
	campaignRepo   repository.CampaignRepository
	broker         broker.Broker
	cfg            *config.Config
	circuitBreaker *CircuitBreaker
}

func NewEmailWorker(
	repo repository.NotificationRepository,
	campaignRepo repository.CampaignRepository,
	b broker.Broker,
	cfg *config.Config,
) *EmailWorker {
	return &EmailWorker{
		repo:         repo,
		campaignRepo: campaignRepo,
		broker:       b,
		cfg:          cfg,
		circuitBreaker: NewCircuitBreaker(
			cfg.Worker.CircuitBreakerMaxFailures,
			cfg.Worker.CircuitBreakerOpenDuration,
		),
	}
}

func (w *EmailWorker) Start(ctx context.Context) {
	log := logger.L()
	log.Info("Email worker started")

	// Declare the Retry Queue automatically so it doesn't need to be created manually
	if err := w.declareRetryQueue(ctx); err != nil {
		log.Error("Failed to declare retry queue", zap.Error(err))
	}

	// Regular notification queue with DLQ options.
	go func() {
		opts := broker.QueueOptions{
			Prefetch: 1,
			QueueArgs: map[string]interface{}{
				"x-dead-letter-exchange":    w.cfg.Queue.Exchange + ".dlq",
				"x-dead-letter-routing-key": "email.dlq",
			},
		}
		if err := w.broker.QueueSubscribeWithOptions(ctx, "email", w.cfg.Queue.Exchange, string(domain.ChannelEmail), w.HandleMessage, opts); err != nil {
			log.Error("Failed to subscribe to email queue", zap.Error(err))
		}
	}()

	// Campaign email queue — custom prefetch and x-max-length for back-pressure.
	prefetch := w.cfg.Worker.CampaignPrefetch
	if prefetch <= 0 {
		prefetch = 20
	}
	go func() {
		opts := broker.QueueOptions{
			Prefetch:  prefetch,
			QueueArgs: campaignEmailQueueArgs,
		}
		if err := w.broker.QueueSubscribeWithOptions(ctx, campaignEmailRoutingKey, w.cfg.Queue.Exchange, campaignEmailRoutingKey, w.HandleCampaignMessage, opts); err != nil {
			log.Error("Failed to subscribe to campaign.email queue", zap.Error(err))
		}
	}()

	<-ctx.Done()
	log.Info("Email worker stopping")
}

// declareRetryQueue ensures the retry exchange and queue are created with the proper DLX config.
// Messages sent here will sit for the TTL, then RabbitMQ routes them back to the main exchange.
func (w *EmailWorker) declareRetryQueue(ctx context.Context) error {
	retryExchange := w.cfg.Queue.Exchange + ".retry"
	retryQueue := "email.retry"
	retryRoutingKey := string(domain.ChannelEmail) + ".retry"

	opts := broker.QueueOptions{
		QueueArgs: map[string]interface{}{
			"x-dead-letter-exchange":    w.cfg.Queue.Exchange,
			"x-dead-letter-routing-key": string(domain.ChannelEmail),
			"x-message-ttl":             int32(60000), // Base TTL of 60 seconds (can be overridden per message if needed)
		},
	}

	return w.broker.QueueDeclare(ctx, retryQueue, retryExchange, retryRoutingKey, opts)
}

func (w *EmailWorker) HandleMessage(ctx context.Context, body []byte) error {
	// Context đã chứa trace từ RabbitMQ (broker tự động extract khi consume message)
	tracer := otel.Tracer("notification-service")
	ctx, span := tracer.Start(ctx, "worker.email.process")
	defer span.End()

	log := logger.L()
	var task dto.NotificationTask
	if err := json.Unmarshal(body, &task); err != nil {
		log.Error("Failed to unmarshal notification task", zap.Error(err))
		return broker.ErrRejectToDLQ // Reject to DLQ immediately on malformed payload
	}

	span.SetAttributes(
		attribute.String("notification.id", task.NotificationID),
		attribute.String("notification.recipient", task.Recipient),
	)

	log.Info("Sending email",
		zap.String("notification_id", task.NotificationID),
		zap.String("recipient", task.Recipient),
	)

	err := w.sendEmail(ctx, task.NotificationID, task.Recipient, task.Data["subject"], task.Data["content"])

	if errors.Is(err, ErrCircuitBreakerOpen) {
		log.Warn("Circuit breaker is open, skipping email send", zap.String("notification_id", task.NotificationID))
		return err // Requeue to try again later
	}

	notificationID, _ := uuid.Parse(task.NotificationID)

	if err != nil {
		log.Error("Failed to send email", zap.Error(err), zap.Int("retry_count", task.RetryCount))

		maxRetries := w.cfg.Worker.MaxRetries

		if task.RetryCount < maxRetries {
			task.RetryCount++
			delay := retryDelay(task.RetryCount)
			log.Info("Scheduling broker-level retry", zap.Duration("delay", delay), zap.Int("attempt", task.RetryCount))

			// Route message to Retry Queue via Retry Exchange
			retryExchange := w.cfg.Queue.Exchange + ".retry"
			routingKey := string(domain.ChannelEmail) + ".retry"

			if publishErr := w.broker.Publish(ctx, retryExchange, routingKey, task); publishErr != nil {
				log.Error("Failed to publish retry message", zap.Error(publishErr))
				return err // Publish fails -> requeue immediately on main queue as fallback
			}
			return nil // Return nil to ACK current message on main queue
		} else {
			log.Error("Max retries reached, marking as failed", zap.String("notification_id", task.NotificationID))
			w.repo.UpdateDeliveryStatus(ctx, notificationID, domain.NotificationStatusFailed, err.Error(), nil, &task.RetryCount)
			return broker.ErrRejectToDLQ // Rejects message to DLQ natively via RabbitMQ
		}
	}

	err = w.repo.UpdateDeliveryStatus(ctx, notificationID, domain.NotificationStatusDelivering, "Waiting for webhook confirmation", nil, &task.RetryCount)
	if err != nil {
		log.Error("Failed to update notification status to delivering", zap.Error(err))
	}

	log.Info("Email sent successfully", zap.String("notification_id", task.NotificationID))
	return nil
}

// HandleCampaignMessage processes a CampaignTask from the campaign.email queue.
// It sends via SMTP, then inserts an audit notification record and updates recipient status
// AFTER the send result is known — avoiding pending-forever records on SMTP timeout.
func (w *EmailWorker) HandleCampaignMessage(ctx context.Context, body []byte) error {
	log := logger.L()
	var task dto.CampaignTask
	if err := json.Unmarshal(body, &task); err != nil {
		log.Error("failed to unmarshal campaign task", zap.Error(err))
		return err
	}

	log.Info("sending campaign email",
		zap.String("campaign_id", task.CampaignID),
		zap.String("user_id", task.UserID),
		zap.String("recipient", task.Recipient),
	)

	// Use "campaign:<campaignID>:<userID>" as the X-Notification-ID header so Mailpit
	// webhooks can be linked back even without a pre-existing notifications row.
	emailID := fmt.Sprintf("campaign:%s:%s", task.CampaignID, task.UserID)
	sendErr := w.sendEmail(ctx, emailID, task.Recipient, task.Subject, task.Content)

	if errors.Is(sendErr, ErrCircuitBreakerOpen) {
		log.Warn("circuit breaker open, NACK campaign message",
			zap.String("campaign_id", task.CampaignID))
		return sendErr
	}

	campaignID, _ := uuid.Parse(task.CampaignID)

	if sendErr != nil {
		log.Error("failed to send campaign email",
			zap.String("campaign_id", task.CampaignID),
			zap.String("user_id", task.UserID),
			zap.Error(sendErr),
		)
		w.insertCampaignAudit(ctx, task, domain.NotificationStatusFailed, sendErr.Error())
		_ = w.campaignRepo.UpdateRecipientStatus(ctx, campaignID, task.UserID, domain.CampaignRecipientStatusFailed)
		_ = w.campaignRepo.IncrementCampaignCounter(ctx, campaignID, "failed_count")
		return sendErr
	}

	now := time.Now().UTC()
	w.insertCampaignAudit(ctx, task, domain.NotificationStatusSent, "")
	_ = w.campaignRepo.UpdateRecipientStatus(ctx, campaignID, task.UserID, domain.CampaignRecipientStatusSent)
	_ = w.campaignRepo.IncrementCampaignCounter(ctx, campaignID, "sent_count")

	log.Info("campaign email sent",
		zap.String("campaign_id", task.CampaignID),
		zap.String("user_id", task.UserID),
		zap.Time("sent_at", now),
	)
	return nil
}

// insertCampaignAudit writes a post-send audit record to the notifications table.
// Failures here are logged but not propagated — the audit log is best-effort.
func (w *EmailWorker) insertCampaignAudit(ctx context.Context, task dto.CampaignTask, status domain.NotificationStatus, errMsg string) {
	log := logger.L()
	now := time.Now().UTC()
	var sentAt *time.Time
	if status == domain.NotificationStatusSent {
		sentAt = &now
	}
	notification := &domain.Notification{
		EventType: task.EventType,
		UserID:    task.UserID,
		Channel:   task.Channel,
		Recipient: task.Recipient,
		Subject:   task.Subject,
		Content:   task.Content,
		Status:    status,
		SentAt:    sentAt,
		ErrorMessage: errMsg,
		Metadata:  fmt.Sprintf(`{"campaign_id":"%s"}`, task.CampaignID),
	}
	if _, err := w.repo.Create(ctx, notification); err != nil {
		log.Error("failed to insert campaign audit notification",
			zap.String("campaign_id", task.CampaignID),
			zap.Error(err),
		)
	}
}

func retryDelay(retryCount int) time.Duration {
	delays := []time.Duration{
		1 * time.Minute,
		5 * time.Minute,
		15 * time.Minute,
	}
	if retryCount < len(delays) {
		return delays[retryCount]
	}
	return delays[len(delays)-1]
}

func (w *EmailWorker) sendEmail(ctx context.Context, id, to, subject, body string) error {
	if to == "" {
		return fmt.Errorf("recipient email address is empty")
	}

	// Span đo lường chính xác thời gian kết nối và gửi qua SMTP
	tracer := otel.Tracer("notification-service")
	_, span := tracer.Start(ctx, "worker.email.send_smtp")
	defer span.End()
	span.SetAttributes(
		attribute.String("smtp.recipient", to),
		attribute.String("smtp.host", w.cfg.SMTP.Host),
	)

	return w.circuitBreaker.Execute(func() error {
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
	})
}
