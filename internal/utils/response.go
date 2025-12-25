package utils

import "net/http"

// Response represents a standard API response
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
}

// ErrorInfo represents error details in the response
type ErrorInfo struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// Meta represents pagination metadata
type Meta struct {
	Page       int   `json:"page,omitempty"`
	PerPage    int   `json:"per_page,omitempty"`
	TotalPages int   `json:"total_pages,omitempty"`
	TotalCount int64 `json:"total_count,omitempty"`
}

// SuccessResponse creates a successful response
func SuccessResponse(data interface{}) Response {
	return Response{
		Success: true,
		Data:    data,
	}
}

// SuccessResponseWithMeta creates a successful response with metadata
func SuccessResponseWithMeta(data interface{}, meta Meta) Response {
	return Response{
		Success: true,
		Data:    data,
		Meta:    &meta,
	}
}

// ErrorResponse creates an error response
func ErrorResponse(code, message string, details map[string]interface{}) Response {
	return Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
			Details: details,
		},
	}
}

// ErrorResponseFromAppError creates an error response from AppError
func ErrorResponseFromAppError(err *AppError) Response {
	return Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    getErrorCode(err.Code),
			Message: err.Message,
			Details: err.Details,
		},
	}
}

// getErrorCode converts HTTP status code to error code string
func getErrorCode(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "BAD_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "CONFLICT"
	case http.StatusUnprocessableEntity:
		return "UNPROCESSABLE_ENTITY"
	case http.StatusTooManyRequests:
		return "TOO_MANY_REQUESTS"
	case http.StatusInternalServerError:
		return "INTERNAL_SERVER_ERROR"
	case http.StatusServiceUnavailable:
		return "SERVICE_UNAVAILABLE"
	default:
		return "UNKNOWN_ERROR"
	}
}

// PaginatedMeta creates pagination metadata
func PaginatedMeta(page, perPage int, totalCount int64) Meta {
	totalPages := int(totalCount) / perPage
	if int(totalCount)%perPage > 0 {
		totalPages++
	}

	return Meta{
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
		TotalCount: totalCount,
	}
}
