package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type inventoryRepository struct {
	db *gorm.DB
}

func NewInventoryRepository(db *gorm.DB) interfaces.InventoryRepository {
	return &inventoryRepository{db: db}
}

func (r *inventoryRepository) CreateWarehouse(ctx context.Context, warehouse *models.Warehouse) error {
	return r.db.WithContext(ctx).Create(warehouse).Error
}

func (r *inventoryRepository) GetWarehouseByID(ctx context.Context, id, businessID string) (*models.Warehouse, error) {
	var warehouse models.Warehouse
	err := r.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).First(&warehouse).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("warehouse not found")
	}
	return &warehouse, err
}

func (r *inventoryRepository) GetDefaultWarehouse(ctx context.Context, businessID string) (*models.Warehouse, error) {
	var warehouse models.Warehouse
	err := r.db.WithContext(ctx).Where("business_id = ? AND is_default = TRUE AND deleted_at IS NULL", businessID).First(&warehouse).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("warehouse not found")
	}
	return &warehouse, err
}

func (r *inventoryRepository) ListWarehouses(ctx context.Context, businessID string) ([]*models.Warehouse, error) {
	var warehouses []models.Warehouse
	if err := r.db.WithContext(ctx).Where("business_id = ? AND deleted_at IS NULL", businessID).Order("is_default DESC, name ASC").Find(&warehouses).Error; err != nil {
		return nil, err
	}
	result := make([]*models.Warehouse, len(warehouses))
	for i := range warehouses {
		result[i] = &warehouses[i]
	}
	return result, nil
}

func (r *inventoryRepository) CreateStockMove(ctx context.Context, move *models.StockMove) error {
	return r.db.WithContext(ctx).Create(move).Error
}

func (r *inventoryRepository) CreateReservation(ctx context.Context, reservation *models.InventoryReservation) error {
	return r.db.WithContext(ctx).Create(reservation).Error
}

func (r *inventoryRepository) ReleaseReservationsByDocument(ctx context.Context, documentID string) error {
	return r.db.WithContext(ctx).Model(&models.InventoryReservation{}).
		Where("document_id = ? AND status = 'active' AND deleted_at IS NULL", documentID).
		Update("status", "released").Error
}
