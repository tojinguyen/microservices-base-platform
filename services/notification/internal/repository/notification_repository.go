package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/domain"
	"gorm.io/gorm"
)

type NotificationRepository interface {
	Create(ctx context.Context, notification *domain.Notification) (*domain.Notification, error)
	ExistsByEventID(ctx context.Context, eventID string) (bool, error)
	UpdateDeliveryStatus(ctx context.Context, notificationID uuid.UUID, status domain.NotificationStatus, errorMessage string, sentAt *time.Time) error
	ClaimPendingBatch(ctx context.Context, limit int) ([]*domain.Notification, error)
	IncrementRetryAndReset(ctx context.Context, notificationID uuid.UUID, errorMessage string, nextRetryAt time.Time) error
}

type notificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) NotificationRepository {
	return &notificationRepository{db: db}
}

func (r *notificationRepository) Create(ctx context.Context, notification *domain.Notification) (*domain.Notification, error) {
	err := r.db.WithContext(ctx).Create(notification).Error
	return notification, err
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

func (r *notificationRepository) ClaimPendingBatch(ctx context.Context, limit int) ([]*domain.Notification, error) {
	var notifications []*domain.Notification
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`
			SELECT * FROM notifications
			WHERE status = 'pending'
			  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
			ORDER BY created_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		`, limit).Scan(&notifications).Error; err != nil {
			return err
		}

		if len(notifications) == 0 {
			return nil
		}

		ids := make([]string, len(notifications))
		for i, n := range notifications {
			ids[i] = n.Id.String()
		}

		return tx.Model(&domain.Notification{}).
			Where("id IN ?", ids).
			Updates(map[string]interface{}{
				"status":     domain.NotificationStatusProcessing,
				"updated_at": time.Now().UTC(),
			}).Error
	})
	return notifications, err
}

func (r *notificationRepository) IncrementRetryAndReset(ctx context.Context, notificationID uuid.UUID, errorMessage string, nextRetryAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&domain.Notification{}).
		Where("id = ?", notificationID).
		Updates(map[string]interface{}{
			"status":        domain.NotificationStatusPending,
			"retry_count":   gorm.Expr("retry_count + 1"),
			"error_message": errorMessage,
			"next_retry_at": nextRetryAt,
			"updated_at":    time.Now().UTC(),
		}).Error
}
