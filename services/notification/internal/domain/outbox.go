package domain

import (
	"time"

	"github.com/google/uuid"
)

type OutboxStatus string

const (
	OutboxStatusPending    OutboxStatus = "pending"
	OutboxStatusProcessing OutboxStatus = "processing"
	OutboxStatusPublished  OutboxStatus = "published"
	OutboxStatusFailed     OutboxStatus = "failed"
)

type OutboxEvent struct {
	ID            uuid.UUID    `gorm:"primaryKey;default:gen_random_uuid()" json:"id"`
	AggregateID   uuid.UUID    `gorm:"not null;type:uuid"                  json:"aggregate_id"`
	AggregateType string       `gorm:"size:100;not null"                   json:"aggregate_type"`
	EventType     string       `gorm:"size:100;not null"                   json:"event_type"`
	Payload       []byte       `gorm:"type:jsonb;not null"                 json:"payload"`
	RoutingKey    string       `gorm:"size:100;not null"                   json:"routing_key"`
	Status        OutboxStatus `gorm:"size:20;not null;default:pending"    json:"status"`
	RetryCount    int          `gorm:"not null;default:0"                  json:"retry_count"`
	LastError     *string      `gorm:"type:text"                           json:"last_error,omitempty"`
	PublishedAt   *time.Time   `json:"published_at,omitempty"`
	CreatedAt     time.Time    `gorm:"not null"                            json:"created_at"`
}
