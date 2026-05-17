package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/domain"
	"gorm.io/gorm"
)

type OutboxRepository interface {
	// CreateWithTx inserts an outbox event inside a caller-provided transaction.
	CreateWithTx(tx *gorm.DB, event *domain.OutboxEvent) error

	// FetchPending returns up to limit unpublished events ordered by created_at.
	// Uses FOR UPDATE SKIP LOCKED so multiple worker pods never process the same row.
	FetchPending(ctx context.Context, limit int) ([]*domain.OutboxEvent, error)

	// MarkPublished sets published_at and status = published.
	MarkPublished(ctx context.Context, id uuid.UUID) error

	// IncrementRetry increments retry_count and records the error message.
	IncrementRetry(ctx context.Context, id uuid.UUID, errMsg string) error

	// MarkFailed sets status = failed so the row is excluded from future polling.
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error
}

type outboxRepository struct {
	db *gorm.DB
}

func NewOutboxRepository(db *gorm.DB) OutboxRepository {
	return &outboxRepository{db: db}
}

func (r *outboxRepository) CreateWithTx(tx *gorm.DB, event *domain.OutboxEvent) error {
	return tx.Create(event).Error
}

func (r *outboxRepository) FetchPending(ctx context.Context, limit int) ([]*domain.OutboxEvent, error) {
	var events []*domain.OutboxEvent
	err := r.db.WithContext(ctx).Raw(`
		SELECT * FROM outbox_events
		WHERE published_at IS NULL AND status = 'pending'
		ORDER BY created_at ASC
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`, limit).Scan(&events).Error
	return events, err
}

func (r *outboxRepository) MarkPublished(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&domain.OutboxEvent{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":       domain.OutboxStatusPublished,
			"published_at": now,
		}).Error
}

func (r *outboxRepository) IncrementRetry(ctx context.Context, id uuid.UUID, errMsg string) error {
	return r.db.WithContext(ctx).
		Model(&domain.OutboxEvent{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"retry_count": gorm.Expr("retry_count + 1"),
			"last_error":  errMsg,
		}).Error
}

func (r *outboxRepository) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	return r.db.WithContext(ctx).
		Model(&domain.OutboxEvent{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":     domain.OutboxStatusFailed,
			"last_error": errMsg,
		}).Error
}
