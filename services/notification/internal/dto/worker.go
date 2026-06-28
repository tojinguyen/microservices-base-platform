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

// CampaignTask is the message published by the campaign dispatcher.
// It differs from NotificationTask in that it carries no NotificationID — the
// audit record is inserted by the email worker AFTER a successful send.
type CampaignTask struct {
	CampaignID string                     `json:"campaign_id"`
	UserID     string                     `json:"user_id"`
	EventType  domain.EventType           `json:"event_type"`
	Recipient  string                     `json:"recipient"`
	Channel    domain.NotificationChannel `json:"channel"`
	Subject    string                     `json:"subject"`
	Content    string                     `json:"content"`
}
