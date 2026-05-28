package domain

import (
	"time"

	"github.com/google/uuid"
)

type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
	StatusDead       JobStatus = "dead"
)

type TranscodingJob struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	VideoID    uuid.UUID `gorm:"type:uuid;not null;index"`
	Status     JobStatus `gorm:"not null;default:'pending'"`
	RetryCount int       `gorm:"not null;default:0"`
	MaxRetries int       `gorm:"not null;default:3"`

	ObjectKey string `gorm:"not null"`
	Bucket    string `gorm:"not null"`
	MimeType  string
	SizeBytes *int64

	HLSBasePath     string
	MasterPlaylist  string
	Renditions      []byte `gorm:"type:jsonb"`
	DurationSeconds *float64
	OutputSizeBytes *int64

	ErrorMessage string
	StartedAt    *time.Time
	CompletedAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (TranscodingJob) TableName() string {
	return "transcoding_jobs"
}
