package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tojinguyen/upload/internal/domain"
	"gorm.io/gorm"
)

type VideoRepository interface {
	Create(ctx context.Context, video *domain.Video) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Video, error)
	Update(ctx context.Context, video *domain.Video) error
	List(ctx context.Context, status string, page, pageSize int) ([]domain.Video, int64, error)
	ListExpiredPending(ctx context.Context, before time.Time, limit int) ([]domain.Video, error)
}

type videoRepository struct {
	db *gorm.DB
}

func NewVideoRepository(db *gorm.DB) VideoRepository {
	return &videoRepository{db: db}
}

func (r *videoRepository) Create(ctx context.Context, video *domain.Video) error {
	return r.db.WithContext(ctx).Create(video).Error
}

func (r *videoRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Video, error) {
	var video domain.Video
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&video).Error; err != nil {
		return nil, err
	}
	return &video, nil
}

func (r *videoRepository) Update(ctx context.Context, video *domain.Video) error {
	return r.db.WithContext(ctx).Save(video).Error
}

func (r *videoRepository) List(ctx context.Context, status string, page, pageSize int) ([]domain.Video, int64, error) {
	query := r.db.WithContext(ctx).Model(&domain.Video{})
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var videos []domain.Video
	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&videos).Error; err != nil {
		return nil, 0, err
	}
	return videos, total, nil
}

func (r *videoRepository) ListExpiredPending(ctx context.Context, before time.Time, limit int) ([]domain.Video, error) {
	var videos []domain.Video
	err := r.db.WithContext(ctx).
		Where("status = ? AND upload_expires_at < ?", domain.StatusPendingUpload, before).
		Limit(limit).
		Find(&videos).Error
	return videos, err
}
