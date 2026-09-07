package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type draftCustomerRepo struct {
	customers map[string]*models.Customer
}

func (r draftCustomerRepo) Create(ctx context.Context, customer *models.Customer) error { return nil }
func (r draftCustomerRepo) GetByID(ctx context.Context, id, businessID string) (*models.Customer, error) {
	customer, ok := r.customers[id]
	if !ok || customer.BusinessID != businessID {
		return nil, gorm.ErrRecordNotFound
	}
	return customer, nil
}
func (r draftCustomerRepo) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Customer, int64, error) {
	return nil, 0, nil
}
func (r draftCustomerRepo) Update(ctx context.Context, customer *models.Customer) error { return nil }
func (r draftCustomerRepo) Delete(ctx context.Context, id string) error                 { return nil }

func newInvoiceDraftTestService(t *testing.T) (*InvoiceService, *gorm.DB, string, string, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	createInvoiceDraftTestSchema(t, db)
	businessID := uuid.NewString()
	oldCustomerID := uuid.NewString()
	newCustomerID := uuid.NewString()
	customerRepo := draftCustomerRepo{customers: map[string]*models.Customer{
		oldCustomerID: {ID: oldCustomerID, BusinessID: businessID, Name: "Old Customer"},
		newCustomerID: {ID: newCustomerID, BusinessID: businessID, Name: "New Customer"},
	}}
	svc := &InvoiceService{
		db:           db,
		repo:         postgres.NewInvoiceRepository(db),
		customerRepo: customerRepo,
		log:          logger.New(),
	}
	return svc, db, businessID, oldCustomerID, newCustomerID
}

func createInvoiceDraftTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE invoices (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			customer_id TEXT,
			version INTEGER NOT NULL DEFAULT 1,
			project_id TEXT,
			branch_id TEXT,
			price_list_id TEXT,
			render_profile_id TEXT,
			invoice_no TEXT,
			origin TEXT NOT NULL DEFAULT 'conversion',
			issued_at DATETIME,
			seller_snapshot TEXT NOT NULL DEFAULT '{}',
			buyer_snapshot TEXT NOT NULL DEFAULT '{}',
			invoice_date DATETIME NOT NULL,
			due_date DATETIME,
			status TEXT NOT NULL DEFAULT 'draft',
			currency TEXT NOT NULL DEFAULT 'INR',
			subtotal REAL DEFAULT 0,
			tax REAL DEFAULT 0,
			discount REAL DEFAULT 0,
			total REAL NOT NULL DEFAULT 0,
			paid_amount REAL DEFAULT 0,
			balance_due REAL DEFAULT 0,
			notes TEXT,
			customer_snapshot TEXT DEFAULT '{}',
			document_json TEXT DEFAULT '{}',
			template_override TEXT DEFAULT '{}',
			payment_display TEXT DEFAULT '{}',
			terms_json TEXT DEFAULT '{}',
			eway_details_json TEXT DEFAULT '{}',
			einvoice_settings_json TEXT DEFAULT '{}',
			custom_fields TEXT DEFAULT '{}',
			additional_charges TEXT DEFAULT '[]',
			origin_subscription_id TEXT,
			origin_run_id TEXT,
			signed_at DATETIME,
			signed_by_profile_id TEXT,
			sign_metadata TEXT DEFAULT '{}',
			pdf_url TEXT,
			pdf_filename TEXT,
			tax_profile TEXT DEFAULT '{}',
			sent_at DATETIME,
			paid_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE invoice_items (
			id TEXT PRIMARY KEY,
			invoice_id TEXT NOT NULL,
			product_id TEXT,
			variant_id TEXT,
			warehouse_id TEXT,
			description TEXT NOT NULL,
			hsn_sac_code TEXT,
			unit TEXT,
			quantity REAL NOT NULL,
			free_quantity REAL DEFAULT 0,
			unit_price REAL NOT NULL,
			mrp REAL DEFAULT 0,
			discount REAL DEFAULT 0,
			tax_rate REAL DEFAULT 0,
			cess_rate REAL DEFAULT 0,
			cess_amount REAL DEFAULT 0,
			custom_fields TEXT DEFAULT '{}',
			charge_snapshot TEXT DEFAULT '[]',
			batch_allocations TEXT DEFAULT '[]',
			serial_ids TEXT DEFAULT '[]',
			total REAL NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE activity_logs (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			business_id TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			actor_role TEXT,
			request_id TEXT,
			ip_address TEXT,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			action TEXT NOT NULL,
			reason TEXT,
			snapshot TEXT DEFAULT '{}',
			diff TEXT DEFAULT '{}',
			metadata TEXT DEFAULT '{}',
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create invoice draft test schema: %v", err)
		}
	}
}

func seedDraftInvoice(t *testing.T, db *gorm.DB, businessID, customerID string, version int, status string) string {
	t.Helper()
	invoiceID := uuid.NewString()
	invoice := &models.Invoice{
		ID:                invoiceID,
		BusinessID:        businessID,
		CustomerID:        models.StringPointer(customerID),
		Version:           version,
		InvoiceNo:         models.StringPointer("DRAFT-001"),
		Status:            status,
		InvoiceDate:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		DueDate:           time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Currency:          "INR",
		Subtotal:          100,
		Tax:               18,
		Total:             118,
		BalanceDue:        118,
		CustomFields:      "{}",
		TaxProfile:        "{}",
		AdditionalCharges: "[]",
	}
	if err := db.Create(invoice).Error; err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	item := &models.InvoiceItem{
		ID:           uuid.NewString(),
		InvoiceID:    invoiceID,
		Description:  "Old Item",
		Quantity:     1,
		UnitPrice:    100,
		TaxRate:      18,
		Total:        118,
		CustomFields: "{}",
	}
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("seed invoice item: %v", err)
	}
	return invoiceID
}

func TestInvoiceService_UpdateDraftByBusiness_PersistsCustomerItemsAndRecalculates(t *testing.T) {
	svc, db, businessID, oldCustomerID, newCustomerID := newInvoiceDraftTestService(t)
	invoiceID := seedDraftInvoice(t, db, businessID, oldCustomerID, 1, "draft")
	ctx := ContextWithActor(context.Background(), ActorContext{
		UserID:    uuid.NewString(),
		Role:      "accountant",
		RequestID: "req-draft-update",
		IPAddress: "127.0.0.1",
	})

	updated, err := svc.UpdateDraftByBusiness(ctx, businessID, invoiceID, UpdateInvoiceDraftInput{
		ExpectedVersion: 1,
		CustomerID:      &newCustomerID,
		CustomerSnapshot: map[string]interface{}{
			"id":                newCustomerID,
			"name":              "New Customer",
			"gstin":             "27ABCDE1234F1Z5",
			"registration_type": "REGISTERED",
			"state_code":        "27",
		},
		Document: map[string]interface{}{
			"tax_mode":            "GST",
			"supplier_state_code": "27",
		},
		Details: map[string]interface{}{
			"price_mode":                 "TAX_EXCLUSIVE",
			"place_of_supply_state_code": "27",
		},
		Items: []UpdateInvoiceDraftLineInput{
			{
				Description:    "Rice bags",
				HSNSACCode:     "1006",
				ItemType:       "GOODS",
				SKU:            "RICE-25",
				Quantity:       1,
				FreeQuantity:   2,
				Unit:           "BAG",
				UnitPricePaise: 100000,
				TaxRateBps:     1800,
				Batch:          "B-42",
			},
		},
		TemplateOverride:   map[string]interface{}{"bill_template_id": "bill_06"},
		PaymentDisplay:     map[string]interface{}{"show_upi_qr": true, "upi_id": "merchant@upi"},
		Terms:              map[string]interface{}{"notes": "Handle with care", "terms_and_conditions": "Due on receipt"},
		Notes:              "Handle with care",
		TermsAndConditions: "Due on receipt",
	})
	if err != nil {
		t.Fatalf("update draft: %v", err)
	}

	if updated.Version != 2 {
		t.Fatalf("version = %d, want 2", updated.Version)
	}
	if updated.CustomerID == nil || *updated.CustomerID != newCustomerID {
		t.Fatalf("customer id = %v, want %q", updated.CustomerID, newCustomerID)
	}
	if updated.Total != 1180 || updated.Tax != 180 || updated.Subtotal != 1000 {
		t.Fatalf("totals = subtotal %.2f tax %.2f total %.2f, want 1000/180/1180", updated.Subtotal, updated.Tax, updated.Total)
	}
	if len(updated.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(updated.Items))
	}
	if updated.Items[0].Description != "Rice bags" || updated.Items[0].FreeQuantity != 2 || updated.Items[0].SKU != "RICE-25" || updated.Items[0].Batch != "B-42" {
		t.Fatalf("updated item not hydrated: %#v", updated.Items[0])
	}
	if updated.CustomerSnapshot["name"] != "New Customer" {
		t.Fatalf("customer snapshot not hydrated: %#v", updated.CustomerSnapshot)
	}
	if updated.PaymentDisplay["upi_id"] != "merchant@upi" {
		t.Fatalf("payment display not hydrated: %#v", updated.PaymentDisplay)
	}

	var logCount int64
	if err := db.Table("activity_logs").
		Where("business_id = ? AND entity_type = ? AND entity_id = ? AND action = ?", businessID, "invoice", invoiceID, "draft_updated").
		Count(&logCount).Error; err != nil {
		t.Fatalf("count activity logs: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("activity log count = %d, want 1", logCount)
	}
}

func TestInvoiceService_UpdateDraftByBusiness_RejectsStaleVersion(t *testing.T) {
	svc, db, businessID, oldCustomerID, _ := newInvoiceDraftTestService(t)
	invoiceID := seedDraftInvoice(t, db, businessID, oldCustomerID, 3, "draft")

	_, err := svc.UpdateDraftByBusiness(context.Background(), businessID, invoiceID, UpdateInvoiceDraftInput{
		ExpectedVersion: 2,
		Items:           []UpdateInvoiceDraftLineInput{{Description: "Item", Quantity: 1, UnitPricePaise: 10000}},
	})
	if err == nil || !strings.Contains(err.Error(), "version conflict") {
		t.Fatalf("err = %v, want version conflict", err)
	}
}

func TestInvoiceService_UpdateDraftByBusiness_RejectsNonDraft(t *testing.T) {
	svc, db, businessID, oldCustomerID, _ := newInvoiceDraftTestService(t)
	invoiceID := seedDraftInvoice(t, db, businessID, oldCustomerID, 1, "paid")

	_, err := svc.UpdateDraftByBusiness(context.Background(), businessID, invoiceID, UpdateInvoiceDraftInput{
		ExpectedVersion: 1,
		Items:           []UpdateInvoiceDraftLineInput{{Description: "Item", Quantity: 1, UnitPricePaise: 10000}},
	})
	if err == nil || !strings.Contains(err.Error(), "only draft invoices") {
		t.Fatalf("err = %v, want non-draft rejection", err)
	}
}
