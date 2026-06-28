package domain

import (
	"encoding/json"

	"github.com/google/uuid"
)

type DLQStatus string

const (
	DLQStatusPending  DLQStatus = "pending"
	DLQStatusReplayed DLQStatus = "replayed"
	DLQStatusIgnored  DLQStatus = "ignored"
)

type DLQMessage struct {
	BaseModel

	NotificationID *uuid.UUID      `gorm:"type:uuid" json:"notification_id"`
	QueueName      string          `gorm:"size:100;not null" json:"queue_name"`
	Payload        json.RawMessage `gorm:"type:jsonb;not null" json:"payload"`
	ErrorMessage   string          `gorm:"type:text;not null" json:"error_message"`
	Status         DLQStatus       `gorm:"size:20;not null;default:pending" json:"status"`
}
