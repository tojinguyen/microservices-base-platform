package dto

import "time"

type NotificationEvent struct {
	EventID    string              `json:"event_id"`
	EventType  string              `json:"event_type"`
	OccurredAt time.Time           `json:"occurred_at"`
	Payload    NotificationPayload `json:"payload"`
}

type NotificationPayload struct {
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
	Subject string `json:"subject"`
	Content string `json:"content"`
}
