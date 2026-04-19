package handler

import (
	"backend/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/tojinguyen/notification/internal/dto"
	"github.com/tojinguyen/notification/internal/service"
)

type NotificationHandler struct {
	service service.NotificationService
}

func NewNotificationHandler(svc service.NotificationService) *NotificationHandler {
	return &NotificationHandler{
		service: svc,
	}
}

// Health godoc
// @Summary Health check
// @Description check service health
// @Tags health
// @Produce json
// @Success 200 {object} response.StandardResponse{data=dto.HealthResponse}
// @Router /health [get]
func (h *NotificationHandler) Health(c *gin.Context) {
	response.OK(c.Writer, dto.HealthResponse{
		Service: "notification-service",
		Status:  "ok",
	})
}

// SendNotification godoc
// @Summary Send notification
// @Description Direct trigger to send a notification (test or synchonous bypass of queue)
// @Tags notifications
// @Accept json
// @Produce json
// @Param event body dto.SendNotificationRequest true "Notification Event"
// @Success 200 {object} response.StandardResponse{data=dto.SendNotificationResponse}
// @Failure 400 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Failure 500 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Router /send [post]
func (h *NotificationHandler) SendNotification(c *gin.Context) {
	var req dto.SendNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	if err := h.service.ProcessEvent(c.Request.Context(), req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, dto.SendNotificationResponse{
		Message: "notification sent successfully",
	})
}
