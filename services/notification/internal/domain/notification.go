package domain

import "time"

type NotificationStatus string

type NotificationChannel string

const (
	NotificationStatusPending NotificationStatus = "pending"
	NotificationStatusSent    NotificationStatus = "sent"
	NotificationStatusFailed  NotificationStatus = "failed"
)

const (
	NotificationChannelEmail NotificationChannel = "email"
)

type Notification struct {
	BaseModel
	EventID        string              `gorm:"size:100;index" json:"event_id"`
	EventType      string              `gorm:"size:100;not null" json:"event_type"`
	UserID         string              `gorm:"size:100;not null;index" json:"user_id"`
	Channel        NotificationChannel `gorm:"size:20;not null;default:'email'" json:"channel"`
	RecipientEmail string              `gorm:"size:255;not null" json:"recipient_email"`
	Subject        string              `gorm:"size:255;not null" json:"subject"`
	Content        string              `gorm:"type:text;not null" json:"content"`
	Status         NotificationStatus  `gorm:"size:20;not null;index" json:"status"`
	ErrorMessage   string              `gorm:"type:text" json:"error_message"`
	SentAt         *time.Time          `json:"sent_at,omitempty"`
}
