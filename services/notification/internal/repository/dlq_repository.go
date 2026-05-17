package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/domain"
	"gorm.io/gorm"
)

type DLQRepository interface {
	Save(ctx context.Context, msg *domain.DLQMessage) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.DLQMessage, error)
	List(ctx context.Context, status string, page, limit int) ([]*domain.DLQMessage, int64, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.DLQStatus) error
}

type dlqRepository struct {
	db *gorm.DB
}

func NewDLQRepository(db *gorm.DB) DLQRepository {
	return &dlqRepository{db: db}
}

func (r *dlqRepository) Save(ctx context.Context, msg *domain.DLQMessage) error {
	return r.db.WithContext(ctx).Create(msg).Error
}

func (r *dlqRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.DLQMessage, error) {
	var msg domain.DLQMessage
	err := r.db.WithContext(ctx).First(&msg, "id = ?", id).Error
	return &msg, err
}

func (r *dlqRepository) List(ctx context.Context, status string, page, limit int) ([]*domain.DLQMessage, int64, error) {
	var msgs []*domain.DLQMessage
	var total int64

	query := r.db.WithContext(ctx).Model(&domain.DLQMessage{})
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&msgs).Error
	return msgs, total, err
}

func (r *dlqRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.DLQStatus) error {
	return r.db.WithContext(ctx).Model(&domain.DLQMessage{}).
		Where("id = ?", id).
		Update("status", status).Error
}
