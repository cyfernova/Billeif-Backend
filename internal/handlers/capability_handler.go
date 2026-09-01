package handlers

import (
	"context"
	"net/http"
	"strings"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type CapabilityLister interface {
	List(ctx context.Context, businessID, userID string, platform services.CapabilityPlatform) (services.CapabilityList, error)
}

type CapabilityHandler struct {
	service CapabilityLister
	log     *logger.Logger
}

type CapabilityAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CapabilityErrorResponse struct {
	Error CapabilityAPIError `json:"error"`
}

func NewCapabilityHandler(service CapabilityLister, log *logger.Logger) *CapabilityHandler {
	return &CapabilityHandler{service: service, log: log}
}

// List godoc
// @Summary List runtime capabilities
// @Description Returns the backend-authoritative, customer-safe runtime capability evaluation for the active business. Provider health is read from cache; this request does not call providers.
// @Tags Capabilities
// @Produce json
// @Param platform query string false "Client platform" Enums(web,ios,android) default(web)
// @Success 200 {object} services.CapabilityList
// @Failure 400 {object} CapabilityErrorResponse
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} CapabilityErrorResponse
// @Failure 503 {object} CapabilityErrorResponse
// @Security BearerAuth
// @Router /capabilities [get]
func (h *CapabilityHandler) List(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		h.writeError(c, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return
	}
	businessID := middleware.GetEffectiveBusinessID(c)
	if businessID == "" {
		h.writeError(c, http.StatusForbidden, "business_scope_required", "An active business is required.")
		return
	}
	platform := services.CapabilityPlatform(strings.ToLower(strings.TrimSpace(c.DefaultQuery("platform", string(services.CapabilityPlatformWeb)))))
	switch platform {
	case services.CapabilityPlatformWeb, services.CapabilityPlatformIOS, services.CapabilityPlatformAndroid:
	default:
		h.writeError(c, http.StatusBadRequest, "invalid_platform", "Platform must be web, ios, or android.")
		return
	}
	if h == nil || h.service == nil {
		h.writeError(c, http.StatusServiceUnavailable, "capability_service_unavailable", "Capability evaluation is unavailable.")
		return
	}
	result, err := h.service.List(c.Request.Context(), businessID, userID, platform)
	if err != nil {
		if h.log != nil {
			h.log.Error("capability evaluation failed", "business_id", businessID, "error", err)
		}
		h.writeError(c, http.StatusInternalServerError, "capability_evaluation_failed", "Capability evaluation failed.")
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *CapabilityHandler) writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, CapabilityErrorResponse{Error: CapabilityAPIError{Code: code, Message: message}})
}
