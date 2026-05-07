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
