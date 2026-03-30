package handlers

import (
	"net/http"
	"strconv"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type POSHandler struct {
	svc *services.POSService
	log *logger.Logger
}

func NewPOSHandler(svc *services.POSService, log *logger.Logger) *POSHandler {
	return &POSHandler{svc: svc, log: log}
}

func (h *POSHandler) CreateSession(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.CreatePOSSessionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	session, err := h.svc.CreateSession(c.Request.Context(), businessID, userID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, session)
}

func (h *POSHandler) SearchCatalog(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	results, err := h.svc.SearchCatalog(c.Request.Context(), businessID, c.Query("q"), c.Query("warehouse_id"), limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": results})
}

func (h *POSHandler) ScanItem(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.ScanPOSItemInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	session, cart, err := h.svc.ScanItem(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": session, "cart": cart})
}

func (h *POSHandler) Checkout(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	var input services.CheckoutPOSCartInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	document, err := h.svc.Checkout(c.Request.Context(), businessID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

func (h *POSHandler) GetReceipt(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	receipt, err := h.svc.GetThermalReceipt(c.Request.Context(), businessID, c.Param("documentID"), c.DefaultQuery("format", "thermal"), c.DefaultQuery("width", "58mm"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, receipt)
}
