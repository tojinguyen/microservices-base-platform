package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tojinguyen/notification/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ScheduleRepository interface {
	Upsert(ctx context.Context, schedule *domain.UserNotificationSchedule) (*domain.UserNotificationSchedule, error)
	GetByUserID(ctx context.Context, userID string) ([]*domain.UserNotificationSchedule, error)
	// FindDueCandidates returns enabled schedules not sent in the last 20 hours.
	// Exact timezone/time-of-day filtering is done in the caller.
	FindDueCandidates(ctx context.Context) ([]*domain.UserNotificationSchedule, error)
	UpdateLastSentAt(ctx context.Context, id uuid.UUID, sentAt time.Time) error
	Delete(ctx context.Context, userID string, eventType domain.EventType) error
}

type scheduleRepository struct {
	db *gorm.DB
}

func NewScheduleRepository(db *gorm.DB) ScheduleRepository {
	return &scheduleRepository{db: db}
}

func (r *scheduleRepository) Upsert(ctx context.Context, s *domain.UserNotificationSchedule) (*domain.UserNotificationSchedule, error) {
	err := r.db.WithContext(ctx).
		Where(domain.UserNotificationSchedule{UserID: s.UserID, EventType: s.EventType}).
		Assign(domain.UserNotificationSchedule{
			SendTime: s.SendTime,
			Timezone: s.Timezone,
			Enabled:  s.Enabled,
			Payload:  s.Payload,
		}).
		FirstOrCreate(s).Error
	if err != nil {
		return nil, err
	}

	// If record already existed, update the mutable fields.
	err = r.db.WithContext(ctx).Model(s).
		Omit(clause.Associations).
		Updates(map[string]interface{}{
			"send_time":  s.SendTime,
			"timezone":   s.Timezone,
			"enabled":    s.Enabled,
			"payload":    s.Payload,
			"updated_at": time.Now().UTC(),
		}).Error
	return s, err
}

func (r *scheduleRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.UserNotificationSchedule, error) {
	var schedules []*domain.UserNotificationSchedule
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Find(&schedules).Error
	return schedules, err
}

func (r *scheduleRepository) FindDueCandidates(ctx context.Context) ([]*domain.UserNotificationSchedule, error) {
	var schedules []*domain.UserNotificationSchedule
	err := r.db.WithContext(ctx).
		Where("enabled = true AND (last_sent_at IS NULL OR last_sent_at < ?)", time.Now().UTC().Add(-20*time.Hour)).
		Find(&schedules).Error
	return schedules, err
}

func (r *scheduleRepository) UpdateLastSentAt(ctx context.Context, id uuid.UUID, sentAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&domain.UserNotificationSchedule{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"last_sent_at": sentAt,
			"updated_at":   time.Now().UTC(),
		}).Error
}

func (r *scheduleRepository) Delete(ctx context.Context, userID string, eventType domain.EventType) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND event_type = ?", userID, eventType).
		Delete(&domain.UserNotificationSchedule{}).Error
}
