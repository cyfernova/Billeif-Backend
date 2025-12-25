package utils

import (
	"errors"
	"fmt"
)

// AppError represents an application error with HTTP status code
type AppError struct {
	Code     int                    `json:"code"`
	Message  string                 `json:"message"`
	Internal error                  `json:"-"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

// Error returns the error message
func (e *AppError) Error() string {
	if e.Internal != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Internal)
	}
	return e.Message
}

// Unwrap returns the internal error
func (e *AppError) Unwrap() error {
	return e.Internal
}

// NewAppError creates a new application error
func NewAppError(code int, message string, internal error) *AppError {
	return &AppError{
		Code:     code,
		Message:  message,
		Internal: internal,
	}
}

// WithDetails adds details to the error
func (e *AppError) WithDetails(details map[string]interface{}) *AppError {
	e.Details = details
	return e
}

// Common error constructors
var (
	ErrBadRequest          = &AppError{Code: 400, Message: "Bad request"}
	ErrUnauthorized        = &AppError{Code: 401, Message: "Unauthorized"}
	ErrForbidden           = &AppError{Code: 403, Message: "Forbidden"}
	ErrNotFound            = &AppError{Code: 404, Message: "Resource not found"}
	ErrConflict            = &AppError{Code: 409, Message: "Resource conflict"}
	ErrUnprocessableEntity = &AppError{Code: 422, Message: "Unprocessable entity"}
	ErrInternalServer      = &AppError{Code: 500, Message: "Internal server error"}
	ErrServiceUnavailable  = &AppError{Code: 503, Message: "Service unavailable"}
)

// IsAppError checks if an error is an AppError
func IsAppError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr)
}

// GetAppError extracts AppError from error, or converts to internal server error
func GetAppError(err error) *AppError {
	if appErr, ok := err.(*AppError); ok {
		return appErr
	}
	return &AppError{
		Code:    500,
		Message: "Internal server error",
		Internal: err,
	}
}
