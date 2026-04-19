package dto

import (
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
