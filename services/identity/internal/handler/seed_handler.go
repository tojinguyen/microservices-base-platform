package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tojinguyen/identity/internal/dto"
	"github.com/tojinguyen/identity/internal/service"
)

type SeedHandler struct {
	svc service.SeedService
}

func NewSeedHandler(svc service.SeedService) *SeedHandler {
	return &SeedHandler{svc: svc}
}

// SeedUsers godoc
// @Summary Seed massive users
// @Description Seed millions of users for testing. Returns Server-Sent Events (SSE) stream.
// @Tags admin
// @Accept json
// @Produce text/event-stream
// @Param request body dto.SeedUsersRequest true "Seed Request"
// @Success 200 {string} string "SSE Stream"
// @Router /admin/seed/users [post]
func (h *SeedHandler) SeedUsers(c *gin.Context) {
	var input dto.SeedUsersRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set default values if not provided
	if input.Prefix == "" {
		input.Prefix = "user"
	}
	if input.Password == "" {
		input.Password = "Seed@1234"
	}
	if input.BatchSize == 0 {
		input.BatchSize = 5000
	}

	// Set headers for SSE
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")

	// Create channel for progress events with buffer
	progressChan := make(chan dto.SeedProgressEvent, 100)

	// Start seeding in background goroutine
	// Dùng c.Request.Context() để tự động hủy seed khi client ngắt kết nối
	go h.svc.SeedUsers(c.Request.Context(), &input, progressChan)

	// Stream events to client
	c.Stream(func(w io.Writer) bool {
		event, ok := <-progressChan
		if !ok {
			return false // Channel closed, end stream
		}

		c.SSEvent(event.Type, event)

		if event.Type == "done" || event.Type == "error" {
			return false // End stream
		}

		return true // Continue streaming
	})
}
