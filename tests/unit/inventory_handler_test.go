package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock InventoryService
// =============================================================================

type MockInventoryService struct {
	mock.Mock
}

func (m *MockInventoryService) ListWarehousesForUser(ctx context.Context, businessID, userID, role string) ([]*models.Warehouse, error) {
	args := m.Called(ctx, businessID, userID, role)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Warehouse), args.Error(1)
}

func (m *MockInventoryService) IsBusinessOwner(ctx context.Context, userID, businessID string) bool {
	args := m.Called(ctx, userID, businessID)
	return args.Bool(0)
}

func (m *MockInventoryService) CreateWarehouse(ctx context.Context, input services.CreateWarehouseInput) (*models.Warehouse, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Warehouse), args.Error(1)
}

func (m *MockInventoryService) UpdateWarehouse(ctx context.Context, businessID, warehouseID string, input services.UpdateWarehouseInput) (*models.Warehouse, error) {
	args := m.Called(ctx, businessID, warehouseID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Warehouse), args.Error(1)
}

func (m *MockInventoryService) DeleteWarehouse(ctx context.Context, businessID, warehouseID string) error {
	args := m.Called(ctx, businessID, warehouseID)
	return args.Error(0)
}

func (m *MockInventoryService) UpsertWarehouseCatalog(ctx context.Context, businessID, warehouseID string, input services.WarehouseCatalogInput) (*models.ProductWarehouseCatalog, error) {
	args := m.Called(ctx, businessID, warehouseID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.ProductWarehouseCatalog), args.Error(1)
}

func (m *MockInventoryService) UpsertWarehousePermissions(ctx context.Context, businessID, warehouseID string, inputs []services.WarehousePermissionInput) ([]*models.WarehousePermission, error) {
	args := m.Called(ctx, businessID, warehouseID, inputs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.WarehousePermission), args.Error(1)
}

func (m *MockInventoryService) ListWarehousePermissions(ctx context.Context, businessID, warehouseID string) ([]*models.WarehousePermission, error) {
	args := m.Called(ctx, businessID, warehouseID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.WarehousePermission), args.Error(1)
}

func (m *MockInventoryService) UserHasWarehouseAccess(ctx context.Context, userID, businessID, warehouseID, permission string) bool {
	args := m.Called(ctx, userID, businessID, warehouseID, permission)
	return args.Bool(0)
}

func (m *MockInventoryService) RecordAdjustment(ctx context.Context, input services.InventoryAdjustmentInput) ([]*models.InventoryBalance, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.InventoryBalance), args.Error(1)
}

func (m *MockInventoryService) TransferStock(ctx context.Context, input services.InventoryTransferInput) error {
	args := m.Called(ctx, input)
	return args.Error(0)
}

func (m *MockInventoryService) ResetStock(ctx context.Context, input services.InventoryResetInput) error {
	args := m.Called(ctx, input)
	return args.Error(0)
}

func (m *MockInventoryService) GetTimeline(ctx context.Context, filter services.InventoryTimelineFilter) ([]services.InventoryTimelineEntry, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]services.InventoryTimelineEntry), args.Error(1)
}

func (m *MockInventoryService) GetValuation(ctx context.Context, filter services.InventoryValuationFilter) ([]services.InventoryValuationRow, float64, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, 0.0, args.Error(2)
	}
	return args.Get(0).([]services.InventoryValuationRow), args.Get(1).(float64), args.Error(2)
}

func (m *MockInventoryService) GetAlerts(ctx context.Context, filter services.InventoryAlertFilter) ([]services.InventoryAlert, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]services.InventoryAlert), args.Error(1)
}

func (m *MockInventoryService) ListBatches(ctx context.Context, businessID, productID, variantID string) ([]*models.ProductBatch, error) {
	args := m.Called(ctx, businessID, productID, variantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.ProductBatch), args.Error(1)
}

func (m *MockInventoryService) ListSerials(ctx context.Context, businessID, productID, variantID string) ([]*models.ProductSerialNumber, error) {
	args := m.Called(ctx, businessID, productID, variantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.ProductSerialNumber), args.Error(1)
}

func (m *MockInventoryService) CreateAssemblyRecipe(ctx context.Context, input services.CreateAssemblyRecipeInput) (*models.AssemblyRecipe, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.AssemblyRecipe), args.Error(1)
}

func (m *MockInventoryService) ListAssemblyRecipes(ctx context.Context, businessID string) ([]*models.AssemblyRecipe, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.AssemblyRecipe), args.Error(1)
}

func (m *MockInventoryService) BuildAssembly(ctx context.Context, businessID, recipeID string, input services.ExecuteAssemblyInput) error {
	args := m.Called(ctx, businessID, recipeID, input)
	return args.Error(0)
}

func (m *MockInventoryService) DisassembleAssembly(ctx context.Context, businessID, recipeID string, input services.ExecuteAssemblyInput) error {
	args := m.Called(ctx, businessID, recipeID, input)
	return args.Error(0)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type InventoryHandlerTestable struct {
	svc *MockInventoryService
	log *logger.Logger
}

func NewInventoryHandlerTestable(svc *MockInventoryService, log *logger.Logger) *InventoryHandlerTestable {
	return &InventoryHandlerTestable{
		svc: svc,
		log: log,
	}
}

func (h *InventoryHandlerTestable) ListWarehouses(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}

	role := c.GetString("role")
	warehouses, err := h.svc.ListWarehousesForUser(c.Request.Context(), businessID, userID, role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": warehouses})
}

func (h *InventoryHandlerTestable) CreateWarehouse(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}

	role := c.GetString("role")
	if role != "admin" && !h.svc.IsBusinessOwner(c.Request.Context(), userID, businessID) {
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

func (h *InventoryHandlerTestable) UpdateWarehouse(c *gin.Context) {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, warehouse)
}

func (h *InventoryHandlerTestable) DeleteWarehouse(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	warehouseID := c.Param("id")

	if !h.hasWarehouseManagementAccess(c, businessID, warehouseID) {
		return
	}

	if err := h.svc.DeleteWarehouse(c.Request.Context(), businessID, warehouseID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *InventoryHandlerTestable) UpsertCatalog(c *gin.Context) {
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

func (h *InventoryHandlerTestable) CreateAdjustment(c *gin.Context) {
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

func (h *InventoryHandlerTestable) CreateTransfer(c *gin.Context) {
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

func (h *InventoryHandlerTestable) ListBatches(c *gin.Context) {
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

func (h *InventoryHandlerTestable) ListSerials(c *gin.Context) {
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

func (h *InventoryHandlerTestable) CreateAssemblyRecipe(c *gin.Context) {
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

func (h *InventoryHandlerTestable) ListAssemblyRecipes(c *gin.Context) {
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

func (h *InventoryHandlerTestable) BuildAssembly(c *gin.Context) {
	h.executeAssembly(c, false)
}

func (h *InventoryHandlerTestable) DisassembleAssembly(c *gin.Context) {
	h.executeAssembly(c, true)
}

func (h *InventoryHandlerTestable) executeAssembly(c *gin.Context, reverse bool) {
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

func (h *InventoryHandlerTestable) Timeline(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	warehouseID := c.Query("warehouse_id")
	if warehouseID != "" && !h.hasWarehousePermission(c, businessID, warehouseID, "view_reports") {
		return
	}

	filter := services.InventoryTimelineFilter{
		BusinessID:  businessID,
		ProductID:   c.Query("product_id"),
		VariantID:   c.Query("variant_id"),
		WarehouseID: warehouseID,
		CategoryID:  c.Query("category_id"),
		Limit:       100,
	}

	rows, err := h.svc.GetTimeline(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (h *InventoryHandlerTestable) Valuation(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}

	warehouseID := c.Query("warehouse_id")
	if warehouseID != "" && !h.hasWarehousePermission(c, businessID, warehouseID, "view_reports") {
		return
	}

	filter := services.InventoryValuationFilter{
		BusinessID:  businessID,
		ProductID:   c.Query("product_id"),
		VariantID:   c.Query("variant_id"),
		WarehouseID: warehouseID,
		CategoryID:  c.Query("category_id"),
	}

	rows, total, err := h.svc.GetValuation(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total_value": total})
}

func (h *InventoryHandlerTestable) hasWarehousePermission(c *gin.Context, businessID, warehouseID, permission string) bool {
	if warehouseID == "" {
		return true
	}
	role := c.GetString("role")
	if role == "admin" {
		return true
	}
	userID := c.GetString("user_id")
	if h.svc.UserHasWarehouseAccess(c.Request.Context(), userID, businessID, warehouseID, permission) {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "access denied to this warehouse"})
	return false
}

func (h *InventoryHandlerTestable) hasWarehouseManagementAccess(c *gin.Context, businessID, warehouseID string) bool {
	if warehouseID == "" {
		return true
	}
	role := c.GetString("role")
	if role == "admin" {
		return true
	}
	userID := c.GetString("user_id")
	if h.svc.IsBusinessOwner(c.Request.Context(), userID, businessID) {
		return true
	}
	if h.svc.UserHasWarehouseAccess(c.Request.Context(), userID, businessID, warehouseID, "manage_warehouse") {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "access denied to manage this warehouse"})
	return false
}

func requireBusinessScope(c *gin.Context) (string, bool) {
	businessID, exists := c.Get("business_id")
	if !exists || businessID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return "", false
	}
	return businessID.(string), true
}

func requireUserScope(c *gin.Context) (string, bool) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user scope required"})
		return "", false
	}
	return userID.(string), true
}

// =============================================================================
// ListWarehouses Tests
// =============================================================================

func TestListWarehouses_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	warehouses := []*models.Warehouse{
		{ID: "wh-1", BusinessID: "biz-123", Name: "Main Warehouse", Code: "MAIN"},
		{ID: "wh-2", BusinessID: "biz-123", Name: "Secondary", Code: "SEC"},
	}
	mockSvc.On("ListWarehousesForUser", mock.Anything, "biz-123", "user-123", "member").Return(warehouses, nil)

	router := gin.New()
	router.GET("/warehouses", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.ListWarehouses(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/warehouses", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListWarehouses_Unauthorized(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	router := gin.New()
	router.GET("/warehouses", func(c *gin.Context) {
		createInventoryTestContext(c, "", "biz-123", "member")
		handler.ListWarehouses(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/warehouses", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

func TestListWarehouses_InternalError(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	mockSvc.On("ListWarehousesForUser", mock.Anything, "biz-123", "user-123", "member").Return(nil, errors.New("database error"))

	router := gin.New()
	router.GET("/warehouses", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.ListWarehouses(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/warehouses", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// CreateWarehouse Tests
// =============================================================================

func TestCreateWarehouse_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	warehouse := &models.Warehouse{
		ID:         "wh-123",
		BusinessID: "biz-123",
		Name:       "New Warehouse",
		Code:       "NEW",
	}
	mockSvc.On("CreateWarehouse", mock.Anything, mock.Anything).Return(warehouse, nil)

	router := gin.New()
	router.POST("/warehouses", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "admin")
		handler.CreateWarehouse(c)
	})

	reqBody := map[string]interface{}{
		"name": "New Warehouse",
		"code": "NEW",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/warehouses", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateWarehouse_Forbidden(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	mockSvc.On("IsBusinessOwner", mock.Anything, "user-123", "biz-123").Return(false)

	router := gin.New()
	router.POST("/warehouses", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.CreateWarehouse(c)
	})

	reqBody := map[string]interface{}{
		"name": "New Warehouse",
		"code": "NEW",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/warehouses", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateWarehouse_InvalidInput(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/warehouses", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "admin")
		handler.CreateWarehouse(c)
	})

	reqBody := map[string]interface{}{
		"code": "NEW",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/warehouses", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// UpdateWarehouse Tests
// =============================================================================

func TestUpdateWarehouse_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	warehouse := &models.Warehouse{
		ID:   "wh-123",
		Name: "Updated Warehouse",
		Code: "UPD",
	}
	mockSvc.On("UpdateWarehouse", mock.Anything, "biz-123", "wh-123", mock.Anything).Return(warehouse, nil)

	router := gin.New()
	router.PUT("/warehouses/:id", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "admin")
		handler.UpdateWarehouse(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Warehouse",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/warehouses/wh-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestUpdateWarehouse_AccessDenied(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	mockSvc.On("IsBusinessOwner", mock.Anything, "user-123", "biz-123").Return(false)
	mockSvc.On("UserHasWarehouseAccess", mock.Anything, "user-123", "biz-123", "wh-123", "manage_warehouse").Return(false)

	router := gin.New()
	router.PUT("/warehouses/:id", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.UpdateWarehouse(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Warehouse",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/warehouses/wh-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DeleteWarehouse Tests
// =============================================================================

func TestDeleteWarehouse_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	mockSvc.On("DeleteWarehouse", mock.Anything, "biz-123", "wh-123").Return(nil)

	router := gin.New()
	router.DELETE("/warehouses/:id", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "admin")
		handler.DeleteWarehouse(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/warehouses/wh-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// UpsertCatalog Tests
// =============================================================================

func TestUpsertCatalog_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	catalog := &models.ProductWarehouseCatalog{
		ID:        "cat-123",
		ProductID: "prod-123",
	}
	mockSvc.On("UserHasWarehouseAccess", mock.Anything, "user-123", "biz-123", "wh-123", "manage_catalog").Return(true)
	mockSvc.On("UpsertWarehouseCatalog", mock.Anything, "biz-123", "wh-123", mock.Anything).Return(catalog, nil)

	router := gin.New()
	router.PUT("/warehouses/:id/catalog", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.UpsertCatalog(c)
	})

	prodID := "123e4567-e89b-12d3-a456-426614174000"
	reqBody := map[string]interface{}{
		"product_id": prodID,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/warehouses/wh-123/catalog", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestUpsertCatalog_AccessDenied(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	mockSvc.On("UserHasWarehouseAccess", mock.Anything, "user-123", "biz-123", "wh-123", "manage_catalog").Return(false)

	router := gin.New()
	router.PUT("/warehouses/:id/catalog", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.UpsertCatalog(c)
	})

	prodID := "123e4567-e89b-12d3-a456-426614174000"
	reqBody := map[string]interface{}{
		"product_id": prodID,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/warehouses/wh-123/catalog", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// CreateAdjustment Tests
// =============================================================================

func TestCreateAdjustment_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	balances := []*models.InventoryBalance{
		{ID: "bal-123", ProductID: "prod-123", OnHand: 100},
	}
	mockSvc.On("RecordAdjustment", mock.Anything, mock.Anything).Return(balances, nil)

	router := gin.New()
	router.POST("/inventory/adjustments", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.CreateAdjustment(c)
	})

	prodID := "123e4567-e89b-12d3-a456-426614174000"
	reqBody := map[string]interface{}{
		"product_id": prodID,
		"quantity":   10,
		"reason":     "Stock count",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/inventory/adjustments", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateAdjustment_WithWarehouse_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	balances := []*models.InventoryBalance{
		{ID: "bal-123", ProductID: "prod-123", OnHand: 100},
	}
	mockSvc.On("UserHasWarehouseAccess", mock.Anything, "user-123", "biz-123", "wh-123", "move_stock").Return(true)
	mockSvc.On("RecordAdjustment", mock.Anything, mock.Anything).Return(balances, nil)

	router := gin.New()
	router.POST("/inventory/adjustments", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.CreateAdjustment(c)
	})

	prodID := "123e4567-e89b-12d3-a456-426614174000"
	reqBody := map[string]interface{}{
		"product_id":   prodID,
		"quantity":     10,
		"warehouse_id": "wh-123",
		"reason":       "Stock count",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/inventory/adjustments", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateAdjustment_InvalidInput(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/inventory/adjustments", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.CreateAdjustment(c)
	})

	reqBody := map[string]interface{}{
		"quantity": 10,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/inventory/adjustments", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// CreateTransfer Tests
// =============================================================================

func TestCreateTransfer_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	fromWhID := "123e4567-e89b-12d3-a456-426614174001"
	toWhID := "123e4567-e89b-12d3-a456-426614174002"

	mockSvc.On("UserHasWarehouseAccess", mock.Anything, "user-123", "biz-123", fromWhID, "move_stock").Return(true)
	mockSvc.On("UserHasWarehouseAccess", mock.Anything, "user-123", "biz-123", toWhID, "move_stock").Return(true)
	mockSvc.On("TransferStock", mock.Anything, mock.Anything).Return(nil)

	router := gin.New()
	router.POST("/inventory/transfers", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.CreateTransfer(c)
	})

	prodID := "123e4567-e89b-12d3-a456-426614174000"
	reqBody := map[string]interface{}{
		"product_id":        prodID,
		"from_warehouse_id": fromWhID,
		"to_warehouse_id":   toWhID,
		"quantity":          10,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/inventory/transfers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateTransfer_AccessDenied(t *testing.T) {
	fromWhID := "123e4567-e89b-12d3-a456-426614174001"
	toWhID := "123e4567-e89b-12d3-a456-426614174002"

	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	mockSvc.On("UserHasWarehouseAccess", mock.Anything, "user-123", "biz-123", fromWhID, "move_stock").Return(false)

	router := gin.New()
	router.POST("/inventory/transfers", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.CreateTransfer(c)
	})

	prodID := "123e4567-e89b-12d3-a456-426614174000"
	reqBody := map[string]interface{}{
		"product_id":        prodID,
		"from_warehouse_id": fromWhID,
		"to_warehouse_id":   toWhID,
		"quantity":          10,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/inventory/transfers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListBatches Tests
// =============================================================================

func TestListBatches_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	batches := []*models.ProductBatch{
		{ID: "batch-123", ProductID: "prod-123", BatchNumber: "B001"},
	}
	mockSvc.On("ListBatches", mock.Anything, "biz-123", "", "").Return(batches, nil)

	router := gin.New()
	router.GET("/inventory/batches", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.ListBatches(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/inventory/batches", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListSerials Tests
// =============================================================================

func TestListSerials_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	serials := []*models.ProductSerialNumber{
		{ID: "serial-123", ProductID: "prod-123", SerialNumber: "SN001"},
	}
	mockSvc.On("ListSerials", mock.Anything, "biz-123", "", "").Return(serials, nil)

	router := gin.New()
	router.GET("/inventory/serials", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.ListSerials(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/inventory/serials", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Assembly Recipe Tests
// =============================================================================

func TestCreateAssemblyRecipe_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	recipe := &models.AssemblyRecipe{
		ID:         "recipe-123",
		BusinessID: "biz-123",
		Name:       "Test Recipe",
	}
	mockSvc.On("CreateAssemblyRecipe", mock.Anything, mock.Anything).Return(recipe, nil)

	router := gin.New()
	router.POST("/assemblies", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.CreateAssemblyRecipe(c)
	})

	prodID := "123e4567-e89b-12d3-a456-426614174000"
	compProdID := "123e4567-e89b-12d3-a456-426614174001"
	reqBody := map[string]interface{}{
		"name":            "Test Recipe",
		"product_id":      prodID,
		"output_quantity": 1.0,
		"components": []map[string]interface{}{
			{"product_id": compProdID, "quantity": 2.0},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/assemblies", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListAssemblyRecipes_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	recipes := []*models.AssemblyRecipe{
		{ID: "recipe-123", BusinessID: "biz-123", Name: "Test Recipe"},
	}
	mockSvc.On("ListAssemblyRecipes", mock.Anything, "biz-123").Return(recipes, nil)

	router := gin.New()
	router.GET("/assemblies", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.ListAssemblyRecipes(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/assemblies", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestBuildAssembly_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	whID := "123e4567-e89b-12d3-a456-426614174001"
	mockSvc.On("UserHasWarehouseAccess", mock.Anything, "user-123", "biz-123", whID, "move_stock").Return(true)
	mockSvc.On("BuildAssembly", mock.Anything, "biz-123", "recipe-123", mock.Anything).Return(nil)

	router := gin.New()
	router.POST("/assemblies/:id/build", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.BuildAssembly(c)
	})

	reqBody := map[string]interface{}{
		"warehouse_id": whID,
		"quantity":     1,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/assemblies/recipe-123/build", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestDisassembleAssembly_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	whID := "123e4567-e89b-12d3-a456-426614174001"
	mockSvc.On("UserHasWarehouseAccess", mock.Anything, "user-123", "biz-123", whID, "move_stock").Return(true)
	mockSvc.On("DisassembleAssembly", mock.Anything, "biz-123", "recipe-123", mock.Anything).Return(nil)

	router := gin.New()
	router.POST("/assemblies/:id/disassemble", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.DisassembleAssembly(c)
	})

	reqBody := map[string]interface{}{
		"warehouse_id": whID,
		"quantity":     1,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/assemblies/recipe-123/disassemble", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Timeline Tests
// =============================================================================

func TestTimeline_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	entries := []services.InventoryTimelineEntry{
		{ID: "entry-123", ProductID: "prod-123", Quantity: 10},
	}
	mockSvc.On("GetTimeline", mock.Anything, mock.Anything).Return(entries, nil)

	router := gin.New()
	router.GET("/inventory/timeline", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.Timeline(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/inventory/timeline", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Valuation Tests
// =============================================================================

func TestValuation_Success(t *testing.T) {
	mockSvc := new(MockInventoryService)
	log := logger.New()
	handler := NewInventoryHandlerTestable(mockSvc, log)

	rows := []services.InventoryValuationRow{
		{ProductID: "prod-123", OnHand: 100, StockValue: 5000},
	}
	mockSvc.On("GetValuation", mock.Anything, mock.Anything).Return(rows, 5000.0, nil)

	router := gin.New()
	router.GET("/inventory/valuation", func(c *gin.Context) {
		createInventoryTestContext(c, "user-123", "biz-123", "member")
		handler.Valuation(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/inventory/valuation", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Helper
// =============================================================================

func createInventoryTestContext(c *gin.Context, userID, businessID, role string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	c.Set("role", role)
}
