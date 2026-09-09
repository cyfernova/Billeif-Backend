package services

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// EInvoiceListItem omits provider request and response payloads from list results.
type EInvoiceListItem struct {
	DocumentID     string     `json:"document_id"`
	DocumentNumber string     `json:"document_number"`
	Status         string     `json:"status"`
	IRN            string     `json:"irn"`
	AckNumber      string     `json:"ack_number"`
	AckDate        *time.Time `json:"ack_date,omitempty"`
	GeneratedAt    *time.Time `json:"generated_at,omitempty"`
}

func (s *TaxComplianceService) ListEInvoices(ctx context.Context, businessID string, page, limit int) ([]EInvoiceListItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	query := func() *gorm.DB {
		return s.db.WithContext(ctx).Table("einvoice_records AS e").
			Joins("JOIN documents AS d ON d.id = e.document_id AND d.business_id = e.business_id").
			Where("e.business_id = ? AND e.deleted_at IS NULL AND d.deleted_at IS NULL", businessID)
	}
	var total int64
	if err := query().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]EInvoiceListItem, 0)
	err := query().Select("e.document_id, d.serial_number AS document_number, e.status, e.irn, e.ack_number, e.ack_date, e.generated_at").
		Order("e.updated_at DESC, e.id DESC").Offset((page - 1) * limit).Limit(limit).Scan(&items).Error
	return items, total, err
}
