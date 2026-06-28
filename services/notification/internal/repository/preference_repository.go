package repository

import (
	"context"

	"github.com/tojinguyen/notification/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PreferenceRepository interface {
	GetByUserID(ctx context.Context, userID string) ([]*domain.NotificationPreference, error)
	IsEnabled(ctx context.Context, userID string, eventType domain.EventType, channel domain.NotificationChannel) (bool, error)
	Upsert(ctx context.Context, pref *domain.NotificationPreference) (*domain.NotificationPreference, error)
}

type preferenceRepository struct {
	db *gorm.DB
}

func NewPreferenceRepository(db *gorm.DB) PreferenceRepository {
	return &preferenceRepository{db: db}
}

func (r *preferenceRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.NotificationPreference, error) {
	var prefs []*domain.NotificationPreference
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Find(&prefs).Error
	return prefs, err
}

func (r *preferenceRepository) IsEnabled(ctx context.Context, userID string, eventType domain.EventType, channel domain.NotificationChannel) (bool, error) {
	var pref domain.NotificationPreference
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND event_type = ? AND channel = ? AND deleted_at IS NULL", userID, eventType, channel).
		First(&pref).Error
	if err == gorm.ErrRecordNotFound {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return pref.Enabled, nil
}

func (r *preferenceRepository) Upsert(ctx context.Context, pref *domain.NotificationPreference) (*domain.NotificationPreference, error) {
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "event_type"}, {Name: "channel"}},
			DoUpdates: clause.AssignmentColumns([]string{"enabled", "updated_at"}),
		}).
		Create(pref).Error
	return pref, err
}
