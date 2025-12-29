package postgres

import (
	"context"
	"gorm.io/gorm"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

type ledgerRepository struct {
	db *gorm.DB
}

func NewLedgerRepository(db *gorm.DB) LedgerRepository {
	return &ledgerRepository{db: db}
}

func (r *ledgerRepository) Create(ctx context.Context, entry *models.LedgerEntry) error {
	return r.db.WithContext(ctx).Create(entry).Error
}

func (r *ledgerRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.LedgerEntry, int64, error) {
	var entries []models.LedgerEntry
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.LedgerEntry{}).Where("business_id = ?", businessID).Order("entry_date DESC")

	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&entries).Error; err != nil {
		return nil, err
	}

	return entries, total, nil
}

func (r *ledgerRepository) GetBalance(ctx context.Context, businessID string) (float64, error) {
	var balance float64
	err := r.db.WithContext(ctx).Model(&models.LedgerEntry{}).Where("business_id = ?", businessID).Select("COALESCE(SUM(CASE WHEN entry_type = 'credit' THEN amount ELSE -amount END))").Scan(&balance).Error
	return balance, err
}
