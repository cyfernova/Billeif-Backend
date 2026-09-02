package services

import (
	"context"
	"strings"
	"sync"
	"testing"

	"invoice-backend/internal/models"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestStorefrontCheckoutPostgresSerializesClaimOrderAndReservation(t *testing.T) {
	database := newInventoryPostgresIntegrationDB(t)
	fixture := seedInventoryTransferFixture(t, database)
	require.NoError(t, database.Model(&models.Warehouse{}).Where("id = ?", fixture.sourceID).Update("is_default", true).Error)
	require.NoError(t, database.AutoMigrate(
		&models.Customer{},
		&models.PriceList{},
		&models.PriceListAssignment{},
		&models.PartyGroup{},
		&models.PartyGroupMember{},
		&models.Storefront{},
		&models.StorefrontDomain{},
		&models.StorefrontCategory{},
		&models.StorefrontProduct{},
		&models.Document{},
		&models.DocumentLine{},
		&models.DocumentRevision{},
		&models.DocumentSequence{},
		&models.RenderProfile{},
		&models.DocumentRenderJob{},
		&models.Invoice{},
		&models.InvoiceItem{},
		&models.OutboxEvent{},
		&models.ActivityLog{},
		&models.Journal{},
		&models.JournalLine{},
		&models.LedgerEntry{},
		&models.AccountingPeriodPolicy{},
		&models.AccountingLockOverride{},
		&models.AccountingAuditEvent{},
		&models.InventoryReservation{},
		&models.APIIdempotencyKey{},
		&models.StorefrontCoupon{},
		&models.StoreOrder{},
		&models.StoreOrderLine{},
		&models.StorefrontCouponRedemption{},
		&models.StoreOrderEvent{},
		&models.WhatsAppConfig{},
	))
	require.NoError(t, database.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS ux_journals_business_source
		ON journals (business_id, source_type, source_id)
		WHERE source_type <> '' AND source_id IS NOT NULL AND deleted_at IS NULL`).Error)

	storefront := &models.Storefront{
		ID: uuid.NewString(), BusinessID: fixture.businessID, Name: "Invariant store", Slug: "invariant-store",
		Status: models.StorefrontStatusPublished, Currency: "INR", AllowCOD: true,
	}
	require.NoError(t, database.Create(storefront).Error)
	require.NoError(t, database.Create(&models.StorefrontProduct{
		ID: uuid.NewString(), StorefrontID: storefront.ID,
		ProductID: fixture.productID, IsPublished: true, DisplayPrice: 1,
	}).Error)

	businessRepo := postgresrepo.NewBusinessRepository(database)
	customerRepo := postgresrepo.NewCustomerRepository(database)
	productRepo := postgresrepo.NewProductRepository(database)
	inventory := NewInventoryService(database, nil, productRepo, businessRepo, nil, logger.New())
	journals := NewJournalService(database, postgresrepo.NewJournalRepository(database), logger.New())
	documents := &DocumentService{
		db: database, businessRepo: businessRepo, customerRepo: customerRepo,
		productRepo: productRepo, inventory: inventory, log: logger.New(),
	}
	invoiceRepo := postgresrepo.NewInvoiceRepository(database)
	if configurer, ok := invoiceRepo.(invoiceIssueStockEffectConfigurer); ok {
		configurer.ConfigureInvoiceIssueStockEffect(func(ctx context.Context, tx *gorm.DB, document *models.Document) error {
			return applyCanonicalInvoiceIssueEffects(ctx, tx, document, inventory, journals)
		})
	}
	invoiceService := NewInvoiceService(
		database, nil, invoiceRepo, businessRepo, productRepo, customerRepo, documents,
		nil, nil, nil, logger.New(),
	)
	documents.salesInvoices = newInvoiceSalesDocumentCreator(invoiceService)
	documents.salesInvoiceIssuer = newInvoiceSalesDocumentIssuer(invoiceService)
	service := &CommerceService{
		db: database, businessRepo: businessRepo, customerRepo: customerRepo, productRepo: productRepo,
		inventory: inventory, documents: documents, log: logger.New(),
	}
	input := StorefrontCheckoutInput{
		PaymentMethod: "cod",
		Customer:      CheckoutCustomerInput{Name: "Checkout buyer", Email: "buyer@example.com"},
		Items:         []CheckoutItemInput{{ProductID: fixture.productID, Quantity: 60}},
	}
	key := uuid.NewString()

	start := make(chan struct{})
	errs := make(chan error, 2)
	results := make(chan *CheckoutResult, 2)
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			result, err := service.Checkout(context.Background(), storefront.Slug, key, input)
			results <- result
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil && !strings.Contains(err.Error(), "still processing") {
			t.Fatalf("concurrent checkout error: %v", err)
		}
	}
	replay, err := service.Checkout(context.Background(), storefront.Slug, key, input)
	require.NoError(t, err)
	require.NotNil(t, replay.Order)
	require.NotNil(t, replay.Order.SalesOrderID)

	assertStorefrontCheckoutCounts(t, database, fixture, storefront.ID, 60)
	changed := input
	changed.Items = []CheckoutItemInput{{ProductID: fixture.productID, Quantity: 61}}
	_, err = service.Checkout(context.Background(), storefront.Slug, key, changed)
	require.ErrorContains(t, err, "different checkout request")

	actorID := uuid.NewString()
	approvalContext := ContextWithActor(context.Background(), ActorContext{UserID: actorID, Role: "owner"})
	approvalResults := make(chan *models.StoreOrder, 2)
	approvalErrors := make(chan error, 2)
	startApproval := make(chan struct{})
	for index := 0; index < 2; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-startApproval
			approved, approveErr := service.ApproveStoreOrder(approvalContext, fixture.businessID, storefront.ID, replay.Order.ID)
			approvalResults <- approved
			approvalErrors <- approveErr
		}()
	}
	close(startApproval)
	group.Wait()
	close(approvalResults)
	close(approvalErrors)
	var invoiceID string
	for approveErr := range approvalErrors {
		require.NoError(t, approveErr)
	}
	for approved := range approvalResults {
		require.NotNil(t, approved)
		require.NotNil(t, approved.SalesInvoiceID)
		if invoiceID == "" {
			invoiceID = *approved.SalesInvoiceID
		}
		require.Equal(t, invoiceID, *approved.SalesInvoiceID)
	}
	assertStorefrontApprovalCounts(t, database, fixture, replay.Order.ID, invoiceID)

	_, err = service.CancelStoreOrder(approvalContext, fixture.businessID, storefront.ID, replay.Order.ID, "unsafe direct cancel")
	require.ErrorContains(t, err, "compensating credit note")

	cancellableInput := input
	cancellableInput.Items = []CheckoutItemInput{{ProductID: fixture.productID, Quantity: 10}}
	cancellable, err := service.Checkout(context.Background(), storefront.Slug, uuid.NewString(), cancellableInput)
	require.NoError(t, err)
	require.NotNil(t, cancellable.Order.SalesOrderID)
	firstCancellation, err := service.CancelStoreOrder(approvalContext, fixture.businessID, storefront.ID, cancellable.Order.ID, "customer request")
	require.NoError(t, err)
	secondCancellation, err := service.CancelStoreOrder(approvalContext, fixture.businessID, storefront.ID, cancellable.Order.ID, "retry")
	require.NoError(t, err)
	require.Equal(t, firstCancellation.CancellationReason, secondCancellation.CancellationReason)
	assertStorefrontCancellationCounts(t, database, fixture, cancellable.Order)
}

func assertStorefrontCheckoutCounts(t *testing.T, database *gorm.DB, fixture inventoryTransferFixture, storefrontID string, reserved float64) {
	t.Helper()
	for model, want := range map[interface{}]int64{
		&models.Customer{}:             1,
		&models.StoreOrder{}:           1,
		&models.Document{}:             1,
		&models.InventoryReservation{}: 1,
	} {
		var count int64
		require.NoError(t, database.Model(model).Count(&count).Error)
		require.Equal(t, want, count)
	}
	var order models.StoreOrder
	require.NoError(t, database.Where("storefront_id = ?", storefrontID).First(&order).Error)
	require.NotNil(t, order.SalesOrderID)
	var balance models.InventoryBalance
	require.NoError(t, database.Where("business_id = ? AND warehouse_id = ?", fixture.businessID, fixture.sourceID).First(&balance).Error)
	require.Equal(t, reserved, balance.Reserved)
}

func assertStorefrontApprovalCounts(t *testing.T, database *gorm.DB, fixture inventoryTransferFixture, orderID, invoiceID string) {
	t.Helper()
	var invoice models.Invoice
	require.NoError(t, database.Where("id = ? AND business_id = ?", invoiceID, fixture.businessID).First(&invoice).Error)
	require.Equal(t, models.InvoiceStatusIssued, invoice.Status)
	var invoiceCount int64
	require.NoError(t, database.Model(&models.Invoice{}).Where("business_id = ?", fixture.businessID).Count(&invoiceCount).Error)
	require.Equal(t, int64(1), invoiceCount)
	var order models.StoreOrder
	require.NoError(t, database.Where("id = ?", orderID).First(&order).Error)
	require.Equal(t, models.StoreOrderStatusConfirmed, order.Status)
	require.Equal(t, invoiceID, models.StringValue(order.SalesInvoiceID))
	var approvedEvents int64
	require.NoError(t, database.Model(&models.StoreOrderEvent{}).Where("store_order_id = ? AND event_type = ?", orderID, "store_order.approved").Count(&approvedEvents).Error)
	require.Equal(t, int64(1), approvedEvents)
	var balance models.InventoryBalance
	require.NoError(t, database.Where("business_id = ? AND warehouse_id = ?", fixture.businessID, fixture.sourceID).First(&balance).Error)
	require.Equal(t, float64(40), balance.OnHand)
	require.Zero(t, balance.Reserved)
	var consumedReservations int64
	require.NoError(t, database.Model(&models.InventoryReservation{}).
		Where("business_id = ? AND document_id = ? AND status = ?", fixture.businessID, models.StringValue(order.SalesOrderID), "consumed").
		Count(&consumedReservations).Error)
	require.Equal(t, int64(1), consumedReservations)
	var journal models.Journal
	require.NoError(t, database.Preload("Lines").
		Where("business_id = ? AND source_type = ? AND source_id = ?", fixture.businessID, "document", invoiceID).
		First(&journal).Error)
	require.Equal(t, models.JournalStatusPosted, journal.Status)
	require.GreaterOrEqual(t, len(journal.Lines), 2)
	var ledgerCount int64
	require.NoError(t, database.Model(&models.LedgerEntry{}).Where("transaction_id = ?", journal.ID).Count(&ledgerCount).Error)
	require.Equal(t, int64(len(journal.Lines)), ledgerCount)
}

func assertStorefrontCancellationCounts(t *testing.T, database *gorm.DB, fixture inventoryTransferFixture, order *models.StoreOrder) {
	t.Helper()
	require.NotNil(t, order)
	require.NotNil(t, order.SalesOrderID)
	var persisted models.StoreOrder
	require.NoError(t, database.Where("id = ?", order.ID).First(&persisted).Error)
	require.Equal(t, models.StoreOrderStatusCancelled, persisted.Status)
	require.Equal(t, "customer request", persisted.CancellationReason)
	var cancelledEvents int64
	require.NoError(t, database.Model(&models.StoreOrderEvent{}).
		Where("store_order_id = ? AND event_type = ?", order.ID, "store_order.cancelled").
		Count(&cancelledEvents).Error)
	require.Equal(t, int64(1), cancelledEvents)
	var releasedReservations int64
	require.NoError(t, database.Model(&models.InventoryReservation{}).
		Where("business_id = ? AND document_id = ? AND status = ?", fixture.businessID, *order.SalesOrderID, "released").
		Count(&releasedReservations).Error)
	require.Equal(t, int64(1), releasedReservations)
	var balance models.InventoryBalance
	require.NoError(t, database.Where("business_id = ? AND warehouse_id = ?", fixture.businessID, fixture.sourceID).First(&balance).Error)
	require.Equal(t, float64(40), balance.OnHand)
	require.Zero(t, balance.Reserved)
}
