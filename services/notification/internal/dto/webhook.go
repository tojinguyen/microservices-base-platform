package dto

type MailpitWebhook struct {
	ID        string   `json:"ID"`
	MessageID string   `json:"MessageID"`
	From      string   `json:"From"`
	Subject   string   `json:"Subject"`
	To        []string `json:"To"`
}
