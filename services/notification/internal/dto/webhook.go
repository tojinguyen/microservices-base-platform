package dto

type MailAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

type MailpitWebhook struct {
	ID        string        `json:"ID"`
	MessageID string        `json:"MessageID"`
	From      MailAddress   `json:"From"`
	Subject   string        `json:"Subject"`
	To        []MailAddress `json:"To"`
}
