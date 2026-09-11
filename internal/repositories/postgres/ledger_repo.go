package postgres

import (
	"context"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type ledgerRepository struct {
	db *gorm.DB
}

func NewLedgerRepository(db *gorm.DB) interfaces.LedgerRepository {
	return &ledgerRepository{db: db}
}

func (r *ledgerRepository) Create(ctx context.Context, entry *models.LedgerEntry) error {
	return r.db.WithContext(ctx).Create(entry).Error
}

func (r *ledgerRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.LedgerEntry, int64, error) {
	var entries []models.LedgerEntry
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.LedgerEntry{}).Where("business_id = ?", businessID).Order("entry_date DESC, created_at DESC, id DESC")

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&entries).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.LedgerEntry, len(entries))
	for i := range entries {
		result[i] = &entries[i]
	}

	return result, total, nil
}

func (r *ledgerRepository) GetBalance(ctx context.Context, businessID string) (float64, error) {
	var balance struct {
		Total float64
	}
	err := r.db.WithContext(ctx).Model(&models.LedgerEntry{}).Where("business_id = ?", businessID).Select("SUM(CASE WHEN entry_type = 'debit' THEN amount ELSE -amount END) as total").Scan(&balance).Error
	return balance.Total, err
}
