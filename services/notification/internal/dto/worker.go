package dto

type NotificationPayload struct {
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
	Subject string `json:"subject"`
	Content string `json:"content"`
}
