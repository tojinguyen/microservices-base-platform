package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tojinguyen/upload/internal/domain"
	"gorm.io/gorm"
)

type UploadSessionRepository interface {
	CreateSession(ctx context.Context, session *domain.UploadSession, parts []domain.UploadPart) error
	GetSessionByID(ctx context.Context, id uuid.UUID) (*domain.UploadSession, error)
	GetSessionWithParts(ctx context.Context, id uuid.UUID) (*domain.UploadSession, error)
	UpdateSession(ctx context.Context, session *domain.UploadSession) error
	GetPartByNumber(ctx context.Context, sessionID uuid.UUID, partNumber int) (*domain.UploadPart, error)
	UpdatePart(ctx context.Context, part *domain.UploadPart) error
	GetUploadedParts(ctx context.Context, sessionID uuid.UUID) ([]domain.UploadPart, error)
	ListExpiredActiveSessions(ctx context.Context, before time.Time, limit int) ([]domain.UploadSession, error)
	TransitionSessionStatus(ctx context.Context, id uuid.UUID, from, to domain.SessionStatus) (bool, error)
}

type uploadSessionRepository struct {
	db *gorm.DB
}

func NewUploadSessionRepository(db *gorm.DB) UploadSessionRepository {
	return &uploadSessionRepository{db: db}
}

func (r *uploadSessionRepository) CreateSession(ctx context.Context, session *domain.UploadSession, parts []domain.UploadPart) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(session).Error; err != nil {
			return err
		}
		if len(parts) > 0 {
			if err := tx.Create(&parts).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *uploadSessionRepository) GetSessionByID(ctx context.Context, id uuid.UUID) (*domain.UploadSession, error) {
	var session domain.UploadSession
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *uploadSessionRepository) GetSessionWithParts(ctx context.Context, id uuid.UUID) (*domain.UploadSession, error) {
	var session domain.UploadSession
	if err := r.db.WithContext(ctx).
		Preload("Parts", func(db *gorm.DB) *gorm.DB {
			return db.Order("part_number ASC")
		}).
		Where("id = ?", id).
		First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *uploadSessionRepository) UpdateSession(ctx context.Context, session *domain.UploadSession) error {
	return r.db.WithContext(ctx).Save(session).Error
}

func (r *uploadSessionRepository) GetPartByNumber(ctx context.Context, sessionID uuid.UUID, partNumber int) (*domain.UploadPart, error) {
	var part domain.UploadPart
	if err := r.db.WithContext(ctx).
		Where("session_id = ? AND part_number = ?", sessionID, partNumber).
		First(&part).Error; err != nil {
		return nil, err
	}
	return &part, nil
}

func (r *uploadSessionRepository) UpdatePart(ctx context.Context, part *domain.UploadPart) error {
	return r.db.WithContext(ctx).Save(part).Error
}

func (r *uploadSessionRepository) GetUploadedParts(ctx context.Context, sessionID uuid.UUID) ([]domain.UploadPart, error) {
	var parts []domain.UploadPart
	if err := r.db.WithContext(ctx).
		Where("session_id = ? AND status = ?", sessionID, domain.PartStatusUploaded).
		Order("part_number ASC").
		Find(&parts).Error; err != nil {
		return nil, err
	}
	return parts, nil
}

func (r *uploadSessionRepository) ListExpiredActiveSessions(ctx context.Context, before time.Time, limit int) ([]domain.UploadSession, error) {
	var sessions []domain.UploadSession
	err := r.db.WithContext(ctx).
		Where("status IN ? AND expires_at < ?", []domain.SessionStatus{
			domain.SessionStatusInitiated,
			domain.SessionStatusInProgress,
		}, before).
		Limit(limit).
		Find(&sessions).Error
	return sessions, err
}

// TransitionSessionStatus atomically transitions session status from `from` to `to`.
// Returns true if the row was updated (i.e., the transition succeeded), false if the
// session was already in a different state (concurrent request won the race).
func (r *uploadSessionRepository) TransitionSessionStatus(ctx context.Context, id uuid.UUID, from, to domain.SessionStatus) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&domain.UploadSession{}).
		Where("id = ? AND status = ?", id, from).
		Update("status", to)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}
