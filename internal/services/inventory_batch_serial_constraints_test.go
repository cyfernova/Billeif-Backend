package services

import (
	"math"
	"strings"
	"testing"

	"invoice-backend/internal/models"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateInventoryBatchAllocationsRequiresPositiveExactTotal(t *testing.T) {
	valid := []BatchAllocationInput{
		{BatchNumber: "LOT-1", Quantity: 2},
		{BatchNumber: "LOT-2", Quantity: 3},
	}
	require.NoError(t, validateInventoryBatchAllocations(5, valid))
	require.NoError(t, validateInventoryBatchAllocations(-5, valid))

	for name, allocations := range map[string][]BatchAllocationInput{
		"total mismatch":      {{BatchNumber: "LOT-1", Quantity: 4}},
		"zero allocation":     {{BatchNumber: "LOT-1", Quantity: 0}},
		"negative allocation": {{BatchNumber: "LOT-1", Quantity: -5}},
		"not finite":          {{BatchNumber: "LOT-1", Quantity: math.Inf(1)}},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, validateInventoryBatchAllocations(5, allocations))
		})
	}
	require.Error(t, validateInventoryBatchAllocations(math.NaN(), valid))
}

func TestNormalizeInventorySerialIDsRejectsAmbiguousSelections(t *testing.T) {
	normalized, err := normalizeInventorySerialIDs(-2, []string{" SERIAL-1 ", "SERIAL-2"})
	require.NoError(t, err)
	require.Equal(t, []string{"SERIAL-1", "SERIAL-2"}, normalized)

	for name, serialIDs := range map[string][]string{
		"duplicate":   {"SERIAL-1", " SERIAL-1 "},
		"blank":       {"SERIAL-1", " "},
		"wrong count": {"SERIAL-1"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := normalizeInventorySerialIDs(2, serialIDs)
			require.Error(t, err)
		})
	}
	_, err = normalizeInventorySerialIDs(math.Inf(1), []string{"SERIAL-1"})
	require.Error(t, err)
}

func TestCreateInventorySerialRejectsAlreadyStockedIdentity(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Exec(`CREATE TABLE product_serial_numbers (
		id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))), business_id TEXT NOT NULL,
		product_id TEXT NOT NULL, variant_id TEXT NOT NULL, batch_id TEXT, warehouse_id TEXT,
		serial_number TEXT NOT NULL, imei TEXT, status TEXT NOT NULL, sold_document_id TEXT,
		sold_document_line_id TEXT, metadata TEXT DEFAULT '{}', created_at DATETIME,
		updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	require.NoError(t, database.Exec(`CREATE UNIQUE INDEX serial_identity ON product_serial_numbers (business_id, serial_number) WHERE deleted_at IS NULL`).Error)
	existing := &models.ProductSerialNumber{
		ID: "serial-id", BusinessID: "business-id", ProductID: "product-id", VariantID: "variant-id",
		WarehouseID: stringPointer("warehouse-id"), SerialNumber: "SERIAL-1", Status: models.SerialStatusAvailable,
	}
	require.NoError(t, database.Create(existing).Error)

	_, err = createInventorySerialTx(database, "business-id", "product-id", "variant-id", "warehouse-id", "SERIAL-1")
	require.ErrorContains(t, err, "already in stock")
	_, err = createInventorySerialTx(database, "business-id", "other-product", "other-variant", "warehouse-id", "SERIAL-1")
	require.ErrorContains(t, err, "another product")

	created, err := createInventorySerialTx(database, "business-id", "product-id", "variant-id", "warehouse-id", "SERIAL-2")
	require.NoError(t, err)
	require.Equal(t, "SERIAL-2", created.SerialNumber)
}
