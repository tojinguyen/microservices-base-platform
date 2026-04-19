package dto

type HealthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
}

type SendNotificationResponse struct {
	Message string `json:"message"`
}
