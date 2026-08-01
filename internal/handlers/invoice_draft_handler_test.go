package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const draftUpdateRequestBody = `{"version":1,"items":[{"description":"Updated item","quantity":1,"unit_price_paise":10000}]}`

func TestInvoiceDraftHandlerRejectsInvalidIfMatchBeforeServiceWork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	handler := NewInvoiceHandler(services.NewInvoiceService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, logger.New()), nil, logger.New())
	router := draftUpdateRouter(handler, businessID)

	for _, fixture := range []struct {
		name    string
		ifMatch string
	}{
		{name: "missing"},
		{name: "empty", ifMatch: " "},
		{name: "non numeric", ifMatch: `"wat"`},
		{name: "zero", ifMatch: "0"},
		{name: "negative", ifMatch: "-1"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPatch, "/api/v1/invoices/"+invoiceID+"/draft", bytes.NewBufferString(draftUpdateRequestBody))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("If-Match", fixture.ifMatch)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
			if got, want := response.Body.String(), `{"error":"If-Match expected invoice version is required"}`; got != want {
				t.Fatalf("body = %s, want %s", got, want)
			}
		})
	}
}

func TestInvoiceDraftHandlerUsesIfMatchVersionAtProductionServiceBoundary(t *testing.T) {
	for _, ifMatch := range []string{"2", `"2"`} {
		t.Run(ifMatch, func(t *testing.T) {
			router, db, businessID, invoiceID := newInvoiceDraftHandler(t, 2)
			request := httptest.NewRequest(http.MethodPatch, "/api/v1/invoices/"+invoiceID+"/draft", bytes.NewBufferString(draftUpdateRequestBody))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("If-Match", ifMatch)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
			}
			var invoice models.Invoice
			if err := db.Where("id = ? AND business_id = ?", invoiceID, businessID).First(&invoice).Error; err != nil {
				t.Fatalf("load invoice: %v", err)
			}
			if invoice.Version != 3 {
				t.Fatalf("stored version = %d, want 3", invoice.Version)
			}
		})
	}
}

func TestInvoiceDraftHandlerReturnsConflictForStaleIfMatch(t *testing.T) {
	router, _, _, invoiceID := newInvoiceDraftHandler(t, 2)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/invoices/"+invoiceID+"/draft", bytes.NewBufferString(draftUpdateRequestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"1"`)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body.String())
	}
}

func newInvoiceDraftHandler(t *testing.T, version int) (*gin.Engine, *gorm.DB, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	createInvoiceDraftHandlerSchema(t, db)
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	invoice := &models.Invoice{
		ID:          invoiceID,
		BusinessID:  businessID,
		Version:     version,
		Status:      models.InvoiceStatusDraft,
		Currency:    "INR",
		InvoiceDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		DueDate:     time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		Total:       100,
		BalanceDue:  100,
	}
	if err := db.Create(invoice).Error; err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	service := services.NewInvoiceService(db, nil, postgres.NewInvoiceRepository(db), nil, nil, nil, nil, nil, nil, nil, logger.New())
	handler := NewInvoiceHandler(service, nil, logger.New())
	return draftUpdateRouter(handler, businessID), db, businessID, invoiceID
}

func draftUpdateRouter(handler *InvoiceHandler, businessID string) *gin.Engine {
	router := gin.New()
	router.PATCH("/api/v1/invoices/:id/draft", func(c *gin.Context) {
		c.Set("business_id", businessID)
		c.Set("user_id", uuid.NewString())
		c.Set("role", "accountant")
		c.Set(middleware.RequestIDKey, "request-update-draft")
		handler.UpdateDraft(c)
	})
	return router
}

func createInvoiceDraftHandlerSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE invoices (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			customer_id TEXT,
			version INTEGER NOT NULL DEFAULT 1,
			project_id TEXT,
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
			id TEXT PRIMARY KEY,
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
			t.Fatalf("create invoice draft handler schema: %v", err)
		}
	}
}
