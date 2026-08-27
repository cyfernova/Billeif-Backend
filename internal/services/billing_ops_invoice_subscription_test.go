package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateInvoiceSubscriptionRejectsAutoSendUntilIssueDeliveryExists(t *testing.T) {
	service := NewBillingOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, logger.New())

	subscription, err := service.CreateInvoiceSubscription(context.Background(), CreateInvoiceSubscriptionInput{
		AutoSend: true,
	})

	if subscription != nil {
		t.Fatalf("subscription = %#v, want nil", subscription)
	}
	if !errors.Is(err, models.ErrInvalidInvoiceLifecycle) {
		t.Fatalf("error = %v, want invalid invoice lifecycle", err)
	}
}

func TestGenerateExistingAutoSendSubscriptionReportsUnsupportedLifecycle(t *testing.T) {
	subscription := &models.InvoiceSubscription{
		Status:   models.InvoiceSubscriptionStatusActive,
		AutoSend: true,
	}

	err := validateInvoiceSubscriptionGeneration(subscription)

	if !errors.Is(err, models.ErrInvalidInvoiceLifecycle) {
		t.Fatalf("error = %v, want invalid invoice lifecycle", err)
	}
}

func TestDispatchDueInvoiceSubscriptionsRejectsAutoSendBeforeQueuedRun(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:dispatch-auto-send?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory dispatch database: %v", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatalf("read in-memory dispatch database handle: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDatabase.Close(); err != nil {
			t.Errorf("close in-memory dispatch database: %v", err)
		}
	})
	for _, statement := range []string{
		`CREATE TABLE invoice_subscriptions (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			status TEXT NOT NULL,
			cadence TEXT NOT NULL,
			timezone TEXT NOT NULL,
			next_run_at DATETIME,
			last_run_at DATETIME,
			auto_send BOOLEAN NOT NULL,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE invoice_subscription_runs (
			id TEXT PRIMARY KEY,
			subscription_id TEXT NOT NULL,
			business_id TEXT NOT NULL,
			scheduled_for DATETIME NOT NULL,
			status TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			invoice_id TEXT,
			document_id TEXT,
			attempt_count INTEGER,
			last_error TEXT,
			metadata TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			completed_at DATETIME,
			deleted_at DATETIME
		)`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("create dispatch fixture table: %v", err)
		}
	}

	dueAt := time.Now().UTC().Add(-time.Minute)
	if err := database.Exec(`
		INSERT INTO invoice_subscriptions (
			id, business_id, status, cadence, timezone, next_run_at, auto_send
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"subscription-auto-send",
		"business-123",
		models.InvoiceSubscriptionStatusActive,
		"monthly",
		"UTC",
		dueAt,
		true,
	).Error; err != nil {
		t.Fatalf("seed due auto-send subscription: %v", err)
	}

	service := NewBillingOpsService(nil, database, nil, nil, nil, nil, nil, nil, nil, logger.New())
	runs, err := service.DispatchDueInvoiceSubscriptions(context.Background(), 10)

	if !errors.Is(err, models.ErrInvalidInvoiceLifecycle) {
		t.Fatalf("error = %v, want invalid invoice lifecycle", err)
	}
	if len(runs) != 0 {
		t.Fatalf("dispatched runs = %d, want 0", len(runs))
	}
	var runCount int64
	if err := database.Model(&models.InvoiceSubscriptionRun{}).Count(&runCount).Error; err != nil {
		t.Fatalf("count dispatched runs: %v", err)
	}
	if runCount != 0 {
		t.Fatalf("persisted queued runs = %d, want 0", runCount)
	}
	var subscription models.InvoiceSubscription
	if err := database.First(&subscription, "id = ?", "subscription-auto-send").Error; err != nil {
		t.Fatalf("reload due subscription: %v", err)
	}
	if subscription.LastRunAt != nil {
		t.Fatalf("last_run_at = %v, want nil", subscription.LastRunAt)
	}
	if subscription.NextRunAt == nil || !subscription.NextRunAt.Equal(dueAt) {
		t.Fatalf("next_run_at = %v, want unchanged %v", subscription.NextRunAt, dueAt)
	}
}

func TestInvoiceSubscriptionCreateItemsDefersFollowPricingToCanonicalBatchResolver(t *testing.T) {
	productID := uuid.NewString()
	variantID := uuid.NewString()
	warehouseID := uuid.NewString()
	line := &models.InvoiceSubscriptionLine{
		ProductID: &productID, VariantID: &variantID, WarehouseID: &warehouseID,
		Description: "Subscription line", Quantity: 2, FreeQuantity: 1,
		UnitPrice: 50, MRP: 60, DiscountAmount: 5, TaxRate: 18, CessRate: 2,
		CustomFields: `{"source":"subscription"}`,
	}

	follow := invoiceSubscriptionCreateItems(&models.InvoiceSubscription{
		PricePolicy: models.InvoiceSubscriptionPricePolicyFollow,
		Lines:       []*models.InvoiceSubscriptionLine{line},
	})
	snapshot := invoiceSubscriptionCreateItems(&models.InvoiceSubscription{
		PricePolicy: models.InvoiceSubscriptionPricePolicyFreeze,
		Lines:       []*models.InvoiceSubscriptionLine{line},
	})

	if len(follow) != 1 || follow[0].UnitPrice != 0 || follow[0].MRP != 0 || follow[0].CessRate != 0 {
		t.Fatalf("follow-pricing item = %#v, want deferred database pricing", follow)
	}
	if follow[0].ProductID != productID || follow[0].VariantID != variantID ||
		follow[0].WarehouseID != warehouseID || follow[0].Discount != 5 {
		t.Fatalf("follow-pricing identity/discount = %#v", follow[0])
	}
	if len(snapshot) != 1 || snapshot[0].UnitPrice != 50 || snapshot[0].MRP != 60 ||
		snapshot[0].CessRate != 2 || snapshot[0].Discount != 5 {
		t.Fatalf("snapshot-pricing item = %#v, want stored subscription facts", snapshot)
	}
}
