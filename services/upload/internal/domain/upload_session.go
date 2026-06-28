package domain

import (
	"time"

	"github.com/google/uuid"
)

type SessionStatus string

const (
	SessionStatusInitiated  SessionStatus = "initiated"
	SessionStatusInProgress SessionStatus = "in_progress"
	SessionStatusCompleting SessionStatus = "completing"
	SessionStatusCompleted  SessionStatus = "completed"
	SessionStatusAborted    SessionStatus = "aborted"
	SessionStatusExpired    SessionStatus = "expired"
)

type PartStatus string

const (
	PartStatusPending  PartStatus = "pending"
	PartStatusUploaded PartStatus = "uploaded"
)

type UploadSession struct {
	ID              uuid.UUID     `gorm:"type:uuid;primaryKey"`
	VideoID         uuid.UUID     `gorm:"type:uuid;not null"`
	S3UploadID      string        `gorm:"column:s3_upload_id;not null"`
	ObjectKey       string        `gorm:"not null"`
	Bucket          string        `gorm:"not null"`
	MimeType        string
	TotalParts      int           `gorm:"not null"`
	PartSizeBytes   int64         `gorm:"not null"`
	TotalSizeBytes  int64         `gorm:"not null"`
	Status          SessionStatus `gorm:"not null;default:'initiated'"`
	ExpiresAt       time.Time     `gorm:"not null"`
	CompletedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time

	Parts []UploadPart `gorm:"foreignKey:SessionID"`
}

func (UploadSession) TableName() string { return "upload_sessions" }

type UploadPart struct {
	ID               uuid.UUID  `gorm:"type:uuid;primaryKey"`
	SessionID        uuid.UUID  `gorm:"type:uuid;not null"`
	PartNumber       int        `gorm:"not null"`
	Etag             string
	SizeBytes        *int64
	Status           PartStatus `gorm:"not null;default:'pending'"`
	PresignURL       string     `gorm:"column:presign_url"`
	PresignExpiresAt *time.Time
	UploadedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (UploadPart) TableName() string { return "upload_parts" }
