package errors

import (
	"fmt"
	"net/http"
)

type AppError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	RootErr error  `json:"-"`
}

func (e *AppError) Error() string {
	if e.RootErr != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.RootErr)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.RootErr
}

func New(code int, message string, rootErr error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		RootErr: rootErr,
	}
}

func BadRequest(err error, message string) *AppError {
	if message == "" {
		message = "Data send is invalid or missing required fields"
	}
	return New(http.StatusBadRequest, message, err)
}

func Unauthorized(message string) *AppError {
	if message == "" {
		message = "You are not logged in or the token is invalid"
	}
	return New(http.StatusUnauthorized, message, nil)
}

func Forbidden(message string) *AppError {
	if message == "" {
		message = "You do not have permission to perform this action"
	}
	return New(http.StatusForbidden, message, nil)
}

func NotFound(message string) *AppError {
	if message == "" {
		message = "Resource not found"
	}
	return New(http.StatusNotFound, message, nil)
}

func InternalServer(err error) *AppError {
	return New(http.StatusInternalServerError, "System is experiencing issues, please try again later", err)
}
