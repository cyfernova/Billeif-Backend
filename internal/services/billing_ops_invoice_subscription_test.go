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

func TestDispatchDueInvoiceSubscriptionsGeneratesValidDueRunsAndReportsInvalidLifecycle(t *testing.T) {
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
			id TEXT PRIMARY KEY, business_id TEXT NOT NULL, customer_id TEXT NOT NULL, name TEXT NOT NULL,
			status TEXT NOT NULL, cadence TEXT NOT NULL, timezone TEXT NOT NULL, start_date DATETIME NOT NULL,
			end_date DATETIME, next_run_at DATETIME, last_run_at DATETIME, auto_send NUMERIC NOT NULL DEFAULT 0,
			price_policy TEXT NOT NULL, price_list_id TEXT, currency TEXT NOT NULL, notes TEXT, metadata TEXT DEFAULT '{}',
			template_invoice_id TEXT, template_document_id TEXT, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE TABLE invoice_subscription_lines (
			id TEXT PRIMARY KEY, subscription_id TEXT NOT NULL, product_id TEXT, variant_id TEXT, warehouse_id TEXT,
			description TEXT NOT NULL, quantity NUMERIC NOT NULL, free_quantity NUMERIC DEFAULT 0,
			unit_price NUMERIC NOT NULL, mrp NUMERIC DEFAULT 0, discount_amount NUMERIC DEFAULT 0,
			tax_rate NUMERIC DEFAULT 0, cess_rate NUMERIC DEFAULT 0, custom_fields TEXT DEFAULT '{}',
			additional_charge TEXT DEFAULT '{}', position INTEGER DEFAULT 0, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE invoice_subscription_runs (
			id TEXT PRIMARY KEY, subscription_id TEXT NOT NULL, business_id TEXT NOT NULL, scheduled_for DATETIME NOT NULL,
			status TEXT NOT NULL, idempotency_key TEXT NOT NULL UNIQUE, invoice_id TEXT, document_id TEXT,
			attempt_count INTEGER DEFAULT 0, last_error TEXT, metadata TEXT DEFAULT '{}', created_at DATETIME,
			updated_at DATETIME, completed_at DATETIME, deleted_at DATETIME
		)`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("create dispatch fixture table: %v", err)
		}
	}

	dueAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	validBusinessID := uuid.NewString()
	validSubscriptionID := uuid.NewString()
	if err := database.Exec(`
		INSERT INTO invoice_subscriptions (
			id, business_id, customer_id, name, status, cadence, timezone, start_date, next_run_at,
			auto_send, price_policy, currency
		) VALUES (?, ?, ?, 'Legacy auto-send', ?, ?, ?, ?, ?, ?, 'freeze_on_create', 'INR')`,
		"subscription-auto-send",
		"business-123",
		uuid.NewString(),
		models.InvoiceSubscriptionStatusActive,
		"monthly",
		"UTC",
		dueAt,
		dueAt,
		true,
	).Error; err != nil {
		t.Fatalf("seed due auto-send subscription: %v", err)
	}
	if err := database.Exec(`INSERT INTO invoice_subscriptions
		(id, business_id, customer_id, name, status, cadence, timezone, start_date, next_run_at,
		auto_send, price_policy, currency)
		VALUES (?, ?, ?, 'Scheduled tea', 'active', 'monthly', 'UTC', ?, ?, 0, 'freeze_on_create', 'INR')`,
		validSubscriptionID, validBusinessID, uuid.NewString(), dueAt, dueAt).Error; err != nil {
		t.Fatalf("seed valid due subscription: %v", err)
	}
	if err := database.Exec(`INSERT INTO invoice_subscription_lines
		(id, subscription_id, product_id, description, quantity, unit_price, position)
		VALUES (?, ?, ?, 'Tea', 2, 100, 0)`, uuid.NewString(), validSubscriptionID, uuid.NewString()).Error; err != nil {
		t.Fatalf("seed valid due subscription line: %v", err)
	}

	creator := &recurringInvoiceCreatorFake{}
	service := &BillingOpsService{db: database, invoices: creator, log: logger.New()}
	result, err := service.DispatchDueInvoiceSubscriptions(context.Background(), 10)

	if !errors.Is(err, models.ErrInvalidInvoiceLifecycle) {
		t.Fatalf("error = %v, want invalid invoice lifecycle", err)
	}
	if result.Due != 2 || result.Completed != 1 || result.Failed != 1 {
		t.Fatalf("dispatch result = %#v, want 2 due, 1 completed, 1 failed", result)
	}
	var runCount int64
	if err := database.Model(&models.InvoiceSubscriptionRun{}).Count(&runCount).Error; err != nil {
		t.Fatalf("count dispatched runs: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("persisted runs = %d, want 1 completed run", runCount)
	}
	var validRun models.InvoiceSubscriptionRun
	if err := database.First(&validRun, "subscription_id = ?", validSubscriptionID).Error; err != nil {
		t.Fatalf("reload valid scheduled run: %v", err)
	}
	if validRun.Status != models.BulkJobStatusCompleted || !validRun.ScheduledFor.Equal(dueAt) || validRun.InvoiceID == nil {
		t.Fatalf("valid scheduled run = %#v, want completed at persisted schedule", validRun)
	}
	if creator.calls != 1 || len(creator.inputs) != 1 || creator.inputs[0].IdempotencyKey != validRun.ID {
		t.Fatalf("canonical invoice calls/inputs = %d/%#v, want one run-bound command", creator.calls, creator.inputs)
	}
	wantNextRunAt, err := cadenceNextRun(dueAt, "monthly", "UTC")
	if err != nil {
		t.Fatalf("calculate expected next run: %v", err)
	}
	var validSubscription models.InvoiceSubscription
	if err := database.First(&validSubscription, "id = ?", validSubscriptionID).Error; err != nil {
		t.Fatalf("reload valid subscription: %v", err)
	}
	if validSubscription.NextRunAt == nil || !validSubscription.NextRunAt.Equal(wantNextRunAt) {
		t.Fatalf("valid next_run_at = %v, want cadence anchored at %v", validSubscription.NextRunAt, wantNextRunAt)
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

type recurringInvoiceCreatorFake struct {
	calls  int
	inputs []CreateInvoiceInput
}

func (f *recurringInvoiceCreatorFake) CreateByBusiness(_ context.Context, businessID string, input CreateInvoiceInput) (*models.Invoice, error) {
	f.calls++
	f.inputs = append(f.inputs, input)
	return &models.Invoice{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(businessID+":"+input.IdempotencyKey)).String()}, nil
}

func (f *recurringInvoiceCreatorFake) GetByBusiness(context.Context, string, string) (*models.Invoice, error) {
	return nil, errors.New("not implemented")
}

func TestGenerateInvoiceSubscriptionNowReplaysOneTenantBoundRun(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:recurring-idempotency?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open recurring invoice database: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE invoice_subscriptions (
			id TEXT PRIMARY KEY, business_id TEXT NOT NULL, customer_id TEXT NOT NULL, name TEXT NOT NULL,
			status TEXT NOT NULL, cadence TEXT NOT NULL, timezone TEXT NOT NULL, start_date DATETIME NOT NULL,
			end_date DATETIME, next_run_at DATETIME, last_run_at DATETIME, auto_send NUMERIC NOT NULL DEFAULT 0,
			price_policy TEXT NOT NULL, price_list_id TEXT, currency TEXT NOT NULL, notes TEXT, metadata TEXT DEFAULT '{}',
			template_invoice_id TEXT, template_document_id TEXT, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE TABLE invoice_subscription_lines (
			id TEXT PRIMARY KEY, subscription_id TEXT NOT NULL, product_id TEXT, variant_id TEXT, warehouse_id TEXT,
			description TEXT NOT NULL, quantity NUMERIC NOT NULL, free_quantity NUMERIC DEFAULT 0,
			unit_price NUMERIC NOT NULL, mrp NUMERIC DEFAULT 0, discount_amount NUMERIC DEFAULT 0,
			tax_rate NUMERIC DEFAULT 0, cess_rate NUMERIC DEFAULT 0, custom_fields TEXT DEFAULT '{}',
			additional_charge TEXT DEFAULT '{}', position INTEGER DEFAULT 0, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE invoice_subscription_runs (
			id TEXT PRIMARY KEY, subscription_id TEXT NOT NULL, business_id TEXT NOT NULL, scheduled_for DATETIME NOT NULL,
			status TEXT NOT NULL, idempotency_key TEXT NOT NULL UNIQUE, invoice_id TEXT, document_id TEXT,
			attempt_count INTEGER DEFAULT 0, last_error TEXT, metadata TEXT DEFAULT '{}', created_at DATETIME,
			updated_at DATETIME, completed_at DATETIME, deleted_at DATETIME
		)`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("create recurring invoice fixture: %v", err)
		}
	}
	businessID := uuid.NewString()
	subscriptionID := uuid.NewString()
	customerID := uuid.NewString()
	productID := uuid.NewString()
	commandKey := uuid.NewString()
	now := time.Now().UTC()
	if err := database.Exec(`INSERT INTO invoice_subscriptions
		(id, business_id, customer_id, name, status, cadence, timezone, start_date, next_run_at, auto_send, price_policy, currency)
		VALUES (?, ?, ?, 'Monthly tea', 'active', 'monthly', 'UTC', ?, ?, 0, 'freeze_on_create', 'INR')`,
		subscriptionID, businessID, customerID, now, now.AddDate(0, 1, 0)).Error; err != nil {
		t.Fatalf("seed recurring invoice subscription: %v", err)
	}
	if err := database.Exec(`INSERT INTO invoice_subscription_lines
		(id, subscription_id, product_id, description, quantity, unit_price, position)
		VALUES (?, ?, ?, 'Tea', 2, 100, 0)`, uuid.NewString(), subscriptionID, productID).Error; err != nil {
		t.Fatalf("seed recurring invoice line: %v", err)
	}
	creator := &recurringInvoiceCreatorFake{}
	service := &BillingOpsService{db: database, invoices: creator, log: logger.New()}

	first, err := service.GenerateInvoiceSubscriptionNow(context.Background(), businessID, subscriptionID, commandKey)
	if err != nil {
		t.Fatalf("first recurring invoice generation: %v", err)
	}
	if err := database.Model(&models.InvoiceSubscription{}).
		Where("id = ?", subscriptionID).
		Update("status", models.InvoiceSubscriptionStatusPaused).Error; err != nil {
		t.Fatalf("pause subscription after completed run: %v", err)
	}
	second, err := service.GenerateInvoiceSubscriptionNow(context.Background(), businessID, subscriptionID, commandKey)
	if err != nil {
		t.Fatalf("replayed recurring invoice generation: %v", err)
	}
	if first.ID != second.ID || first.InvoiceID == nil || second.InvoiceID == nil || *first.InvoiceID != *second.InvoiceID {
		t.Fatalf("run replay mismatch: first=%#v second=%#v", first, second)
	}
	if creator.calls != 1 || len(creator.inputs) != 1 || creator.inputs[0].IdempotencyKey != first.ID {
		t.Fatalf("canonical invoice calls/inputs = %d/%#v, want one run-bound command", creator.calls, creator.inputs)
	}
	var runCount int64
	if err := database.Model(&models.InvoiceSubscriptionRun{}).Where("business_id = ?", businessID).Count(&runCount).Error; err != nil {
		t.Fatalf("count recurring runs: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("recurring run count = %d, want 1", runCount)
	}
}
