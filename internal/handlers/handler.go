package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/cyfernova/invoice-backend/internal/utils"
)

// Base handler with common response methods
type Handler struct{}

// Success sends a successful response
func (h *Handler) Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, utils.Response{
		Success: true,
		Data:    data,
	})
}

// Created sends a created response
func (h *Handler) Created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, utils.Response{
		Success: true,
		Data:    data,
	})
}

// NoContent sends a no content response
func (h *Handler) NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Error sends an error response
func (h *Handler) Error(c *gin.Context, err error) {
	if appErr, ok := err.(*utils.AppError); ok {
		c.JSON(appErr.Code, utils.Response{
			Success: false,
			Error: &utils.ErrorInfo{
				Code:    getErrorCode(appErr.Code),
				Message: appErr.Message,
				Details: appErr.Details,
			},
		})
		return
	}

	c.JSON(http.StatusInternalServerError, utils.Response{
		Success: false,
		Error: &utils.ErrorInfo{
			Code:    "INTERNAL_ERROR",
			Message: "An internal error occurred",
		},
	})
}

// BadRequest sends a bad request response
func (h *Handler) BadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, utils.Response{
		Success: false,
		Error: &utils.ErrorInfo{
			Code:    "BAD_REQUEST",
			Message: message,
		},
	})
}

// Unauthorized sends an unauthorized response
func (h *Handler) Unauthorized(c *gin.Context, message string) {
	c.JSON(http.StatusUnauthorized, utils.Response{
		Success: false,
		Error: &utils.ErrorInfo{
			Code:    "UNAUTHORIZED",
			Message: message,
		},
	})
}

// Forbidden sends a forbidden response
func (h *Handler) Forbidden(c *gin.Context, message string) {
	c.JSON(http.StatusForbidden, utils.Response{
		Success: false,
		Error: &utils.ErrorInfo{
			Code:    "FORBIDDEN",
			Message: message,
		},
	})
}

// NotFound sends a not found response
func (h *Handler) NotFound(c *gin.Context, message string) {
	c.JSON(http.StatusNotFound, utils.Response{
		Success: false,
		Error: &utils.ErrorInfo{
			Code:    "NOT_FOUND",
			Message: message,
		},
	})
}

// getErrorCode converts HTTP status code to error code string
func getErrorCode(statusCode int) string {
	switch statusCode {
	case 400:
		return "BAD_REQUEST"
	case 401:
		return "UNAUTHORIZED"
	case 403:
		return "FORBIDDEN"
	case 404:
		return "NOT_FOUND"
	case 409:
		return "CONFLICT"
	case 422:
		return "UNPROCESSABLE_ENTITY"
	case 500:
		return "INTERNAL_SERVER_ERROR"
	case 503:
		return "SERVICE_UNAVAILABLE"
	default:
		return "UNKNOWN_ERROR"
	}
}
