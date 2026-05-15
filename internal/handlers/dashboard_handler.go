package handlers

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type DashboardHandler struct {
	svc *services.DashboardService
	log *logger.Logger
}

func NewDashboardHandler(svc *services.DashboardService, log *logger.Logger) *DashboardHandler {
	return &DashboardHandler{svc: svc, log: log.Named("dashboard")}
}

// Summary handles GET /dashboard/summary.
func (h *DashboardHandler) Summary(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}

	summary, err := h.svc.Summary(c.Request.Context(), businessID, userID)
	if err != nil {
		h.log.Error("failed to build dashboard summary", "business_id", businessID, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load dashboard summary"})
		return
	}

	c.JSON(http.StatusOK, summary)
}
