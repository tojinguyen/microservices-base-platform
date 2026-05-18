package service

import (
	"context"
	"encoding/json"
	"fmt"

	"backend/pkg/broker"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/config"
	"github.com/tojinguyen/notification/internal/domain"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/repository"
)

type DLQService interface {
	ListDLQMessages(ctx context.Context, status string, page, limit int) ([]dto.DLQMessageResponse, int64, error)
	ReplayDLQMessage(ctx context.Context, id string) error
}

type dlqService struct {
	repo             repository.DLQRepository
	notificationRepo repository.NotificationRepository
	broker           broker.Broker
	cfg              *config.Config
}

func NewDLQService(
	repo repository.DLQRepository,
	notificationRepo repository.NotificationRepository,
	broker broker.Broker,
	cfg *config.Config,
) DLQService {
	return &dlqService{
		repo:             repo,
		notificationRepo: notificationRepo,
		broker:           broker,
		cfg:              cfg,
	}
}

func (s *dlqService) ListDLQMessages(ctx context.Context, status string, page, limit int) ([]dto.DLQMessageResponse, int64, error) {
	msgs, total, err := s.repo.List(ctx, status, page, limit)
	if err != nil {
		return nil, 0, err
	}

	resp := make([]dto.DLQMessageResponse, len(msgs))
	for i, m := range msgs {
		notiID := ""
		if m.NotificationID != nil {
			notiID = m.NotificationID.String()
		}
		resp[i] = dto.DLQMessageResponse{
			ID:             m.Id.String(),
			NotificationID: notiID,
			QueueName:      m.QueueName,
			Payload:        string(m.Payload),
			ErrorMessage:   m.ErrorMessage,
			Status:         string(m.Status),
			CreatedAt:      m.CreatedAt,
		}
	}

	return resp, total, nil
}

func (s *dlqService) ReplayDLQMessage(ctx context.Context, idStr string) error {
	id, err := uuid.Parse(idStr)
	if err != nil {
		return fmt.Errorf("invalid message id: %w", err)
	}

	msg, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("dlq message not found: %w", err)
	}

	if msg.Status == domain.DLQStatusReplayed {
		return fmt.Errorf("message has already been replayed")
	}

	// 1. Parse payload
	var task dto.NotificationTask
	if err := json.Unmarshal(msg.Payload, &task); err != nil {
		return fmt.Errorf("failed to parse payload: %w", err)
	}

	// 2. Reset RetryCount to 0 to restart the retry lifecycle
	task.RetryCount = 0

	// 3. Re-publish back to the main exchange of RabbitMQ
	routingKey := msg.QueueName // 'email' or 'webhook'
	if err := s.broker.Publish(ctx, s.cfg.Queue.Exchange, routingKey, task); err != nil {
		return fmt.Errorf("failed to republish message: %w", err)
	}

	// 4. Update status in GORM dlq_messages table
	if err := s.repo.UpdateStatus(ctx, id, domain.DLQStatusReplayed); err != nil {
		return fmt.Errorf("failed to update status to replayed: %w", err)
	}

	// 5. Update original GORM notifications status back to pending/delivering
	if msg.NotificationID != nil {
		_ = s.notificationRepo.UpdateDeliveryStatus(ctx, *msg.NotificationID, domain.NotificationStatusPending, "Replayed from DLQ", nil, nil)
	}

	return nil
}
