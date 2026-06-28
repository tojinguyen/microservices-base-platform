package handler

import (
	"strconv"

	pkgerrors "backend/pkg/errors"
	"backend/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/tojinguyen/upload/internal/dto"
	"github.com/tojinguyen/upload/internal/service"
)

type UploadSessionHandler struct {
	svc service.UploadSessionService
}

func NewUploadSessionHandler(svc service.UploadSessionService) *UploadSessionHandler {
	return &UploadSessionHandler{svc: svc}
}

func (h *UploadSessionHandler) InitChunkedUpload(c *gin.Context) {
	var req dto.InitChunkedUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.InitChunkedUpload(c.Request.Context(), req)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.Created(c.Writer, resp)
}

func (h *UploadSessionHandler) GetSession(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.GetSession(c.Request.Context(), id)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

func (h *UploadSessionHandler) GetPartURL(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	partNumber, err := parsePartNumber(c)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.GetPartURL(c.Request.Context(), id, partNumber)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

func (h *UploadSessionHandler) ConfirmPart(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	partNumber, err := parsePartNumber(c)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	var req dto.ConfirmPartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.ConfirmPart(c.Request.Context(), id, partNumber, req)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

func (h *UploadSessionHandler) CompleteSession(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.CompleteSession(c.Request.Context(), id)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

func (h *UploadSessionHandler) AbortSession(c *gin.Context) {
	id, err := parseUUID(c, "id")
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	resp, err := h.svc.AbortSession(c.Request.Context(), id)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, resp)
}

func parsePartNumber(c *gin.Context) (int, error) {
	raw := c.Param("number")
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, pkgerrors.BadRequest(nil, "part number must be a positive integer")
	}
	return n, nil
}
