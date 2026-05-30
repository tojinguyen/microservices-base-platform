package domain

import (
	"time"

	"github.com/google/uuid"
)

type CampaignStatus string

const (
	CampaignStatusPending     CampaignStatus = "pending"
	CampaignStatusDispatching CampaignStatus = "dispatching"
	CampaignStatusCompleted   CampaignStatus = "completed"
	CampaignStatusFailed      CampaignStatus = "failed"
)

type CampaignRecipientStatus string

const (
	CampaignRecipientStatusPending    CampaignRecipientStatus = "pending"
	CampaignRecipientStatusDispatched CampaignRecipientStatus = "dispatched"
	CampaignRecipientStatusSent       CampaignRecipientStatus = "sent"
	CampaignRecipientStatusFailed     CampaignRecipientStatus = "failed"
)

type Campaign struct {
	BaseModel

	Title                string              `gorm:"size:255;not null"                    json:"title"`
	Subject              string              `gorm:"size:255"                             json:"subject"`
	Content              string              `gorm:"type:text;not null"                   json:"content"`
	Channel              NotificationChannel `gorm:"size:20;not null"                     json:"channel"`
	EventType            EventType           `gorm:"size:100;not null"                    json:"event_type"`
	TargetAudience       string              `gorm:"size:50;not null;default:all_users"   json:"target_audience"`
	FilterCriteria       string              `gorm:"type:jsonb;default:'{}'"              json:"filter_criteria"`
	Status               CampaignStatus      `gorm:"size:20;not null;default:pending"     json:"status"`
	ScheduledAt          time.Time           `gorm:"not null"                             json:"scheduled_at"`
	TotalRecipients       int                 `gorm:"default:0"                            json:"total_recipients"`
	LastDispatchedCursor  string              `gorm:"size:36;default:''"                   json:"last_dispatched_cursor"`
	DispatchedCount       int                 `gorm:"default:0"                            json:"dispatched_count"`
	SentCount            int                 `gorm:"default:0"                            json:"sent_count"`
	FailedCount          int                 `gorm:"default:0"                            json:"failed_count"`
}

// CampaignRecipient does not embed BaseModel — no soft-delete needed; records are append-only.
type CampaignRecipient struct {
	Id         uuid.UUID               `gorm:"primaryKey;default:gen_random_uuid()" json:"id"`
	CampaignID uuid.UUID               `gorm:"not null;index"                       json:"campaign_id"`
	UserID     string                  `gorm:"size:100;not null"                    json:"user_id"`
	Recipient  string                  `gorm:"size:255;not null"                    json:"recipient"`
	Status     CampaignRecipientStatus `gorm:"size:20;not null;default:pending"     json:"status"`
	CreatedAt  time.Time               `gorm:"autoCreateTime"                       json:"created_at"`
	UpdatedAt  time.Time               `gorm:"autoUpdateTime"                       json:"updated_at"`
}
