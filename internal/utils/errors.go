package utils

import (
	"fmt"
	"net/http"
)

type AppError struct {
	Code    string
	Message string
	Err     error
	HTTP    int
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func NewError(code, message string, httpStatus int, err error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
		HTTP:    httpStatus,
	}
}

var (
	ErrNotFound       = NewError("NOT_FOUND", "Resource not found", http.StatusNotFound, nil)
	ErrBadRequest     = NewError("BAD_REQUEST", "Invalid request", http.StatusBadRequest, nil)
	ErrUnauthorized   = NewError("UNAUTHORIZED", "Unauthorized", http.StatusUnauthorized, nil)
	ErrForbidden      = NewError("FORBIDDEN", "Forbidden", http.StatusForbidden, nil)
	ErrConflict       = NewError("CONFLICT", "Resource conflict", http.StatusConflict, nil)
	ErrInternalServer = NewError("INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError, nil)
	ErrValidation     = NewError("VALIDATION_ERROR", "Validation failed", http.StatusBadRequest, nil)
)

func WrapError(err error, message string) *AppError {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*AppError); ok {
		return appErr
	}
	return NewError("INTERNAL_ERROR", message, http.StatusInternalServerError, err)
}
