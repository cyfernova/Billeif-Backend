package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaxComplianceService_FetchGSTINFallback(t *testing.T) {
	svc := NewTaxComplianceService(nil, nil, nil, nil, nil, logger.New())

	result, err := svc.FetchGSTIN(context.Background(), "29ABCDE1234F1Z5")
	require.NoError(t, err)
	require.Equal(t, "ABCDE1234F", result.PAN)
	require.Equal(t, "local_fallback", result.Source)
	require.True(t, result.IsValid)
}

func TestTaxComplianceService_BuildGSTR1Report(t *testing.T) {
	svc, db, businessID := newTaxComplianceTestService(t)
	ctx := context.Background()
	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)

	docs := []*models.Document{
		newTestDocument("sales-b2b", businessID, models.DocumentTypeSalesInvoice, "SI-0001", "29ABCDE1234F1Z5", "29", 1000, 180, 1180, "1001", "PCS"),
		newTestDocument("sales-b2cl", businessID, models.DocumentTypeSalesInvoice, "SI-0002", "", "27", 300000, 54000, 354000, "1002", "PCS"),
		newTestDocument("sales-b2cs", businessID, models.DocumentTypeSalesInvoice, "SI-0003", "", "29", 500, 90, 590, "1003", "PCS"),
		newTestDocument("credit-b2b", businessID, models.DocumentTypeCreditNote, "CN-0001", "29ABCDE1234F1Z5", "29", 200, 36, 236, "1001", "PCS"),
		newTestDocument("credit-cdnur", businessID, models.DocumentTypeCreditNote, "CN-0002", "", "27", 100, 18, 118, "1002", "PCS"),
		newTestDocument("export", businessID, models.DocumentTypeSalesInvoice, "EXPORT-000000000001", "", "96", 1000, 0, 1000, "INVALID", "BAD"),
	}
	docs[5].ExportType = "lut"
	docs[5].GSTTreatment = models.DocumentGSTTreatmentExportLUT

	for _, doc := range docs {
		insertTestDocument(t, db, doc)
	}

	var loaded models.Document
	require.NoError(t, db.Preload("Lines").Where("id = ?", docs[0].ID).First(&loaded).Error)
	require.Len(t, loaded.Lines, 1)

	report, err := svc.buildGSTR1Report(ctx, businessID, GSTReportOptions{
		PeriodStart: periodStart, PeriodEnd: periodEnd, FilingFrequency: "monthly",
	}, false)
	require.NoError(t, err)

	sections := nestedMap(report, "sections")
	require.Len(t, sections["b2b"], 1)
	require.Len(t, sections["b2cl"], 1)
	require.Len(t, sections["cdnr"], 1)
	require.Len(t, sections["cdnur"], 1)
	require.Len(t, sections["exports"], 1)
	require.Len(t, sections["b2cs"], 1)
	require.Len(t, sections["hsn_summary"], 3)

	warnings := strings.Join(interfaceSliceToStrings(report["warnings"]), " | ")
	require.Contains(t, warnings, "invalid HSN")
	require.Contains(t, warnings, "16 characters")

	quarterly, err := svc.buildGSTR1Report(ctx, businessID, GSTReportOptions{
		PeriodStart: periodStart, PeriodEnd: periodEnd, FilingFrequency: "quarterly",
	}, true)
	require.NoError(t, err)
	qSections := nestedMap(quarterly, "sections")
	require.NotNil(t, qSections["b2b"])
	require.NotNil(t, qSections["cdnr"])
	require.Nil(t, qSections["b2cs"])
	require.Contains(t, strings.Join(interfaceSliceToStrings(quarterly["warnings"]), " | "), "Quarterly JSON export includes only B2B and CDNR")
}

func TestTaxComplianceService_ImportGSTR2B(t *testing.T) {
	svc, db, businessID := newTaxComplianceTestService(t)
	ctx := context.Background()
	periodStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)

	bookDoc := newTestDocument("purchase-match", businessID, models.DocumentTypePurchaseInvoice, "PI-0001", "29VENDOR1234F1Z5", "29", 1000, 180, 1180, "3001", "PCS")
	expenseDoc := newTestDocument("expense-missing", businessID, models.DocumentTypeExpense, "EX-0001", "29EXPNS1234F1Z5", "29", 500, 90, 590, "3002", "PCS")
	insertTestDocument(t, db, bookDoc)
	insertTestDocument(t, db, expenseDoc)

	imported, results, err := svc.ImportGSTR2B(ctx, businessID, ImportGSTR2BInput{
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Source:      "api",
		Lines: []GSTR2BImportLineInput{
			{SupplierGSTIN: "29VENDOR1234F1Z5", DocumentNumber: "PI-0001", TaxableAmount: 1000, TaxAmount: 180},
			{SupplierGSTIN: "29MISSING1234F1Z5", DocumentNumber: "PI-404", TaxableAmount: 100, TaxAmount: 18},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, imported.ID)
	require.Len(t, results, 3)

	statuses := map[string]int{}
	for _, item := range results {
		statuses[item.Status]++
	}
	require.Equal(t, 1, statuses[models.GSTR2BMatchStatusMatched])
	require.Equal(t, 1, statuses[models.GSTR2BMatchStatusMissingInBooks])
	require.Equal(t, 1, statuses[models.GSTR2BMatchStatusMissingInPortal])
}

func newTaxComplianceTestService(t *testing.T) (*TaxComplianceService, *gorm.DB, string) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	createTaxComplianceTestSchema(t, db)

	business := &models.BusinessProfile{
		ID:                 "biz-1",
		OwnerID:            "owner-1",
		Name:               "Test Biz",
		Email:              "biz@example.com",
		Currency:           "INR",
		GSTFilingFrequency: "monthly",
		BusinessStateCode:  "29",
		CompositionEnabled: true,
	}
	insertTestBusiness(t, db, business)

	var businessRepo interfaces.BusinessRepository = postgresrepo.NewBusinessRepository(db)
	var customerRepo interfaces.CustomerRepository = postgresrepo.NewCustomerRepository(db)
	var vendorRepo interfaces.VendorRepository = postgresrepo.NewVendorRepository(db)

	return NewTaxComplianceService(nil, db, businessRepo, customerRepo, vendorRepo, logger.New()), db, business.ID
}

func createTaxComplianceTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()

	statements := []string{
		`CREATE TABLE business_profiles (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			name TEXT NOT NULL,
			email TEXT NOT NULL,
			currency TEXT NOT NULL,
			gst_filing_frequency TEXT,
			business_state_code TEXT,
			composition_enabled NUMERIC,
			deleted_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE documents (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			document_type TEXT NOT NULL,
			party_type TEXT NOT NULL,
			status TEXT NOT NULL,
			draft_state TEXT NOT NULL,
			tax_mode TEXT NOT NULL,
			gst_treatment TEXT NOT NULL,
			place_of_supply TEXT,
			party_gstin TEXT,
			serial_number TEXT NOT NULL,
			issue_date DATETIME NOT NULL,
			currency TEXT NOT NULL,
			locale TEXT NOT NULL,
			subtotal REAL,
			tax_total REAL,
			cess_total REAL,
			total REAL,
			balance_due REAL,
			bill_of_supply NUMERIC,
			export_type TEXT,
			supply_type TEXT,
			deleted_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE document_lines (
			id TEXT PRIMARY KEY,
			document_id TEXT NOT NULL,
			description TEXT NOT NULL,
			hsnsac_code TEXT,
			uqc_code TEXT,
			quantity REAL,
			line_subtotal REAL,
			tax_amount REAL,
			cess_amount REAL,
			line_total REAL,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE gst_report_runs (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			report_type TEXT NOT NULL,
			period_start DATETIME NOT NULL,
			period_end DATETIME NOT NULL,
			filing_frequency TEXT,
			export_format TEXT,
			status TEXT,
			warnings TEXT,
			payload TEXT,
			created_by TEXT,
			deleted_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE gstr2b_imports (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			period_start DATETIME NOT NULL,
			period_end DATETIME NOT NULL,
			source TEXT NOT NULL,
			status TEXT,
			notes TEXT,
			raw_payload TEXT,
			deleted_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE gstr2b_import_lines (
			id TEXT PRIMARY KEY,
			import_id TEXT NOT NULL,
			business_id TEXT NOT NULL,
			supplier_gstin TEXT,
			supplier_name TEXT,
			document_number TEXT,
			document_date DATETIME,
			document_type TEXT,
			taxable_amount REAL,
			tax_amount REAL,
			cgst_amount REAL,
			sgst_amount REAL,
			igst_amount REAL,
			cess_amount REAL,
			place_of_supply TEXT,
			raw_payload TEXT,
			deleted_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE gstr2b_match_results (
			id TEXT PRIMARY KEY,
			import_id TEXT NOT NULL,
			business_id TEXT NOT NULL,
			import_line_id TEXT,
			document_id TEXT,
			status TEXT NOT NULL,
			mismatch_reason TEXT,
			books_taxable_amount REAL,
			import_taxable_amount REAL,
			books_tax_amount REAL,
			import_tax_amount REAL,
			metadata TEXT,
			deleted_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
	}

	for _, stmt := range statements {
		require.NoError(t, db.Exec(stmt).Error)
	}
}

func insertTestBusiness(t *testing.T, db *gorm.DB, business *models.BusinessProfile) {
	t.Helper()

	now := time.Now().UTC()
	require.NoError(t, db.Table("business_profiles").Create(map[string]interface{}{
		"id":                   business.ID,
		"owner_id":             business.OwnerID,
		"name":                 business.Name,
		"email":                business.Email,
		"currency":             business.Currency,
		"gst_filing_frequency": business.GSTFilingFrequency,
		"business_state_code":  business.BusinessStateCode,
		"composition_enabled":  business.CompositionEnabled,
		"created_at":           now,
		"updated_at":           now,
	}).Error)
}

func insertTestDocument(t *testing.T, db *gorm.DB, doc *models.Document) {
	t.Helper()

	require.NoError(t, db.Table("documents").Create(map[string]interface{}{
		"id":              doc.ID,
		"business_id":     doc.BusinessID,
		"document_type":   doc.DocumentType,
		"party_type":      doc.PartyType,
		"status":          doc.Status,
		"draft_state":     doc.DraftState,
		"tax_mode":        doc.TaxMode,
		"gst_treatment":   doc.GSTTreatment,
		"place_of_supply": doc.PlaceOfSupply,
		"party_gstin":     doc.PartyGSTIN,
		"serial_number":   doc.SerialNumber,
		"issue_date":      doc.IssueDate,
		"currency":        doc.Currency,
		"locale":          doc.Locale,
		"subtotal":        doc.Subtotal,
		"tax_total":       doc.TaxTotal,
		"cess_total":      doc.CessTotal,
		"total":           doc.Total,
		"balance_due":     doc.BalanceDue,
		"bill_of_supply":  doc.BillOfSupply,
		"export_type":     doc.ExportType,
		"supply_type":     doc.SupplyType,
		"created_at":      doc.IssueDate,
		"updated_at":      doc.IssueDate,
	}).Error)

	for _, line := range doc.Lines {
		require.NoError(t, db.Table("document_lines").Create(map[string]interface{}{
			"id":            line.ID,
			"document_id":   doc.ID,
			"description":   line.Description,
			"hsnsac_code":   line.HSNSACCode,
			"uqc_code":      line.UQCCode,
			"quantity":      line.Quantity,
			"line_subtotal": line.LineSubtotal,
			"tax_amount":    line.TaxAmount,
			"cess_amount":   line.CessAmount,
			"line_total":    line.LineTotal,
			"created_at":    doc.IssueDate,
			"updated_at":    doc.IssueDate,
		}).Error)
	}
}

func newTestDocument(id, businessID, documentType, serial, partyGSTIN, pos string, subtotal, tax, total float64, hsn, uqc string) *models.Document {
	now := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	return &models.Document{
		ID:            id,
		BusinessID:    businessID,
		DocumentType:  documentType,
		PartyType:     models.DocumentPartyTypeVendor,
		Status:        models.DocumentStatusIssued,
		DraftState:    models.DocumentDraftStateFinal,
		TaxMode:       models.DocumentTaxModeGST,
		GSTTreatment:  models.DocumentGSTTreatmentRegular,
		PlaceOfSupply: pos,
		PartyGSTIN:    partyGSTIN,
		SerialNumber:  serial,
		IssueDate:     now,
		Currency:      "INR",
		Locale:        "en-IN",
		Subtotal:      subtotal,
		TaxTotal:      tax,
		Total:         total,
		BalanceDue:    total,
		Lines: []*models.DocumentLine{
			{
				ID:           id + "-line-1",
				Description:  "Item",
				HSNSACCode:   hsn,
				UQCCode:      uqc,
				Quantity:     1,
				LineSubtotal: subtotal,
				TaxAmount:    tax,
				LineTotal:    total,
			},
		},
	}
}

func interfaceSliceToStrings(value interface{}) []string {
	items, ok := value.([]string)
	if ok {
		return items
	}
	raw, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if typed, ok := item.(string); ok {
			result = append(result, typed)
		}
	}
	return result
}
