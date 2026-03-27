package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type journalRepository struct {
	db *gorm.DB
}

func NewJournalRepository(db *gorm.DB) interfaces.JournalRepository {
	return &journalRepository{db: db}
}

func (r *journalRepository) Create(ctx context.Context, journal *models.Journal) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit(clause.Associations).Create(journal).Error; err != nil {
			return err
		}
		if len(journal.Lines) == 0 {
			return nil
		}
		for _, line := range journal.Lines {
			line.JournalID = journal.ID
		}
		return tx.Create(&journal.Lines).Error
	})
}

func (r *journalRepository) GetByID(ctx context.Context, id, businessID string) (*models.Journal, error) {
	var journal models.Journal
	err := r.db.WithContext(ctx).Preload("Lines").Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).First(&journal).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("journal not found")
	}
	return &journal, err
}

func (r *journalRepository) ListByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Journal, int64, error) {
	var journals []models.Journal
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.Journal{}).Where("business_id = ? AND deleted_at IS NULL", businessID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("posting_date DESC, created_at DESC").Offset(offset).Limit(limit).Find(&journals).Error; err != nil {
		return nil, 0, err
	}
	result := make([]*models.Journal, len(journals))
	for i := range journals {
		result[i] = &journals[i]
	}
	return result, total, nil
}

func (r *journalRepository) Update(ctx context.Context, journal *models.Journal) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Journal{}).Where("id = ?", journal.ID).Updates(map[string]interface{}{
			"name":           journal.Name,
			"reference":      journal.Reference,
			"status":         journal.Status,
			"posting_date":   journal.PostingDate,
			"notes":          journal.Notes,
			"source_type":    journal.SourceType,
			"source_id":      journal.SourceID,
			"reversal_of_id": journal.ReversalOfID,
			"posted_at":      journal.PostedAt,
			"reversed_at":    journal.ReversedAt,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("journal_id = ?", journal.ID).Delete(&models.JournalLine{}).Error; err != nil {
			return err
		}
		if len(journal.Lines) == 0 {
			return nil
		}
		for _, line := range journal.Lines {
			line.JournalID = journal.ID
		}
		return tx.Create(&journal.Lines).Error
	})
}

func (r *journalRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Journal{}).Error
}
