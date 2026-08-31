package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type concurrentRecurringInvoiceCreator struct {
	mu    sync.Mutex
	calls int
}

func (f *concurrentRecurringInvoiceCreator) CreateByBusiness(_ context.Context, businessID string, input CreateInvoiceInput) (*models.Invoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return &models.Invoice{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(businessID+":"+input.IdempotencyKey)).String()}, nil
}

func (f *concurrentRecurringInvoiceCreator) GetByBusiness(context.Context, string, string) (*models.Invoice, error) {
	return nil, errors.New("not implemented")
}

func (f *concurrentRecurringInvoiceCreator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestGenerateInvoiceSubscriptionNowPostgresSerializesSameCommand(t *testing.T) {
	database := newInventoryPostgresIntegrationDB(t)
	fixture := seedInventoryTransferFixture(t, database)
	require.NoError(t, database.AutoMigrate(
		&models.InvoiceSubscription{},
		&models.InvoiceSubscriptionLine{},
		&models.InvoiceSubscriptionRun{},
		&models.ActivityLog{},
	))
	subscription := &models.InvoiceSubscription{
		ID: uuid.NewString(), BusinessID: fixture.businessID, CustomerID: uuid.NewString(),
		Name: "Monthly tea", Status: models.InvoiceSubscriptionStatusActive, Cadence: "monthly",
		Timezone: "UTC", StartDate: time.Now().UTC(), PricePolicy: models.InvoiceSubscriptionPricePolicyFreeze,
		Currency: "INR",
		Lines: []*models.InvoiceSubscriptionLine{{
			ID: uuid.NewString(), ProductID: &fixture.productID, Description: "Tea", Quantity: 1, UnitPrice: 100,
		}},
	}
	require.NoError(t, database.Create(subscription).Error)
	creator := &concurrentRecurringInvoiceCreator{}
	service := &BillingOpsService{db: database, invoices: creator, log: logger.New()}
	commandKey := uuid.NewString()
	results := make(chan *models.InvoiceSubscriptionRun, 2)
	errorsChannel := make(chan error, 2)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			run, err := service.GenerateInvoiceSubscriptionNow(context.Background(), fixture.businessID, subscription.ID, commandKey)
			results <- run
			errorsChannel <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		require.NoError(t, err)
	}
	var runID, invoiceID string
	for run := range results {
		require.NotNil(t, run)
		require.NotNil(t, run.InvoiceID)
		if runID == "" {
			runID, invoiceID = run.ID, *run.InvoiceID
		}
		require.Equal(t, runID, run.ID)
		require.Equal(t, invoiceID, *run.InvoiceID)
	}
	require.Equal(t, 1, creator.callCount())
	var runCount int64
	require.NoError(t, database.Model(&models.InvoiceSubscriptionRun{}).
		Where("business_id = ? AND subscription_id = ?", fixture.businessID, subscription.ID).
		Count(&runCount).Error)
	require.Equal(t, int64(1), runCount)
}

func TestDispatchDueInvoiceSubscriptionsPostgresSerializesScheduledRun(t *testing.T) {
	database := newInventoryPostgresIntegrationDB(t)
	fixture := seedInventoryTransferFixture(t, database)
	require.NoError(t, database.AutoMigrate(
		&models.InvoiceSubscription{},
		&models.InvoiceSubscriptionLine{},
		&models.InvoiceSubscriptionRun{},
		&models.ActivityLog{},
	))
	dueAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	subscription := &models.InvoiceSubscription{
		ID: uuid.NewString(), BusinessID: fixture.businessID, CustomerID: uuid.NewString(),
		Name: "Scheduled tea", Status: models.InvoiceSubscriptionStatusActive, Cadence: "monthly",
		Timezone: "UTC", StartDate: dueAt, NextRunAt: &dueAt,
		PricePolicy: models.InvoiceSubscriptionPricePolicyFreeze, Currency: "INR",
		Lines: []*models.InvoiceSubscriptionLine{{
			ID: uuid.NewString(), ProductID: &fixture.productID, Description: "Tea", Quantity: 1, UnitPrice: 100,
		}},
	}
	require.NoError(t, database.Create(subscription).Error)
	creator := &concurrentRecurringInvoiceCreator{}
	service := &BillingOpsService{db: database, invoices: creator, log: logger.New()}
	errorsChannel := make(chan error, 2)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := service.DispatchDueInvoiceSubscriptions(context.Background(), 10)
			errorsChannel <- err
		}()
	}
	close(start)
	group.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		require.NoError(t, err)
	}
	require.Equal(t, 1, creator.callCount())
	var runs []models.InvoiceSubscriptionRun
	require.NoError(t, database.Where("business_id = ? AND subscription_id = ?", fixture.businessID, subscription.ID).Find(&runs).Error)
	require.Len(t, runs, 1)
	require.Equal(t, models.BulkJobStatusCompleted, runs[0].Status)
	require.WithinDuration(t, dueAt, runs[0].ScheduledFor, time.Microsecond)
}
