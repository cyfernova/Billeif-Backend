package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDefaultEntitlementSeedsForSubscriptionUsesCatalogLimits(t *testing.T) {
	seeds := defaultEntitlementSeedsForSubscription(&models.Subscription{
		Plan:     "starter",
		PlanCode: "pro",
		Status:   "active",
	})

	for _, featureKey := range []string{
		FeatureOnlineStore,
		FeatureMultiCurrency,
		FeatureExportDocuments,
		FeatureSEZDocuments,
		FeatureDeemedExportDocuments,
		FeatureMultiUser,
		FeatureCustomRoles,
		FeatureMultiBusiness,
		FeatureBranches,
		FeaturePrioritySupport,
		FeatureDriveStorageMB,
		FeatureWhatsAppNotifications,
	} {
		if !seedEnabled(seeds, featureKey) {
			t.Fatalf("expected %s to be enabled", featureKey)
		}
	}

	require.Equal(t, int64(3), *seedLimit(seeds, FeatureMultiUser))
	require.Nil(t, seedLimit(seeds, FeatureBranches))
	require.Equal(t, int64(512), *seedLimit(seeds, FeatureDriveStorageMB))
}

func TestFeatureEntitlementsNeedSyncDetectsDisabledRows(t *testing.T) {
	seeds := defaultEntitlementSeedsForSubscription(&models.Subscription{Plan: "starter", PlanCode: "pro", Status: "active"})
	entitlements := make([]*models.FeatureEntitlement, 0, len(seeds))
	for _, seed := range seeds {
		entitlements = append(entitlements, &models.FeatureEntitlement{
			FeatureKey: seed.FeatureKey,
			Enabled:    seed.Enabled,
			LimitValue: seed.LimitValue,
		})
	}

	if featureEntitlementsNeedSync(entitlements, seeds) {
		t.Fatal("expected complete enabled entitlement set to be current")
	}

	entitlements[0].Enabled = false
	if !featureEntitlementsNeedSync(entitlements, seeds) {
		t.Fatal("expected disabled entitlement to require sync")
	}

	entitlements[0].Enabled = true
	for _, entitlement := range entitlements {
		if entitlement.FeatureKey == FeatureMultiUser {
			entitlement.LimitValue = int64Pointer(1)
			break
		}
	}
	if !featureEntitlementsNeedSync(entitlements, seeds) {
		t.Fatal("expected stale entitlement limit to require sync")
	}

	freeSeeds := defaultEntitlementSeedsForSubscription(&models.Subscription{Plan: "free", PlanCode: "free", Status: "active"})
	freeEntitlements := make([]*models.FeatureEntitlement, 0, len(freeSeeds))
	for _, seed := range freeSeeds {
		freeEntitlements = append(freeEntitlements, &models.FeatureEntitlement{
			FeatureKey: seed.FeatureKey,
			Enabled:    seed.Enabled,
			LimitValue: seed.LimitValue,
		})
	}
	freeEntitlements[0].LimitValue = int64Pointer(1)
	if !featureEntitlementsNeedSync(freeEntitlements, freeSeeds) {
		t.Fatal("expected stale limit on disabled entitlement to require sync")
	}
}

func TestPermissionOverrideValue(t *testing.T) {
	override, ok := permissionOverrideValue(`{"storefront.manage":true}`, PermissionStorefrontManage)
	if !ok || !override {
		t.Fatalf("expected direct permission override to be applied")
	}

	override, ok = permissionOverrideValue(`{"*":false}`, PermissionOrdersManage)
	if !ok || override {
		t.Fatalf("expected wildcard permission override to be applied")
	}

	if _, ok := permissionOverrideValue(`{"storefront.manage":"yes"}`, PermissionStorefrontManage); ok {
		t.Fatalf("expected non-bool override to be ignored")
	}
}

func TestPublicStorefrontCatalogResponseOmitsInternalFields(t *testing.T) {
	storefront := &models.Storefront{
		ID:           "store-1",
		BusinessID:   "biz-secret",
		Name:         "Public Store",
		Slug:         "public-store",
		Status:       models.StorefrontStatusPublished,
		Currency:     "INR",
		Settings:     `{"theme":"hidden"}`,
		BlockedUsers: `["blocked@example.com"]`,
	}
	category := &models.StorefrontCategory{
		ID:           "cat-1",
		StorefrontID: "store-1",
		Name:         "Tea",
		Slug:         "tea",
		SEO:          `{"secret":"hidden"}`,
	}
	item := &StorefrontCatalogItem{
		StorefrontProduct: &models.StorefrontProduct{
			ID:             "sf-prod-1",
			StorefrontID:   "store-1",
			ProductID:      "prod-1",
			DisplayPrice:   120,
			CompareAtPrice: 150,
			SEO:            `{"title":"Tea"}`,
			Metadata:       `{"label":"fresh"}`,
		},
		Product: &models.Product{
			ID:                "prod-1",
			BusinessID:        "biz-secret",
			Name:              "Assam Tea",
			SKU:               "TEA-1",
			Price:             130,
			CostPrice:         80,
			StockLevel:        999,
			LowStockThreshold: 25,
			ImageKey:          "private/key",
			ImageURL:          "https://cdn.example.com/tea.png",
			Currency:          "INR",
			Unit:              "PCS",
		},
	}

	resp := publicStorefrontCatalogResponse(storefront, []*models.StorefrontCategory{category}, []*StorefrontCatalogItem{item})

	if resp.Storefront.Name != "Public Store" || resp.Storefront.Slug != "public-store" {
		t.Fatalf("expected public storefront fields to be preserved")
	}
	if len(resp.Categories) != 1 || len(resp.Products) != 1 {
		t.Fatalf("expected one category and product in public response")
	}
	product := resp.Products[0]
	if product.ProductID != "prod-1" || product.Name != "Assam Tea" || product.ImageURL == "" {
		t.Fatalf("expected public product fields to be preserved: %+v", product)
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal public catalog response: %v", err)
	}
	for _, forbidden := range []string{"business_id", "cost_price", "stock_level", "low_stock_threshold", "image_key", "blocked_users", "settings", "storefront_id"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("public catalog leaked %q in %s", forbidden, string(body))
		}
	}
}

func TestEnsurePublishedStorefrontRejectsDraft(t *testing.T) {
	err := ensurePublishedStorefront(&models.Storefront{Status: models.StorefrontStatusDraft})
	if err == nil {
		t.Fatal("expected draft storefront to be rejected")
	}
}

func TestCheckoutInputMatchesOrderRejectsDifferentReplayBody(t *testing.T) {
	input := StorefrontCheckoutInput{
		PaymentMethod: "cod",
		Customer:      CheckoutCustomerInput{Name: "Alice", Email: "alice@example.com"},
		Items:         []CheckoutItemInput{{ProductID: "11111111-1111-1111-1111-111111111111", Quantity: 1}},
	}
	order := &models.StoreOrder{
		Snapshot: mustMarshalMap(map[string]interface{}{
			"checkout_fingerprint": checkoutInputFingerprint(input),
		}),
	}

	if !checkoutInputMatchesOrder(order, input) {
		t.Fatal("expected identical checkout input to match existing idempotent order")
	}

	changed := input
	changed.Customer.Email = "mallory@example.com"
	if checkoutInputMatchesOrder(order, changed) {
		t.Fatal("expected changed checkout input to be rejected for reused idempotency key")
	}
}

func TestStorefrontCheckoutClaimAllowsOneWorkerAndReplaysCompletedOrder(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE api_idempotency_keys (
		id TEXT PRIMARY KEY,
		business_id TEXT NOT NULL,
		command TEXT NOT NULL,
		idempotency_key TEXT NOT NULL,
		request_hash TEXT NOT NULL,
		status TEXT NOT NULL,
		result_type TEXT,
		result_id TEXT,
		created_at DATETIME,
		updated_at DATETIME,
		completed_at DATETIME,
		UNIQUE (business_id, command, idempotency_key)
	)`).Error)
	service := &CommerceService{db: db}
	ctx := context.Background()
	businessID := "business-1"
	storefrontID := "storefront-1"
	key := "checkout-key-1"
	hash := strings.Repeat("a", 64)

	claimed, replayID, err := service.claimStorefrontCheckout(ctx, businessID, storefrontID, key, hash)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Empty(t, replayID)

	claimed, replayID, err = service.claimStorefrontCheckout(ctx, businessID, storefrontID, key, hash)
	require.ErrorContains(t, err, "still processing")
	require.False(t, claimed)
	require.Empty(t, replayID)

	_, _, err = service.claimStorefrontCheckout(ctx, businessID, storefrontID, key, strings.Repeat("b", 64))
	require.ErrorContains(t, err, "different checkout request")

	orderID := "order-1"
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return completeStorefrontCheckoutClaim(tx, businessID, storefrontID, key, hash, orderID)
	}))
	claimed, replayID, err = service.claimStorefrontCheckout(ctx, businessID, storefrontID, key, hash)
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, orderID, replayID)
}

func TestCanonicalStorefrontInvoiceInputUsesStableOrderIdentity(t *testing.T) {
	orderID := "11111111-1111-4111-8111-111111111111"
	customerID := "22222222-2222-4222-8222-222222222222"
	productID := "33333333-3333-4333-8333-333333333333"
	warehouseID := "44444444-4444-4444-8444-444444444444"
	order := &models.StoreOrder{
		ID: orderID, BusinessID: "55555555-5555-4555-8555-555555555555",
		CustomerID: &customerID, Currency: "INR", Notes: "Store order", OrderedAt: time.Now().UTC(),
		Lines: []*models.StoreOrderLine{{
			ProductID: &productID, WarehouseID: &warehouseID, Title: "Tea", Quantity: 2,
			UnitPrice: 125, TaxRate: 18,
		}},
	}
	business := &models.BusinessProfile{DefaultGSTTreatment: models.DocumentGSTTreatmentRegular}
	key := storefrontInvoiceIdempotencyKey(orderID)

	input, err := canonicalStorefrontInvoiceInput(order, business, key)
	require.NoError(t, err)
	require.Equal(t, models.InvoiceOriginStorefront, input.Origin)
	require.Equal(t, key, input.IdempotencyKey)
	require.Equal(t, customerID, input.CustomerID)
	require.Len(t, input.Items, 1)
	require.Equal(t, productID, input.Items[0].ProductID)
	require.Equal(t, warehouseID, input.Items[0].WarehouseID)
	require.Equal(t, orderID, input.TaxProfile.SourceLinkage["store_order_id"])
	require.Equal(t, "5d1a65e4-dfa9-58fa-98d4-18f2f60b4a1f", key)

	origin, err := normalizedInvoiceCreateOrigin(input)
	require.NoError(t, err)
	require.Equal(t, models.InvoiceOriginStorefront, origin)
}

func TestStorefrontApprovalUsesCanonicalCreateAndIssueCommands(t *testing.T) {
	businessID := uuid.NewString()
	orderID := uuid.NewString()
	customerID := uuid.NewString()
	productID := uuid.NewString()
	creator := &canonicalInvoiceCreatorFake{}
	issuer := &posSalesDocumentIssuerFake{}
	documents := &DocumentService{
		salesInvoices:      newInvoiceSalesDocumentCreator(creator),
		salesInvoiceIssuer: issuer,
	}
	service := &CommerceService{
		businessRepo: atomicBusinessRepositoryFake{business: &models.BusinessProfile{
			ID: businessID, Currency: "INR", DefaultGSTTreatment: models.DocumentGSTTreatmentExempt,
		}},
		documents: documents,
	}
	order := &models.StoreOrder{
		ID: orderID, BusinessID: businessID, CustomerID: &customerID, Currency: "INR", OrderedAt: time.Now().UTC(),
		Lines: []*models.StoreOrderLine{{ProductID: &productID, Title: "Tea", Quantity: 1, UnitPrice: 100}},
	}

	invoiceID, err := service.createSalesInvoiceForOrder(context.Background(), order)
	require.NoError(t, err)
	require.Equal(t, issuer.invoiceID, invoiceID)
	require.Equal(t, models.InvoiceOriginStorefront, creator.input.Origin)
	require.Equal(t, storefrontInvoiceIdempotencyKey(orderID), creator.input.IdempotencyKey)
	require.Equal(t, creator.input.IdempotencyKey, issuer.input.IdempotencyKey)
	require.Equal(t, "bill_of_supply", issuer.input.DocumentType)
	require.Equal(t, "WEB", issuer.input.Series)
}

func TestCancelStoreOrderRejectsIssuedInvoiceWithoutCompensatingWorkflow(t *testing.T) {
	db := newStoreOrderLifecycleTestDB(t)
	businessID := uuid.NewString()
	storefrontID := uuid.NewString()
	orderID := uuid.NewString()
	invoiceID := uuid.NewString()
	require.NoError(t, db.Create(&models.Storefront{
		ID: storefrontID, BusinessID: businessID, Name: "Store", Slug: "store", Status: models.StorefrontStatusPublished,
	}).Error)
	require.NoError(t, db.Create(&models.StoreOrder{
		ID: orderID, BusinessID: businessID, StorefrontID: storefrontID, SalesInvoiceID: &invoiceID,
		PublicToken: uuid.NewString(), OrderNumber: "WEB-1", Status: models.StoreOrderStatusConfirmed,
		PaymentStatus: models.StoreOrderPaymentStatusCOD, Currency: "INR", OrderedAt: time.Now().UTC(),
	}).Error)
	service := &CommerceService{db: db}

	_, err := service.CancelStoreOrder(context.Background(), businessID, storefrontID, orderID, "customer request")
	require.ErrorContains(t, err, "issued storefront orders require a compensating credit note")

	var persisted models.StoreOrder
	require.NoError(t, db.First(&persisted, "id = ?", orderID).Error)
	require.Equal(t, models.StoreOrderStatusConfirmed, persisted.Status)
	require.Nil(t, persisted.CancelledAt)
	require.Empty(t, persisted.CancellationReason)
	var cancelledEvents int64
	require.NoError(t, db.Model(&models.StoreOrderEvent{}).
		Where("store_order_id = ? AND event_type = ?", orderID, "store_order.cancelled").
		Count(&cancelledEvents).Error)
	require.Zero(t, cancelledEvents)
}

func TestCancelStoreOrderIsIdempotentBeforeInvoice(t *testing.T) {
	db := newStoreOrderLifecycleTestDB(t)
	businessID := uuid.NewString()
	storefrontID := uuid.NewString()
	orderID := uuid.NewString()
	require.NoError(t, db.Create(&models.Storefront{
		ID: storefrontID, BusinessID: businessID, Name: "Store", Slug: "store", Status: models.StorefrontStatusPublished,
	}).Error)
	require.NoError(t, db.Create(&models.StoreOrder{
		ID: orderID, BusinessID: businessID, StorefrontID: storefrontID, PublicToken: uuid.NewString(),
		OrderNumber: "WEB-2", Status: models.StoreOrderStatusAwaitingApproval,
		PaymentStatus: models.StoreOrderPaymentStatusCOD, Currency: "INR", OrderedAt: time.Now().UTC(),
	}).Error)
	service := &CommerceService{db: db}

	first, err := service.CancelStoreOrder(context.Background(), businessID, storefrontID, orderID, "customer request")
	require.NoError(t, err)
	require.NotNil(t, first.CancelledAt)
	firstCancelledAt := *first.CancelledAt
	second, err := service.CancelStoreOrder(context.Background(), businessID, storefrontID, orderID, "changed retry reason")
	require.NoError(t, err)
	require.Equal(t, models.StoreOrderStatusCancelled, second.Status)
	require.Equal(t, "customer request", second.CancellationReason)
	require.NotNil(t, second.CancelledAt)
	require.Equal(t, firstCancelledAt, *second.CancelledAt)

	var cancelledEvents int64
	require.NoError(t, db.Model(&models.StoreOrderEvent{}).
		Where("store_order_id = ? AND event_type = ?", orderID, "store_order.cancelled").
		Count(&cancelledEvents).Error)
	require.Equal(t, int64(1), cancelledEvents)
}

func TestCancelStoreOrderRollsBackWhenEventCannotPersist(t *testing.T) {
	db := newStoreOrderLifecycleTestDB(t)
	businessID := uuid.NewString()
	storefrontID := uuid.NewString()
	orderID := uuid.NewString()
	require.NoError(t, db.Create(&models.Storefront{
		ID: storefrontID, BusinessID: businessID, Name: "Store", Slug: "store", Status: models.StorefrontStatusPublished,
	}).Error)
	require.NoError(t, db.Create(&models.StoreOrder{
		ID: orderID, BusinessID: businessID, StorefrontID: storefrontID, PublicToken: uuid.NewString(),
		OrderNumber: "WEB-3", Status: models.StoreOrderStatusAwaitingApproval,
		PaymentStatus: models.StoreOrderPaymentStatusCOD, Currency: "INR", OrderedAt: time.Now().UTC(),
	}).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_store_order_cancel_event
		BEFORE INSERT ON store_order_events
		BEGIN SELECT RAISE(FAIL, 'event write failed'); END`).Error)
	service := &CommerceService{db: db}

	_, err := service.CancelStoreOrder(context.Background(), businessID, storefrontID, orderID, "customer request")
	require.ErrorContains(t, err, "event write failed")

	var persisted models.StoreOrder
	require.NoError(t, db.First(&persisted, "id = ?", orderID).Error)
	require.Equal(t, models.StoreOrderStatusAwaitingApproval, persisted.Status)
	require.Nil(t, persisted.CancelledAt)
	require.Empty(t, persisted.CancellationReason)
}

func TestCancelStoreOrderRollsBackWhenReservationReleaseFails(t *testing.T) {
	db := newStoreOrderLifecycleTestDB(t)
	businessID := uuid.NewString()
	storefrontID := uuid.NewString()
	orderID := uuid.NewString()
	salesOrderID := uuid.NewString()
	require.NoError(t, db.Create(&models.Storefront{
		ID: storefrontID, BusinessID: businessID, Name: "Store", Slug: "store", Status: models.StorefrontStatusPublished,
	}).Error)
	require.NoError(t, db.Create(&models.StoreOrder{
		ID: orderID, BusinessID: businessID, StorefrontID: storefrontID, SalesOrderID: &salesOrderID,
		PublicToken: uuid.NewString(), OrderNumber: "WEB-4", Status: models.StoreOrderStatusAwaitingApproval,
		PaymentStatus: models.StoreOrderPaymentStatusCOD, Currency: "INR", OrderedAt: time.Now().UTC(),
	}).Error)
	service := &CommerceService{db: db, inventory: &InventoryService{db: db}}

	_, err := service.CancelStoreOrder(context.Background(), businessID, storefrontID, orderID, "customer request")
	require.ErrorContains(t, err, "inventory_reservations")

	var persisted models.StoreOrder
	require.NoError(t, db.First(&persisted, "id = ?", orderID).Error)
	require.Equal(t, models.StoreOrderStatusAwaitingApproval, persisted.Status)
	require.Nil(t, persisted.CancelledAt)
	var cancelledEvents int64
	require.NoError(t, db.Model(&models.StoreOrderEvent{}).
		Where("store_order_id = ? AND event_type = ?", orderID, "store_order.cancelled").
		Count(&cancelledEvents).Error)
	require.Zero(t, cancelledEvents)
}

func TestCancelStoreOrderReleasesReservationExactlyOnce(t *testing.T) {
	db := newStoreOrderLifecycleTestDB(t)
	addInventoryLifecycleTables(t, db)
	businessID := uuid.NewString()
	storefrontID := uuid.NewString()
	orderID := uuid.NewString()
	salesOrderID := uuid.NewString()
	productID := uuid.NewString()
	variantID := uuid.NewString()
	warehouseID := uuid.NewString()
	reservationID := uuid.NewString()
	require.NoError(t, db.Create(&models.Storefront{
		ID: storefrontID, BusinessID: businessID, Name: "Store", Slug: "store", Status: models.StorefrontStatusPublished,
	}).Error)
	require.NoError(t, db.Create(&models.StoreOrder{
		ID: orderID, BusinessID: businessID, StorefrontID: storefrontID, SalesOrderID: &salesOrderID,
		PublicToken: uuid.NewString(), OrderNumber: "WEB-5", Status: models.StoreOrderStatusAwaitingApproval,
		PaymentStatus: models.StoreOrderPaymentStatusCOD, Currency: "INR", OrderedAt: time.Now().UTC(),
	}).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO products (id, business_id, name, sku, price, currency, unit, stock_level, is_active) VALUES (?, ?, 'Tea', 'TEA', 100, 'INR', 'PCS', 100, 1)",
		productID, businessID,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO product_variants (id, business_id, product_id, name, sku, is_default, stock_level, reserved_level, is_active) VALUES (?, ?, ?, 'Default', 'TEA', 1, 100, 60, 1)",
		variantID, businessID, productID,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO inventory_balances (id, business_id, product_id, variant_id, warehouse_id, batch_key, on_hand, reserved, stock_value) VALUES (?, ?, ?, ?, ?, '', 100, 60, 0)",
		uuid.NewString(), businessID, productID, variantID, warehouseID,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO inventory_reservations (id, business_id, product_id, warehouse_id, document_id, quantity, status) VALUES (?, ?, ?, ?, ?, 60, 'active')",
		reservationID, businessID, productID, warehouseID, salesOrderID,
	).Error)
	service := &CommerceService{db: db, inventory: &InventoryService{db: db}}

	first, err := service.CancelStoreOrder(context.Background(), businessID, storefrontID, orderID, "customer request")
	require.NoError(t, err)
	require.Equal(t, models.StoreOrderStatusCancelled, first.Status)
	second, err := service.CancelStoreOrder(context.Background(), businessID, storefrontID, orderID, "retry")
	require.NoError(t, err)
	require.Equal(t, models.StoreOrderStatusCancelled, second.Status)

	var reservation models.InventoryReservation
	require.NoError(t, db.First(&reservation, "id = ?", reservationID).Error)
	require.Equal(t, "released", reservation.Status)
	var balance models.InventoryBalance
	require.NoError(t, db.First(&balance, "business_id = ? AND product_id = ? AND warehouse_id = ?", businessID, productID, warehouseID).Error)
	require.Zero(t, balance.Reserved)
	var releaseMoves int64
	require.NoError(t, db.Model(&models.StockMove{}).
		Where("document_id = ? AND direction = ?", salesOrderID, models.StockMoveDirectionRelease).
		Count(&releaseMoves).Error)
	require.Equal(t, int64(1), releaseMoves)
	var cancelledEvents int64
	require.NoError(t, db.Model(&models.StoreOrderEvent{}).
		Where("store_order_id = ? AND event_type = ?", orderID, "store_order.cancelled").
		Count(&cancelledEvents).Error)
	require.Equal(t, int64(1), cancelledEvents)
}

func TestStorefrontInvoiceStockConsumesSourceReservation(t *testing.T) {
	db := newStoreOrderLifecycleTestDB(t)
	addInventoryLifecycleTables(t, db)
	businessID := uuid.NewString()
	salesOrderID := uuid.NewString()
	invoiceID := uuid.NewString()
	lineID := uuid.NewString()
	productID := uuid.NewString()
	variantID := uuid.NewString()
	warehouseID := uuid.NewString()
	reservationID := uuid.NewString()
	require.NoError(t, db.Exec(
		"INSERT INTO products (id, business_id, name, sku, price, currency, unit, stock_level, is_active) VALUES (?, ?, 'Tea', 'TEA', 100, 'INR', 'PCS', 100, 1)",
		productID, businessID,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO product_variants (id, business_id, product_id, name, sku, is_default, stock_level, reserved_level, is_active) VALUES (?, ?, ?, 'Default', 'TEA', 1, 100, 60, 1)",
		variantID, businessID, productID,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO warehouses (id, business_id, name, code, is_default) VALUES (?, ?, 'Main', 'MAIN', 1)",
		warehouseID, businessID,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO inventory_balances (id, business_id, product_id, variant_id, warehouse_id, batch_key, on_hand, reserved, stock_value) VALUES (?, ?, ?, ?, ?, '', 100, 60, 0)",
		uuid.NewString(), businessID, productID, variantID, warehouseID,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO inventory_reservations (id, business_id, product_id, warehouse_id, document_id, quantity, status) VALUES (?, ?, ?, ?, ?, 60, 'active')",
		reservationID, businessID, productID, warehouseID, salesOrderID,
	).Error)
	service := &InventoryService{db: db}
	document := &models.Document{
		ID: invoiceID, BusinessID: businessID, DocumentType: models.DocumentTypeSalesInvoice,
		Direction: models.DocumentDirectionOutward, SerialNumber: "WEB-1",
		SourceLinkage: mustMarshalMap(map[string]interface{}{"sales_order_document_id": salesOrderID}),
		Lines: []*models.DocumentLine{{
			ID: lineID, DocumentID: invoiceID, ProductID: &productID, VariantID: &variantID,
			WarehouseID: &warehouseID, Quantity: 60, StockEffect: "out",
		}},
	}

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return service.ApplyDocumentTx(context.Background(), tx, document)
	}))

	var reservation models.InventoryReservation
	require.NoError(t, db.First(&reservation, "id = ?", reservationID).Error)
	require.Equal(t, "consumed", reservation.Status)
	var balance models.InventoryBalance
	require.NoError(t, db.First(&balance, "business_id = ? AND product_id = ? AND warehouse_id = ?", businessID, productID, warehouseID).Error)
	require.Equal(t, float64(40), balance.OnHand)
	require.Zero(t, balance.Reserved)
	var releaseMoves int64
	require.NoError(t, db.Model(&models.StockMove{}).
		Where("document_id = ? AND direction = ?", salesOrderID, models.StockMoveDirectionRelease).
		Count(&releaseMoves).Error)
	require.Equal(t, int64(1), releaseMoves)
}

func newStoreOrderLifecycleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE storefronts (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, name TEXT NOT NULL, slug TEXT NOT NULL,
		status TEXT NOT NULL, currency TEXT NOT NULL, allow_cod NUMERIC NOT NULL DEFAULT 1,
		allow_online_payment NUMERIC NOT NULL DEFAULT 0, auto_invoice_on_paid NUMERIC NOT NULL DEFAULT 1,
		minimum_order_value NUMERIC NOT NULL DEFAULT 0, settings TEXT DEFAULT '{}', blocked_users TEXT DEFAULT '[]',
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE storefront_domains (
		id TEXT PRIMARY KEY, storefront_id TEXT NOT NULL, domain TEXT, is_primary NUMERIC DEFAULT 0,
		verified_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE store_orders (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, storefront_id TEXT NOT NULL, branch_id TEXT,
		customer_id TEXT, coupon_id TEXT, sales_order_id TEXT, sales_invoice_id TEXT,
		public_token TEXT NOT NULL, order_number TEXT NOT NULL, status TEXT NOT NULL, payment_status TEXT NOT NULL,
		payment_method TEXT, currency TEXT NOT NULL, exchange_rate NUMERIC DEFAULT 1,
		fx_provider TEXT, fx_base_currency TEXT, fx_quote_currency TEXT, fx_rate_timestamp DATETIME,
		subtotal NUMERIC DEFAULT 0, discount_total NUMERIC DEFAULT 0, tax_total NUMERIC DEFAULT 0,
		shipping_total NUMERIC DEFAULT 0, total NUMERIC DEFAULT 0, snapshot TEXT DEFAULT '{}',
		billing_address TEXT DEFAULT '{}', shipping_address TEXT DEFAULT '{}', notes TEXT,
		idempotency_key TEXT, external_order_id TEXT, external_payment_id TEXT, gateway_order_id TEXT,
		gateway_payment_id TEXT, webhook_reference TEXT, ordered_at DATETIME, paid_at DATETIME,
		cancelled_at DATETIME, cancellation_reason TEXT, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE store_order_lines (
		id TEXT PRIMARY KEY, store_order_id TEXT NOT NULL, deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE store_order_events (
		id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))), store_order_id TEXT NOT NULL,
		event_type TEXT NOT NULL, status TEXT, payload TEXT DEFAULT '{}', created_at DATETIME,
		updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	return db
}

func addInventoryLifecycleTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`CREATE TABLE products (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, name TEXT, sku TEXT, barcode TEXT, price NUMERIC,
		cost_price NUMERIC DEFAULT 0, currency TEXT, unit TEXT, stock_level INTEGER DEFAULT 0,
		min_stock INTEGER DEFAULT 0, low_stock_threshold INTEGER DEFAULT 0, is_active NUMERIC DEFAULT 1,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE product_variants (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, product_id TEXT NOT NULL, name TEXT, sku TEXT,
		barcode TEXT, is_default NUMERIC DEFAULT 0, price NUMERIC DEFAULT 0, cost_price NUMERIC DEFAULT 0,
		stock_level NUMERIC DEFAULT 0, reserved_level NUMERIC DEFAULT 0, low_stock_threshold NUMERIC DEFAULT 0,
		is_active NUMERIC DEFAULT 1, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE warehouses (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, branch_id TEXT, name TEXT NOT NULL, code TEXT NOT NULL,
		is_default NUMERIC DEFAULT 0, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE inventory_balances (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, product_id TEXT NOT NULL, variant_id TEXT NOT NULL,
		warehouse_id TEXT NOT NULL, batch_id TEXT, batch_key TEXT NOT NULL DEFAULT '', on_hand NUMERIC DEFAULT 0,
		reserved NUMERIC DEFAULT 0, stock_value NUMERIC DEFAULT 0, last_recorded_at DATETIME,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE inventory_reservations (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, product_id TEXT NOT NULL, warehouse_id TEXT,
		document_id TEXT NOT NULL, document_line_id TEXT, quantity NUMERIC NOT NULL, status TEXT NOT NULL,
		expires_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE stock_moves (
		id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))), business_id TEXT NOT NULL, product_id TEXT NOT NULL,
		variant_id TEXT, warehouse_id TEXT, source_warehouse_id TEXT, document_id TEXT, document_line_id TEXT,
		project_id TEXT, batch_id TEXT, serial_number_id TEXT, transaction_type TEXT, direction TEXT NOT NULL,
		quantity NUMERIC NOT NULL, unit_cost NUMERIC DEFAULT 0, reason TEXT, actor_id TEXT, actor_role TEXT,
		metadata TEXT DEFAULT '{}', recorded_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE inventory_snapshots (
		id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))), business_id TEXT NOT NULL, snapshot_date DATETIME,
		product_id TEXT NOT NULL, variant_id TEXT NOT NULL, warehouse_id TEXT NOT NULL, on_hand NUMERIC DEFAULT 0,
		reserved NUMERIC DEFAULT 0, stock_value NUMERIC DEFAULT 0, created_at DATETIME, updated_at DATETIME,
		deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE inventory_event_logs (
		id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))), business_id TEXT NOT NULL, event_type TEXT NOT NULL,
		entity_type TEXT, entity_id TEXT, payload TEXT DEFAULT '{}', created_at DATETIME, updated_at DATETIME,
		deleted_at DATETIME
	)`).Error)
}

func TestNormalizePublicPaymentMethodRejectsUnsupportedMethods(t *testing.T) {
	if got := normalizePublicPaymentMethod(""); got != "cod" {
		t.Fatalf("expected blank payment method to default to cod, got %q", got)
	}
	if got := normalizePublicPaymentMethod("online"); got != "online" {
		t.Fatalf("expected online payment method, got %q", got)
	}
	if got := normalizePublicPaymentMethod("invoice-me-later"); got != "" {
		t.Fatalf("expected unsupported payment method to be rejected, got %q", got)
	}
}

func TestValidateDriveAssetUploadRejectsActiveContentAndOversize(t *testing.T) {
	if _, err := validateDriveAssetUpload(CreateDriveAssetInput{
		ContentType: "text/html",
		SizeBytes:   1024,
	}); err == nil {
		t.Fatal("expected active content type to be rejected")
	}

	if _, err := validateDriveAssetUpload(CreateDriveAssetInput{
		ContentType: "image/png",
		SizeBytes:   MaxDriveAssetUploadBytes + 1,
	}); err == nil {
		t.Fatal("expected oversized upload to be rejected")
	}

	contentType, err := validateDriveAssetUpload(CreateDriveAssetInput{
		ContentType: " IMAGE/PNG ",
		SizeBytes:   1024,
	})
	if err != nil {
		t.Fatalf("expected image/png upload to be accepted: %v", err)
	}
	if contentType != "image/png" {
		t.Fatalf("expected content type to normalize, got %q", contentType)
	}
}

func TestCreateDriveUploadSignsDeclaredSizeAndAccountsForQuotaUsage(t *testing.T) {
	db := newDriveUploadTestDB(t)
	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://storage.example.com")
		options.UsePathStyle = true
	})
	service := &CommerceService{
		cfg: &config.Config{S3: config.S3Config{BucketDrive: "private-drive"}},
		db:  db,
		s3:  &S3Service{client: client},
	}

	session, err := service.CreateDriveUpload(context.Background(), "business-123", "user-456", CreateDriveAssetInput{
		Name:        "invoice.pdf",
		ContentType: "application/pdf",
		SizeBytes:   8192,
	})
	require.NoError(t, err)
	require.Equal(t, "8192", session.RequiredHeaders["Content-Length"])
	require.Equal(t, "application/pdf", session.RequiredHeaders["Content-Type"])
	parsed, err := url.Parse(session.UploadURL)
	require.NoError(t, err)
	require.Contains(t, parsed.EscapedPath(), "/private-drive/business-123/")
	require.Equal(t, "business-123", session.Asset.BusinessID)
	require.NotNil(t, session.Asset.UploadedBy)
	require.Equal(t, "user-456", *session.Asset.UploadedBy)

	var usageBytes int64
	require.NoError(t, db.Model(&models.DriveAsset{}).
		Select("COALESCE(SUM(size_bytes), 0)").
		Where("business_id = ? AND deleted_at IS NULL", "business-123").
		Scan(&usageBytes).Error)
	require.Equal(t, int64(8192), usageBytes)
}

func newDriveUploadTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE feature_entitlements (
		id TEXT PRIMARY KEY,
		business_id TEXT NOT NULL,
		feature_key TEXT NOT NULL,
		enabled NUMERIC NOT NULL,
		limit_value INTEGER,
		metadata TEXT DEFAULT '{}',
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE drive_assets (
		id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
		business_id TEXT NOT NULL,
		uploaded_by TEXT,
		name TEXT NOT NULL,
		folder_path TEXT,
		bucket TEXT NOT NULL,
		object_key TEXT NOT NULL,
		content_type TEXT,
		size_bytes INTEGER NOT NULL,
		category TEXT,
		metadata TEXT DEFAULT '{}',
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error)

	for index, seed := range defaultEntitlementSeedsForSubscription(&models.Subscription{Plan: "starter", PlanCode: "pro", Status: "active"}) {
		require.NoError(t, db.Exec(
			"INSERT INTO feature_entitlements (id, business_id, feature_key, enabled, limit_value, metadata, created_at, updated_at) VALUES (?, ?, ?, ?, ?, '{}', ?, ?)",
			fmt.Sprintf("entitlement-%d", index), "business-123", seed.FeatureKey, seed.Enabled, seed.LimitValue, time.Now().UTC(), time.Now().UTC(),
		).Error)
	}
	return db
}

func seedEnabled(seeds []entitlementSeed, featureKey string) bool {
	for _, seed := range seeds {
		if seed.FeatureKey == featureKey {
			return seed.Enabled
		}
	}
	return false
}

func seedLimit(seeds []entitlementSeed, featureKey string) *int64 {
	for _, seed := range seeds {
		if seed.FeatureKey == featureKey {
			return seed.LimitValue
		}
	}
	return nil
}
