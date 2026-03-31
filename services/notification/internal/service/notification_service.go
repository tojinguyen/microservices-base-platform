package service

import (
	appErrors "backend/pkg/errors"
	"backend/pkg/logger"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
	"go.uber.org/zap"
)

type NotificationService interface {
	ProcessEvent(ctx context.Context, event dto.NotificationEvent) error
}

type notificationService struct {
	repo   repository.NotificationRepository
	sender EmailSender
}

func NewNotificationService(repo repository.NotificationRepository, sender EmailSender) NotificationService {
	return &notificationService{
		repo:   repo,
		sender: sender,
	}
}

func (s *notificationService) ProcessEvent(ctx context.Context, event dto.NotificationEvent) error {
	if err := validateEvent(event); err != nil {
		return err
	}

	exists, err := s.repo.ExistsByEventID(ctx, event.EventID)
	if err != nil {
		return err
	}
	if exists {
		logger.FromContext(ctx).Info("notification event already processed", zap.String("event_id", event.EventID))
		return nil
	}

	subject := event.Payload.Subject
	if strings.TrimSpace(subject) == "" {
		subject = fmt.Sprintf("Notification: %s", event.EventType)
	}

	notification := &domain.Notification{
		EventID:        event.EventID,
		EventType:      event.EventType,
		UserID:         event.Payload.UserID,
		Channel:        domain.NotificationChannelEmail,
		RecipientEmail: event.Payload.Email,
		Subject:        subject,
		Content:        event.Payload.Content,
		Status:         domain.NotificationStatusPending,
	}

	if err := s.repo.Create(ctx, notification); err != nil {
		return err
	}

	sendErr := s.sender.Send(ctx, EmailMessage{
		To:      notification.RecipientEmail,
		Subject: notification.Subject,
		Body:    notification.Content,
	})
	if sendErr != nil {
		errorMessage := trimErrorMessage(sendErr.Error(), 1000)
		if err := s.repo.UpdateDeliveryStatus(ctx, notification.Id, domain.NotificationStatusFailed, errorMessage, nil); err != nil {
			return err
		}

		logger.FromContext(ctx).Warn("notification email sending failed",
			zap.String("event_id", notification.EventID),
			zap.String("recipient", notification.RecipientEmail),
			zap.Error(sendErr),
		)
		return nil
	}

	sentAt := time.Now().UTC()
	if err := s.repo.UpdateDeliveryStatus(ctx, notification.Id, domain.NotificationStatusSent, "", &sentAt); err != nil {
		return err
	}

	return nil
}

func validateEvent(event dto.NotificationEvent) error {
	if strings.TrimSpace(event.EventType) == "" {
		return appErrors.BadRequest(nil, "event_type is required")
	}
	if strings.TrimSpace(event.Payload.UserID) == "" {
		return appErrors.BadRequest(nil, "payload.user_id is required")
	}
	if strings.TrimSpace(event.Payload.Email) == "" {
		return appErrors.BadRequest(nil, "payload.email is required")
	}
	if strings.TrimSpace(event.Payload.Content) == "" {
		return appErrors.BadRequest(nil, "payload.content is required")
	}
	return nil
}

func trimErrorMessage(input string, maxLen int) string {
	if maxLen <= 0 || len(input) <= maxLen {
		return input
	}
	return input[:maxLen]
}
