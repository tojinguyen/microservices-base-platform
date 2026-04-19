package repository

import (
	"context"

	"github.com/tojinguyen/notification/internal/domain"
	"gorm.io/gorm"
)

type TemplateRepository interface {
	GetByEventType(ctx context.Context, eventType domain.EventType) (*domain.NotificationTemplate, error)
}

type templateRepository struct {
	db *gorm.DB
}

func NewTemplateRepository(db *gorm.DB) TemplateRepository {
	return &templateRepository{db: db}
}

func (r *templateRepository) GetByEventType(ctx context.Context, eventType domain.EventType) (*domain.NotificationTemplate, error) {
	var template domain.NotificationTemplate
	err := r.db.WithContext(ctx).Where("event_type = ?", eventType).First(&template).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}
