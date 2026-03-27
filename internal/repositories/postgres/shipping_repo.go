package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type shippingRepository struct {
	db *gorm.DB
}

func NewShippingRepository(db *gorm.DB) interfaces.ShippingRepository {
	return &shippingRepository{db: db}
}

func (r *shippingRepository) CreateShipment(ctx context.Context, shipment *models.Shipment) error {
	return r.db.WithContext(ctx).Create(shipment).Error
}

func (r *shippingRepository) GetShipmentByDocument(ctx context.Context, businessID, documentID string) (*models.Shipment, error) {
	var shipment models.Shipment
	err := r.db.WithContext(ctx).
		Where("business_id = ? AND document_id = ? AND deleted_at IS NULL", businessID, documentID).
		Order("created_at DESC").
		First(&shipment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("shipment not found")
	}
	return &shipment, err
}

func (r *shippingRepository) UpdateShipment(ctx context.Context, shipment *models.Shipment) error {
	return r.db.WithContext(ctx).Save(shipment).Error
}

func (r *shippingRepository) CreateShippingLabel(ctx context.Context, label *models.ShippingLabel) error {
	return r.db.WithContext(ctx).Create(label).Error
}

func (r *shippingRepository) GetShippingLabelByDocument(ctx context.Context, businessID, documentID string) (*models.ShippingLabel, error) {
	var label models.ShippingLabel
	err := r.db.WithContext(ctx).
		Where("business_id = ? AND document_id = ? AND deleted_at IS NULL", businessID, documentID).
		Order("created_at DESC").
		First(&label).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("shipping label not found")
	}
	return &label, err
}

func (r *shippingRepository) UpdateShippingLabel(ctx context.Context, label *models.ShippingLabel) error {
	return r.db.WithContext(ctx).Save(label).Error
}
