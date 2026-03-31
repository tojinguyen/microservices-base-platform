package handler

import (
	"backend/pkg/response"

	"github.com/gin-gonic/gin"
)

type NotificationHandler struct{}

func NewNotificationHandler() *NotificationHandler {
	return &NotificationHandler{}
}

func (h *NotificationHandler) Health(c *gin.Context) {
	response.OK(c.Writer, map[string]string{
		"service": "notification-service",
		"status":  "ok",
	})
}
