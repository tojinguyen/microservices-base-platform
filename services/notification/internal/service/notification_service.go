package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"text/template"
	"time"

	"backend/pkg/broker"
	"backend/pkg/logger"
	"backend/pkg/trace"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type NotificationService interface {
	CreateNotification(ctx context.Context, event dto.SendNotificationRequest) (dto.SendNotificationResponse, error)
	CreateNotificationWithTemplate(ctx context.Context, event dto.SendNotificationRequest, tmpl *domain.NotificationTemplate) (dto.SendNotificationResponse, error)
	ScheduleNotification(ctx context.Context, req dto.ScheduleNotificationRequest) (dto.ScheduleNotificationResponse, error)
	SeedTemplates(ctx context.Context) error
	UpdateStatus(ctx context.Context, notificationID string, status domain.NotificationStatus, errorMessage string, sentAt *time.Time) error
	HandleMailpitWebhook(ctx context.Context, webhook dto.MailpitWebhook) error
	PublishWebhookResponse(ctx context.Context, webhook dto.MailpitWebhook) error
	ListNotifications(ctx context.Context, req dto.ListNotificationsRequest) (dto.ListNotificationsResponse, error)
}

type notificationService struct {
	db           *gorm.DB
	repo         repository.NotificationRepository
	outboxRepo   repository.OutboxRepository
	templateRepo repository.TemplateRepository
	prefSvc      PreferenceService
	broker       broker.Broker
	cfg          *config.Config
}

func NewNotificationService(
	db *gorm.DB,
	repo repository.NotificationRepository,
	outboxRepo repository.OutboxRepository,
	templateRepo repository.TemplateRepository,
	prefSvc PreferenceService,
	broker broker.Broker,
	cfg *config.Config,
) NotificationService {
	return &notificationService{
		db:           db,
		repo:         repo,
		outboxRepo:   outboxRepo,
		templateRepo: templateRepo,
		prefSvc:      prefSvc,
		broker:       broker,
		cfg:          cfg,
	}
}

func (s *notificationService) CreateNotification(ctx context.Context, event dto.SendNotificationRequest) (dto.SendNotificationResponse, error) {
	tmpl, err := s.templateRepo.GetByEventType(ctx, event.EventType)
	if err != nil {
		logger.L().Error("Failed to get template for event type", zap.String("event_type", string(event.EventType)), zap.Error(err))
		return dto.SendNotificationResponse{}, fmt.Errorf("failed to get template for event %s: %w", event.EventType, err)
	}
	return s.createFromTemplate(ctx, event, tmpl)
}

func (s *notificationService) CreateNotificationWithTemplate(ctx context.Context, event dto.SendNotificationRequest, tmpl *domain.NotificationTemplate) (dto.SendNotificationResponse, error) {
	return s.createFromTemplate(ctx, event, tmpl)
}

func (s *notificationService) createFromTemplate(ctx context.Context, event dto.SendNotificationRequest, tmpl *domain.NotificationTemplate) (dto.SendNotificationResponse, error) {
	tracer := otel.Tracer("notification-service")
	ctx, span := tracer.Start(ctx, "service.create_notification")
	defer span.End()

	if s.prefSvc != nil {
		enabled, err := s.prefSvc.IsNotificationEnabled(ctx, event.UserID, tmpl.EventType, tmpl.Channel)
		if err != nil {
			logger.L().Warn("preference check failed, delivering anyway",
				zap.String("user_id", event.UserID),
				zap.String("event_type", string(event.EventType)),
				zap.Error(err),
			)
		} else if !enabled {
			return dto.SendNotificationResponse{Message: "notification skipped - user opted out"}, nil
		}
	}

	title, err := s.render(tmpl.Subject, event.Payload)
	if err != nil {
		logger.L().Error("Failed to render title for notification", zap.String("event_type", string(event.EventType)), zap.Error(err))
		return dto.SendNotificationResponse{}, fmt.Errorf("failed to render title: %w", err)
	}

	content, err := s.render(tmpl.Content, event.Payload)
	if err != nil {
		logger.L().Error("Failed to render content for notification", zap.String("event_type", string(event.EventType)), zap.Error(err))
		return dto.SendNotificationResponse{}, fmt.Errorf("failed to render content: %w", err)
	}

	var metadataStr string
	if event.Metadata != nil {
		b, err := json.Marshal(event.Metadata)
		if err != nil {
			logger.L().Error("Failed to marshal metadata for notification", zap.String("event_type", string(event.EventType)), zap.Error(err))
			return dto.SendNotificationResponse{}, fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataStr = string(b)
	}

	notification := &domain.Notification{
		EventType: tmpl.EventType,
		UserID:    event.UserID,
		Channel:   tmpl.Channel,
		Status:    domain.NotificationStatusPending,
		Metadata:  metadataStr,
		Subject:   title,
		Content:   content,
	}

	if v, ok := event.Payload["recipient"].(string); ok {
		notification.Recipient = v
	}

	if tmpl.Channel == domain.ChannelEmail {
		if notification.Recipient == "" {
			return dto.SendNotificationResponse{}, fmt.Errorf("recipient is required for email channel")
		}
		if _, err := mail.ParseAddress(notification.Recipient); err != nil {
			return dto.SendNotificationResponse{}, fmt.Errorf("invalid recipient email address %q: %w", notification.Recipient, err)
		}
	}

	createdNotification, err := s.createWithOutbox(ctx, notification)
	if err != nil {
		return dto.SendNotificationResponse{}, err
	}

	return dto.SendNotificationResponse{
		NotificationID: createdNotification.Id.String(),
		Message:        "notification sent successfully",
	}, nil
}

// createWithOutbox writes the notification and its outbox event atomically in one transaction.
// The outbox worker polls outbox_events and publishes to RabbitMQ, guaranteeing delivery
// even if the service crashes after the DB commit.
func (s *notificationService) createWithOutbox(ctx context.Context, notification *domain.Notification) (*domain.Notification, error) {
	tracer := otel.Tracer("notification-service")
	ctx, span := tracer.Start(ctx, "service.create_with_outbox")
	defer span.End()

	var created *domain.Notification

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		created, txErr = s.repo.CreateWithTx(tx, notification)
		if txErr != nil {
			return txErr
		}

		// Inject trace context vào payload để Outbox Worker có thể khôi phục và tiếp tục chuỗi trace
		traceCtx := trace.InjectMap(ctx)

		payload, txErr := json.Marshal(map[string]any{
			"notification_id": created.Id,
			"user_id":         created.UserID,
			"event_type":      created.EventType,
			"channel":         created.Channel,
			"recipient":       created.Recipient,
			"subject":         created.Subject,
			"content":         created.Content,
			"retry_count":     created.RetryCount,
			// event_id is the notification ID — used by consumers for deduplication
			"event_id": created.Id,
			// trace_context carries the W3C traceparent for distributed tracing across async boundaries
			"trace_context": traceCtx,
		})
		if txErr != nil {
			return txErr
		}

		outboxEvent := &domain.OutboxEvent{
			AggregateID:   created.Id,
			AggregateType: "notification",
			EventType:     "notification.created",
			Payload:       payload,
			RoutingKey:    string(created.Channel),
			Status:        domain.OutboxStatusPending,
		}
		return s.outboxRepo.CreateWithTx(tx, outboxEvent)
	})

	return created, err
}

func (s *notificationService) ScheduleNotification(ctx context.Context, req dto.ScheduleNotificationRequest) (dto.ScheduleNotificationResponse, error) {
	if !req.ScheduledAt.After(time.Now().UTC()) {
		return dto.ScheduleNotificationResponse{}, fmt.Errorf("scheduled_at must be in the future")
	}
	if req.ScheduledAt.After(time.Now().UTC().AddDate(1, 0, 0)) {
		return dto.ScheduleNotificationResponse{}, fmt.Errorf("scheduled_at cannot be more than 1 year in the future")
	}

	tmpl, err := s.templateRepo.GetByEventType(ctx, req.EventType)
	if err != nil {
		return dto.ScheduleNotificationResponse{}, fmt.Errorf("failed to get template for event %s: %w", req.EventType, err)
	}

	if s.prefSvc != nil {
		enabled, err := s.prefSvc.IsNotificationEnabled(ctx, req.UserID, tmpl.EventType, tmpl.Channel)
		if err != nil {
			logger.L().Warn("preference check failed, scheduling anyway",
				zap.String("user_id", req.UserID),
				zap.String("event_type", string(req.EventType)),
				zap.Error(err),
			)
		} else if !enabled {
			return dto.ScheduleNotificationResponse{Message: "notification skipped - user opted out"}, nil
		}
	}

	title, err := s.render(tmpl.Subject, req.Payload)
	if err != nil {
		return dto.ScheduleNotificationResponse{}, fmt.Errorf("failed to render title: %w", err)
	}

	content, err := s.render(tmpl.Content, req.Payload)
	if err != nil {
		return dto.ScheduleNotificationResponse{}, fmt.Errorf("failed to render content: %w", err)
	}

	var metadataStr string
	if req.Metadata != nil {
		b, err := json.Marshal(req.Metadata)
		if err != nil {
			return dto.ScheduleNotificationResponse{}, fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataStr = string(b)
	}

	notification := &domain.Notification{
		EventType:   tmpl.EventType,
		UserID:      req.UserID,
		Channel:     tmpl.Channel,
		Status:      domain.NotificationStatusPending,
		Metadata:    metadataStr,
		Subject:     title,
		Content:     content,
		ScheduledAt: req.ScheduledAt.UTC(),
	}

	if v, ok := req.Payload["recipient"].(string); ok {
		notification.Recipient = v
	}

	if tmpl.Channel == domain.ChannelEmail {
		if notification.Recipient == "" {
			return dto.ScheduleNotificationResponse{}, fmt.Errorf("recipient is required for email channel")
		}
		if _, err := mail.ParseAddress(notification.Recipient); err != nil {
			return dto.ScheduleNotificationResponse{}, fmt.Errorf("invalid recipient email address %q: %w", notification.Recipient, err)
		}
	}

	created, err := s.createWithOutbox(ctx, notification)
	if err != nil {
		return dto.ScheduleNotificationResponse{}, err
	}

	logger.L().Info("notification scheduled",
		zap.String("id", created.Id.String()),
		zap.Time("scheduled_at", created.ScheduledAt),
	)

	return dto.ScheduleNotificationResponse{
		NotificationID: created.Id.String(),
		ScheduledAt:    created.ScheduledAt,
		Message:        "notification scheduled successfully",
	}, nil
}

func (s *notificationService) UpdateStatus(ctx context.Context, notificationID string, status domain.NotificationStatus, errorMessage string, sentAt *time.Time) error {
	id, err := uuid.Parse(notificationID)
	if err != nil {
		return err
	}
	return s.repo.UpdateDeliveryStatus(ctx, id, status, errorMessage, sentAt, nil)
}

func (s *notificationService) HandleMailpitWebhook(ctx context.Context, webhook dto.MailpitWebhook) error {
	url := fmt.Sprintf("http://mailpit:8025/api/v1/message/%s", webhook.ID)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch message from mailpit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mailpit returned status %d", resp.StatusCode)
	}

	var msgDetail struct {
		ID      string
		Headers map[string][]string
	}
	if err := json.NewDecoder(resp.Body).Decode(&msgDetail); err != nil {
		return fmt.Errorf("failed to decode mailpit response: %w", err)
	}

	// 2. Extract X-Notification-ID
	notificationIDs := msgDetail.Headers["X-Notification-ID"]
	if len(notificationIDs) == 0 {
		return fmt.Errorf("X-Notification-ID header not found in mailpit message %s", webhook.ID)
	}
	notificationID := notificationIDs[0]

	// 3. Update status to Sent
	now := time.Now().UTC()
	return s.UpdateStatus(ctx, notificationID, domain.NotificationStatusSent, "", &now)
}

func (s *notificationService) PublishWebhookResponse(ctx context.Context, webhook dto.MailpitWebhook) error {
	return s.broker.Publish(ctx, s.cfg.Queue.Exchange, s.cfg.Queue.WebhookMailpit, webhook)
}

type cursorPayload struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

func encodeCursor(createdAt time.Time, id string) string {
	b, _ := json.Marshal(cursorPayload{CreatedAt: createdAt, ID: id})
	return base64.URLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (*cursorPayload, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var p cursorPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *notificationService) ListNotifications(ctx context.Context, req dto.ListNotificationsRequest) (dto.ListNotificationsResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	filter := repository.NotificationFilter{
		UserID:    req.UserID,
		Status:    req.Status,
		Channel:   req.Channel,
		EventType: req.EventType,
		Limit:     req.Limit + 1,
	}

	if req.From != "" {
		t, err := time.Parse(time.RFC3339, req.From)
		if err != nil {
			return dto.ListNotificationsResponse{}, fmt.Errorf("invalid from: %w", err)
		}
		filter.From = &t
	}
	if req.To != "" {
		t, err := time.Parse(time.RFC3339, req.To)
		if err != nil {
			return dto.ListNotificationsResponse{}, fmt.Errorf("invalid to: %w", err)
		}
		filter.To = &t
	}
	if req.Cursor != "" {
		p, err := decodeCursor(req.Cursor)
		if err != nil {
			return dto.ListNotificationsResponse{}, fmt.Errorf("invalid cursor: %w", err)
		}
		id, err := uuid.Parse(p.ID)
		if err != nil {
			return dto.ListNotificationsResponse{}, fmt.Errorf("invalid cursor id: %w", err)
		}
		filter.Cursor = &repository.RepoCursor{CreatedAt: p.CreatedAt, ID: id}
	}

	rows, err := s.repo.List(ctx, filter)
	if err != nil {
		return dto.ListNotificationsResponse{}, err
	}

	hasMore := len(rows) > req.Limit
	if hasMore {
		rows = rows[:req.Limit]
	}

	items := make([]dto.NotificationItem, len(rows))
	for i, n := range rows {
		items[i] = dto.NotificationItem{
			ID:           n.Id.String(),
			EventType:    n.EventType,
			Channel:      n.Channel,
			Status:       n.Status,
			Subject:      n.Subject,
			Recipient:    n.Recipient,
			RetryCount:   n.RetryCount,
			ErrorMessage: n.ErrorMessage,
			SentAt:       n.SentAt,
			CreatedAt:    n.CreatedAt,
		}
	}

	var nextCursor string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = encodeCursor(last.CreatedAt, last.Id.String())
	}

	return dto.ListNotificationsResponse{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (s *notificationService) render(tmplStr string, data interface{}) (string, error) {
	t, err := template.New("notification").Parse(tmplStr)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
