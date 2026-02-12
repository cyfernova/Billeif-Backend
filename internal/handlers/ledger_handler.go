package handlers

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type LedgerHandler struct {
	svc *services.LedgerService
	log *logger.Logger
}

func NewLedgerHandler(svc *services.LedgerService, log *logger.Logger) *LedgerHandler {
	return &LedgerHandler{svc: svc, log: log}
}

// List retrieves all ledger entries for a business
// @Summary List ledger entries
// @Description Returns a list of general ledger entries for a specific business.
// @Tags Ledger
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /ledger [get]
func (h *LedgerHandler) List(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("ledger_handler").With("operation", "list")
	businessID := c.Query("business_id")
	if businessID == "" {
		log.Warn("missing business_id query param")
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	page, limit := utils.ParsePagination(c)

	entries, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		log.Error("failed to list ledger entries", "error", err, "business_id", businessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("ledger entries listed", "business_id", businessID, "count", len(entries), "total", total)

	c.JSON(http.StatusOK, gin.H{
		"data":  entries,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// Balance returns the current ledger balance for a business
// @Summary Get ledger balance
// @Description Returns the total balance for a specific business from the general ledger.
// @Tags Ledger
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Success 200 {object} map[string]float64
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /ledger/balance [get]
func (h *LedgerHandler) Balance(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("ledger_handler").With("operation", "balance")
	businessID := c.Query("business_id")
	if businessID == "" {
		log.Warn("missing business_id query param")
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	balance, err := h.svc.GetBalance(c.Request.Context(), businessID)
	if err != nil {
		log.Error("failed to get ledger balance", "error", err, "business_id", businessID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Debug("ledger balance fetched", "business_id", businessID)

	c.JSON(http.StatusOK, gin.H{"balance": balance})
}
