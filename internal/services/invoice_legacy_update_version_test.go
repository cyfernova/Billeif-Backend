package services

import (
	"context"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
)

type legacyDraftMetadataRepository struct {
	interfaces.CanonicalInvoiceRepository
	invoice        *models.Invoice
	legacyCalls    int
	versionedCalls int
	expected       int
}

func (r *legacyDraftMetadataRepository) GetByID(context.Context, string, string) (*models.Invoice, error) {
	copy := *r.invoice
	return &copy, nil
}

func (r *legacyDraftMetadataRepository) Update(context.Context, *models.Invoice) error {
	r.legacyCalls++
	return nil
}

func (r *legacyDraftMetadataRepository) UpdateDraftMetadataVersioned(
	_ context.Context,
	invoice *models.Invoice,
	expectedVersion int,
) error {
	r.versionedCalls++
	r.expected = expectedVersion
	r.invoice = invoice
	return nil
}

func TestInvoiceServiceLegacyDraftMetadataUpdateIncrementsVersionWithCAS(t *testing.T) {
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	repository := &legacyDraftMetadataRepository{invoice: &models.Invoice{
		ID:         invoiceID,
		BusinessID: businessID,
		Status:     models.InvoiceStatusDraft,
		Version:    4,
	}}
	service := NewInvoiceService(
		nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New(),
	)

	updated, err := service.UpdateByBusiness(
		context.Background(),
		businessID,
		invoiceID,
		UpdateInvoiceInput{Notes: "render-affecting change"},
	)

	if err != nil {
		t.Fatalf("legacy draft metadata update: %v", err)
	}
	if repository.versionedCalls != 1 || repository.legacyCalls != 0 || repository.expected != 4 {
		t.Fatalf(
			"versioned/legacy/expected = %d/%d/%d, want 1/0/4",
			repository.versionedCalls,
			repository.legacyCalls,
			repository.expected,
		)
	}
	if updated.Version != 5 {
		t.Fatalf("updated version = %d, want 5", updated.Version)
	}
}
