package dto

import "github.com/tojinguyen/notification/internal/domain"

type NotificationTask struct {
	NotificationID string                     `json:"notification_id"`
	UserID         string                     `json:"user_id"`
	EventType      domain.EventType           `json:"event_type"`
	Recipient      string                     `json:"recipient"`
	Channel        domain.NotificationChannel `json:"channel"`
	Data           map[string]string          `json:"data"`
	RetryCount     int                        `json:"retry_count"`
}
