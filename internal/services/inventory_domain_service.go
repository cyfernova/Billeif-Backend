package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"invoice-backend/internal/models"

	"gorm.io/gorm"
)

const (
	warehousePermissionViewCatalog   = "view_catalog"
	warehousePermissionManageCatalog = "manage_catalog"
	warehousePermissionMoveStock     = "move_stock"
	warehousePermissionViewReports   = "view_reports"
	warehousePermissionManage        = "manage_warehouse"
)

type BatchAllocationInput struct {
	BatchID        string                 `json:"batch_id,omitempty"`
	BatchNumber    string                 `json:"batch_number,omitempty"`
	Quantity       float64                `json:"quantity" binding:"required,gt=0"`
	ManufacturedAt *time.Time             `json:"manufactured_at,omitempty"`
	ExpiresAt      *time.Time             `json:"expires_at,omitempty"`
	ExtraFields    map[string]interface{} `json:"extra_fields,omitempty"`
}

type WarehouseCatalogInput struct {
	ProductID     string   `json:"product_id" binding:"required,uuid"`
	IsVisible     *bool    `json:"is_visible,omitempty"`
	IsActive      *bool    `json:"is_active,omitempty"`
	PriceOverride *float64 `json:"price_override,omitempty"`
}

type WarehousePermissionInput struct {
	UserID             string `json:"user_id" binding:"required,uuid"`
	CanViewCatalog     bool   `json:"can_view_catalog"`
	CanManageCatalog   bool   `json:"can_manage_catalog"`
	CanMoveStock       bool   `json:"can_move_stock"`
	CanViewReports     bool   `json:"can_view_reports"`
	CanManageWarehouse bool   `json:"can_manage_warehouse"`
}

type CreateWarehouseInput struct {
	BusinessID string `json:"business_id,omitempty"`
	BranchID   string `json:"branch_id,omitempty" binding:"omitempty,uuid"`
	Name       string `json:"name" binding:"required"`
	Code       string `json:"code" binding:"required"`
	Address    string `json:"address"`
	City       string `json:"city"`
	State      string `json:"state"`
	Country    string `json:"country"`
	PostalCode string `json:"postal_code"`
	IsDefault  bool   `json:"is_default"`
}

type UpdateWarehouseInput struct {
	BranchID   string `json:"branch_id,omitempty" binding:"omitempty,uuid"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	Address    string `json:"address"`
	City       string `json:"city"`
	State      string `json:"state"`
	Country    string `json:"country"`
	PostalCode string `json:"postal_code"`
	IsDefault  *bool  `json:"is_default,omitempty"`
}

type InventoryAdjustmentInput struct {
	UserID           string                 `json:"user_id,omitempty"`
	BusinessID       string                 `json:"business_id,omitempty"`
	ProjectID        string                 `json:"project_id,omitempty" binding:"omitempty,uuid"`
	ProductID        string                 `json:"product_id" binding:"required,uuid"`
	VariantID        string                 `json:"variant_id,omitempty"`
	WarehouseID      string                 `json:"warehouse_id,omitempty"`
	Quantity         float64                `json:"quantity" binding:"required"`
	Reason           string                 `json:"reason"`
	UnitCost         float64                `json:"unit_cost"`
	BatchAllocations []BatchAllocationInput `json:"batch_allocations,omitempty"`
	SerialIDs        []string               `json:"serial_ids,omitempty"`
}

type InventoryTransferInput struct {
	UserID           string                 `json:"user_id,omitempty"`
	BusinessID       string                 `json:"business_id,omitempty"`
	ProjectID        string                 `json:"project_id,omitempty" binding:"omitempty,uuid"`
	ProductID        string                 `json:"product_id" binding:"required,uuid"`
	VariantID        string                 `json:"variant_id,omitempty"`
	FromWarehouseID  string                 `json:"from_warehouse_id" binding:"required,uuid"`
	ToWarehouseID    string                 `json:"to_warehouse_id" binding:"required,uuid"`
	Quantity         float64                `json:"quantity" binding:"required,gt=0"`
	Reason           string                 `json:"reason"`
	UnitCost         float64                `json:"unit_cost"`
	BatchAllocations []BatchAllocationInput `json:"batch_allocations,omitempty"`
	SerialIDs        []string               `json:"serial_ids,omitempty"`
}

type InventoryResetInput struct {
	UserID      string  `json:"user_id,omitempty"`
	BusinessID  string  `json:"business_id,omitempty"`
	ProjectID   string  `json:"project_id,omitempty" binding:"omitempty,uuid"`
	ProductID   string  `json:"product_id" binding:"required,uuid"`
	VariantID   string  `json:"variant_id,omitempty"`
	WarehouseID string  `json:"warehouse_id,omitempty"`
	TargetQty   float64 `json:"target_qty" binding:"required,gte=0"`
	Reason      string  `json:"reason"`
}

type InventoryTimelineFilter struct {
	BusinessID   string
	ProductID    string
	VariantID    string
	WarehouseID  string
	WarehouseIDs []string
	CategoryID   string
	DateFrom     *time.Time
	DateTo       *time.Time
	Limit        int
}

type InventoryValuationFilter struct {
	BusinessID   string
	ProductID    string
	VariantID    string
	WarehouseID  string
	WarehouseIDs []string
	CategoryID   string
	At           *time.Time
}

type InventoryAlertFilter struct {
	BusinessID       string
	WarehouseID      string
	WarehouseIDs     []string
	VariantID        string
	ProductID        string
	IncludeExpiry    bool
	ExpiryWithinDays int
}

type CreateAssemblyRecipeInput struct {
	BusinessID     string                         `json:"business_id,omitempty"`
	Name           string                         `json:"name" binding:"required"`
	ProductID      string                         `json:"product_id" binding:"required,uuid"`
	VariantID      string                         `json:"variant_id,omitempty"`
	OutputQuantity float64                        `json:"output_quantity" binding:"required,gt=0"`
	Metadata       map[string]interface{}         `json:"metadata,omitempty"`
	Components     []AssemblyRecipeComponentInput `json:"components" binding:"required,min=1,dive"`
}

type AssemblyRecipeComponentInput struct {
	ProductID string  `json:"product_id" binding:"required,uuid"`
	VariantID string  `json:"variant_id,omitempty"`
	Quantity  float64 `json:"quantity" binding:"required,gt=0"`
}

type ExecuteAssemblyComponentMovementInput struct {
	ProductID        string                 `json:"product_id" binding:"required,uuid"`
	VariantID        string                 `json:"variant_id,omitempty"`
	BatchAllocations []BatchAllocationInput `json:"batch_allocations,omitempty"`
	SerialIDs        []string               `json:"serial_ids,omitempty"`
}

type ExecuteAssemblyInput struct {
	BusinessID      string                                  `json:"business_id,omitempty"`
	UserID          string                                  `json:"user_id,omitempty"`
	WarehouseID     string                                  `json:"warehouse_id" binding:"required,uuid"`
	Quantity        float64                                 `json:"quantity" binding:"required,gt=0"`
	Reason          string                                  `json:"reason"`
	OutputBatches   []BatchAllocationInput                  `json:"output_batches,omitempty"`
	OutputSerialIDs []string                                `json:"output_serial_ids,omitempty"`
	ComponentMoves  []ExecuteAssemblyComponentMovementInput `json:"component_moves,omitempty"`
}

type InventoryTimelineEntry struct {
	ID              string                 `json:"id"`
	ProductID       string                 `json:"product_id"`
	VariantID       *string                `json:"variant_id,omitempty"`
	ProductName     string                 `json:"product_name,omitempty"`
	VariantName     string                 `json:"variant_name,omitempty"`
	WarehouseID     *string                `json:"warehouse_id,omitempty"`
	WarehouseName   string                 `json:"warehouse_name,omitempty"`
	Direction       string                 `json:"direction"`
	TransactionType string                 `json:"transaction_type"`
	Quantity        float64                `json:"quantity"`
	UnitCost        float64                `json:"unit_cost"`
	Reason          string                 `json:"reason,omitempty"`
	RecordedAt      time.Time              `json:"recorded_at"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

type InventoryValuationRow struct {
	ProductID     string  `json:"product_id"`
	ProductName   string  `json:"product_name"`
	VariantID     string  `json:"variant_id"`
	VariantName   string  `json:"variant_name"`
	WarehouseID   string  `json:"warehouse_id"`
	WarehouseName string  `json:"warehouse_name"`
	OnHand        float64 `json:"on_hand"`
	Reserved      float64 `json:"reserved"`
	Available     float64 `json:"available"`
	UnitCost      float64 `json:"unit_cost"`
	StockValue    float64 `json:"stock_value"`
}

type InventoryAlert struct {
	AlertType     string     `json:"alert_type"`
	ProductID     string     `json:"product_id"`
	ProductName   string     `json:"product_name"`
	VariantID     string     `json:"variant_id"`
	VariantName   string     `json:"variant_name"`
	WarehouseID   string     `json:"warehouse_id,omitempty"`
	WarehouseName string     `json:"warehouse_name,omitempty"`
	CurrentQty    float64    `json:"current_qty"`
	Threshold     float64    `json:"threshold"`
	BatchID       string     `json:"batch_id,omitempty"`
	BatchNumber   string     `json:"batch_number,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

func (s *InventoryService) ListWarehouses(ctx context.Context, businessID string) ([]*models.Warehouse, error) {
	warehouses, err := s.loadWarehouses(ctx, businessID)
	if err != nil {
		return nil, err
	}
	if err := s.decorateWarehouseSummaries(ctx, businessID, warehouses, "", true); err != nil {
		return nil, err
	}
	return warehouses, nil
}

func (s *InventoryService) ListWarehousesForUser(ctx context.Context, businessID, userID, role string) ([]*models.Warehouse, error) {
	if role == "admin" || s.IsBusinessOwner(ctx, userID, businessID) || s.db == nil {
		return s.ListWarehouses(ctx, businessID)
	}

	var total int64
	if err := s.db.WithContext(ctx).
		Model(&models.WarehousePermission{}).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Count(&total).Error; err != nil {
		return nil, err
	}
	if total == 0 {
		warehouses, err := s.loadWarehouses(ctx, businessID)
		if err != nil {
			return nil, err
		}
		result := s.filterWarehousesByBranchAccess(ctx, userID, businessID, warehouses)
		if err := s.decorateWarehouseSummaries(ctx, businessID, result, userID, false); err != nil {
			return nil, err
		}
		return result, nil
	}

	var rows []models.Warehouse
	if err := s.db.WithContext(ctx).
		Model(&models.Warehouse{}).
		Joins("JOIN warehouse_permissions wp ON wp.warehouse_id = warehouses.id AND wp.business_id = warehouses.business_id AND wp.deleted_at IS NULL").
		Where("warehouses.business_id = ? AND warehouses.deleted_at IS NULL AND wp.user_id = ? AND wp.can_view_catalog = ?", businessID, userID, true).
		Order("CASE WHEN warehouses.is_default THEN 0 ELSE 1 END ASC, warehouses.created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]*models.Warehouse, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	result = s.filterWarehousesByBranchAccess(ctx, userID, businessID, result)
	if err := s.decorateWarehouseSummaries(ctx, businessID, result, userID, false); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *InventoryService) filterWarehousesByBranchAccess(ctx context.Context, userID, businessID string, warehouses []*models.Warehouse) []*models.Warehouse {
	result := make([]*models.Warehouse, 0, len(warehouses))
	for _, warehouse := range warehouses {
		if s.userHasWarehouseBranchAccess(ctx, userID, businessID, warehouse) {
			result = append(result, warehouse)
		}
	}
	return result
}

func (s *InventoryService) loadWarehouses(ctx context.Context, businessID string) ([]*models.Warehouse, error) {
	if s.repo != nil {
		return s.repo.ListWarehouses(ctx, businessID)
	}
	return s.listWarehousesFromDB(ctx, businessID)
}

func (s *InventoryService) listWarehousesFromDB(ctx context.Context, businessID string) ([]*models.Warehouse, error) {
	var rows []models.Warehouse
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("CASE WHEN is_default THEN 0 ELSE 1 END ASC, created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*models.Warehouse, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	return result, nil
}

func (s *InventoryService) IsBusinessOwner(ctx context.Context, userID, businessID string) bool {
	if userID == "" || businessID == "" || s.businessRepo == nil {
		return false
	}
	business, err := s.businessRepo.GetByID(ctx, businessID)
	return err == nil && business.OwnerID == userID
}

func (s *InventoryService) decorateWarehouseSummaries(ctx context.Context, businessID string, warehouses []*models.Warehouse, userID string, fullAccess bool) error {
	if len(warehouses) == 0 || s.db == nil {
		return nil
	}

	warehouseIDs := make([]string, 0, len(warehouses))
	for _, warehouse := range warehouses {
		warehouseIDs = append(warehouseIDs, warehouse.ID)
	}

	type countRow struct {
		WarehouseID string
		Total       int64
	}

	var productCounts []countRow
	if err := s.db.WithContext(ctx).
		Model(&models.InventoryBalance{}).
		Select("warehouse_id, COUNT(DISTINCT product_id) AS total").
		Where("business_id = ? AND warehouse_id IN ? AND deleted_at IS NULL", businessID, warehouseIDs).
		Group("warehouse_id").
		Scan(&productCounts).Error; err != nil {
		if isMissingWarehouseSummaryTableErr(err) {
			return nil
		}
		return err
	}
	productCountByWarehouse := map[string]int64{}
	for _, row := range productCounts {
		productCountByWarehouse[row.WarehouseID] = row.Total
	}

	var lowStockCounts []countRow
	lowStockSQL := `
		SELECT ib.warehouse_id, COUNT(*) AS total
		FROM (
			SELECT warehouse_id, product_id, variant_id, SUM(on_hand) AS on_hand
			FROM inventory_balances
			WHERE business_id = ? AND warehouse_id IN ? AND deleted_at IS NULL
			GROUP BY warehouse_id, product_id, variant_id
		) ib
		JOIN products p ON p.id = ib.product_id AND p.deleted_at IS NULL
		LEFT JOIN product_variants pv ON pv.id = ib.variant_id AND pv.deleted_at IS NULL
		WHERE COALESCE(NULLIF(pv.low_stock_threshold, 0), NULLIF(p.low_stock_threshold, 0), p.min_stock, 0) > 0
		  AND ib.on_hand <= COALESCE(NULLIF(pv.low_stock_threshold, 0), NULLIF(p.low_stock_threshold, 0), p.min_stock, 0)
		GROUP BY ib.warehouse_id
	`
	if err := s.db.WithContext(ctx).Raw(lowStockSQL, businessID, warehouseIDs).Scan(&lowStockCounts).Error; err != nil {
		if isMissingWarehouseSummaryTableErr(err) {
			return nil
		}
		return err
	}
	lowStockCountByWarehouse := map[string]int64{}
	for _, row := range lowStockCounts {
		lowStockCountByWarehouse[row.WarehouseID] = row.Total
	}

	permissionSummaryByWarehouse := map[string][]string{}
	if fullAccess {
		for _, warehouse := range warehouses {
			permissionSummaryByWarehouse[warehouse.ID] = fullWarehousePermissionSummary()
		}
	} else {
		var totalPermissionRows []countRow
		if err := s.db.WithContext(ctx).
			Model(&models.WarehousePermission{}).
			Select("warehouse_id, COUNT(*) AS total").
			Where("business_id = ? AND warehouse_id IN ? AND deleted_at IS NULL", businessID, warehouseIDs).
			Group("warehouse_id").
			Scan(&totalPermissionRows).Error; err != nil {
			return err
		}
		totalPermissionsByWarehouse := map[string]int64{}
		for _, row := range totalPermissionRows {
			totalPermissionsByWarehouse[row.WarehouseID] = row.Total
		}

		var permissionRows []models.WarehousePermission
		if err := s.db.WithContext(ctx).
			Where("business_id = ? AND warehouse_id IN ? AND user_id = ? AND deleted_at IS NULL", businessID, warehouseIDs, userID).
			Find(&permissionRows).Error; err != nil {
			return err
		}
		for i := range permissionRows {
			permissionSummaryByWarehouse[permissionRows[i].WarehouseID] = warehousePermissionSummary(&permissionRows[i])
		}
		for _, warehouse := range warehouses {
			if totalPermissionsByWarehouse[warehouse.ID] == 0 {
				permissionSummaryByWarehouse[warehouse.ID] = fullWarehousePermissionSummary()
			}
		}
	}

	for _, warehouse := range warehouses {
		warehouse.ProductCount = productCountByWarehouse[warehouse.ID]
		warehouse.LowStockCount = lowStockCountByWarehouse[warehouse.ID]
		warehouse.PermissionSummary = permissionSummaryByWarehouse[warehouse.ID]
	}

	return nil
}

func fullWarehousePermissionSummary() []string {
	return []string{
		warehousePermissionViewCatalog,
		warehousePermissionManageCatalog,
		warehousePermissionMoveStock,
		warehousePermissionViewReports,
		warehousePermissionManage,
	}
}

func warehousePermissionSummary(permission *models.WarehousePermission) []string {
	summary := make([]string, 0, 5)
	if permission.CanViewCatalog {
		summary = append(summary, warehousePermissionViewCatalog)
	}
	if permission.CanManageCatalog {
		summary = append(summary, warehousePermissionManageCatalog)
	}
	if permission.CanMoveStock {
		summary = append(summary, warehousePermissionMoveStock)
	}
	if permission.CanViewReports {
		summary = append(summary, warehousePermissionViewReports)
	}
	if permission.CanManageWarehouse {
		summary = append(summary, warehousePermissionManage)
	}
	return summary
}

func isMissingWarehouseSummaryTableErr(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "no such table") || strings.Contains(value, "does not exist")
}

func (s *InventoryService) CreateWarehouse(ctx context.Context, input CreateWarehouseInput) (*models.Warehouse, error) {
	warehouse := &models.Warehouse{
		BusinessID: input.BusinessID,
		BranchID:   stringPointer(input.BranchID),
		Name:       input.Name,
		Code:       strings.ToUpper(strings.TrimSpace(input.Code)),
		Address:    input.Address,
		City:       input.City,
		State:      input.State,
		Country:    input.Country,
		PostalCode: input.PostalCode,
		IsDefault:  input.IsDefault,
	}
	if warehouse.Code == "" {
		return nil, fmt.Errorf("warehouse code is required")
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if warehouse.IsDefault {
			if err := tx.Model(&models.Warehouse{}).
				Where("business_id = ? AND deleted_at IS NULL", input.BusinessID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(warehouse).Error
	}); err != nil {
		return nil, err
	}
	return warehouse, nil
}

func (s *InventoryService) UpdateWarehouse(ctx context.Context, businessID, warehouseID string, input UpdateWarehouseInput) (*models.Warehouse, error) {
	var warehouse models.Warehouse
	if err := s.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", warehouseID, businessID).First(&warehouse).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.Name != "" {
			warehouse.Name = input.Name
		}
		if input.BranchID != "" {
			warehouse.BranchID = stringPointer(input.BranchID)
		}
		if input.Code != "" {
			warehouse.Code = strings.ToUpper(strings.TrimSpace(input.Code))
		}
		if input.Address != "" {
			warehouse.Address = input.Address
		}
		if input.City != "" {
			warehouse.City = input.City
		}
		if input.State != "" {
			warehouse.State = input.State
		}
		if input.Country != "" {
			warehouse.Country = input.Country
		}
		if input.PostalCode != "" {
			warehouse.PostalCode = input.PostalCode
		}
		if input.IsDefault != nil {
			if *input.IsDefault {
				if err := tx.Model(&models.Warehouse{}).
					Where("business_id = ? AND deleted_at IS NULL", businessID).
					Update("is_default", false).Error; err != nil {
					return err
				}
			}
			warehouse.IsDefault = *input.IsDefault
		}
		return tx.Save(&warehouse).Error
	}); err != nil {
		return nil, err
	}
	return &warehouse, nil
}

func (s *InventoryService) DeleteWarehouse(ctx context.Context, businessID, warehouseID string) error {
	var warehouse models.Warehouse
	if err := s.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", warehouseID, businessID).First(&warehouse).Error; err != nil {
		return err
	}
	if warehouse.IsDefault {
		return fmt.Errorf("main warehouse cannot be deleted")
	}
	return s.db.WithContext(ctx).Delete(&warehouse).Error
}

func (s *InventoryService) UpsertWarehouseCatalog(ctx context.Context, businessID, warehouseID string, input WarehouseCatalogInput) (*models.ProductWarehouseCatalog, error) {
	record := &models.ProductWarehouseCatalog{}
	err := s.db.WithContext(ctx).
		Where("business_id = ? AND warehouse_id = ? AND product_id = ? AND deleted_at IS NULL", businessID, warehouseID, input.ProductID).
		First(record).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		record.BusinessID = businessID
		record.WarehouseID = warehouseID
		record.ProductID = input.ProductID
		record.IsVisible = true
		record.IsActive = true
	}
	if input.IsVisible != nil {
		record.IsVisible = *input.IsVisible
	}
	if input.IsActive != nil {
		record.IsActive = *input.IsActive
	}
	record.PriceOverride = input.PriceOverride
	if record.ID == "" {
		if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
			return nil, err
		}
		return record, nil
	}
	if err := s.db.WithContext(ctx).Save(record).Error; err != nil {
		return nil, err
	}
	return record, nil
}

func (s *InventoryService) UpsertWarehousePermissions(ctx context.Context, businessID, warehouseID string, inputs []WarehousePermissionInput) ([]*models.WarehousePermission, error) {
	results := make([]*models.WarehousePermission, 0, len(inputs))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, input := range inputs {
			record := &models.WarehousePermission{}
			err := tx.Where("business_id = ? AND warehouse_id = ? AND user_id = ? AND deleted_at IS NULL", businessID, warehouseID, input.UserID).
				First(record).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				record.BusinessID = businessID
				record.WarehouseID = warehouseID
				record.UserID = input.UserID
			}
			record.CanViewCatalog = input.CanViewCatalog
			record.CanManageCatalog = input.CanManageCatalog
			record.CanMoveStock = input.CanMoveStock
			record.CanViewReports = input.CanViewReports
			record.CanManageWarehouse = input.CanManageWarehouse
			if record.ID == "" {
				if err := tx.Create(record).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Save(record).Error; err != nil {
					return err
				}
			}
			results = append(results, record)
		}
		return nil
	})
	return results, err
}

func (s *InventoryService) ListWarehousePermissions(ctx context.Context, businessID, warehouseID string) ([]*models.WarehousePermission, error) {
	var records []models.WarehousePermission
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND warehouse_id = ? AND deleted_at IS NULL", businessID, warehouseID).
		Order("created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]*models.WarehousePermission, 0, len(records))
	for i := range records {
		result = append(result, &records[i])
	}
	return result, nil
}

func (s *InventoryService) UserHasWarehouseAccess(ctx context.Context, userID, businessID, warehouseID, permission string) bool {
	if userID == "" || businessID == "" || warehouseID == "" {
		return false
	}
	business, err := s.businessRepo.GetByID(ctx, businessID)
	if err == nil && business.OwnerID == userID {
		return true
	}

	var warehouse models.Warehouse
	if err := s.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", warehouseID, businessID).First(&warehouse).Error; err != nil {
		return false
	}
	if !s.userHasWarehouseBranchAccess(ctx, userID, businessID, &warehouse) {
		return false
	}

	var total int64
	if err := s.db.WithContext(ctx).Model(&models.WarehousePermission{}).
		Where("business_id = ? AND warehouse_id = ? AND deleted_at IS NULL", businessID, warehouseID).
		Count(&total).Error; err != nil {
		return false
	}
	if total == 0 {
		return true
	}

	var permissionRow models.WarehousePermission
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND warehouse_id = ? AND user_id = ? AND deleted_at IS NULL", businessID, warehouseID, userID).
		First(&permissionRow).Error; err != nil {
		return false
	}
	switch permission {
	case warehousePermissionManageCatalog:
		return permissionRow.CanManageCatalog
	case warehousePermissionMoveStock:
		return permissionRow.CanMoveStock
	case warehousePermissionViewReports:
		return permissionRow.CanViewReports
	case warehousePermissionManage:
		return permissionRow.CanManageWarehouse
	default:
		return permissionRow.CanViewCatalog
	}
}

func (s *InventoryService) userHasWarehouseBranchAccess(ctx context.Context, userID, businessID string, warehouse *models.Warehouse) bool {
	if warehouse == nil {
		return false
	}
	var members []*models.TeamMember
	if s.teamRepo != nil {
		rows, err := s.teamRepo.GetByUserID(ctx, userID)
		if err != nil {
			return false
		}
		members = rows
	} else if s.db != nil {
		var rows []models.TeamMember
		if err := s.db.WithContext(ctx).
			Where("user_id = ? AND business_id = ? AND status = ? AND deleted_at IS NULL", userID, businessID, "active").
			Find(&rows).Error; err != nil {
			return false
		}
		for i := range rows {
			members = append(members, &rows[i])
		}
	}

	for _, member := range members {
		if member == nil || member.BusinessID != businessID || !strings.EqualFold(member.Status, "active") {
			continue
		}
		scopes := branchScopes(member.BranchScopeJSON)
		if len(scopes) == 0 || containsStringValue(scopes, "*") {
			return true
		}
		if warehouse.BranchID != nil && containsStringValue(scopes, *warehouse.BranchID) {
			return true
		}
		return false
	}
	return false
}

func containsStringValue(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func (s *InventoryService) ReportWarehouseScope(ctx context.Context, userID, businessID string, branchIDs []string) (bool, []string, error) {
	if userID == "" || businessID == "" {
		return false, nil, fmt.Errorf("user and business scope are required")
	}
	if s.IsBusinessOwner(ctx, userID, businessID) && len(branchIDs) == 0 {
		return true, nil, nil
	}

	var warehouses []models.Warehouse
	query := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID)
	if len(branchIDs) > 0 {
		query = query.Where("branch_id IN ?", branchIDs)
	}
	if err := query.Order("id ASC").Find(&warehouses).Error; err != nil {
		return false, nil, err
	}
	if len(warehouses) == 0 {
		return false, []string{}, nil
	}

	warehouseIDs := make([]string, 0, len(warehouses))
	for i := range warehouses {
		warehouseIDs = append(warehouseIDs, warehouses[i].ID)
	}

	type permissionCount struct {
		WarehouseID string
		Total       int64
	}
	var counts []permissionCount
	if err := s.db.WithContext(ctx).
		Model(&models.WarehousePermission{}).
		Select("warehouse_id, COUNT(*) AS total").
		Where("business_id = ? AND warehouse_id IN ? AND deleted_at IS NULL", businessID, warehouseIDs).
		Group("warehouse_id").
		Scan(&counts).Error; err != nil {
		return false, nil, err
	}
	totalByWarehouse := make(map[string]int64, len(counts))
	for _, count := range counts {
		totalByWarehouse[count.WarehouseID] = count.Total
	}

	var userPermissions []models.WarehousePermission
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND warehouse_id IN ? AND user_id = ? AND deleted_at IS NULL", businessID, warehouseIDs, userID).
		Find(&userPermissions).Error; err != nil {
		return false, nil, err
	}
	canViewByWarehouse := make(map[string]bool, len(userPermissions))
	for _, permission := range userPermissions {
		canViewByWarehouse[permission.WarehouseID] = permission.CanViewReports
	}

	allowed := make([]string, 0, len(warehouseIDs))
	for _, warehouseID := range warehouseIDs {
		if totalByWarehouse[warehouseID] == 0 || canViewByWarehouse[warehouseID] {
			allowed = append(allowed, warehouseID)
		}
	}
	return len(branchIDs) == 0 && len(allowed) == len(warehouseIDs), allowed, nil
}

func (s *InventoryService) EnsureDefaultVariant(ctx context.Context, businessID string, product *models.Product) (*models.ProductVariant, error) {
	var result *models.ProductVariant
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		variant, err := s.ensureDefaultVariantTx(tx, businessID, product)
		if err != nil {
			return err
		}
		result = variant
		return nil
	})
	return result, err
}

func (s *InventoryService) ensureDefaultVariantTx(tx *gorm.DB, businessID string, product *models.Product) (*models.ProductVariant, error) {
	var variant models.ProductVariant
	err := tx.Where("product_id = ? AND is_default = TRUE AND deleted_at IS NULL", product.ID).First(&variant).Error
	if err == nil {
		return &variant, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	variant = models.ProductVariant{
		BusinessID:        businessID,
		ProductID:         product.ID,
		Name:              "Default",
		SKU:               product.SKU,
		Barcode:           firstNonEmpty(product.Barcode, product.SKU),
		IsDefault:         true,
		Price:             product.Price,
		CostPrice:         product.CostPrice,
		StockLevel:        float64(product.StockLevel),
		LowStockThreshold: float64(maxInt64(product.LowStockThreshold, product.MinStock)),
		IsActive:          product.IsActive,
	}
	if err := tx.Create(&variant).Error; err != nil {
		return nil, err
	}
	return &variant, nil
}

func (s *InventoryService) resolveVariantTx(tx *gorm.DB, businessID, productID, variantID string) (*models.Product, *models.ProductVariant, error) {
	var product models.Product
	if err := tx.Where("id = ? AND business_id = ? AND deleted_at IS NULL", productID, businessID).First(&product).Error; err != nil {
		return nil, nil, err
	}
	if variantID == "" {
		variant, err := s.ensureDefaultVariantTx(tx, businessID, &product)
		return &product, variant, err
	}
	var variant models.ProductVariant
	if err := tx.Where("id = ? AND business_id = ? AND product_id = ? AND deleted_at IS NULL", variantID, businessID, productID).First(&variant).Error; err != nil {
		return nil, nil, err
	}
	return &product, &variant, nil
}

func (s *InventoryService) RecordAdjustment(ctx context.Context, input InventoryAdjustmentInput) ([]*models.InventoryBalance, error) {
	var balances []*models.InventoryBalance
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		warehouseID, err := s.resolveWarehouseIDTx(tx, input.BusinessID, input.WarehouseID)
		if err != nil {
			return err
		}
		product, variant, err := s.resolveVariantTx(tx, input.BusinessID, input.ProductID, input.VariantID)
		if err != nil {
			return err
		}
		balances, err = s.applyInventoryMutationTx(tx, inventoryMutationInput{
			BusinessID:       input.BusinessID,
			Product:          product,
			Variant:          variant,
			WarehouseID:      warehouseID,
			ProjectID:        input.ProjectID,
			Quantity:         input.Quantity,
			Reason:           input.Reason,
			UnitCost:         input.UnitCost,
			BatchAllocations: input.BatchAllocations,
			SerialIDs:        input.SerialIDs,
			TransactionType:  models.InventoryTransactionTypeAdjustment,
		})
		return err
	})
	return balances, err
}

func (s *InventoryService) TransferStock(ctx context.Context, input InventoryTransferInput) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.FromWarehouseID == input.ToWarehouseID {
			return fmt.Errorf("source and destination warehouses must differ")
		}
		product, variant, err := s.resolveVariantTx(tx, input.BusinessID, input.ProductID, input.VariantID)
		if err != nil {
			return err
		}
		if variant.TrackSerials {
			return s.transferSerialsTx(tx, product, variant, input)
		}
		if variant.TrackBatches {
			if len(input.BatchAllocations) == 0 {
				return fmt.Errorf("batch allocations are required for batch-tracked variants")
			}
			for _, allocation := range input.BatchAllocations {
				batch, err := s.resolveBatchTx(tx, input.BusinessID, product.ID, variant.ID, allocation)
				if err != nil {
					return err
				}
				if _, err := s.applySimpleBalanceTx(tx, simpleBalanceMutation{
					BusinessID:        input.BusinessID,
					ProductID:         product.ID,
					VariantID:         variant.ID,
					WarehouseID:       input.FromWarehouseID,
					ProjectID:         input.ProjectID,
					Quantity:          -allocation.Quantity,
					UnitCost:          input.UnitCost,
					Reason:            firstNonEmpty(input.Reason, "warehouse transfer"),
					TransactionType:   models.InventoryTransactionTypeTransfer,
					Direction:         models.StockMoveDirectionOut,
					Batch:             batch,
					SourceWarehouseID: &input.FromWarehouseID,
				}); err != nil {
					return err
				}
				if _, err := s.applySimpleBalanceTx(tx, simpleBalanceMutation{
					BusinessID:        input.BusinessID,
					ProductID:         product.ID,
					VariantID:         variant.ID,
					WarehouseID:       input.ToWarehouseID,
					ProjectID:         input.ProjectID,
					Quantity:          allocation.Quantity,
					UnitCost:          input.UnitCost,
					Reason:            firstNonEmpty(input.Reason, "warehouse transfer"),
					TransactionType:   models.InventoryTransactionTypeTransfer,
					Direction:         models.StockMoveDirectionIn,
					Batch:             batch,
					SourceWarehouseID: &input.FromWarehouseID,
				}); err != nil {
					return err
				}
			}
			return s.refreshCachesTx(tx, product.ID, variant.ID, input.FromWarehouseID, input.ToWarehouseID)
		}
		if _, err := s.applySimpleBalanceTx(tx, simpleBalanceMutation{
			BusinessID:        input.BusinessID,
			ProductID:         product.ID,
			VariantID:         variant.ID,
			WarehouseID:       input.FromWarehouseID,
			ProjectID:         input.ProjectID,
			Quantity:          -input.Quantity,
			UnitCost:          input.UnitCost,
			Reason:            firstNonEmpty(input.Reason, "warehouse transfer"),
			TransactionType:   models.InventoryTransactionTypeTransfer,
			Direction:         models.StockMoveDirectionOut,
			SourceWarehouseID: &input.FromWarehouseID,
		}); err != nil {
			return err
		}
		if _, err := s.applySimpleBalanceTx(tx, simpleBalanceMutation{
			BusinessID:        input.BusinessID,
			ProductID:         product.ID,
			VariantID:         variant.ID,
			WarehouseID:       input.ToWarehouseID,
			ProjectID:         input.ProjectID,
			Quantity:          input.Quantity,
			UnitCost:          input.UnitCost,
			Reason:            firstNonEmpty(input.Reason, "warehouse transfer"),
			TransactionType:   models.InventoryTransactionTypeTransfer,
			Direction:         models.StockMoveDirectionIn,
			SourceWarehouseID: &input.FromWarehouseID,
		}); err != nil {
			return err
		}
		return s.refreshCachesTx(tx, product.ID, variant.ID, input.FromWarehouseID, input.ToWarehouseID)
	})
}

func (s *InventoryService) ResetStock(ctx context.Context, input InventoryResetInput) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		warehouseID, err := s.resolveWarehouseIDTx(tx, input.BusinessID, input.WarehouseID)
		if err != nil {
			return err
		}
		product, variant, err := s.resolveVariantTx(tx, input.BusinessID, input.ProductID, input.VariantID)
		if err != nil {
			return err
		}
		if variant.TrackSerials {
			return fmt.Errorf("serial-tracked variants cannot be reset without explicit serial-level operations")
		}
		current, err := s.currentOnHandTx(tx, input.BusinessID, product.ID, variant.ID, warehouseID)
		if err != nil {
			return err
		}
		delta := input.TargetQty - current
		if math.Abs(delta) < 0.0001 {
			return nil
		}
		_, err = s.applySimpleBalanceTx(tx, simpleBalanceMutation{
			BusinessID:      input.BusinessID,
			ProductID:       product.ID,
			VariantID:       variant.ID,
			WarehouseID:     warehouseID,
			ProjectID:       input.ProjectID,
			Quantity:        delta,
			Reason:          firstNonEmpty(input.Reason, "inventory reset"),
			TransactionType: models.InventoryTransactionTypeReset,
			Direction:       directionForDelta(delta),
			UnitCost:        variant.CostPrice,
		})
		if err != nil {
			return err
		}
		return s.refreshCachesTx(tx, product.ID, variant.ID, warehouseID)
	})
}

func (s *InventoryService) GetTimeline(ctx context.Context, filter InventoryTimelineFilter) ([]InventoryTimelineEntry, error) {
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	query := s.db.WithContext(ctx).
		Table("stock_moves sm").
		Select(`
			sm.id,
			sm.product_id,
			sm.variant_id,
			p.name AS product_name,
			COALESCE(pv.name, '') AS variant_name,
			sm.warehouse_id,
			COALESCE(w.name, '') AS warehouse_name,
			sm.direction,
			sm.transaction_type,
			sm.quantity,
			sm.unit_cost,
			sm.reason,
			sm.metadata,
			sm.recorded_at`).
		Joins("JOIN products p ON p.id = sm.product_id").
		Joins("LEFT JOIN product_variants pv ON pv.id = sm.variant_id").
		Joins("LEFT JOIN warehouses w ON w.id = sm.warehouse_id").
		Where("sm.business_id = ? AND sm.deleted_at IS NULL", filter.BusinessID).
		Order("sm.recorded_at DESC").
		Limit(filter.Limit)
	if filter.ProductID != "" {
		query = query.Where("sm.product_id = ?", filter.ProductID)
	}
	if filter.VariantID != "" {
		query = query.Where("sm.variant_id = ?", filter.VariantID)
	}
	if filter.WarehouseID != "" {
		query = query.Where("sm.warehouse_id = ? OR sm.source_warehouse_id = ?", filter.WarehouseID, filter.WarehouseID)
	} else if filter.WarehouseIDs != nil {
		if len(filter.WarehouseIDs) == 0 {
			return []InventoryTimelineEntry{}, nil
		}
		query = query.Where("sm.warehouse_id IN ? OR sm.source_warehouse_id IN ?", filter.WarehouseIDs, filter.WarehouseIDs)
	}
	if filter.CategoryID != "" {
		query = query.Where("p.category_id = ?", filter.CategoryID)
	}
	if filter.DateFrom != nil {
		query = query.Where("sm.recorded_at >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		query = query.Where("sm.recorded_at <= ?", *filter.DateTo)
	}
	rows := []struct {
		ID              string
		ProductID       string
		VariantID       *string
		ProductName     string
		VariantName     string
		WarehouseID     *string
		WarehouseName   string
		Direction       string
		TransactionType string
		Quantity        float64
		UnitCost        float64
		Reason          string
		Metadata        string
		RecordedAt      time.Time
	}{}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]InventoryTimelineEntry, 0, len(rows))
	for _, row := range rows {
		entry := InventoryTimelineEntry{
			ID:              row.ID,
			ProductID:       row.ProductID,
			VariantID:       row.VariantID,
			ProductName:     row.ProductName,
			VariantName:     row.VariantName,
			WarehouseID:     row.WarehouseID,
			WarehouseName:   row.WarehouseName,
			Direction:       row.Direction,
			TransactionType: row.TransactionType,
			Quantity:        row.Quantity,
			UnitCost:        row.UnitCost,
			Reason:          row.Reason,
			Metadata:        unmarshalJSONMap(row.Metadata),
			RecordedAt:      row.RecordedAt,
		}
		result = append(result, entry)
	}
	return result, nil
}

func (s *InventoryService) GetValuation(ctx context.Context, filter InventoryValuationFilter) ([]InventoryValuationRow, float64, error) {
	if filter.At == nil {
		rows := []InventoryValuationRow{}
		query := s.db.WithContext(ctx).Table("inventory_balances ib").
			Select(`
				ib.product_id,
				p.name AS product_name,
				ib.variant_id,
				COALESCE(pv.name, '') AS variant_name,
				ib.warehouse_id,
				COALESCE(w.name, '') AS warehouse_name,
				SUM(ib.on_hand) AS on_hand,
				SUM(ib.reserved) AS reserved,
				SUM(ib.on_hand - ib.reserved) AS available,
				COALESCE(MAX(pv.cost_price), MAX(p.cost_price), 0) AS unit_cost,
				SUM(ib.stock_value) AS stock_value`).
			Joins("JOIN products p ON p.id = ib.product_id").
			Joins("LEFT JOIN product_variants pv ON pv.id = ib.variant_id").
			Joins("LEFT JOIN warehouses w ON w.id = ib.warehouse_id").
			Where("ib.business_id = ? AND ib.deleted_at IS NULL", filter.BusinessID).
			Group("ib.product_id, p.name, ib.variant_id, pv.name, ib.warehouse_id, w.name").
			Order("p.name ASC, pv.name ASC")
		if filter.ProductID != "" {
			query = query.Where("ib.product_id = ?", filter.ProductID)
		}
		if filter.VariantID != "" {
			query = query.Where("ib.variant_id = ?", filter.VariantID)
		}
		if filter.WarehouseID != "" {
			query = query.Where("ib.warehouse_id = ?", filter.WarehouseID)
		} else if filter.WarehouseIDs != nil {
			if len(filter.WarehouseIDs) == 0 {
				return []InventoryValuationRow{}, 0, nil
			}
			query = query.Where("ib.warehouse_id IN ?", filter.WarehouseIDs)
		}
		if filter.CategoryID != "" {
			query = query.Where("p.category_id = ?", filter.CategoryID)
		}
		if err := query.Scan(&rows).Error; err != nil {
			return nil, 0, err
		}
		total := 0.0
		for _, row := range rows {
			total += row.StockValue
		}
		return rows, total, nil
	}

	timeline, err := s.GetTimeline(ctx, InventoryTimelineFilter{
		BusinessID:   filter.BusinessID,
		ProductID:    filter.ProductID,
		VariantID:    filter.VariantID,
		WarehouseID:  filter.WarehouseID,
		WarehouseIDs: filter.WarehouseIDs,
		CategoryID:   filter.CategoryID,
		DateTo:       filter.At,
		Limit:        10000,
	})
	if err != nil {
		return nil, 0, err
	}
	type key struct {
		ProductID     string
		VariantID     string
		WarehouseID   string
		ProductName   string
		VariantName   string
		WarehouseName string
	}
	acc := map[key]*InventoryValuationRow{}
	for i := len(timeline) - 1; i >= 0; i-- {
		entry := timeline[i]
		k := key{
			ProductID:     entry.ProductID,
			VariantID:     derefString(entry.VariantID),
			WarehouseID:   derefString(entry.WarehouseID),
			ProductName:   entry.ProductName,
			VariantName:   entry.VariantName,
			WarehouseName: entry.WarehouseName,
		}
		row := acc[k]
		if row == nil {
			row = &InventoryValuationRow{
				ProductID:     entry.ProductID,
				ProductName:   entry.ProductName,
				VariantID:     derefString(entry.VariantID),
				VariantName:   entry.VariantName,
				WarehouseID:   derefString(entry.WarehouseID),
				WarehouseName: entry.WarehouseName,
				UnitCost:      entry.UnitCost,
			}
			acc[k] = row
		}
		switch entry.Direction {
		case models.StockMoveDirectionIn:
			row.OnHand += entry.Quantity
		case models.StockMoveDirectionOut:
			row.OnHand -= entry.Quantity
		case models.StockMoveDirectionReserve:
			row.Reserved += entry.Quantity
		case models.StockMoveDirectionRelease:
			row.Reserved -= entry.Quantity
		}
		if entry.UnitCost > 0 {
			row.UnitCost = entry.UnitCost
		}
	}
	rows := make([]InventoryValuationRow, 0, len(acc))
	total := 0.0
	for _, row := range acc {
		row.Available = row.OnHand - row.Reserved
		row.StockValue = row.OnHand * row.UnitCost
		total += row.StockValue
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ProductName == rows[j].ProductName {
			return rows[i].VariantName < rows[j].VariantName
		}
		return rows[i].ProductName < rows[j].ProductName
	})
	return rows, total, nil
}

func (s *InventoryService) GetAlerts(ctx context.Context, filter InventoryAlertFilter) ([]InventoryAlert, error) {
	valuationRows, _, err := s.GetValuation(ctx, InventoryValuationFilter{
		BusinessID:   filter.BusinessID,
		ProductID:    filter.ProductID,
		VariantID:    filter.VariantID,
		WarehouseID:  filter.WarehouseID,
		WarehouseIDs: filter.WarehouseIDs,
	})
	if err != nil {
		return nil, err
	}
	alerts := make([]InventoryAlert, 0)
	for _, row := range valuationRows {
		var threshold float64
		if err := s.db.WithContext(ctx).Table("product_variants pv").
			Select("COALESCE(NULLIF(pv.low_stock_threshold, 0), NULLIF(p.low_stock_threshold, 0), p.min_stock, 0)").
			Joins("JOIN products p ON p.id = pv.product_id").
			Where("pv.id = ?", row.VariantID).
			Scan(&threshold).Error; err != nil {
			return nil, err
		}
		if row.Available <= threshold {
			alerts = append(alerts, InventoryAlert{
				AlertType:     "low_stock",
				ProductID:     row.ProductID,
				ProductName:   row.ProductName,
				VariantID:     row.VariantID,
				VariantName:   row.VariantName,
				WarehouseID:   row.WarehouseID,
				WarehouseName: row.WarehouseName,
				CurrentQty:    row.Available,
				Threshold:     threshold,
			})
		}
	}
	if filter.IncludeExpiry {
		if filter.ExpiryWithinDays <= 0 {
			filter.ExpiryWithinDays = 30
		}
		var rows []struct {
			ProductID   string
			ProductName string
			VariantID   string
			VariantName string
			BatchID     string
			BatchNumber string
			ExpiresAt   *time.Time
			CurrentQty  float64
		}
		query := s.db.WithContext(ctx).Table("product_batches pb").
			Select(`
				pb.product_id,
				p.name AS product_name,
				pb.variant_id,
				COALESCE(pv.name, '') AS variant_name,
				pb.id AS batch_id,
				pb.batch_number,
				pb.expires_at,
				COALESCE(SUM(ib.on_hand - ib.reserved), 0) AS current_qty`).
			Joins("JOIN products p ON p.id = pb.product_id").
			Joins("JOIN product_variants pv ON pv.id = pb.variant_id").
			Joins("LEFT JOIN inventory_balances ib ON ib.batch_id = pb.id AND ib.deleted_at IS NULL").
			Where("pb.business_id = ? AND pb.deleted_at IS NULL AND pb.expires_at IS NOT NULL AND pb.expires_at <= ?", filter.BusinessID, time.Now().AddDate(0, 0, filter.ExpiryWithinDays)).
			Group("pb.product_id, p.name, pb.variant_id, pv.name, pb.id, pb.batch_number, pb.expires_at").
			Order("pb.expires_at ASC")
		if filter.ProductID != "" {
			query = query.Where("pb.product_id = ?", filter.ProductID)
		}
		if filter.VariantID != "" {
			query = query.Where("pb.variant_id = ?", filter.VariantID)
		}
		if filter.WarehouseIDs != nil {
			if len(filter.WarehouseIDs) == 0 {
				return alerts, nil
			}
			query = query.Where("ib.warehouse_id IN ?", filter.WarehouseIDs)
		}
		if err := query.Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			alerts = append(alerts, InventoryAlert{
				AlertType:   "expiry",
				ProductID:   row.ProductID,
				ProductName: row.ProductName,
				VariantID:   row.VariantID,
				VariantName: row.VariantName,
				CurrentQty:  row.CurrentQty,
				BatchID:     row.BatchID,
				BatchNumber: row.BatchNumber,
				ExpiresAt:   row.ExpiresAt,
			})
		}
	}
	return alerts, nil
}

func (s *InventoryService) ListBatches(ctx context.Context, businessID, productID, variantID string, warehouseIDs []string) ([]*models.ProductBatch, error) {
	var batches []models.ProductBatch
	query := s.db.WithContext(ctx).Where("business_id = ? AND deleted_at IS NULL", businessID).Order("expires_at ASC NULLS LAST, batch_number ASC")
	if warehouseIDs != nil {
		if len(warehouseIDs) == 0 {
			return []*models.ProductBatch{}, nil
		}
		query = query.Where("EXISTS (SELECT 1 FROM inventory_balances ib WHERE ib.batch_id = product_batches.id AND ib.warehouse_id IN ? AND ib.deleted_at IS NULL)", warehouseIDs)
	}
	if productID != "" {
		query = query.Where("product_id = ?", productID)
	}
	if variantID != "" {
		query = query.Where("variant_id = ?", variantID)
	}
	if err := query.Find(&batches).Error; err != nil {
		return nil, err
	}
	result := make([]*models.ProductBatch, 0, len(batches))
	for i := range batches {
		result = append(result, &batches[i])
	}
	return result, nil
}

func (s *InventoryService) ListSerials(ctx context.Context, businessID, productID, variantID string, warehouseIDs []string) ([]*models.ProductSerialNumber, error) {
	var serials []models.ProductSerialNumber
	query := s.db.WithContext(ctx).Where("business_id = ? AND deleted_at IS NULL", businessID).Order("created_at DESC")
	if warehouseIDs != nil {
		if len(warehouseIDs) == 0 {
			return []*models.ProductSerialNumber{}, nil
		}
		query = query.Where("warehouse_id IN ?", warehouseIDs)
	}
	if productID != "" {
		query = query.Where("product_id = ?", productID)
	}
	if variantID != "" {
		query = query.Where("variant_id = ?", variantID)
	}
	if err := query.Find(&serials).Error; err != nil {
		return nil, err
	}
	result := make([]*models.ProductSerialNumber, 0, len(serials))
	for i := range serials {
		result = append(result, &serials[i])
	}
	return result, nil
}

type inventoryMutationInput struct {
	BusinessID       string
	Product          *models.Product
	Variant          *models.ProductVariant
	WarehouseID      string
	ProjectID        string
	Quantity         float64
	Reason           string
	UnitCost         float64
	BatchAllocations []BatchAllocationInput
	SerialIDs        []string
	TransactionType  string
	DocumentID       *string
	DocumentLineID   *string
}

type simpleBalanceMutation struct {
	BusinessID        string
	ProductID         string
	VariantID         string
	WarehouseID       string
	ProjectID         string
	Quantity          float64
	UnitCost          float64
	Reason            string
	TransactionType   string
	Direction         string
	Batch             *models.ProductBatch
	SerialNumber      *models.ProductSerialNumber
	SourceWarehouseID *string
	DocumentID        *string
	DocumentLineID    *string
}

func (s *InventoryService) applyInventoryMutationTx(tx *gorm.DB, input inventoryMutationInput) ([]*models.InventoryBalance, error) {
	if input.Quantity == 0 {
		return nil, nil
	}
	if input.Product.IsService {
		return nil, nil
	}
	if input.Variant.TrackSerials {
		return s.applySerialMutationTx(tx, input)
	}
	if input.Variant.TrackBatches {
		return s.applyBatchMutationTx(tx, input)
	}
	balance, err := s.applySimpleBalanceTx(tx, simpleBalanceMutation{
		BusinessID:      input.BusinessID,
		ProductID:       input.Product.ID,
		VariantID:       input.Variant.ID,
		WarehouseID:     input.WarehouseID,
		ProjectID:       input.ProjectID,
		Quantity:        input.Quantity,
		UnitCost:        input.effectiveUnitCost(),
		Reason:          input.Reason,
		TransactionType: input.TransactionType,
		Direction:       directionForDelta(input.Quantity),
		DocumentID:      input.DocumentID,
		DocumentLineID:  input.DocumentLineID,
	})
	if err != nil {
		return nil, err
	}
	if err := s.refreshCachesTx(tx, input.Product.ID, input.Variant.ID, input.WarehouseID); err != nil {
		return nil, err
	}
	return []*models.InventoryBalance{balance}, nil
}

func (input inventoryMutationInput) effectiveUnitCost() float64 {
	if input.UnitCost > 0 {
		return input.UnitCost
	}
	if input.Variant != nil && input.Variant.CostPrice > 0 {
		return input.Variant.CostPrice
	}
	if input.Product != nil {
		return input.Product.CostPrice
	}
	return 0
}

func (s *InventoryService) applyBatchMutationTx(tx *gorm.DB, input inventoryMutationInput) ([]*models.InventoryBalance, error) {
	if len(input.BatchAllocations) == 0 {
		return nil, fmt.Errorf("batch allocations are required for batch-tracked variants")
	}
	total := 0.0
	balances := make([]*models.InventoryBalance, 0, len(input.BatchAllocations))
	for _, allocation := range input.BatchAllocations {
		total += allocation.Quantity
		batch, err := s.resolveBatchTx(tx, input.BusinessID, input.Product.ID, input.Variant.ID, allocation)
		if err != nil {
			return nil, err
		}
		qty := allocation.Quantity
		if input.Quantity < 0 {
			qty = -allocation.Quantity
		}
		balance, err := s.applySimpleBalanceTx(tx, simpleBalanceMutation{
			BusinessID:      input.BusinessID,
			ProductID:       input.Product.ID,
			VariantID:       input.Variant.ID,
			WarehouseID:     input.WarehouseID,
			ProjectID:       input.ProjectID,
			Quantity:        qty,
			UnitCost:        input.effectiveUnitCost(),
			Reason:          input.Reason,
			TransactionType: input.TransactionType,
			Direction:       directionForDelta(qty),
			Batch:           batch,
			DocumentID:      input.DocumentID,
			DocumentLineID:  input.DocumentLineID,
		})
		if err != nil {
			return nil, err
		}
		balances = append(balances, balance)
	}
	if math.Abs(total-math.Abs(input.Quantity)) > 0.0001 {
		return nil, fmt.Errorf("batch allocations must sum to the requested quantity")
	}
	if err := s.refreshCachesTx(tx, input.Product.ID, input.Variant.ID, input.WarehouseID); err != nil {
		return nil, err
	}
	return balances, nil
}

func (s *InventoryService) applySerialMutationTx(tx *gorm.DB, input inventoryMutationInput) ([]*models.InventoryBalance, error) {
	if len(input.SerialIDs) == 0 {
		return nil, fmt.Errorf("serial IDs are required for serial-tracked variants")
	}
	if math.Abs(math.Abs(input.Quantity)-float64(len(input.SerialIDs))) > 0.0001 {
		return nil, fmt.Errorf("serial ID count must match quantity")
	}
	balances := make([]*models.InventoryBalance, 0, len(input.SerialIDs))
	for _, serialValue := range input.SerialIDs {
		var serial *models.ProductSerialNumber
		var err error
		if input.Quantity > 0 {
			serial, err = s.createOrLoadSerialTx(tx, input.BusinessID, input.Product.ID, input.Variant.ID, input.WarehouseID, serialValue)
			if err != nil {
				return nil, err
			}
			serial.Status = models.SerialStatusAvailable
		} else {
			serial, err = s.requireAvailableSerialTx(tx, input.BusinessID, input.Product.ID, input.Variant.ID, input.WarehouseID, serialValue)
			if err != nil {
				return nil, err
			}
			serial.Status = s.serialStatusForTransaction(input.TransactionType)
			serial.WarehouseID = nil
			if input.DocumentID != nil {
				serial.SoldDocumentID = input.DocumentID
			}
			if input.DocumentLineID != nil {
				serial.SoldDocumentLineID = input.DocumentLineID
			}
		}
		if err := tx.Save(serial).Error; err != nil {
			return nil, err
		}
		balance, err := s.applySimpleBalanceTx(tx, simpleBalanceMutation{
			BusinessID:      input.BusinessID,
			ProductID:       input.Product.ID,
			VariantID:       input.Variant.ID,
			WarehouseID:     input.WarehouseID,
			ProjectID:       input.ProjectID,
			Quantity:        directionScalar(input.Quantity),
			UnitCost:        input.effectiveUnitCost(),
			Reason:          input.Reason,
			TransactionType: input.TransactionType,
			Direction:       directionForDelta(input.Quantity),
			SerialNumber:    serial,
			DocumentID:      input.DocumentID,
			DocumentLineID:  input.DocumentLineID,
		})
		if err != nil {
			return nil, err
		}
		balances = append(balances, balance)
	}
	if err := s.refreshCachesTx(tx, input.Product.ID, input.Variant.ID, input.WarehouseID); err != nil {
		return nil, err
	}
	return balances, nil
}

func (s *InventoryService) transferSerialsTx(tx *gorm.DB, product *models.Product, variant *models.ProductVariant, input InventoryTransferInput) error {
	if len(input.SerialIDs) == 0 {
		return fmt.Errorf("serial IDs are required for serial-tracked transfers")
	}
	if math.Abs(input.Quantity-float64(len(input.SerialIDs))) > 0.0001 {
		return fmt.Errorf("serial ID count must match transfer quantity")
	}
	for _, serialValue := range input.SerialIDs {
		serial, err := s.requireAvailableSerialTx(tx, input.BusinessID, product.ID, variant.ID, input.FromWarehouseID, serialValue)
		if err != nil {
			return err
		}
		if _, err := s.applySimpleBalanceTx(tx, simpleBalanceMutation{
			BusinessID:        input.BusinessID,
			ProductID:         product.ID,
			VariantID:         variant.ID,
			WarehouseID:       input.FromWarehouseID,
			ProjectID:         input.ProjectID,
			Quantity:          -1,
			UnitCost:          input.UnitCost,
			Reason:            firstNonEmpty(input.Reason, "warehouse transfer"),
			TransactionType:   models.InventoryTransactionTypeTransfer,
			Direction:         models.StockMoveDirectionOut,
			SerialNumber:      serial,
			SourceWarehouseID: &input.FromWarehouseID,
		}); err != nil {
			return err
		}
		targetWarehouseID := input.ToWarehouseID
		serial.WarehouseID = &targetWarehouseID
		serial.Status = models.SerialStatusAvailable
		if err := tx.Save(serial).Error; err != nil {
			return err
		}
		if _, err := s.applySimpleBalanceTx(tx, simpleBalanceMutation{
			BusinessID:        input.BusinessID,
			ProductID:         product.ID,
			VariantID:         variant.ID,
			WarehouseID:       input.ToWarehouseID,
			ProjectID:         input.ProjectID,
			Quantity:          1,
			UnitCost:          input.UnitCost,
			Reason:            firstNonEmpty(input.Reason, "warehouse transfer"),
			TransactionType:   models.InventoryTransactionTypeTransfer,
			Direction:         models.StockMoveDirectionIn,
			SerialNumber:      serial,
			SourceWarehouseID: &input.FromWarehouseID,
		}); err != nil {
			return err
		}
	}
	return s.refreshCachesTx(tx, product.ID, variant.ID, input.FromWarehouseID, input.ToWarehouseID)
}

func (s *InventoryService) resolveWarehouseIDTx(tx *gorm.DB, businessID, warehouseID string) (string, error) {
	if warehouseID != "" {
		var warehouse models.Warehouse
		if err := tx.Where("id = ? AND business_id = ? AND deleted_at IS NULL", warehouseID, businessID).First(&warehouse).Error; err != nil {
			return "", err
		}
		return warehouse.ID, nil
	}
	warehouse, err := s.EnsureDefaultWarehouse(tx.Statement.Context, businessID)
	if err != nil {
		return "", err
	}
	return warehouse.ID, nil
}

func (s *InventoryService) applySimpleBalanceTx(tx *gorm.DB, input simpleBalanceMutation) (*models.InventoryBalance, error) {
	batchKey := ""
	var batchID *string
	if input.Batch != nil {
		batchKey = input.Batch.ID
		batchID = &input.Batch.ID
	}
	var balance models.InventoryBalance
	err := tx.Where("business_id = ? AND product_id = ? AND variant_id = ? AND warehouse_id = ? AND batch_key = ? AND deleted_at IS NULL",
		input.BusinessID, input.ProductID, input.VariantID, input.WarehouseID, batchKey).
		First(&balance).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		balance = models.InventoryBalance{
			BusinessID:  input.BusinessID,
			ProductID:   input.ProductID,
			VariantID:   input.VariantID,
			WarehouseID: input.WarehouseID,
			BatchID:     batchID,
			BatchKey:    batchKey,
		}
	}
	nextOnHand := balance.OnHand + input.Quantity
	if nextOnHand < -0.0001 {
		return nil, fmt.Errorf("insufficient stock")
	}
	balance.OnHand = clampZero(nextOnHand)
	balance.StockValue = clampMoney(balance.StockValue + (input.Quantity * input.UnitCost))
	now := time.Now()
	balance.LastRecordedAt = &now
	if balance.ID == "" {
		if err := tx.Create(&balance).Error; err != nil {
			return nil, err
		}
	} else {
		if err := tx.Save(&balance).Error; err != nil {
			return nil, err
		}
	}
	move := models.StockMove{
		BusinessID:        input.BusinessID,
		ProductID:         input.ProductID,
		VariantID:         stringPointer(input.VariantID),
		WarehouseID:       stringPointer(input.WarehouseID),
		SourceWarehouseID: input.SourceWarehouseID,
		ProjectID:         projectIDPointer(input.ProjectID),
		BatchID:           batchID,
		TransactionType:   input.TransactionType,
		Direction:         input.Direction,
		Quantity:          math.Abs(input.Quantity),
		UnitCost:          input.UnitCost,
		Reason:            input.Reason,
		Metadata:          mustMarshalMap(map[string]interface{}{"source": "inventory_service"}),
		RecordedAt:        now,
		DocumentID:        input.DocumentID,
		DocumentLineID:    input.DocumentLineID,
	}
	if input.SerialNumber != nil {
		move.SerialNumberID = &input.SerialNumber.ID
		move.Metadata = mustMarshalMap(map[string]interface{}{
			"source":        "inventory_service",
			"serial_number": input.SerialNumber.SerialNumber,
			"imei":          input.SerialNumber.IMEI,
		})
	}
	if err := tx.Create(&move).Error; err != nil {
		return nil, err
	}
	return &balance, s.emitInventoryEventTx(tx, input.BusinessID, "stock_updated", "product_variant", input.VariantID, map[string]interface{}{
		"product_id":       input.ProductID,
		"variant_id":       input.VariantID,
		"warehouse_id":     input.WarehouseID,
		"transaction_type": input.TransactionType,
		"direction":        input.Direction,
		"quantity":         math.Abs(input.Quantity),
	})
}

func (s *InventoryService) resolveBatchTx(tx *gorm.DB, businessID, productID, variantID string, allocation BatchAllocationInput) (*models.ProductBatch, error) {
	if allocation.BatchID != "" {
		var batch models.ProductBatch
		if err := tx.Where("id = ? AND business_id = ? AND variant_id = ? AND deleted_at IS NULL", allocation.BatchID, businessID, variantID).First(&batch).Error; err != nil {
			return nil, err
		}
		return &batch, nil
	}
	if strings.TrimSpace(allocation.BatchNumber) == "" {
		return nil, fmt.Errorf("batch_number or batch_id is required")
	}
	var batch models.ProductBatch
	err := tx.Where("business_id = ? AND variant_id = ? AND batch_number = ? AND deleted_at IS NULL", businessID, variantID, allocation.BatchNumber).First(&batch).Error
	if err == nil {
		return &batch, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	batch = models.ProductBatch{
		BusinessID:     businessID,
		ProductID:      productID,
		VariantID:      variantID,
		BatchNumber:    allocation.BatchNumber,
		ManufacturedAt: allocation.ManufacturedAt,
		ExpiresAt:      allocation.ExpiresAt,
		ExtraFields:    mustMarshalMap(allocation.ExtraFields),
		IsActive:       true,
	}
	if err := tx.Create(&batch).Error; err != nil {
		return nil, err
	}
	return &batch, nil
}

func (s *InventoryService) createOrLoadSerialTx(tx *gorm.DB, businessID, productID, variantID, warehouseID, serialValue string) (*models.ProductSerialNumber, error) {
	serialValue = strings.TrimSpace(serialValue)
	if serialValue == "" {
		return nil, fmt.Errorf("serial number is required")
	}
	var serial models.ProductSerialNumber
	err := tx.Where("(serial_number = ? OR imei = ?) AND business_id = ? AND deleted_at IS NULL", serialValue, serialValue, businessID).First(&serial).Error
	if err == nil {
		if serial.Status != models.SerialStatusAvailable {
			return nil, fmt.Errorf("serial %s is not available for stock in", serialValue)
		}
		if serial.WarehouseID != nil && *serial.WarehouseID != warehouseID {
			return nil, fmt.Errorf("serial %s already belongs to another warehouse", serialValue)
		}
		return &serial, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	serial = models.ProductSerialNumber{
		BusinessID:   businessID,
		ProductID:    productID,
		VariantID:    variantID,
		WarehouseID:  stringPointer(warehouseID),
		SerialNumber: serialValue,
		Status:       models.SerialStatusAvailable,
	}
	if err := tx.Create(&serial).Error; err != nil {
		return nil, err
	}
	return &serial, nil
}

func (s *InventoryService) requireAvailableSerialTx(tx *gorm.DB, businessID, productID, variantID, warehouseID, serialValue string) (*models.ProductSerialNumber, error) {
	var serial models.ProductSerialNumber
	err := tx.Where("(serial_number = ? OR imei = ?) AND business_id = ? AND product_id = ? AND variant_id = ? AND deleted_at IS NULL", serialValue, serialValue, businessID, productID, variantID).First(&serial).Error
	if err != nil {
		return nil, err
	}
	if serial.Status != models.SerialStatusAvailable {
		return nil, fmt.Errorf("serial %s is not available", serialValue)
	}
	if serial.WarehouseID == nil || *serial.WarehouseID != warehouseID {
		return nil, fmt.Errorf("serial %s is not in the requested warehouse", serialValue)
	}
	return &serial, nil
}

func (s *InventoryService) currentOnHandTx(tx *gorm.DB, businessID, productID, variantID, warehouseID string) (float64, error) {
	var qty float64
	if err := tx.Model(&models.InventoryBalance{}).
		Where("business_id = ? AND product_id = ? AND variant_id = ? AND warehouse_id = ? AND deleted_at IS NULL", businessID, productID, variantID, warehouseID).
		Select("COALESCE(SUM(on_hand), 0)").
		Scan(&qty).Error; err != nil {
		return 0, err
	}
	return qty, nil
}

func (s *InventoryService) refreshCachesTx(tx *gorm.DB, productID, variantID string, warehouseIDs ...string) error {
	var variantTotals struct {
		OnHand   float64
		Reserved float64
	}
	if err := tx.Model(&models.InventoryBalance{}).
		Where("product_id = ? AND variant_id = ? AND deleted_at IS NULL", productID, variantID).
		Select("COALESCE(SUM(on_hand), 0) AS on_hand, COALESCE(SUM(reserved), 0) AS reserved").
		Scan(&variantTotals).Error; err != nil {
		return err
	}
	if err := tx.Model(&models.ProductVariant{}).
		Where("id = ?", variantID).
		Updates(map[string]interface{}{
			"stock_level":    variantTotals.OnHand,
			"reserved_level": variantTotals.Reserved,
		}).Error; err != nil {
		return err
	}

	var productTotals struct {
		OnHand float64
	}
	if err := tx.Model(&models.ProductVariant{}).
		Where("product_id = ? AND deleted_at IS NULL", productID).
		Select("COALESCE(SUM(stock_level), 0) AS on_hand").
		Scan(&productTotals).Error; err != nil {
		return err
	}
	if err := tx.Model(&models.Product{}).
		Where("id = ?", productID).
		Update("stock_level", int64(math.Round(productTotals.OnHand))).Error; err != nil {
		return err
	}

	today := truncateToDate(time.Now().UTC())
	for _, warehouseID := range uniqueStrings(warehouseIDs) {
		if warehouseID == "" {
			continue
		}
		var snapshot models.InventorySnapshot
		err := tx.Where("product_id = ? AND variant_id = ? AND warehouse_id = ? AND snapshot_date = ? AND deleted_at IS NULL", productID, variantID, warehouseID, today).
			First(&snapshot).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var totals struct {
			OnHand     float64
			Reserved   float64
			StockValue float64
		}
		if err := tx.Model(&models.InventoryBalance{}).
			Where("product_id = ? AND variant_id = ? AND warehouse_id = ? AND deleted_at IS NULL", productID, variantID, warehouseID).
			Select("COALESCE(SUM(on_hand), 0) AS on_hand, COALESCE(SUM(reserved), 0) AS reserved, COALESCE(SUM(stock_value), 0) AS stock_value").
			Scan(&totals).Error; err != nil {
			return err
		}
		if snapshot.ID == "" {
			snapshot = models.InventorySnapshot{
				BusinessID:   "",
				SnapshotDate: today,
				ProductID:    productID,
				VariantID:    variantID,
				WarehouseID:  warehouseID,
			}
			var product models.Product
			if err := tx.Select("business_id").Where("id = ?", productID).First(&product).Error; err == nil {
				snapshot.BusinessID = product.BusinessID
			}
		}
		snapshot.OnHand = totals.OnHand
		snapshot.Reserved = totals.Reserved
		snapshot.StockValue = totals.StockValue
		if snapshot.ID == "" {
			if err := tx.Create(&snapshot).Error; err != nil {
				return err
			}
		} else if err := tx.Save(&snapshot).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *InventoryService) emitInventoryEventTx(tx *gorm.DB, businessID, eventType, entityType, entityID string, payload map[string]interface{}) error {
	return tx.Create(&models.InventoryEventLog{
		BusinessID: businessID,
		EventType:  eventType,
		EntityType: entityType,
		EntityID:   entityID,
		Payload:    mustMarshalMap(payload),
	}).Error
}

func (s *InventoryService) serialStatusForTransaction(transactionType string) string {
	switch transactionType {
	case models.InventoryTransactionTypeAssembly:
		return models.SerialStatusConsumed
	case models.InventoryTransactionTypeTransfer:
		return models.SerialStatusAvailable
	default:
		return models.SerialStatusSold
	}
}

func directionForDelta(delta float64) string {
	if delta >= 0 {
		return models.StockMoveDirectionIn
	}
	return models.StockMoveDirectionOut
}

func directionScalar(delta float64) float64 {
	if delta >= 0 {
		return 1
	}
	return -1
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func clampZero(value float64) float64 {
	if math.Abs(value) < 0.0001 {
		return 0
	}
	return value
}

func clampMoney(value float64) float64 {
	if math.Abs(value) < 0.0001 {
		return 0
	}
	return value
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	v := value
	return &v
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func truncateToDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func marshalStringSlice(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	buf, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(buf)
}

func mustMarshalBatchAllocations(values []BatchAllocationInput) string {
	if len(values) == 0 {
		return "[]"
	}
	buf, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(buf)
}

func unmarshalBatchAllocations(raw string) []BatchAllocationInput {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var values []BatchAllocationInput
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return values
}

func unmarshalStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return values
}

func (s *InventoryService) reserveInventoryTx(tx *gorm.DB, businessID, productID, variantID, warehouseID string, quantity float64, documentID string, documentLineID *string, reason string) error {
	var balance models.InventoryBalance
	if err := tx.Where("business_id = ? AND product_id = ? AND variant_id = ? AND warehouse_id = ? AND batch_key = '' AND deleted_at IS NULL",
		businessID, productID, variantID, warehouseID).First(&balance).Error; err != nil {
		return err
	}
	available := balance.OnHand - balance.Reserved
	if available < quantity {
		return fmt.Errorf("insufficient available stock for reservation")
	}
	balance.Reserved += quantity
	now := time.Now()
	balance.LastRecordedAt = &now
	if err := tx.Save(&balance).Error; err != nil {
		return err
	}
	if err := tx.Create(&models.StockMove{
		BusinessID:      businessID,
		ProductID:       productID,
		VariantID:       stringPointer(variantID),
		WarehouseID:     stringPointer(warehouseID),
		DocumentID:      &documentID,
		DocumentLineID:  documentLineID,
		TransactionType: models.InventoryTransactionTypeReservation,
		Direction:       models.StockMoveDirectionReserve,
		Quantity:        quantity,
		Reason:          reason,
		Metadata:        mustMarshalMap(map[string]interface{}{"source": "document_reservation"}),
		RecordedAt:      now,
	}).Error; err != nil {
		return err
	}
	return s.refreshCachesTx(tx, productID, variantID, warehouseID)
}

func (s *InventoryService) releaseInventoryTx(tx *gorm.DB, businessID, productID, variantID, warehouseID string, quantity float64, documentID string, documentLineID *string, reason string) error {
	var balance models.InventoryBalance
	if err := tx.Where("business_id = ? AND product_id = ? AND variant_id = ? AND warehouse_id = ? AND batch_key = '' AND deleted_at IS NULL",
		businessID, productID, variantID, warehouseID).First(&balance).Error; err != nil {
		return err
	}
	balance.Reserved = clampZero(balance.Reserved - quantity)
	now := time.Now()
	balance.LastRecordedAt = &now
	if err := tx.Save(&balance).Error; err != nil {
		return err
	}
	if err := tx.Create(&models.StockMove{
		BusinessID:      businessID,
		ProductID:       productID,
		VariantID:       stringPointer(variantID),
		WarehouseID:     stringPointer(warehouseID),
		DocumentID:      &documentID,
		DocumentLineID:  documentLineID,
		TransactionType: models.InventoryTransactionTypeRelease,
		Direction:       models.StockMoveDirectionRelease,
		Quantity:        quantity,
		Reason:          reason,
		Metadata:        mustMarshalMap(map[string]interface{}{"source": "document_release"}),
		RecordedAt:      now,
	}).Error; err != nil {
		return err
	}
	return s.refreshCachesTx(tx, productID, variantID, warehouseID)
}
