package domain

import "time"

type Notification struct {
	BaseModel

	EventID   string    `gorm:"size:100;index" json:"event_id"`
	EventType EventType `gorm:"size:100;not null" json:"event_type"`
	UserID    string    `gorm:"size:100;not null;index" json:"user_id"`

	Channel NotificationChannel `gorm:"size:20;not null;index" json:"channel"`

	Recipient string `gorm:"size:255;not null" json:"recipient"`

	Subject string `gorm:"size:255" json:"subject"`
	Content string `gorm:"type:text;not null" json:"content"`

	Status       NotificationStatus `gorm:"size:20;not null;index" json:"status"`
	RetryCount   int                `gorm:"default:0" json:"retry_count"`
	NextRetryAt  *time.Time         `gorm:"index" json:"next_retry_at,omitempty"`
	ErrorMessage string             `gorm:"type:text" json:"error_message"`

	Metadata string `gorm:"type:text" json:"metadata"`

	SentAt *time.Time `json:"sent_at,omitempty"`
}

type NotificationTemplate struct {
	BaseModel
	EventType EventType           `gorm:"size:100;uniqueIndex;not null"`
	Channel   NotificationChannel `gorm:"size:20;not null"`
	Subject   string              `gorm:"size:255"`
	Content   string              `gorm:"type:text;not null"`
}
