package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResolveTrustedPOSCheckoutCartReloadsAuthoritativeClientCommand(t *testing.T) {
	service := newPOSCatalogCheckoutFixture(t)
	seedPOSCatalogProduct(t, service.db, "business-1", "warehouse-1", "product-1")

	cart, err := service.resolveTrustedPOSCheckoutCart(
		context.Background(),
		"business-1",
		"warehouse-1",
		POSSessionCart{},
		CheckoutPOSCartInput{Lines: []POSSessionCartLine{{
			ProductID:   "product-1",
			Name:        "Spoofed name",
			Description: "Spoofed description",
			Quantity:    2,
			UnitPrice:   0.01,
			TaxRate:     99,
			Metadata:    map[string]interface{}{"discount": 250},
		}}},
	)
	if err != nil {
		t.Fatalf("resolve trusted POS command: %v", err)
	}
	if len(cart.Items) != 1 {
		t.Fatalf("cart items = %d, want 1", len(cart.Items))
	}
	line := cart.Items[0]
	if line.Name != "Trusted product" || line.Description != "Trusted product" {
		t.Fatalf("catalog description was not authoritative: %#v", line)
	}
	if line.UnitPrice != 125 || line.TaxRate != 18 || line.Quantity != 2 {
		t.Fatalf("catalog money fields were not authoritative: %#v", line)
	}
	if len(line.Metadata) != 0 {
		t.Fatalf("client metadata reached trusted cart: %#v", line.Metadata)
	}
	if cart.Total != 295 {
		t.Fatalf("trusted cart total = %.2f, want 295.00", cart.Total)
	}
}

func TestResolveTrustedPOSCheckoutCartRejectsCrossTenantAndInvalidQuantity(t *testing.T) {
	service := newPOSCatalogCheckoutFixture(t)
	seedPOSCatalogProduct(t, service.db, "business-2", "warehouse-2", "product-other")

	for name, line := range map[string]POSSessionCartLine{
		"cross tenant product": {ProductID: "product-other", Quantity: 1},
		"zero quantity":        {ProductID: "product-other", Quantity: 0},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.resolveTrustedPOSCheckoutCart(
				context.Background(),
				"business-1",
				"warehouse-1",
				POSSessionCart{},
				CheckoutPOSCartInput{Lines: []POSSessionCartLine{line}},
			)
			if err == nil {
				t.Fatal("expected untrusted POS command to be rejected")
			}
		})
	}
}

func TestResolveTrustedPOSCheckoutCartUsesServerCart(t *testing.T) {
	cart, err := (&POSService{}).resolveTrustedPOSCheckoutCart(context.Background(), "business-1", "warehouse-1", POSSessionCart{
		Items: []POSSessionCartLine{{
			ProductID: "product-1",
			Quantity:  2,
			UnitPrice: 150,
			TaxRate:   10,
		}},
	}, CheckoutPOSCartInput{})
	if err != nil {
		t.Fatalf("expected server cart to be accepted: %v", err)
	}
	if cart.Total != 330 {
		t.Fatalf("expected server cart total to be recalculated, got %.2f", cart.Total)
	}
}

func newPOSCatalogCheckoutFixture(t *testing.T) *POSService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open POS catalog database: %v", err)
	}
	statements := []string{
		`CREATE TABLE products (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			name TEXT NOT NULL,
			sku TEXT NOT NULL,
			barcode TEXT,
			price REAL NOT NULL,
			mrp REAL DEFAULT 0,
			hsn_sac_code TEXT,
			uqc_code TEXT,
			gst_metadata TEXT DEFAULT '{}',
			default_cess_rate REAL DEFAULT 0,
			unit TEXT,
			is_active NUMERIC DEFAULT 1,
			deleted_at DATETIME
		)`,
		`CREATE TABLE product_variants (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			product_id TEXT NOT NULL,
			name TEXT NOT NULL,
			sku TEXT NOT NULL,
			barcode TEXT,
			attributes TEXT DEFAULT '{}',
			price REAL DEFAULT 0,
			mrp REAL DEFAULT 0,
			default_cess_rate REAL DEFAULT 0,
			is_active NUMERIC DEFAULT 1,
			deleted_at DATETIME
		)`,
		`CREATE TABLE product_warehouse_catalogs (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			product_id TEXT NOT NULL,
			warehouse_id TEXT NOT NULL,
			is_visible NUMERIC DEFAULT 1,
			is_active NUMERIC DEFAULT 1,
			price_override REAL,
			deleted_at DATETIME
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create POS catalog table: %v", err)
		}
	}
	return &POSService{db: db}
}

func seedPOSCatalogProduct(t *testing.T, db *gorm.DB, businessID, warehouseID, productID string) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO products (id, business_id, name, sku, price, mrp, hsn_sac_code, uqc_code, gst_metadata, default_cess_rate, unit, is_active)
		 VALUES (?, ?, 'Trusted product', 'TRUST-1', 125, 150, '0902', 'PCS', '{"tax_rate":18}', 0, 'PCS', 1)`,
		productID,
		businessID,
	).Error; err != nil {
		t.Fatalf("seed POS product: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO product_warehouse_catalogs (id, business_id, product_id, warehouse_id, is_visible, is_active)
		 VALUES (?, ?, ?, ?, 1, 1)`,
		uuid.NewString(),
		businessID,
		productID,
		warehouseID,
	).Error; err != nil {
		t.Fatalf("seed POS warehouse catalog: %v", err)
	}
}

func TestPOSCheckoutCreatesAndIssuesCanonicalInvoice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open POS test database: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE pos_sessions (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			pos_profile_id TEXT,
			status TEXT NOT NULL,
			session_name TEXT,
			warehouse_id TEXT,
			currency TEXT,
			cart_payload TEXT,
			last_checked_out_document_id TEXT,
			last_scanned_code TEXT,
			metadata TEXT,
			opened_at DATETIME,
			closed_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create POS session table: %v", err)
	}

	businessID := uuid.NewString()
	userID := uuid.NewString()
	sessionID := uuid.NewString()
	warehouseID := uuid.NewString()
	session := &models.POSSession{
		ID:          sessionID,
		BusinessID:  businessID,
		UserID:      userID,
		Status:      posSessionStatusOpen,
		WarehouseID: &warehouseID,
		Currency:    "INR",
		CartPayload: mustMarshalAny(POSSessionCart{Items: []POSSessionCartLine{{
			Description: "Counter item",
			Quantity:    1,
			UnitPrice:   100,
		}}}, "{}"),
		Metadata: "{}",
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("seed POS session: %v", err)
	}

	log := logger.New()
	creator := &canonicalInvoiceCreatorFake{}
	issuer := &posSalesDocumentIssuerFake{}
	documents := &DocumentService{
		salesInvoices:      newInvoiceSalesDocumentCreator(creator),
		salesInvoiceIssuer: issuer,
	}
	inventory := NewInventoryService(
		db,
		nil,
		nil,
		atomicBusinessRepositoryFake{business: &models.BusinessProfile{ID: businessID, OwnerID: userID}},
		nil,
		log,
	)
	service := NewPOSService(
		db,
		documents,
		nil,
		inventory,
		NewEntitlementService(nil, db, fixedSubscriptionRepository{subscription: &models.Subscription{
			BusinessID: businessID,
			Plan:       "enterprise",
			PlanCode:   "biz",
			Status:     "active",
		}}, log),
		log,
	)
	idempotencyKey := uuid.NewString()

	document, err := service.Checkout(context.Background(), businessID, userID, sessionID, idempotencyKey, CheckoutPOSCartInput{
		PartyType:    models.DocumentPartyTypeManual,
		Status:       models.DocumentStatusIssued,
		TaxMode:      models.DocumentTaxModeNonGST,
		GSTTreatment: models.DocumentGSTTreatmentRegular,
		Source:       "pos",
	})
	if err != nil {
		t.Fatalf("checkout active POS payload: %v", err)
	}
	if creator.calls != 1 {
		t.Fatalf("canonical creator calls = %d, want 1", creator.calls)
	}
	if issuer.calls != 1 || issuer.invoiceID != document.ID {
		t.Fatalf("canonical issuer calls/invoice = %d/%q, want 1/%q", issuer.calls, issuer.invoiceID, document.ID)
	}
	if issuer.input.IdempotencyKey != idempotencyKey || issuer.input.ExpectedVersion != 1 ||
		issuer.input.DocumentType != "bill_of_supply" || issuer.input.Series != "POS" {
		t.Fatalf("canonical POS issue command = %#v", issuer.input)
	}
	if creator.input.IdempotencyKey != idempotencyKey ||
		creator.input.Origin != models.InvoiceOriginPOS ||
		creator.input.Currency != "INR" ||
		creator.input.BuyerSnapshot.Name != "Counter sale" {
		t.Fatalf("canonical POS command = %#v", creator.input)
	}
	if creator.input.TaxProfile.GSTTreatment != models.DocumentGSTTreatmentExempt {
		t.Fatalf("canonical GST treatment = %q, want exempt for non-GST POS sale",
			creator.input.TaxProfile.GSTTreatment)
	}
	sourceLinkage := creator.input.TaxProfile.SourceLinkage
	if sourceLinkage["source"] != "pos" || sourceLinkage["pos_session"] != sessionID {
		t.Fatalf("canonical POS source linkage = %#v", sourceLinkage)
	}
	if document.Status != models.DocumentStatusIssued ||
		document.DraftState != models.DocumentDraftStateFinal ||
		document.SerialNumber != "POS/26-27/000001" {
		t.Fatalf("POS canonical lifecycle = status %q state %q number %q, want issued invoice",
			document.Status, document.DraftState, document.SerialNumber)
	}
}

type posSalesDocumentIssuerFake struct {
	calls     int
	invoiceID string
	input     IssueInvoiceInput
}

func (f *posSalesDocumentIssuerFake) IssueSalesInvoiceDocument(
	_ context.Context,
	businessID, invoiceID string,
	input IssueInvoiceInput,
) (*IssueInvoiceResult, error) {
	f.calls++
	f.invoiceID = invoiceID
	f.input = input
	issuedAt := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	invoiceNumber := "POS/26-27/000001"
	return &IssueInvoiceResult{
		Invoice: &models.Invoice{
			ID:          invoiceID,
			BusinessID:  businessID,
			Status:      models.InvoiceStatusIssued,
			Version:     2,
			InvoiceNo:   &invoiceNumber,
			IssuedAt:    &issuedAt,
			InvoiceDate: issuedAt,
			Currency:    "INR",
			TaxProfile:  `{"gst_treatment":"exempt","bill_of_supply":true}`,
		},
		FinalRender: &models.DocumentRenderJob{},
	}, nil
}

type fixedSubscriptionRepository struct {
	subscription *models.Subscription
}

func (r fixedSubscriptionRepository) Create(context.Context, *models.Subscription) error {
	return errors.New("not implemented")
}

func (r fixedSubscriptionRepository) GetByID(context.Context, string, string) (*models.Subscription, error) {
	return r.subscription, nil
}

func (r fixedSubscriptionRepository) GetByBusinessID(context.Context, string) (*models.Subscription, error) {
	return r.subscription, nil
}

func (r fixedSubscriptionRepository) Update(context.Context, *models.Subscription) error {
	return errors.New("not implemented")
}

func TestCanonicalPOSCheckoutInputReplaysSameSessionAndKey(t *testing.T) {
	service, repo, template, ctx := newAtomicInvoiceServiceFixture(t)
	session := &models.POSSession{ID: uuid.NewString(), Currency: "INR"}
	idempotencyKey := uuid.NewString()
	checkout := CheckoutPOSCartInput{
		PartyType: models.DocumentPartyTypeManual,
		TaxMode:   models.DocumentTaxModeNonGST,
	}
	items := []CreateInvoiceItemInput{{
		Description: "Stable counter item",
		Quantity:    1,
		UnitPrice:   100,
	}}

	firstInput, err := canonicalPOSCheckoutInput(session, idempotencyKey, checkout, items)
	if err != nil {
		t.Fatalf("map first POS checkout: %v", err)
	}
	first, err := service.CreateByBusiness(ctx, template.BusinessID, firstInput)
	if err != nil {
		t.Fatalf("create first POS checkout: %v", err)
	}

	time.Sleep(time.Millisecond)
	retryInput, err := canonicalPOSCheckoutInput(session, idempotencyKey, checkout, items)
	if err != nil {
		t.Fatalf("map retry POS checkout: %v", err)
	}
	replay, err := service.CreateByBusiness(ctx, template.BusinessID, retryInput)
	if err != nil {
		t.Fatalf("replay same POS checkout: %v", err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay invoice ID = %s, want %s", replay.ID, first.ID)
	}
	if repo.executions != 1 {
		t.Fatalf("atomic executions = %d, want 1", repo.executions)
	}
}

func TestCanonicalPOSCheckoutInputValidatesPartyTypeAndIDPairing(t *testing.T) {
	session := &models.POSSession{ID: uuid.NewString(), Currency: "INR"}
	customerID := uuid.NewString()
	fixtures := []struct {
		name      string
		partyType string
		partyID   string
		wantError bool
	}{
		{name: "anonymous manual", partyType: models.DocumentPartyTypeManual},
		{name: "anonymous manual with whitespace ID", partyType: models.DocumentPartyTypeManual, partyID: " \t "},
		{name: "anonymous default", partyType: ""},
		{name: "identified customer", partyType: models.DocumentPartyTypeCustomer, partyID: customerID},
		{name: "identified customer with padded ID", partyType: models.DocumentPartyTypeCustomer, partyID: " \t" + customerID + " "},
		{name: "customer with invalid ID", partyType: models.DocumentPartyTypeCustomer, partyID: "not-a-uuid", wantError: true},
		{name: "customer without ID", partyType: models.DocumentPartyTypeCustomer, wantError: true},
		{name: "manual with ID", partyType: models.DocumentPartyTypeManual, partyID: customerID, wantError: true},
		{name: "default manual with ID", partyType: "", partyID: customerID, wantError: true},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			input, err := canonicalPOSCheckoutInput(session, uuid.NewString(), CheckoutPOSCartInput{
				PartyType: fixture.partyType,
				PartyID:   fixture.partyID,
				TaxMode:   models.DocumentTaxModeNonGST,
			}, []CreateInvoiceItemInput{{Description: "Counter item", Quantity: 1, UnitPrice: 100}})

			if fixture.wantError {
				var invalid *idempotency.InvalidPayloadError
				if !errors.As(err, &invalid) {
					t.Fatalf("error = %T %v, want invalid payload", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("map valid party pairing: %v", err)
			}
			if fixture.partyType == models.DocumentPartyTypeCustomer {
				if input.CustomerID != customerID || !input.BuyerSnapshot.IsEmpty() {
					t.Fatalf("identified customer mapping = %#v", input)
				}
			} else if input.CustomerID != "" || input.BuyerSnapshot.Name != "Counter sale" {
				t.Fatalf("anonymous manual mapping = %#v", input)
			}
		})
	}
}

func TestCanonicalPOSCheckoutInputNormalizesCustomerIDBeforeLookupAndPersistence(t *testing.T) {
	service, repo, template, ctx := newAtomicInvoiceServiceFixture(t)
	session := &models.POSSession{ID: uuid.NewString(), Currency: "INR"}

	input, err := canonicalPOSCheckoutInput(session, uuid.NewString(), CheckoutPOSCartInput{
		PartyType: models.DocumentPartyTypeCustomer,
		PartyID:   " \t" + template.CustomerID + " ",
		TaxMode:   models.DocumentTaxModeNonGST,
	}, template.Items)
	if err != nil {
		t.Fatalf("map padded customer ID: %v", err)
	}
	invoice, err := service.CreateByBusiness(ctx, template.BusinessID, input)
	if err != nil {
		t.Fatalf("look up normalized customer ID: %v", err)
	}
	if input.CustomerID != template.CustomerID {
		t.Fatalf("mapped customer ID = %q, want %q", input.CustomerID, template.CustomerID)
	}
	if models.StringValue(invoice.CustomerID) != template.CustomerID {
		t.Fatalf("invoice customer ID = %q, want %q", models.StringValue(invoice.CustomerID), template.CustomerID)
	}
	if models.StringValue(repo.last.Invoice.CustomerID) != template.CustomerID {
		t.Fatalf("persisted customer ID = %q, want %q", models.StringValue(repo.last.Invoice.CustomerID), template.CustomerID)
	}
}
