package handler

import (
	"backend/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tojinguyen/upload/internal/dto"
	"github.com/tojinguyen/upload/internal/service"
)

type VideoHandler struct {
	svc service.VideoService
}

func NewVideoHandler(svc service.VideoService) *VideoHandler {
	return &VideoHandler{svc: svc}
}

// InitUpload godoc
// @Summary      Init video upload
// @Description  Create a video record and return a presigned PUT URL for direct upload
// @Tags         videos
// @Accept       json
// @Produce      json
// @Param        body body dto.InitUploadRequest true "Init upload request"
// @Success      201 {object} response.StandardResponse{data=dto.InitUploadResponse}
// @Failure      400 {object} response.StandardResponse
// @Failure      500 {object} response.StandardResponse
// @Router       /videos [post]
func (h *VideoHandler) InitUpload(c *gin.Context) {
	var req dto.InitUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.InitUpload(c.Request.Context(), req)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.Created(c.Writer, resp)
}

// CompleteUpload godoc
// @Summary      Complete video upload
// @Description  Confirm object exists in storage, update status to uploaded, publish event
// @Tags         videos
// @Produce      json
// @Param        id path string true "Video ID"
// @Success      200 {object} response.StandardResponse{data=dto.CompleteUploadResponse}
// @Failure      400 {object} response.StandardResponse
// @Failure      404 {object} response.StandardResponse
// @Failure      500 {object} response.StandardResponse
// @Router       /videos/{id}/complete [post]
func (h *VideoHandler) CompleteUpload(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.CompleteUpload(c.Request.Context(), id)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

// AbortUpload godoc
// @Summary      Abort video upload
// @Description  Delete object from storage and mark video as aborted
// @Tags         videos
// @Produce      json
// @Param        id path string true "Video ID"
// @Success      200 {object} response.StandardResponse{data=dto.AbortUploadResponse}
// @Failure      404 {object} response.StandardResponse
// @Failure      500 {object} response.StandardResponse
// @Router       /videos/{id}/abort [post]
func (h *VideoHandler) AbortUpload(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.AbortUpload(c.Request.Context(), id)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

// GetVideo godoc
// @Summary      Get video metadata
// @Description  Returns metadata for a specific video
// @Tags         videos
// @Produce      json
// @Param        id path string true "Video ID"
// @Success      200 {object} response.StandardResponse{data=dto.VideoResponse}
// @Failure      404 {object} response.StandardResponse
// @Failure      500 {object} response.StandardResponse
// @Router       /videos/{id} [get]
func (h *VideoHandler) GetVideo(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.GetVideo(c.Request.Context(), id)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

// ListVideos godoc
// @Summary      List videos
// @Description  Returns paginated list of videos with optional status filter
// @Tags         videos
// @Produce      json
// @Param        status    query string false "Filter by status"
// @Param        page      query int    false "Page number (default 1)"
// @Param        page_size query int    false "Page size (default 20, max 100)"
// @Success      200 {object} response.StandardResponse{data=dto.ListVideosResponse}
// @Failure      400 {object} response.StandardResponse
// @Failure      500 {object} response.StandardResponse
// @Router       /videos [get]
func (h *VideoHandler) ListVideos(c *gin.Context) {
	var req dto.ListVideosRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.ListVideos(c.Request.Context(), req)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

func parseUUID(c *gin.Context, param string) (uuid.UUID, error) {
	raw := c.Param(param)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}
