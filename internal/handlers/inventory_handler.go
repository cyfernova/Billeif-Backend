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

// ListWarehouses returns all warehouses for a business
// @Summary List warehouses
// @Description Returns all warehouses for the business
// @Tags Inventory
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /warehouses [get]
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

// CreateWarehouse creates a new warehouse
// @Summary Create warehouse
// @Description Creates a new warehouse for the business
// @Tags Inventory
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateWarehouseInput true "Warehouse details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /warehouses [post]
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

// UpdateWarehouse updates an existing warehouse
// @Summary Update warehouse
// @Description Updates an existing warehouse by ID
// @Tags Inventory
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Warehouse ID"
// @Param input body services.UpdateWarehouseInput true "Warehouse update details"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /warehouses/{id} [put]
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

// DeleteWarehouse deletes a warehouse
// @Summary Delete warehouse
// @Description Deletes a warehouse by ID
// @Tags Inventory
// @Produce json
// @Security BearerAuth
// @Param id path string true "Warehouse ID"
// @Success 204 {string} string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /warehouses/{id} [delete]
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

// CreateAdjustment creates an inventory adjustment
// @Summary Create inventory adjustment
// @Description Creates an inventory adjustment
// @Tags Inventory
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.InventoryAdjustmentInput true "Adjustment details"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /inventory/adjustments [post]
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

// CreateTransfer creates an inventory transfer
// @Summary Create inventory transfer
// @Description Creates an inventory transfer between warehouses
// @Tags Inventory
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.InventoryTransferInput true "Transfer details"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /inventory/transfers [post]
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

func (h *InventoryHandler) ListTransfers(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page := parseIntOrDefault(c.Query("page"), 1)
	limit := parseIntOrDefault(c.Query("limit"), 20)
	rows, err := h.svc.GetTimeline(c.Request.Context(), services.InventoryTimelineFilter{
		BusinessID: businessID,
		Limit:      page * limit,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	transfers := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		if row.TransactionType != "transfer" || row.Direction != "out" {
			continue
		}
		transfers = append(transfers, gin.H{
			"id":                row.ID,
			"product_id":        row.ProductID,
			"product_name":      row.ProductName,
			"from_warehouse_id": row.WarehouseID,
			"from_warehouse":    row.WarehouseName,
			"quantity":          row.Quantity,
			"status":            "completed",
			"created_at":        row.RecordedAt,
			"completed_at":      row.RecordedAt,
		})
	}
	start := (page - 1) * limit
	if start > len(transfers) {
		start = len(transfers)
	}
	end := start + limit
	if end > len(transfers) {
		end = len(transfers)
	}
	c.JSON(http.StatusOK, gin.H{
		"data":  transfers[start:end],
		"items": transfers[start:end],
		"total": len(transfers),
		"page":  page,
		"limit": limit,
	})
}

func (h *InventoryHandler) CompleteTransfer(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "status": "completed"})
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

// Timeline returns inventory timeline
// @Summary Inventory timeline
// @Description Returns inventory movement timeline
// @Tags Inventory
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /inventory/timeline [get]
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

// Valuation returns inventory valuation
// @Summary Inventory valuation
// @Description Returns inventory valuation report
// @Tags Inventory
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /inventory/valuation [get]
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

// Alerts returns inventory alerts
// @Summary Inventory alerts
// @Description Returns inventory alerts for low stock and expiry
// @Tags Inventory
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /inventory/alerts [get]
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

// ListBatches returns batch inventory
// @Summary List batches
// @Description Returns batch inventory information
// @Tags Inventory
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /inventory/batches [get]
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

// ListSerials returns serial number inventory
// @Summary List serials
// @Description Returns serial number inventory information
// @Tags Inventory
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /inventory/serials [get]
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

// CreateAssemblyRecipe creates an assembly recipe
// @Summary Create assembly recipe
// @Description Creates an assembly recipe for bundled products
// @Tags Inventory
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateAssemblyRecipeInput true "Recipe details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /assemblies [post]
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

// ListAssemblyRecipes returns all assembly recipes
// @Summary List assembly recipes
// @Description Returns all assembly recipes for the business
// @Tags Inventory
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /assemblies [get]
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

// BuildAssembly builds an assembly
// @Summary Build assembly
// @Description Builds an assembly from its recipe
// @Tags Inventory
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Assembly Recipe ID"
// @Param input body services.ExecuteAssemblyInput true "Build details"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /assemblies/{id}/build [post]
func (h *InventoryHandler) BuildAssembly(c *gin.Context) {
	h.executeAssembly(c, false)
}

// DisassembleAssembly disassembles an assembly
// @Summary Disassemble assembly
// @Description Disassembles an assembly back into components
// @Tags Inventory
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Assembly Recipe ID"
// @Param input body services.ExecuteAssemblyInput true "Disassembly details"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /assemblies/{id}/disassemble [post]
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
