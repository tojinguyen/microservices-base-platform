package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/domain"
	"gorm.io/gorm"
)

type NotificationRepository interface {
	Create(ctx context.Context, notification *domain.Notification) error
	ExistsByEventID(ctx context.Context, eventID string) (bool, error)
	UpdateDeliveryStatus(ctx context.Context, notificationID uuid.UUID, status domain.NotificationStatus, errorMessage string, sentAt *time.Time) error
	GetPending(ctx context.Context, limit int) ([]*domain.Notification, error)
}

type notificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) NotificationRepository {
	return &notificationRepository{db: db}
}

func (r *notificationRepository) Create(ctx context.Context, notification *domain.Notification) error {
	return r.db.WithContext(ctx).Create(notification).Error
}

func (r *notificationRepository) ExistsByEventID(ctx context.Context, eventID string) (bool, error) {
	if eventID == "" {
		return false, nil
	}

	var count int64
	err := r.db.WithContext(ctx).Model(&domain.Notification{}).Where("event_id = ?", eventID).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *notificationRepository) UpdateDeliveryStatus(ctx context.Context, notificationID uuid.UUID, status domain.NotificationStatus, errorMessage string, sentAt *time.Time) error {
	updates := map[string]interface{}{
		"status":        status,
		"updated_at":    time.Now().UTC(),
		"error_message": nil,
		"sent_at":       nil,
	}

	if errorMessage != "" {
		updates["error_message"] = errorMessage
	}

	if sentAt != nil {
		updates["sent_at"] = *sentAt
	}

	return r.db.WithContext(ctx).
		Model(&domain.Notification{}).
		Where("id = ?", notificationID).
		Updates(updates).Error
}

func (r *notificationRepository) GetPending(ctx context.Context, limit int) ([]*domain.Notification, error) {
	var notifications []*domain.Notification
	err := r.db.WithContext(ctx).
		Where("status = ?", domain.NotificationStatusPending).
		Order("created_at asc").
		Limit(limit).
		Find(&notifications).Error
	return notifications, err
}
