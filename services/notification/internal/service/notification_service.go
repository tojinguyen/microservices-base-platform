package service

import (
	"context"
	"encoding/json"

	"bytes"
	"fmt"
	"text/template"

	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
)

type NotificationService interface {
	ProcessEvent(ctx context.Context, event dto.SendNotificationRequest) error
}

type notificationService struct {
	repo         repository.NotificationRepository
	templateRepo repository.TemplateRepository
}

func NewNotificationService(repo repository.NotificationRepository, templateRepo repository.TemplateRepository) NotificationService {
	return &notificationService{
		repo:         repo,
		templateRepo: templateRepo,
	}
}

func (s *notificationService) ProcessEvent(ctx context.Context, event dto.SendNotificationRequest) error {
	// 1. Get template correspond with event type
	tmpl, err := s.templateRepo.GetByEventType(ctx, event.EventType)
	if err != nil {
		return fmt.Errorf("failed to get template for event %s: %w", event.EventType, err)
	}

	// 2. Render content from Payload
	title, err := s.render(tmpl.Subject, event.Payload)
	if err != nil {
		return fmt.Errorf("failed to render title: %w", err)
	}

	content, err := s.render(tmpl.Content, event.Payload)
	if err != nil {
		return fmt.Errorf("failed to render content: %w", err)
	}

	// 3. Xử lý Metadata
	var metadataStr string
	if event.Metadata != nil {
		b, err := json.Marshal(event.Metadata)
		if err != nil {
			return err
		}
		metadataStr = string(b)
	}

	// 4. Tạo đối tượng Notification với Channel lấy từ Template
	notification := &domain.Notification{
		EventType: tmpl.EventType,
		UserID:    event.UserID,
		Channel:   tmpl.Channel,
		Status:    domain.NotificationStatusPending,
		Metadata:  metadataStr,
		Subject:   title,
		Content:   content,
	}

	// Lấy recipient từ payload (có thể quy định key cứng hoặc lấy từ User Profile tùy logic sau này)
	if v, ok := event.Payload["recipient"].(string); ok {
		notification.Recipient = v
	}

	if err := s.repo.Create(ctx, notification); err != nil {
		return err
	}
	return nil
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
