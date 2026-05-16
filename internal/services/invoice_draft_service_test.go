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
	if err := db.AutoMigrate(&models.Invoice{}, &models.InvoiceItem{}); err != nil {
		t.Fatalf("migrate sqlite invoice schema: %v", err)
	}
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

func seedDraftInvoice(t *testing.T, db *gorm.DB, businessID, customerID string, version int, status string) string {
	t.Helper()
	invoiceID := uuid.NewString()
	invoice := &models.Invoice{
		ID:                invoiceID,
		BusinessID:        businessID,
		CustomerID:        customerID,
		Version:           version,
		InvoiceNo:         "DRAFT-001",
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

	updated, err := svc.UpdateDraftByBusiness(context.Background(), businessID, invoiceID, UpdateInvoiceDraftInput{
		Version:    1,
		CustomerID: &newCustomerID,
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
	if updated.CustomerID != newCustomerID {
		t.Fatalf("customer id = %q, want %q", updated.CustomerID, newCustomerID)
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
}

func TestInvoiceService_UpdateDraftByBusiness_RejectsStaleVersion(t *testing.T) {
	svc, db, businessID, oldCustomerID, _ := newInvoiceDraftTestService(t)
	invoiceID := seedDraftInvoice(t, db, businessID, oldCustomerID, 3, "draft")

	_, err := svc.UpdateDraftByBusiness(context.Background(), businessID, invoiceID, UpdateInvoiceDraftInput{
		Version: 2,
		Items:   []UpdateInvoiceDraftLineInput{{Description: "Item", Quantity: 1, UnitPricePaise: 10000}},
	})
	if err == nil || !strings.Contains(err.Error(), "version conflict") {
		t.Fatalf("err = %v, want version conflict", err)
	}
}

func TestInvoiceService_UpdateDraftByBusiness_RejectsNonDraft(t *testing.T) {
	svc, db, businessID, oldCustomerID, _ := newInvoiceDraftTestService(t)
	invoiceID := seedDraftInvoice(t, db, businessID, oldCustomerID, 1, "paid")

	_, err := svc.UpdateDraftByBusiness(context.Background(), businessID, invoiceID, UpdateInvoiceDraftInput{
		Version: 1,
		Items:   []UpdateInvoiceDraftLineInput{{Description: "Item", Quantity: 1, UnitPricePaise: 10000}},
	})
	if err == nil || !strings.Contains(err.Error(), "only draft invoices") {
		t.Fatalf("err = %v, want non-draft rejection", err)
	}
}
