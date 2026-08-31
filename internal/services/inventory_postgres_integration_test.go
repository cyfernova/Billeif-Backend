package services

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestInventoryTransferPostgresSerializesConcurrentInsufficientStock(t *testing.T) {
	database := newInventoryPostgresIntegrationDB(t)
	service := NewInventoryService(database, nil, nil, nil, nil, logger.New())
	fixture := seedInventoryTransferFixture(t, database)

	inputs := []InventoryTransferInput{
		{BusinessID: fixture.businessID, ProductID: fixture.productID, VariantID: fixture.variantID, FromWarehouseID: fixture.sourceID, ToWarehouseID: fixture.targetOneID, Quantity: 60, UnitCost: 1},
		{BusinessID: fixture.businessID, ProductID: fixture.productID, VariantID: fixture.variantID, FromWarehouseID: fixture.sourceID, ToWarehouseID: fixture.targetTwoID, Quantity: 60, UnitCost: 1},
	}
	start := make(chan struct{})
	errs := make(chan error, len(inputs))
	var group sync.WaitGroup
	for _, input := range inputs {
		input := input
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			errs <- service.TransferStock(context.Background(), input)
		}()
	}
	close(start)
	group.Wait()
	close(errs)

	successes := 0
	insufficient := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case strings.Contains(err.Error(), "insufficient stock"):
			insufficient++
		default:
			t.Fatalf("unexpected concurrent transfer error: %v", err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, insufficient)

	var source models.InventoryBalance
	require.NoError(t, database.Where("business_id = ? AND warehouse_id = ?", fixture.businessID, fixture.sourceID).First(&source).Error)
	require.Equal(t, 40.0, source.OnHand)
	var total float64
	require.NoError(t, database.Model(&models.InventoryBalance{}).Where("business_id = ?", fixture.businessID).Select("COALESCE(SUM(on_hand), 0)").Scan(&total).Error)
	require.Equal(t, 100.0, total)
	var moveCount int64
	require.NoError(t, database.Model(&models.StockMove{}).Where("business_id = ?", fixture.businessID).Count(&moveCount).Error)
	require.Equal(t, int64(2), moveCount)
}

func TestInventoryTransferPostgresRejectsCrossTenantDestinationWarehouse(t *testing.T) {
	database := newInventoryPostgresIntegrationDB(t)
	service := NewInventoryService(database, nil, nil, nil, nil, logger.New())
	fixture := seedInventoryTransferFixture(t, database)
	otherBusinessID := uuid.NewString()
	otherWarehouseID := uuid.NewString()
	require.NoError(t, database.Create(&models.BusinessProfile{ID: otherBusinessID, OwnerID: "owner-2", Name: "Other", Email: "other@example.com", Currency: "INR"}).Error)
	require.NoError(t, database.Create(&models.Warehouse{ID: otherWarehouseID, BusinessID: otherBusinessID, Name: "Other", Code: "OTHER"}).Error)

	err := service.TransferStock(context.Background(), InventoryTransferInput{
		BusinessID: fixture.businessID, ProductID: fixture.productID, VariantID: fixture.variantID,
		FromWarehouseID: fixture.sourceID, ToWarehouseID: otherWarehouseID, Quantity: 10, UnitCost: 1,
	})
	require.Error(t, err)

	var source models.InventoryBalance
	require.NoError(t, database.Where("business_id = ? AND warehouse_id = ?", fixture.businessID, fixture.sourceID).First(&source).Error)
	require.Equal(t, 100.0, source.OnHand)
	var crossTenantBalanceCount int64
	require.NoError(t, database.Model(&models.InventoryBalance{}).Where("business_id = ? AND warehouse_id = ?", fixture.businessID, otherWarehouseID).Count(&crossTenantBalanceCount).Error)
	require.Zero(t, crossTenantBalanceCount)
}

func TestInventoryBatchTransferPostgresRejectsAllocationMismatchWithoutMovement(t *testing.T) {
	database := newInventoryPostgresIntegrationDB(t)
	service := NewInventoryService(database, nil, nil, nil, nil, logger.New())
	fixture := seedInventoryTransferFixture(t, database)
	require.NoError(t, database.Model(&models.ProductVariant{}).
		Where("id = ?", fixture.variantID).
		Update("track_batches", true).Error)
	batch := &models.ProductBatch{
		ID: uuid.NewString(), BusinessID: fixture.businessID, ProductID: fixture.productID,
		VariantID: fixture.variantID, BatchNumber: "LOT-1", IsActive: true,
	}
	require.NoError(t, database.Create(batch).Error)
	require.NoError(t, database.Model(&models.InventoryBalance{}).
		Where("business_id = ? AND warehouse_id = ?", fixture.businessID, fixture.sourceID).
		Updates(map[string]interface{}{"batch_id": batch.ID, "batch_key": batch.ID}).Error)

	err := service.TransferStock(context.Background(), InventoryTransferInput{
		BusinessID: fixture.businessID, ProductID: fixture.productID, VariantID: fixture.variantID,
		FromWarehouseID: fixture.sourceID, ToWarehouseID: fixture.targetOneID, Quantity: 10, UnitCost: 1,
		BatchAllocations: []BatchAllocationInput{{BatchID: batch.ID, Quantity: 1}},
	})
	require.ErrorContains(t, err, "must sum to the requested quantity")

	var source models.InventoryBalance
	require.NoError(t, database.Where("business_id = ? AND warehouse_id = ?", fixture.businessID, fixture.sourceID).First(&source).Error)
	require.Equal(t, 100.0, source.OnHand)
	var moveCount int64
	require.NoError(t, database.Model(&models.StockMove{}).Where("business_id = ?", fixture.businessID).Count(&moveCount).Error)
	require.Zero(t, moveCount)
}

func TestInventorySerialTransferPostgresSerializesSameSerial(t *testing.T) {
	database := newInventoryPostgresIntegrationDB(t)
	service := NewInventoryService(database, nil, nil, nil, nil, logger.New())
	fixture := seedInventoryTransferFixture(t, database)
	require.NoError(t, database.Model(&models.ProductVariant{}).
		Where("id = ?", fixture.variantID).
		Update("track_serials", true).Error)
	require.NoError(t, database.Model(&models.InventoryBalance{}).
		Where("business_id = ? AND warehouse_id = ?", fixture.businessID, fixture.sourceID).
		Updates(map[string]interface{}{"on_hand": 1, "stock_value": 1}).Error)
	serial := &models.ProductSerialNumber{
		ID: uuid.NewString(), BusinessID: fixture.businessID, ProductID: fixture.productID,
		VariantID: fixture.variantID, WarehouseID: &fixture.sourceID, SerialNumber: "SERIAL-1",
		Status: models.SerialStatusAvailable,
	}
	require.NoError(t, database.Create(serial).Error)

	inputs := []InventoryTransferInput{
		{BusinessID: fixture.businessID, ProductID: fixture.productID, VariantID: fixture.variantID, FromWarehouseID: fixture.sourceID, ToWarehouseID: fixture.targetOneID, Quantity: 1, UnitCost: 1, SerialIDs: []string{serial.SerialNumber}},
		{BusinessID: fixture.businessID, ProductID: fixture.productID, VariantID: fixture.variantID, FromWarehouseID: fixture.sourceID, ToWarehouseID: fixture.targetTwoID, Quantity: 1, UnitCost: 1, SerialIDs: []string{serial.SerialNumber}},
	}
	start := make(chan struct{})
	errs := make(chan error, len(inputs))
	var group sync.WaitGroup
	for _, input := range inputs {
		input := input
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			errs <- service.TransferStock(context.Background(), input)
		}()
	}
	close(start)
	group.Wait()
	close(errs)
	successes, rejections := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if strings.Contains(err.Error(), "requested warehouse") {
			rejections++
		} else {
			t.Fatalf("unexpected concurrent serial transfer error: %v", err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, rejections)

	var persistedSerial models.ProductSerialNumber
	require.NoError(t, database.First(&persistedSerial, "id = ?", serial.ID).Error)
	require.NotNil(t, persistedSerial.WarehouseID)
	require.Contains(t, []string{fixture.targetOneID, fixture.targetTwoID}, *persistedSerial.WarehouseID)
	var total float64
	require.NoError(t, database.Model(&models.InventoryBalance{}).
		Where("business_id = ?", fixture.businessID).
		Select("COALESCE(SUM(on_hand), 0)").Scan(&total).Error)
	require.Equal(t, 1.0, total)
	var moveCount int64
	require.NoError(t, database.Model(&models.StockMove{}).Where("business_id = ?", fixture.businessID).Count(&moveCount).Error)
	require.Equal(t, int64(2), moveCount)
}

func TestInventoryDocumentStockEffectPostgresUsesOuterTransaction(t *testing.T) {
	database := newInventoryPostgresIntegrationDB(t)
	service := NewInventoryService(database, nil, nil, nil, nil, logger.New())
	fixture := seedInventoryTransferFixture(t, database)
	document := &models.Document{
		ID:           uuid.NewString(),
		BusinessID:   fixture.businessID,
		DocumentType: models.DocumentTypeSalesInvoice,
		Status:       models.DocumentStatusIssued,
		SerialNumber: "POS/26-27/000001",
		Lines: []*models.DocumentLine{{
			ID:          uuid.NewString(),
			ProductID:   models.StringPointer(fixture.productID),
			VariantID:   models.StringPointer(fixture.variantID),
			WarehouseID: models.StringPointer(fixture.sourceID),
			Quantity:    60,
			StockEffect: "out",
		}},
	}

	err := database.Transaction(func(tx *gorm.DB) error {
		if err := service.ApplyDocumentTx(context.Background(), tx, document); err != nil {
			return err
		}
		return errors.New("abort issue")
	})
	require.ErrorContains(t, err, "abort issue")
	assertInventoryDocumentEffect(t, database, fixture, 100, 0)

	require.NoError(t, database.Transaction(func(tx *gorm.DB) error {
		return service.ApplyDocumentTx(context.Background(), tx, document)
	}))
	assertInventoryDocumentEffect(t, database, fixture, 40, 1)

	secondDocument := *document
	secondDocument.ID = uuid.NewString()
	secondLine := *document.Lines[0]
	secondLine.ID = uuid.NewString()
	secondLine.DocumentID = secondDocument.ID
	secondDocument.Lines = []*models.DocumentLine{&secondLine}
	require.ErrorContains(t, database.Transaction(func(tx *gorm.DB) error {
		return service.ApplyDocumentTx(context.Background(), tx, &secondDocument)
	}), "insufficient stock")
	assertInventoryDocumentEffect(t, database, fixture, 40, 1)
}

func assertInventoryDocumentEffect(t *testing.T, database *gorm.DB, fixture inventoryTransferFixture, onHand float64, moves int64) {
	t.Helper()
	var balance models.InventoryBalance
	require.NoError(t, database.Where("business_id = ? AND warehouse_id = ?", fixture.businessID, fixture.sourceID).First(&balance).Error)
	require.Equal(t, onHand, balance.OnHand)
	var moveCount int64
	require.NoError(t, database.Model(&models.StockMove{}).Where("business_id = ?", fixture.businessID).Count(&moveCount).Error)
	require.Equal(t, moves, moveCount)
}

type inventoryTransferFixture struct {
	businessID  string
	productID   string
	variantID   string
	sourceID    string
	targetOneID string
	targetTwoID string
}

func seedInventoryTransferFixture(t *testing.T, database *gorm.DB) inventoryTransferFixture {
	t.Helper()
	fixture := inventoryTransferFixture{
		businessID: uuid.NewString(), productID: uuid.NewString(), variantID: uuid.NewString(),
		sourceID: uuid.NewString(), targetOneID: uuid.NewString(), targetTwoID: uuid.NewString(),
	}
	require.NoError(t, database.Create(&models.BusinessProfile{ID: fixture.businessID, OwnerID: "owner-1", Name: "Acme", Email: "acme@example.com", Currency: "INR"}).Error)
	for _, warehouse := range []*models.Warehouse{
		{ID: fixture.sourceID, BusinessID: fixture.businessID, Name: "Source", Code: "SRC"},
		{ID: fixture.targetOneID, BusinessID: fixture.businessID, Name: "Target One", Code: "T1"},
		{ID: fixture.targetTwoID, BusinessID: fixture.businessID, Name: "Target Two", Code: "T2"},
	} {
		require.NoError(t, database.Create(warehouse).Error)
	}
	require.NoError(t, database.Create(&models.Product{ID: fixture.productID, BusinessID: fixture.businessID, Name: "Phone", SKU: "PHONE", Price: 1, Currency: "INR", Unit: "PCS", IsActive: true}).Error)
	require.NoError(t, database.Create(&models.ProductVariant{ID: fixture.variantID, BusinessID: fixture.businessID, ProductID: fixture.productID, Name: "Default", SKU: "PHONE-DEFAULT", IsDefault: true, CostPrice: 1, IsActive: true}).Error)
	require.NoError(t, database.Create(&models.InventoryBalance{BusinessID: fixture.businessID, ProductID: fixture.productID, VariantID: fixture.variantID, WarehouseID: fixture.sourceID, OnHand: 100, StockValue: 100}).Error)
	return fixture
}

func newInventoryPostgresIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured; skipping inventory PostgreSQL concurrency integration")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	admin, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := "inventory_invariants_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)).Error)
	t.Cleanup(func() {
		if err := admin.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema)).Error; err != nil {
			t.Errorf("drop isolated inventory schema: %v", err)
		}
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := gorm.Open(gormpostgres.Open(parsed.String()), &gorm.Config{})
	require.NoError(t, err)
	sqlDatabase, err := database.DB()
	require.NoError(t, err)
	sqlDatabase.SetMaxOpenConns(6)
	t.Cleanup(func() {
		if err := sqlDatabase.Close(); err != nil {
			t.Errorf("close inventory PostgreSQL pool: %v", err)
		}
	})
	require.NoError(t, database.AutoMigrate(
		&models.BusinessProfile{}, &models.Warehouse{}, &models.Product{}, &models.ProductVariant{},
		&models.ProductBatch{}, &models.ProductSerialNumber{}, &models.InventoryBalance{}, &models.StockMove{},
		&models.InventorySnapshot{}, &models.InventoryEventLog{},
	))
	return database
}
