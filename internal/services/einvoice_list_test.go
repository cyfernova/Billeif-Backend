package services

import (
	"context"
	"testing"

	"invoice-backend/internal/models"

	"github.com/stretchr/testify/require"
)

func TestListEInvoicesIncludesSalesInvoicesAndScopesPagination(t *testing.T) {
	service, db, businessID := newTaxComplianceTestService(t)
	require.NoError(t, db.Exec(`CREATE TABLE einvoice_records (id TEXT PRIMARY KEY, business_id TEXT, document_id TEXT, status TEXT, irn TEXT, ack_number TEXT, ack_date DATETIME, generated_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`).Error)
	for _, item := range []struct{ id, business, kind string }{
		{"invoice", businessID, models.DocumentTypeSalesInvoice},
		{"note", businessID, models.DocumentTypeCreditNote},
		{"foreign", "other-business", models.DocumentTypeSalesInvoice},
		{"deleted", businessID, models.DocumentTypeSalesInvoice},
	} {
		document := newTestDocument(item.id, item.business, item.kind, item.id, "29ABCDE1234F1Z5", "29", 100, 18, 118, "9403", "NOS")
		insertTestDocument(t, db, document)
		require.NoError(t, db.Exec(`INSERT INTO einvoice_records (id, business_id, document_id, status, updated_at) VALUES (?, ?, ?, 'generated', '2026-09-09')`, item.id, item.business, item.id).Error)
	}
	require.NoError(t, db.Exec(`UPDATE documents SET deleted_at = '2026-09-09' WHERE id = 'deleted'`).Error)
	first, total, err := service.ListEInvoices(context.Background(), businessID, 1, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, first, 1)
	second, _, err := service.ListEInvoices(context.Background(), businessID, 2, 1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.ElementsMatch(t, []string{"invoice", "note"}, []string{first[0].DocumentID, second[0].DocumentID})
	empty, total, err := service.ListEInvoices(context.Background(), "empty-business", 1, 20)
	require.NoError(t, err)
	require.Zero(t, total)
	require.NotNil(t, empty)
	require.Empty(t, empty)
}
