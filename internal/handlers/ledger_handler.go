package handlers

import (
	"net/http"
	"strconv"

	"invoice-backend/internal/services"
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

func (h *LedgerHandler) List(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	entries, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  entries,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *LedgerHandler) Balance(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	balance, err := h.svc.GetBalance(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"balance": balance})
}
