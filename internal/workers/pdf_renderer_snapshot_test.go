package workers

import (
	"context"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestFrozenRenderPartiesPreferIssuedSourceLinkageSnapshots(t *testing.T) {
	document := &models.Document{
		ID:         "invoice-1",
		BusinessID: "business-1",
		PartyID:    models.StringPointer("live-customer-must-not-load"),
		SourceLinkage: `{
			"seller_snapshot":{
				"name":"Frozen Seller",
				"email":"seller@example.com",
				"phone":"111",
				"address":"Seller Street",
				"city":"Mumbai",
				"state":"Maharashtra",
				"postal_code":"400001",
				"country":"India",
				"gstin":"27AAAAA0000A1Z5"
			},
			"buyer_snapshot":{
				"name":"Frozen Buyer",
				"email":"buyer@example.com",
				"phone":"222",
				"address":"Buyer Street",
				"city":"Pune",
				"state":"Maharashtra",
				"postal_code":"411001",
				"country":"India",
				"gstin":"27BBBBB0000B1Z5"
			}
		}`,
	}

	business, party, ok := frozenRenderParties(document, labelsForLocale("en-IN"))

	if !ok || business == nil || business.Name != "Frozen Seller" ||
		business.GSTIN != "27AAAAA0000A1Z5" {
		t.Fatalf("frozen seller = %#v, ok=%v", business, ok)
	}
	if party.name != "Frozen Buyer" || party.email != "buyer@example.com" ||
		party.taxID != "27BBBBB0000B1Z5" {
		t.Fatalf("frozen buyer = %#v", party)
	}
	if len(party.address) != 3 ||
		party.address[0] != "Buyer Street" ||
		party.address[1] != "Pune, Maharashtra, 411001" ||
		party.address[2] != "India" {
		t.Fatalf("frozen buyer address = %#v", party.address)
	}
}

func TestFinalSnapshotRendererDoesNotQueryLiveCompliance(t *testing.T) {
	svc, queries := complianceCountingRenderContainer(t)
	document := frozenFinalRenderDocument()

	if _, _, err := renderFinalDocumentPDF(context.Background(), svc, document, nil); err != nil {
		t.Fatalf("render final snapshot: %v", err)
	}
	if queries.count != 0 {
		t.Fatalf("final snapshot render executed %d live compliance queries, want zero", queries.count)
	}
}

func TestPreviewAndGenericRendererStillQueryLiveCompliance(t *testing.T) {
	svc, queries := complianceCountingRenderContainer(t)
	document := frozenFinalRenderDocument()

	if _, _, err := renderDocumentPDF(context.Background(), svc, document, nil); err != nil {
		t.Fatalf("render live document: %v", err)
	}
	if queries.count != 2 {
		t.Fatalf("live document render executed %d compliance queries, want two", queries.count)
	}
}

type queryCountingGORMLogger struct {
	count int
}

func (l *queryCountingGORMLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface {
	return l
}

func (*queryCountingGORMLogger) Info(context.Context, string, ...interface{})  {}
func (*queryCountingGORMLogger) Warn(context.Context, string, ...interface{})  {}
func (*queryCountingGORMLogger) Error(context.Context, string, ...interface{}) {}

func (l *queryCountingGORMLogger) Trace(
	_ context.Context,
	_ time.Time,
	fc func() (string, int64),
	_ error,
) {
	l.count++
	_, _ = fc()
}

func complianceCountingRenderContainer(t *testing.T) (*services.Container, *queryCountingGORMLogger) {
	t.Helper()
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new SQL mock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	queries := &queryCountingGORMLogger{}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: queries})
	if err != nil {
		t.Fatalf("open GORM database: %v", err)
	}
	cfg := &config.Config{}
	log := logger.NewWithEnv("test")
	compliance := services.NewTaxComplianceService(
		cfg,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		log,
	)
	return &services.Container{TaxCompliance: compliance}, queries
}

func frozenFinalRenderDocument() *models.Document {
	return &models.Document{
		ID:               "final-invoice",
		BusinessID:       "final-business",
		DocumentType:     models.DocumentTypeSalesInvoice,
		Status:           models.DocumentStatusIssued,
		DraftState:       models.DocumentDraftStateFinal,
		SerialNumber:     "INV/26-27/000001",
		IssueDate:        time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC),
		Currency:         "INR",
		Locale:           "en-IN",
		SourceLinkage:    `{"seller_snapshot":{"name":"Frozen Seller"},"buyer_snapshot":{"name":"Frozen Buyer"}}`,
		Subtotal:         100,
		Total:            100,
		BalanceDue:       100,
		GenerateEInvoice: true,
		GenerateEWayBill: true,
		Lines: []*models.DocumentLine{{
			ID:           "final-line",
			DocumentID:   "final-invoice",
			Description:  "Frozen issued line",
			Quantity:     1,
			UnitPrice:    100,
			LineSubtotal: 100,
			LineTotal:    100,
		}},
	}
}
