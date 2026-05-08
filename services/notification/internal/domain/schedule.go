package domain

import "time"

// UserNotificationSchedule stores a user's recurring daily notification preference.
// send_time is in "HH:MM" format (24-hour clock) interpreted in the user's timezone.
type UserNotificationSchedule struct {
	BaseModel

	UserID    string    `gorm:"size:100;not null;index"       json:"user_id"`
	EventType EventType `gorm:"size:100;not null"             json:"event_type"`
	SendTime  string    `gorm:"size:5;not null"               json:"send_time"` // "HH:MM"
	Timezone  string    `gorm:"size:100;not null;default:UTC" json:"timezone"`
	Enabled   bool      `gorm:"not null;default:true"         json:"enabled"`
	Payload   string    `gorm:"type:text"                     json:"-"` // JSON map for template vars

	LastSentAt *time.Time `json:"last_sent_at,omitempty"`
}
