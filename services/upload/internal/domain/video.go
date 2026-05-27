package domain

import (
	"time"

	"github.com/google/uuid"
)

type VideoStatus string

const (
	StatusPendingUpload VideoStatus = "pending_upload"
	StatusUploaded      VideoStatus = "uploaded"
	StatusAborted       VideoStatus = "aborted"
	StatusExpired       VideoStatus = "expired"
)

type Video struct {
	ID             uuid.UUID   `gorm:"type:uuid;primaryKey"`
	Title          string      `gorm:"not null"`
	Description    string
	OwnerID        *uuid.UUID  `gorm:"type:uuid"`
	ObjectKey      string      `gorm:"not null;uniqueIndex"`
	Bucket         string      `gorm:"not null"`
	MimeType       string
	SizeBytes      *int64
	Etag           string
	Status         VideoStatus `gorm:"not null;default:'pending_upload'"`
	StorageURL     string
	UploadExpiresAt *time.Time
	UploadedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (Video) TableName() string {
	return "videos"
}
