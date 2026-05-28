package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/tojinguyen/transcoder/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type JobRepository interface {
	Create(ctx context.Context, job *domain.TranscodingJob) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.TranscodingJob, error)
	GetByVideoID(ctx context.Context, videoID uuid.UUID) (*domain.TranscodingJob, error)
	FindOrCreateByVideoID(ctx context.Context, job *domain.TranscodingJob) (*domain.TranscodingJob, bool, error)
	Update(ctx context.Context, job *domain.TranscodingJob) error
}

type jobRepository struct {
	db *gorm.DB
}

func NewJobRepository(db *gorm.DB) JobRepository {
	return &jobRepository{db: db}
}

func (r *jobRepository) Create(ctx context.Context, job *domain.TranscodingJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *jobRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.TranscodingJob, error) {
	var job domain.TranscodingJob
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) GetByVideoID(ctx context.Context, videoID uuid.UUID) (*domain.TranscodingJob, error) {
	var job domain.TranscodingJob
	if err := r.db.WithContext(ctx).Where("video_id = ?", videoID).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

// FindOrCreateByVideoID — idempotent: returns existing job if present, otherwise creates one.
// The boolean reports whether a new job was created.
func (r *jobRepository) FindOrCreateByVideoID(ctx context.Context, job *domain.TranscodingJob) (*domain.TranscodingJob, bool, error) {
	existing, err := r.GetByVideoID(ctx, job.VideoID)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	// Use ON CONFLICT DO NOTHING on video_id to be safe across concurrent inserts.
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "video_id"}},
		DoNothing: true,
	}).Create(job)
	if res.Error != nil {
		return nil, false, res.Error
	}
	if res.RowsAffected == 0 {
		// Someone else inserted it; fetch it back.
		existing, err = r.GetByVideoID(ctx, job.VideoID)
		if err != nil {
			return nil, false, err
		}
		return existing, false, nil
	}
	return job, true, nil
}

func (r *jobRepository) Update(ctx context.Context, job *domain.TranscodingJob) error {
	return r.db.WithContext(ctx).Save(job).Error
}
