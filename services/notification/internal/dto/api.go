package dto

import (
	"time"

	"github.com/tojinguyen/notification/internal/domain"
)

type SendNotificationRequest struct {
	UserID    string                 `json:"user_id" validate:"required"`
	EventType domain.EventType       `json:"event_type" validate:"required"`
	Payload   map[string]interface{} `json:"payload" validate:"required"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type ScheduleNotificationRequest struct {
	UserID      string                 `json:"user_id"      binding:"required"`
	EventType   domain.EventType       `json:"event_type"   binding:"required"`
	Payload     map[string]interface{} `json:"payload"      binding:"required"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	ScheduledAt time.Time              `json:"scheduled_at" binding:"required"`
}

type ScheduleNotificationResponse struct {
	NotificationID string    `json:"notification_id"`
	ScheduledAt    time.Time `json:"scheduled_at"`
	Message        string    `json:"message"`
}

type HealthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
}

type SendNotificationResponse struct {
	NotificationID string `json:"notification_id"`
	Message        string `json:"message"`
}

type ListNotificationsRequest struct {
	UserID    string                     `form:"user_id"`
	Status    domain.NotificationStatus  `form:"status"`
	Channel   domain.NotificationChannel `form:"channel"`
	EventType domain.EventType           `form:"event_type"`
	From      string                     `form:"from"`
	To        string                     `form:"to"`
	Cursor    string                     `form:"cursor"`
	Limit     int                        `form:"limit"`
}

type NotificationItem struct {
	ID           string                     `json:"id"`
	EventType    domain.EventType           `json:"event_type"`
	Channel      domain.NotificationChannel `json:"channel"`
	Status       domain.NotificationStatus  `json:"status"`
	Subject      string                     `json:"subject"`
	Recipient    string                     `json:"recipient"`
	RetryCount   int                        `json:"retry_count"`
	ErrorMessage string                     `json:"error_message,omitempty"`
	SentAt       *time.Time                 `json:"sent_at,omitempty"`
	CreatedAt    time.Time                  `json:"created_at"`
}

type ListNotificationsResponse struct {
	Items      []NotificationItem `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
	HasMore    bool               `json:"has_more"`
}

type PreferenceItem struct {
	ID        string                     `json:"id"`
	EventType domain.EventType           `json:"event_type"`
	Channel   domain.NotificationChannel `json:"channel"`
	Enabled   bool                       `json:"enabled"`
	UpdatedAt time.Time                  `json:"updated_at"`
}

type GetPreferencesResponse struct {
	UserID      string           `json:"user_id"`
	Preferences []PreferenceItem `json:"preferences"`
}

type UpsertPreferenceItem struct {
	EventType domain.EventType           `json:"event_type" binding:"required"`
	Channel   domain.NotificationChannel `json:"channel"    binding:"required"`
	Enabled   bool                       `json:"enabled"`
}

type UpsertPreferencesRequest struct {
	Preferences []UpsertPreferenceItem `json:"preferences" binding:"required,min=1"`
}

type UpsertPreferencesResponse struct {
	UserID      string           `json:"user_id"`
	Preferences []PreferenceItem `json:"preferences"`
	Message     string           `json:"message"`
}

// --- Schedule DTOs ---

type UpsertScheduleRequest struct {
	UserID    string                 `json:"user_id"    binding:"required"`
	EventType domain.EventType       `json:"event_type" binding:"required"`
	SendTime  string                 `json:"send_time"  binding:"required"` // "HH:MM"
	Timezone  string                 `json:"timezone"   binding:"required"` // IANA tz, e.g. "Asia/Ho_Chi_Minh"
	Enabled   bool                   `json:"enabled"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
}

type ScheduleItem struct {
	ID         string           `json:"id"`
	EventType  domain.EventType `json:"event_type"`
	SendTime   string           `json:"send_time"`
	Timezone   string           `json:"timezone"`
	Enabled    bool             `json:"enabled"`
	LastSentAt *time.Time       `json:"last_sent_at,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
}

type GetSchedulesResponse struct {
	UserID    string         `json:"user_id"`
	Schedules []ScheduleItem `json:"schedules"`
}

type DeleteScheduleRequest struct {
	EventType domain.EventType `json:"event_type" binding:"required"`
}

// --- Campaign DTOs ---

type RecipientInput struct {
	UserID    string `json:"user_id"   binding:"required"`
	Recipient string `json:"recipient" binding:"required"`
}

type CreateCampaignRequest struct {
	Title          string                     `json:"title"           binding:"required"`
	Subject        string                     `json:"subject"`
	Content        string                     `json:"content"         binding:"required"`
	Channel        domain.NotificationChannel `json:"channel"         binding:"required"`
	EventType      domain.EventType           `json:"event_type"      binding:"required"`
	ScheduledAt    time.Time                  `json:"scheduled_at"    binding:"required"`
	TargetAudience string                     `json:"target_audience"` // e.g., "all_users", "specific"
	Recipients     []RecipientInput           `json:"recipients"`      // Optional, only used when TargetAudience == "specific"
}

type CreateCampaignResponse struct {
	CampaignID      string    `json:"campaign_id"`
	TotalRecipients int       `json:"total_recipients"`
	ScheduledAt     time.Time `json:"scheduled_at"`
	Message         string    `json:"message"`
}

type CampaignStatsResponse struct {
	CampaignID           string                `json:"campaign_id"`
	Status               domain.CampaignStatus `json:"status"`
	TotalRecipients      int                   `json:"total_recipients"`
	LastDispatchedOffset int                   `json:"last_dispatched_offset"`
	DispatchedCount      int                   `json:"dispatched_count"`
	SentCount            int                   `json:"sent_count"`
	FailedCount          int                   `json:"failed_count"`
	PendingCount         int                   `json:"pending_count"`
	ProgressPct          float64               `json:"progress_pct"`
}

type DLQMessageResponse struct {
	ID             string    `json:"id"`
	NotificationID string    `json:"notification_id,omitempty"`
	QueueName      string    `json:"queue_name"`
	Payload        string    `json:"payload"`
	ErrorMessage   string    `json:"error_message"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

