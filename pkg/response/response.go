package response

import (
	"encoding/json"
	"net/http"

	"backend/pkg/errors"
	"backend/pkg/logger"

	"go.uber.org/zap"
)

type StandardResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Meta    interface{} `json:"meta,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

func OK(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, StandardResponse{
		Success: true,
		Data:    data,
	})
}

func Created(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusCreated, StandardResponse{
		Success: true,
		Data:    data,
	})
}

func OKWithMeta(w http.ResponseWriter, data interface{}, meta interface{}) {
	writeJSON(w, http.StatusOK, StandardResponse{
		Success: true,
		Data:    data,
		Meta:    meta,
	})
}

func Error(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *errors.AppError

	// Check if it's a known AppError, if not, wrap it as InternalServerError
	if customErr, ok := err.(*errors.AppError); ok {
		appErr = customErr
	} else {
		// If it's an unknown error (e.g., code crash, nil pointer), wrap it as 500
		appErr = errors.InternalServer(err)
	}

	// Write log for debugging.
	// Get logger from context (contains trace_id from your middleware)
	log := logger.FromContext(r.Context())
	if appErr.Code >= 500 {
		// Log Error for server errors
		log.Error("Internal Server Error", zap.Error(appErr.RootErr))
	} else {
		// Log Warn for client errors (400, 401, 404...)
		log.Warn("Client Error", zap.Int("code", appErr.Code), zap.String("msg", appErr.Message))
	}

	// Return only Code and Message to Frontend for security
	errPayload := map[string]interface{}{
		"code":    appErr.Code,
		"message": appErr.Message,
	}

	writeJSON(w, appErr.Code, StandardResponse{
		Success: false,
		Error:   errPayload,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// If encoding fails, log the error but don't send it to client (since headers are already sent)
		logger.L().Error("Failed to encode JSON response", zap.Error(err))
	}
}
