package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/domain"
	"gorm.io/gorm"
)

// NotificationFilter holds all optional filters for List queries.
type NotificationFilter struct {
	UserID    string
	Status    domain.NotificationStatus
	Channel   domain.NotificationChannel
	EventType domain.EventType
	From      *time.Time
	To        *time.Time
	Cursor    *RepoCursor
	Limit     int
}

// RepoCursor encodes the position of the last seen item for cursor pagination.
type RepoCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type NotificationRepository interface {
	// Create inserts a notification using its own DB connection.
	Create(ctx context.Context, notification *domain.Notification) (*domain.Notification, error)
	// CreateWithTx inserts a notification inside a caller-provided transaction.
	CreateWithTx(tx *gorm.DB, notification *domain.Notification) (*domain.Notification, error)
	ExistsByEventID(ctx context.Context, eventID string) (bool, error)
	UpdateDeliveryStatus(ctx context.Context, notificationID uuid.UUID, status domain.NotificationStatus, errorMessage string, sentAt *time.Time, retryCount *int) error
	ClaimPendingBatch(ctx context.Context, limit int) ([]*domain.Notification, error)
	IncrementRetryAndReset(ctx context.Context, notificationID uuid.UUID, errorMessage string, nextRetryAt time.Time) error
	List(ctx context.Context, filter NotificationFilter) ([]*domain.Notification, error)
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

func (r *notificationRepository) CreateWithTx(tx *gorm.DB, notification *domain.Notification) (*domain.Notification, error) {
	err := tx.Create(notification).Error
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

func (r *notificationRepository) UpdateDeliveryStatus(ctx context.Context, notificationID uuid.UUID, status domain.NotificationStatus, errorMessage string, sentAt *time.Time, retryCount *int) error {
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

	if retryCount != nil {
		updates["retry_count"] = *retryCount
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
			  AND scheduled_at <= NOW()
			  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
			ORDER BY scheduled_at ASC, created_at ASC
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

func (r *notificationRepository) List(ctx context.Context, filter NotificationFilter) ([]*domain.Notification, error) {
	db := r.db.WithContext(ctx).Model(&domain.Notification{}).
		Where("user_id = ?", filter.UserID)

	if filter.Status != "" {
		db = db.Where("status = ?", filter.Status)
	}
	if filter.Channel != "" {
		db = db.Where("channel = ?", filter.Channel)
	}
	if filter.EventType != "" {
		db = db.Where("event_type = ?", filter.EventType)
	}
	if filter.From != nil {
		db = db.Where("created_at >= ?", filter.From)
	}
	if filter.To != nil {
		db = db.Where("created_at <= ?", filter.To)
	}
	if filter.Cursor != nil {
		db = db.Where("(created_at, id) < (?, ?)", filter.Cursor.CreatedAt, filter.Cursor.ID)
	}

	var results []*domain.Notification
	err := db.Order("created_at DESC, id DESC").Limit(filter.Limit).Find(&results).Error
	return results, err
}
