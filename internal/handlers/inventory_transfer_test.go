package handlers

import (
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"

	"github.com/stretchr/testify/require"
)

func TestCompletedTransferRecordsUsePersistedInboundMove(t *testing.T) {
	fromWarehouseID := "11111111-1111-1111-1111-111111111111"
	toWarehouseID := "22222222-2222-2222-2222-222222222222"
	recordedAt := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)

	records := completedTransferRecords([]services.InventoryTimelineEntry{
		{
			ID: "outbound-move", TransactionType: models.InventoryTransactionTypeTransfer,
			Direction: models.StockMoveDirectionOut,
		},
		{
			ID: "inbound-move", ProductID: "product-1", ProductName: "Phone",
			WarehouseID: &toWarehouseID, WarehouseName: "Retail",
			SourceWarehouseID: &fromWarehouseID, SourceWarehouseName: "Central",
			TransactionType: models.InventoryTransactionTypeTransfer,
			Direction:       models.StockMoveDirectionIn, Quantity: 3, RecordedAt: recordedAt,
		},
		{
			ID: "adjustment", TransactionType: models.InventoryTransactionTypeAdjustment,
			Direction: models.StockMoveDirectionIn,
		},
	})

	require.Equal(t, []inventoryTransferRecord{{
		ID: "inbound-move", ProductID: "product-1", ProductName: "Phone",
		FromWarehouseID: fromWarehouseID, FromWarehouse: "Central",
		ToWarehouseID: toWarehouseID, ToWarehouse: "Retail",
		Quantity: 3, Status: "completed", CreatedAt: recordedAt, CompletedAt: recordedAt,
	}}, records)
}
