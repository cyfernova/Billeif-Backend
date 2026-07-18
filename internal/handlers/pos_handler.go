package handlers

import (
	"net/http"
	"strconv"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
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

// CreateSession creates a new POS session
// @Summary Create POS session
// @Description Creates a new Point of Sale session
// @Tags POS
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreatePOSSessionInput true "Session details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /pos/sessions [post]
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

// ListSessions returns POS sessions for the active business
// @Summary List POS sessions
// @Description Lists Point of Sale sessions
// @Tags POS
// @Produce json
// @Security BearerAuth
// @Param status query string false "Session status (active/open/closed)"
// @Param page query int false "Page number"
// @Param limit query int false "Page size"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /pos/sessions [get]
func (h *POSHandler) ListSessions(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	sessions, total, err := h.svc.ListSessions(c.Request.Context(), businessID, userID, page, limit, c.Query("status"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response := utils.NewPaginatedResponse(sessions, total, page, limit)
	c.JSON(http.StatusOK, gin.H{
		"data":        response.Data,
		"items":       sessions,
		"total":       response.Total,
		"page":        response.Page,
		"limit":       response.Limit,
		"total_pages": response.TotalPages,
		"has_next":    response.HasNext,
		"has_prev":    response.HasPrev,
	})
}

// CloseSession closes an active POS session
// @Summary Close POS session
// @Description Closes a Point of Sale session
// @Tags POS
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Router /pos/sessions/{id}/close [post]
func (h *POSHandler) CloseSession(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	session, err := h.svc.CloseSession(c.Request.Context(), businessID, userID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, session)
}

// SearchCatalog searches the product catalog
// @Summary Search POS catalog
// @Description Searches the product catalog for POS
// @Tags POS
// @Produce json
// @Security BearerAuth
// @Param q query string false "Search query"
// @Param warehouse_id query string false "Warehouse ID"
// @Param limit query int false "Result limit"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /pos/catalog/search [get]
func (h *POSHandler) SearchCatalog(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	results, err := h.svc.SearchCatalog(c.Request.Context(), businessID, userID, c.Query("q"), c.Query("warehouse_id"), limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": results})
}

// ScanItem scans an item into the POS cart
// @Summary Scan POS item
// @Description Scans an item into the POS cart
// @Tags POS
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Cart ID"
// @Param input body services.ScanPOSItemInput true "Item details"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /pos/carts/{id}/items/scan [post]
func (h *POSHandler) ScanItem(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ScanPOSItemInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	session, cart, err := h.svc.ScanItem(c.Request.Context(), businessID, userID, c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": session, "cart": cart})
}

// Checkout completes POS cart checkout
// @Summary POS checkout
// @Description Completes checkout for a POS cart
// @Tags POS
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Cart ID"
// @Param input body services.CheckoutPOSCartInput true "Checkout details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /pos/carts/{id}/checkout [post]
func (h *POSHandler) Checkout(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
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
	document, err := h.svc.Checkout(c.Request.Context(), businessID, userID, c.Param("id"), idempotencyKey, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, document)
}

// GetReceipt retrieves a POS receipt
// @Summary Get POS receipt
// @Description Returns a thermal receipt for a document
// @Tags POS
// @Produce json
// @Security BearerAuth
// @Param documentID path string true "Document ID"
// @Param format query string false "Receipt format (thermal)"
// @Param width query string false "Receipt width (58mm, 80mm)"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /pos/receipts/{documentID} [get]
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
