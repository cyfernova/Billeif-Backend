package handlers

import (
	"net/http"
	"strconv"
	"time"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type InventoryHandler struct {
	svc *services.InventoryService
	log *logger.Logger
}

func NewInventoryHandler(svc *services.InventoryService, log *logger.Logger) *InventoryHandler {
	return &InventoryHandler{svc: svc, log: log}
}

func (h *InventoryHandler) ListWarehouses(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	warehouses, err := h.svc.ListWarehousesForUser(c.Request.Context(), businessID, userID, middleware.GetRole(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": warehouses})
}

func (h *InventoryHandler) CreateWarehouse(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	if middleware.GetRole(c) != "admin" && !h.svc.IsBusinessOwner(c.Request.Context(), userID, businessID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the business owner can create warehouses"})
		return
	}
	var input services.CreateWarehouseInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	warehouse, err := h.svc.CreateWarehouse(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, warehouse)
}

func (h *InventoryHandler) UpdateWarehouse(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpdateWarehouseInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	warehouseID := c.Param("id")
	if !h.hasWarehouseManagementAccess(c, businessID, warehouseID) {
		return
	}
	warehouse, err := h.svc.UpdateWarehouse(c.Request.Context(), businessID, warehouseID, input)
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, warehouse)
}

func (h *InventoryHandler) DeleteWarehouse(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	warehouseID := c.Param("id")
	if !h.hasWarehouseManagementAccess(c, businessID, warehouseID) {
		return
	}
	if err := h.svc.DeleteWarehouse(c.Request.Context(), businessID, warehouseID); err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *InventoryHandler) UpsertCatalog(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	warehouseID := c.Param("id")
	if !h.hasWarehousePermission(c, businessID, warehouseID, "manage_catalog") {
		return
	}
	var input services.WarehouseCatalogInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	record, err := h.svc.UpsertWarehouseCatalog(c.Request.Context(), businessID, warehouseID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *InventoryHandler) UpsertPermissions(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	warehouseID := c.Param("id")
	if !h.hasWarehouseManagementAccess(c, businessID, warehouseID) {
		return
	}
	var inputs []services.WarehousePermissionInput
	if err := c.ShouldBindJSON(&inputs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	records, err := h.svc.UpsertWarehousePermissions(c.Request.Context(), businessID, warehouseID, inputs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": records})
}

func (h *InventoryHandler) ListPermissions(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	warehouseID := c.Param("id")
	if !h.hasWarehouseManagementAccess(c, businessID, warehouseID) {
		return
	}
	records, err := h.svc.ListWarehousePermissions(c.Request.Context(), businessID, warehouseID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": records})
}

func (h *InventoryHandler) CreateAdjustment(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.InventoryAdjustmentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	input.UserID = userID
	if input.WarehouseID != "" && !h.hasWarehousePermission(c, businessID, input.WarehouseID, "move_stock") {
		return
	}
	balances, err := h.svc.RecordAdjustment(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": balances})
}

func (h *InventoryHandler) CreateTransfer(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.InventoryTransferInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	input.UserID = userID
	if !h.hasWarehousePermission(c, businessID, input.FromWarehouseID, "move_stock") || !h.hasWarehousePermission(c, businessID, input.ToWarehouseID, "move_stock") {
		return
	}
	if err := h.svc.TransferStock(c.Request.Context(), input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *InventoryHandler) ResetStock(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.InventoryResetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	input.UserID = userID
	if input.WarehouseID != "" && !h.hasWarehousePermission(c, businessID, input.WarehouseID, "move_stock") {
		return
	}
	if err := h.svc.ResetStock(c.Request.Context(), input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *InventoryHandler) Timeline(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	filter := services.InventoryTimelineFilter{
		BusinessID:  businessID,
		ProductID:   c.Query("product_id"),
		VariantID:   c.Query("variant_id"),
		WarehouseID: c.Query("warehouse_id"),
		CategoryID:  c.Query("category_id"),
		Limit:       parseIntOrDefault(c.Query("limit"), 100),
	}
	if from := c.Query("date_from"); from != "" {
		if parsed, err := time.Parse(time.RFC3339, from); err == nil {
			filter.DateFrom = &parsed
		}
	}
	if to := c.Query("date_to"); to != "" {
		if parsed, err := time.Parse(time.RFC3339, to); err == nil {
			filter.DateTo = &parsed
		}
	}
	if filter.WarehouseID != "" && !h.hasWarehousePermission(c, businessID, filter.WarehouseID, "view_reports") {
		return
	}
	rows, err := h.svc.GetTimeline(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (h *InventoryHandler) Valuation(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	filter := services.InventoryValuationFilter{
		BusinessID:  businessID,
		ProductID:   c.Query("product_id"),
		VariantID:   c.Query("variant_id"),
		WarehouseID: c.Query("warehouse_id"),
		CategoryID:  c.Query("category_id"),
	}
	if at := c.Query("at"); at != "" {
		if parsed, err := time.Parse(time.RFC3339, at); err == nil {
			filter.At = &parsed
		}
	}
	if filter.WarehouseID != "" && !h.hasWarehousePermission(c, businessID, filter.WarehouseID, "view_reports") {
		return
	}
	rows, total, err := h.svc.GetValuation(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total_value": total})
}

func (h *InventoryHandler) Alerts(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	filter := services.InventoryAlertFilter{
		BusinessID:       businessID,
		WarehouseID:      c.Query("warehouse_id"),
		ProductID:        c.Query("product_id"),
		VariantID:        c.Query("variant_id"),
		IncludeExpiry:    c.Query("include_expiry") == "true",
		ExpiryWithinDays: parseIntOrDefault(c.Query("expiry_within_days"), 30),
	}
	if filter.WarehouseID != "" && !h.hasWarehousePermission(c, businessID, filter.WarehouseID, "view_reports") {
		return
	}
	alerts, err := h.svc.GetAlerts(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": alerts})
}

func (h *InventoryHandler) ListBatches(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	batches, err := h.svc.ListBatches(c.Request.Context(), businessID, c.Query("product_id"), c.Query("variant_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": batches})
}

func (h *InventoryHandler) ListSerials(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	serials, err := h.svc.ListSerials(c.Request.Context(), businessID, c.Query("product_id"), c.Query("variant_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": serials})
}

func (h *InventoryHandler) CreateAssemblyRecipe(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateAssemblyRecipeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	recipe, err := h.svc.CreateAssemblyRecipe(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, recipe)
}

func (h *InventoryHandler) ListAssemblyRecipes(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	recipes, err := h.svc.ListAssemblyRecipes(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": recipes})
}

func (h *InventoryHandler) BuildAssembly(c *gin.Context) {
	h.executeAssembly(c, false)
}

func (h *InventoryHandler) DisassembleAssembly(c *gin.Context) {
	h.executeAssembly(c, true)
}

func (h *InventoryHandler) executeAssembly(c *gin.Context, reverse bool) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.ExecuteAssemblyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.hasWarehousePermission(c, businessID, input.WarehouseID, "move_stock") {
		return
	}
	var err error
	if reverse {
		err = h.svc.DisassembleAssembly(c.Request.Context(), businessID, c.Param("id"), input)
	} else {
		err = h.svc.BuildAssembly(c.Request.Context(), businessID, c.Param("id"), input)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *InventoryHandler) hasWarehousePermission(c *gin.Context, businessID, warehouseID, permission string) bool {
	if warehouseID == "" {
		return true
	}
	if middleware.GetRole(c) == "admin" {
		return true
	}
	userID := middleware.GetUserID(c)
	if h.svc.UserHasWarehouseAccess(c.Request.Context(), userID, businessID, warehouseID, permission) {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "access denied to this warehouse"})
	return false
}

func (h *InventoryHandler) hasWarehouseManagementAccess(c *gin.Context, businessID, warehouseID string) bool {
	if warehouseID == "" {
		return true
	}
	if middleware.GetRole(c) == "admin" {
		return true
	}
	userID := middleware.GetUserID(c)
	if h.svc.IsBusinessOwner(c.Request.Context(), userID, businessID) {
		return true
	}
	if h.svc.UserHasWarehouseAccess(c.Request.Context(), userID, businessID, warehouseID, "manage_warehouse") {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "access denied to manage this warehouse"})
	return false
}

func parseIntOrDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
