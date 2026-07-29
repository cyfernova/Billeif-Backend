package services

import (
	"context"
	"testing"

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
