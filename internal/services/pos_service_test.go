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

func TestResolveTrustedPOSCheckoutCartRejectsClientLinesFallback(t *testing.T) {
	_, err := resolveTrustedPOSCheckoutCart(POSSessionCart{}, CheckoutPOSCartInput{
		Lines: []POSSessionCartLine{{
			ProductID: "product-1",
			Quantity:  1,
			UnitPrice: 1,
		}},
	})
	if err == nil {
		t.Fatal("expected client-supplied POS fallback lines to be rejected")
	}
}

func TestResolveTrustedPOSCheckoutCartUsesServerCart(t *testing.T) {
	cart, err := resolveTrustedPOSCheckoutCart(POSSessionCart{
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

func TestPOSCheckoutMapsActivePayloadToCanonicalUnnumberedDraft(t *testing.T) {
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
	documents := &DocumentService{salesInvoices: newInvoiceSalesDocumentCreator(creator)}
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
		NewEntitlementService(nil, db, nil, log),
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
	if document.Status != models.DocumentStatusDraft ||
		document.DraftState != models.DocumentDraftStateDraft ||
		document.SerialNumber != "" {
		t.Fatalf("POS canonical lifecycle = status %q state %q number %q, want unnumbered draft",
			document.Status, document.DraftState, document.SerialNumber)
	}
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
