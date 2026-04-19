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
	SeedTemplates(ctx context.Context) error
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
