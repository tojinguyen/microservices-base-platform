package handler

import (
	"errors"

	"backend/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/tojinguyen/notification/internal/domain"
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

	resp, err := h.service.CreateNotification(c.Request.Context(), req)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

// ScheduleNotification godoc
// @Summary Schedule a notification for future delivery
// @Description Inserts a notification that will be delivered at the specified scheduled_at time
// @Tags notifications
// @Accept json
// @Produce json
// @Param body body dto.ScheduleNotificationRequest true "Schedule request"
// @Success 202 {object} response.StandardResponse{data=dto.ScheduleNotificationResponse}
// @Failure 400 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Failure 500 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Router /schedule [post]
func (h *NotificationHandler) ScheduleNotification(c *gin.Context) {
	var req dto.ScheduleNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.service.ScheduleNotification(c.Request.Context(), req)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

// ListNotifications godoc
// @Summary List notification history
// @Description Returns paginated notification history for a user using cursor-based pagination
// @Tags notifications
// @Produce json
// @Param user_id    query string false "User ID (required)"
// @Param status     query string false "Filter by status (pending|processing|sent|delivering|failed)"
// @Param channel    query string false "Filter by channel (email|sms|zalo|push)"
// @Param event_type query string false "Filter by event type"
// @Param from       query string false "Start time RFC3339 (e.g. 2026-01-01T00:00:00Z)"
// @Param to         query string false "End time RFC3339"
// @Param cursor     query string false "Opaque pagination cursor from previous response"
// @Param limit      query int    false "Page size (default 20, max 100)"
// @Success 200 {object} response.StandardResponse{data=dto.ListNotificationsResponse}
// @Failure 400 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Failure 500 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Router / [get]
func (h *NotificationHandler) ListNotifications(c *gin.Context) {
	var req dto.ListNotificationsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}
	if req.UserID == "" {
		response.Error(c.Writer, c.Request, errors.New("user_id is required"))
		return
	}

	resp, err := h.service.ListNotifications(c.Request.Context(), req)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

// HandleMailpitWebhook godoc
// @Summary Handle Mailpit webhook
// @Description Callback from Mailpit when an email is received
// @Tags webhooks
// @Accept json
// @Produce json
// @Success 200 {object} response.StandardResponse{data=string}
// @Router /webhooks/mailpit [post]
func (h *NotificationHandler) HandleMailpitWebhook(c *gin.Context) {
	var webhook dto.MailpitWebhook
	if err := c.ShouldBindJSON(&webhook); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	if err := h.service.PublishWebhookResponse(c.Request.Context(), webhook); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, "webhook accepted for processing")
}

type PreferenceHandler struct {
	prefSvc service.PreferenceService
}

func NewPreferenceHandler(prefSvc service.PreferenceService) *PreferenceHandler {
	return &PreferenceHandler{prefSvc: prefSvc}
}

// GetPreferences godoc
// @Summary Get user notification preferences
// @Description Returns toggleable notification preferences for a specific user
// @Tags preferences
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} response.StandardResponse{data=dto.GetPreferencesResponse}
// @Failure 400 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Failure 500 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Router /api/v1/users/{id}/notification-preferences [get]
func (h *PreferenceHandler) GetPreferences(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c.Writer, c.Request, errors.New("user id is required"))
		return
	}

	resp, err := h.prefSvc.GetPreferences(c.Request.Context(), userID)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

// UpsertPreferences godoc
// @Summary Update user notification preferences
// @Description Upserts notification preferences for a user. Only promotion_campaign event is configurable.
// @Tags preferences
// @Accept json
// @Produce json
// @Param id   path string                        true "User ID"
// @Param body body dto.UpsertPreferencesRequest  true "Preferences payload"
// @Success 200 {object} response.StandardResponse{data=dto.UpsertPreferencesResponse}
// @Failure 400 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Failure 500 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Router /api/v1/users/{id}/notification-preferences [put]
func (h *PreferenceHandler) UpsertPreferences(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c.Writer, c.Request, errors.New("user id is required"))
		return
	}

	var req dto.UpsertPreferencesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.prefSvc.UpsertPreferences(c.Request.Context(), userID, req)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

// --- ScheduleHandler ---

type ScheduleHandler struct {
	svc service.SchedulerService
}

func NewScheduleHandler(svc service.SchedulerService) *ScheduleHandler {
	return &ScheduleHandler{svc: svc}
}

// UpsertSchedule godoc
// @Summary Set a recurring daily notification schedule
// @Description Creates or updates a daily notification schedule for a user (identified by path :id).
// @Tags schedules
// @Accept json
// @Produce json
// @Param id   path string                    true "User ID"
// @Param body body dto.UpsertScheduleRequest true "Schedule config"
// @Success 200 {object} response.StandardResponse{data=dto.ScheduleItem}
// @Failure 400 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Failure 500 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Router /api/v1/users/{id}/notification-schedules [put]
func (h *ScheduleHandler) UpsertSchedule(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c.Writer, c.Request, errors.New("user id is required"))
		return
	}

	var req dto.UpsertScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}
	req.UserID = userID

	item, err := h.svc.UpsertSchedule(c.Request.Context(), req)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, item)
}

// GetSchedules godoc
// @Summary Get all notification schedules for a user
// @Tags schedules
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} response.StandardResponse{data=dto.GetSchedulesResponse}
// @Failure 400 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Failure 500 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Router /api/v1/users/{id}/notification-schedules [get]
func (h *ScheduleHandler) GetSchedules(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c.Writer, c.Request, errors.New("user id is required"))
		return
	}

	resp, err := h.svc.GetSchedules(c.Request.Context(), userID)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

// DeleteSchedule godoc
// @Summary Delete a notification schedule
// @Tags schedules
// @Accept json
// @Produce json
// @Param id   path string                     true "User ID"
// @Param body body dto.DeleteScheduleRequest  true "Event type to delete"
// @Success 200 {object} response.StandardResponse{data=string}
// @Failure 400 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Failure 500 {object} response.StandardResponse{error=object{code=int,message=string}}
// @Router /api/v1/users/{id}/notification-schedules [delete]
func (h *ScheduleHandler) DeleteSchedule(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c.Writer, c.Request, errors.New("user id is required"))
		return
	}

	var req dto.DeleteScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	if err := h.svc.DeleteSchedule(c.Request.Context(), userID, domain.EventType(req.EventType)); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, "schedule deleted")
}
