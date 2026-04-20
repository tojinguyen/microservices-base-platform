package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"text/template"
	"time"

	"backend/pkg/broker"
	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
)

type NotificationService interface {
	ProcessEvent(ctx context.Context, event dto.SendNotificationRequest) error
	SeedTemplates(ctx context.Context) error
	UpdateStatus(ctx context.Context, notificationID string, status domain.NotificationStatus, errorMessage string, sentAt *time.Time) error
	HandleMailpitWebhook(ctx context.Context, webhook dto.MailpitWebhook) error
	PublishWebhookResponse(ctx context.Context, webhook dto.MailpitWebhook) error
}

type notificationService struct {
	repo         repository.NotificationRepository
	templateRepo repository.TemplateRepository
	broker       broker.Broker
	cfg          *config.Config
}

func NewNotificationService(repo repository.NotificationRepository, templateRepo repository.TemplateRepository, broker broker.Broker, cfg *config.Config) NotificationService {
	return &notificationService{
		repo:         repo,
		templateRepo: templateRepo,
		broker:       broker,
		cfg:          cfg,
	}
}

func (s *notificationService) ProcessEvent(ctx context.Context, event dto.SendNotificationRequest) error {
	tmpl, err := s.templateRepo.GetByEventType(ctx, event.EventType)
	if err != nil {
		return fmt.Errorf("failed to get template for event %s: %w", event.EventType, err)
	}

	title, err := s.render(tmpl.Subject, event.Payload)
	if err != nil {
		return fmt.Errorf("failed to render title: %w", err)
	}

	content, err := s.render(tmpl.Content, event.Payload)
	if err != nil {
		return fmt.Errorf("failed to render content: %w", err)
	}

	var metadataStr string
	if event.Metadata != nil {
		b, err := json.Marshal(event.Metadata)
		if err != nil {
			return err
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

	if err := s.repo.Create(ctx, notification); err != nil {
		return err
	}
	return nil
}

func (s *notificationService) UpdateStatus(ctx context.Context, notificationID string, status domain.NotificationStatus, errorMessage string, sentAt *time.Time) error {
	id, err := uuid.Parse(notificationID)
	if err != nil {
		return err
	}
	return s.repo.UpdateDeliveryStatus(ctx, id, status, errorMessage, sentAt)
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
