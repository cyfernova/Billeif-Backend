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
		&models.StorefrontCategory{},
		&models.StorefrontProduct{},
		&models.Document{},
		&models.DocumentLine{},
		&models.DocumentRevision{},
		&models.InventoryReservation{},
		&models.APIIdempotencyKey{},
		&models.StorefrontCoupon{},
		&models.StoreOrder{},
		&models.StoreOrderLine{},
		&models.StorefrontCouponRedemption{},
		&models.StoreOrderEvent{},
		&models.WhatsAppConfig{},
	))

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
	documents := &DocumentService{
		db: database, businessRepo: businessRepo, customerRepo: customerRepo,
		productRepo: productRepo, inventory: inventory, log: logger.New(),
	}
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
