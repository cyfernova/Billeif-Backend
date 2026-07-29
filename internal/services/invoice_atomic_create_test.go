package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
)

type atomicInvoiceRepositoryFake struct {
	mu         sync.Mutex
	entries    map[string]atomicInvoiceEntry
	executions int
	last       interfaces.AtomicInvoiceDraft
	err        error
}

type atomicInvoiceEntry struct {
	hash    string
	invoice *models.Invoice
}

func (r *atomicInvoiceRepositoryFake) CreateDraftAtomic(ctx context.Context, command interfaces.AtomicInvoiceDraft) (*interfaces.AtomicInvoiceDraftResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	scope := command.BusinessID + "/" + command.Command + "/" + command.IdempotencyKey
	if existing, ok := r.entries[scope]; ok {
		if existing.hash != command.RequestHash {
			return nil, &idempotency.ConflictError{}
		}
		return &interfaces.AtomicInvoiceDraftResult{Invoice: cloneInvoiceForAtomicTest(existing.invoice), Replayed: true}, nil
	}
	r.executions++
	r.last = command
	r.entries[scope] = atomicInvoiceEntry{hash: command.RequestHash, invoice: cloneInvoiceForAtomicTest(command.Invoice)}
	return &interfaces.AtomicInvoiceDraftResult{Invoice: command.Invoice}, nil
}

func (r *atomicInvoiceRepositoryFake) Create(ctx context.Context, invoice *models.Invoice) error {
	return errors.New("legacy create must not be called")
}
func (r *atomicInvoiceRepositoryFake) GetByID(ctx context.Context, id, businessID string) (*models.Invoice, error) {
	return nil, errors.New("not implemented")
}
func (r *atomicInvoiceRepositoryFake) GetByIDInternal(ctx context.Context, id string) (*models.Invoice, error) {
	return nil, errors.New("not implemented")
}
func (r *atomicInvoiceRepositoryFake) GetByInvoiceNo(ctx context.Context, businessID, invoiceNo string) (*models.Invoice, error) {
	return nil, errors.New("not implemented")
}
func (r *atomicInvoiceRepositoryFake) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error) {
	return nil, 0, errors.New("not implemented")
}
func (r *atomicInvoiceRepositoryFake) GetItems(ctx context.Context, invoiceID string) ([]*models.InvoiceItem, error) {
	return nil, errors.New("not implemented")
}
func (r *atomicInvoiceRepositoryFake) Update(ctx context.Context, invoice *models.Invoice) error {
	return errors.New("not implemented")
}
func (r *atomicInvoiceRepositoryFake) UpdateStatus(ctx context.Context, invoiceID string, status string) error {
	return errors.New("not implemented")
}
func (r *atomicInvoiceRepositoryFake) UpdatePDFURL(ctx context.Context, invoiceID, pdfURL string) error {
	return errors.New("not implemented")
}
func (r *atomicInvoiceRepositoryFake) Delete(ctx context.Context, id string) error {
	return errors.New("not implemented")
}

type atomicBusinessRepositoryFake struct {
	business *models.BusinessProfile
}

func (r atomicBusinessRepositoryFake) Create(context.Context, *models.BusinessProfile) error {
	return nil
}
func (r atomicBusinessRepositoryFake) GetByID(ctx context.Context, id string) (*models.BusinessProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.business == nil || r.business.ID != id {
		return nil, errors.New("business not found")
	}
	return r.business, nil
}
func (r atomicBusinessRepositoryFake) Update(context.Context, *models.BusinessProfile) error {
	return nil
}
func (r atomicBusinessRepositoryFake) Delete(context.Context, string) error { return nil }
func (r atomicBusinessRepositoryFake) List(context.Context, string, int, int) ([]*models.BusinessProfile, int64, error) {
	return nil, 0, nil
}

type atomicCustomerRepositoryFake struct {
	customer *models.Customer
}

func (r atomicCustomerRepositoryFake) Create(context.Context, *models.Customer) error { return nil }
func (r atomicCustomerRepositoryFake) GetByID(ctx context.Context, id, businessID string) (*models.Customer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.customer == nil || r.customer.ID != id || r.customer.BusinessID != businessID {
		return nil, errors.New("customer not found")
	}
	return r.customer, nil
}
func (r atomicCustomerRepositoryFake) GetByBusinessID(context.Context, string, int, int) ([]*models.Customer, int64, error) {
	return nil, 0, nil
}
func (r atomicCustomerRepositoryFake) Update(context.Context, *models.Customer) error { return nil }
func (r atomicCustomerRepositoryFake) Delete(context.Context, string) error           { return nil }

type atomicProductRepositoryFake struct{}

func (atomicProductRepositoryFake) Create(context.Context, *models.Product) error { return nil }
func (atomicProductRepositoryFake) GetByID(context.Context, string, string) (*models.Product, error) {
	return nil, errors.New("product not found")
}
func (atomicProductRepositoryFake) GetByIDWithoutTenant(context.Context, string) (*models.Product, error) {
	return nil, errors.New("product not found")
}
func (atomicProductRepositoryFake) GetByBusinessID(context.Context, string, int, int) ([]*models.Product, int64, error) {
	return nil, 0, nil
}
func (atomicProductRepositoryFake) GetBySKU(context.Context, string, string) (*models.Product, error) {
	return nil, errors.New("product not found")
}
func (atomicProductRepositoryFake) Update(context.Context, *models.Product) error { return nil }
func (atomicProductRepositoryFake) Delete(context.Context, string) error          { return nil }
func (atomicProductRepositoryFake) AdjustStock(context.Context, string, int64) error {
	return nil
}

func TestInvoiceServiceCreateRequiresUUIDIdempotencyKey(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	input.IdempotencyKey = ""

	invoice, err := service.Create(ctx, input)

	if invoice != nil {
		t.Fatalf("invoice = %#v, want nil", invoice)
	}
	var invalid *idempotency.InvalidKeyError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %T %v, want *idempotency.InvalidKeyError", err, err)
	}
	if repo.executions != 0 {
		t.Fatalf("atomic executions = %d, want 0", repo.executions)
	}
}

func TestInvoiceServiceCreateFailsClosedWithoutAtomicRepository(t *testing.T) {
	service, _, input, ctx := newAtomicInvoiceServiceFixture(t)
	service.repo = nil

	invoice, err := service.Create(ctx, input)

	if invoice != nil || err == nil {
		t.Fatalf("invoice/error = %#v/%v, want nil/error", invoice, err)
	}
	if err.Error() != "canonical invoice repository is not configured" {
		t.Fatalf("error = %q, want fail-closed repository error", err)
	}
}

func TestInvoiceServiceCreatePersistsCanonicalDraftProjectionAtomically(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)

	invoice, err := service.Create(ctx, input)

	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	if invoice.ID == "" || invoice.InvoiceNo != nil || invoice.IssuedAt != nil || invoice.Version != 1 || invoice.Status != models.InvoiceStatusDraft {
		t.Fatalf("invalid draft lifecycle: %#v", invoice)
	}
	if invoice.SellerSnapshot.Name != "Acme Seller" || invoice.BuyerSnapshot.Name != "Buyer Ltd" {
		t.Fatalf("legal party snapshots = seller %#v buyer %#v", invoice.SellerSnapshot, invoice.BuyerSnapshot)
	}
	command := repo.last
	if command.Invoice != invoice {
		t.Fatal("atomic command did not persist the production invoice instance")
	}
	document := command.Document
	if document == nil || document.ID != invoice.ID {
		t.Fatalf("document projection = %#v, want same ID %s", document, invoice.ID)
	}
	if document.BusinessID != invoice.BusinessID ||
		models.StringValue(document.PartyID) != models.StringValue(invoice.CustomerID) ||
		document.Currency != invoice.Currency ||
		document.Subtotal != invoice.Subtotal ||
		document.TaxTotal+document.CessTotal != invoice.Tax ||
		document.Total != invoice.Total ||
		document.BalanceDue != invoice.BalanceDue {
		t.Fatalf("invoice/document facts diverged:\ninvoice=%#v\ndocument=%#v", invoice, document)
	}
	sourceLinkage := unmarshalJSONMap(document.SourceLinkage)
	if sourceLinkage["source_invoice_id"] != invoice.ID ||
		sourceLinkage["invoice_origin"] != string(invoice.Origin) ||
		sourceLinkage["seller_snapshot"] == nil ||
		sourceLinkage["buyer_snapshot"] == nil {
		t.Fatalf("document source/legal snapshots = %#v", sourceLinkage)
	}
	if len(invoice.Items) != 1 || len(document.Lines) != 1 {
		t.Fatalf("line counts invoice=%d document=%d, want 1/1", len(invoice.Items), len(document.Lines))
	}
	item, line := invoice.Items[0], document.Lines[0]
	if line.ID != item.ID || line.DocumentID != invoice.ID ||
		line.Description != item.Description || line.HSNSACCode != item.HSNSACCode ||
		line.Unit != item.Unit || line.Quantity != item.Quantity ||
		line.UnitPrice != item.UnitPrice || line.TaxRate != item.TaxRate ||
		line.CessRate != item.CessRate || line.LineTotal != item.Total {
		t.Fatalf("invoice/document line facts diverged:\nitem=%#v\nline=%#v", item, line)
	}
	if command.Activity == nil || command.Activity.EntityID != invoice.ID || command.Activity.Action != "created" {
		t.Fatalf("activity = %#v, want canonical create activity", command.Activity)
	}
	if len(command.OutboxEvents) != 0 || len(command.RenderJobs) != 0 || len(command.EmailDeliveries) != 0 {
		t.Fatalf("plain draft created optional rows: outbox=%d render=%d email=%d",
			len(command.OutboxEvents), len(command.RenderJobs), len(command.EmailDeliveries))
	}
}

func TestInvoiceServiceCreateProjectsGSTAndCessAsDistinctTaxComponents(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	input.TaxProfile.PlaceOfSupply = "27"

	invoice, err := service.Create(ctx, input)
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	document := repo.last.Document
	if document == nil || len(document.Lines) != 1 {
		t.Fatalf("document projection = %#v, want one projected line", document)
	}
	line := document.Lines[0]

	const (
		wantGST  = 360.0
		wantCess = 20.0
	)
	if document.TaxTotal != wantGST || document.CessTotal != wantCess {
		t.Fatalf("document tax/cess = %.2f/%.2f, want %.2f/%.2f",
			document.TaxTotal, document.CessTotal, wantGST, wantCess)
	}
	if document.Subtotal+document.TaxTotal+document.CessTotal != document.Total {
		t.Fatalf("document components do not reconcile: subtotal %.2f + tax %.2f + cess %.2f != total %.2f",
			document.Subtotal, document.TaxTotal, document.CessTotal, document.Total)
	}
	if line.TaxAmount != wantGST ||
		line.CGSTAmount != wantGST/2 ||
		line.SGSTAmount != wantGST/2 ||
		line.IGSTAmount != 0 ||
		line.CGSTRate != 9 ||
		line.SGSTRate != 9 ||
		line.IGSTRate != 0 {
		t.Fatalf("projected GST components = %#v, want intra-state 9%% CGST + 9%% SGST", line)
	}
	if invoice.Tax != wantGST+wantCess {
		t.Fatalf("legacy invoice tax total = %.2f, want %.2f", invoice.Tax, wantGST+wantCess)
	}
}

func TestInvoiceServiceCreateReplaysSameKeyAndConflictsOnChangedPayload(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	first, err := service.Create(ctx, input)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	replay, err := service.Create(ctx, input)
	if err != nil {
		t.Fatalf("replay create: %v", err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay invoice id = %s, want %s", replay.ID, first.ID)
	}
	if repo.executions != 1 {
		t.Fatalf("atomic executions = %d, want 1", repo.executions)
	}

	changed := input
	changed.Notes = "changed payload"
	conflictInvoice, err := service.Create(ctx, changed)
	if conflictInvoice != nil {
		t.Fatalf("conflict invoice = %#v, want nil", conflictInvoice)
	}
	var conflict *idempotency.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %T %v, want *idempotency.ConflictError", err, err)
	}
	if repo.executions != 1 {
		t.Fatalf("atomic executions after conflict = %d, want 1", repo.executions)
	}
}

func TestInvoiceServiceCreateHashIncludesTrustedSubscriptionOrigin(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	input.OriginSubscriptionID = uuid.NewString()
	input.OriginRunID = uuid.NewString()

	if _, err := service.Create(ctx, input); err != nil {
		t.Fatalf("first create: %v", err)
	}
	changed := input
	changed.OriginRunID = uuid.NewString()

	invoice, err := service.Create(ctx, changed)
	if invoice != nil {
		t.Fatalf("changed-origin invoice = %#v, want nil", invoice)
	}
	var conflict *idempotency.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("changed-origin error = %T %v, want *idempotency.ConflictError", err, err)
	}
	if repo.executions != 1 {
		t.Fatalf("atomic executions = %d, want 1", repo.executions)
	}
}

func TestInvoiceServiceCreateConcurrentSameKeyExecutesAtomicCommandOnce(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	const contenders = 16
	results := make(chan *models.Invoice, contenders)
	errs := make(chan error, contenders)
	var group sync.WaitGroup
	for index := 0; index < contenders; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			invoice, err := service.Create(ctx, input)
			results <- invoice
			errs <- err
		}()
	}
	group.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent create: %v", err)
		}
	}
	var invoiceID string
	for invoice := range results {
		if invoice == nil {
			t.Fatal("concurrent create returned nil invoice")
		}
		if invoiceID == "" {
			invoiceID = invoice.ID
		}
		if invoice.ID != invoiceID {
			t.Fatalf("concurrent invoice id = %s, want %s", invoice.ID, invoiceID)
		}
	}
	if repo.executions != 1 {
		t.Fatalf("atomic executions = %d, want 1", repo.executions)
	}
}

type canonicalInvoiceCreatorFake struct {
	calls int
	input CreateInvoiceInput
}

func (f *canonicalInvoiceCreatorFake) CreateByBusiness(_ context.Context, businessID string, input CreateInvoiceInput) (*models.Invoice, error) {
	f.calls++
	f.input = input
	return &models.Invoice{
		ID:          uuid.NewString(),
		BusinessID:  businessID,
		CustomerID:  models.StringPointer(input.CustomerID),
		Status:      models.InvoiceStatusDraft,
		Origin:      models.InvoiceOriginManual,
		Version:     1,
		InvoiceDate: input.InvoiceDate,
		DueDate:     input.DueDate,
		Currency:    "INR",
		Subtotal:    100,
		Total:       100,
		BalanceDue:  100,
		Items: []*models.InvoiceItem{{
			ID:          uuid.NewString(),
			Description: input.Items[0].Description,
			Quantity:    input.Items[0].Quantity,
			UnitPrice:   input.Items[0].UnitPrice,
			Total:       100,
		}},
	}, nil
}

func TestDocumentServiceSalesInvoiceDelegationCallsCanonicalCreatorOnceWithoutRecursion(t *testing.T) {
	creator := &canonicalInvoiceCreatorFake{}
	service := &DocumentService{salesInvoices: newInvoiceSalesDocumentCreator(creator)}
	businessID := uuid.NewString()
	customerID := uuid.NewString()
	idempotencyKey := uuid.NewString()
	dueDate := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	document, err := service.CreateByType(context.Background(), businessID, models.DocumentTypeSalesInvoice, CreateDocumentInput{
		IdempotencyKey: idempotencyKey,
		PartyID:        customerID,
		PartyType:      models.DocumentPartyTypeCustomer,
		IssueDate:      time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		DueDate:        &dueDate,
		Notes:          "delegated",
		Lines: []CreateDocumentLineInput{{
			Description: "Canonical line",
			Quantity:    1,
			UnitPrice:   100,
		}},
	})

	if err != nil {
		t.Fatalf("create delegated sales document: %v", err)
	}
	if document == nil {
		t.Fatal("delegated document is nil")
	}
	if creator.calls != 1 {
		t.Fatalf("canonical creator calls = %d, want 1", creator.calls)
	}
	if creator.input.BusinessID != businessID ||
		creator.input.CustomerID != customerID ||
		creator.input.IdempotencyKey != idempotencyKey ||
		len(creator.input.Items) != 1 ||
		creator.input.Items[0].Description != "Canonical line" {
		t.Fatalf("delegated input = %#v", creator.input)
	}
}

func TestDocumentServiceSalesInvoiceDelegationRejectsFieldsCanonicalInvoiceCannotPreserve(t *testing.T) {
	dueDate := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	base := CreateDocumentInput{
		IdempotencyKey: uuid.NewString(),
		PartyID:        uuid.NewString(),
		PartyType:      models.DocumentPartyTypeCustomer,
		IssueDate:      time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		DueDate:        &dueDate,
		Lines: []CreateDocumentLineInput{{
			Description: "Canonical line",
			Quantity:    1,
			UnitPrice:   100,
		}},
	}
	fixtures := []struct {
		name   string
		mutate func(*CreateDocumentInput)
	}{
		{name: "branch", mutate: func(input *CreateDocumentInput) { input.BranchID = uuid.NewString() }},
		{name: "foreign currency", mutate: func(input *CreateDocumentInput) {
			input.Currency = "USD"
			input.ExchangeRate = 83
		}},
		{name: "line discount", mutate: func(input *CreateDocumentInput) {
			input.Lines[0].DiscountAmount = 10
		}},
		{name: "packing metadata", mutate: func(input *CreateDocumentInput) {
			input.Lines[0].PackingMetadata = map[string]interface{}{"box": "A"}
		}},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			creator := &canonicalInvoiceCreatorFake{}
			service := &DocumentService{salesInvoices: newInvoiceSalesDocumentCreator(creator)}
			input := base
			input.Lines = append([]CreateDocumentLineInput(nil), base.Lines...)
			fixture.mutate(&input)

			document, err := service.CreateByType(context.Background(), uuid.NewString(), models.DocumentTypeSalesInvoice, input)
			if err == nil || document != nil {
				t.Fatalf("document/error = %#v/%v, want nil/unsupported-field error", document, err)
			}
			if creator.calls != 0 {
				t.Fatalf("canonical creator calls = %d, want 0", creator.calls)
			}
		})
	}
}

func TestInvoiceDocumentProjectionPreservesSupportedEditorFields(t *testing.T) {
	customerID := uuid.NewString()
	invoice := &models.Invoice{
		ID:                uuid.NewString(),
		BusinessID:        uuid.NewString(),
		CustomerID:        &customerID,
		Status:            models.InvoiceStatusDraft,
		Origin:            models.InvoiceOriginManual,
		Version:           1,
		InvoiceDate:       time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		DueDate:           time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
		Currency:          "INR",
		CustomFields:      `{"reference":"A-100","terms_and_conditions":"Net 30"}`,
		AdditionalCharges: `[{"name":"Freight","amount":25}]`,
	}

	document := invoiceDocumentProjection(invoice)
	extraFields := unmarshalJSONMap(document.ExtraFields)
	customFields, _ := extraFields["custom_fields"].(map[string]interface{})
	additionalCharges, _ := extraFields["additional_charges"].([]interface{})

	if document.Terms != "Net 30" {
		t.Fatalf("document terms = %q, want Net 30", document.Terms)
	}
	if customFields["reference"] != "A-100" {
		t.Fatalf("document custom fields = %#v, want reference A-100", customFields)
	}
	if len(additionalCharges) != 1 {
		t.Fatalf("document additional charges = %#v, want one charge", additionalCharges)
	}
}

func newAtomicInvoiceServiceFixture(t *testing.T) (*InvoiceService, *atomicInvoiceRepositoryFake, CreateInvoiceInput, context.Context) {
	t.Helper()
	businessID := uuid.NewString()
	customerID := uuid.NewString()
	repo := &atomicInvoiceRepositoryFake{entries: make(map[string]atomicInvoiceEntry)}
	businessRepo := atomicBusinessRepositoryFake{business: &models.BusinessProfile{
		ID:         businessID,
		Name:       "Acme Seller",
		Email:      "seller@example.test",
		Address:    "Seller Street",
		State:      "Maharashtra",
		Country:    "India",
		PostalCode: "400001",
		GSTIN:      "27AAAAA0000A1Z5",
		Currency:   "INR",
	}}
	customerRepo := atomicCustomerRepositoryFake{customer: &models.Customer{
		ID:         customerID,
		BusinessID: businessID,
		Name:       "Buyer Ltd",
		Email:      "buyer@example.test",
		Address:    "Buyer Street",
		State:      "Maharashtra",
		Country:    "India",
		PostalCode: "400002",
		GSTIN:      "27BBBBB0000B1Z5",
		StateCode:  "27",
	}}
	service := NewInvoiceService(
		nil,
		nil,
		repo,
		businessRepo,
		atomicProductRepositoryFake{},
		customerRepo,
		nil,
		nil,
		nil,
		nil,
		logger.New(),
	)
	ctx := ContextWithActor(context.Background(), ActorContext{
		UserID:    uuid.NewString(),
		Role:      "accountant",
		RequestID: "request-atomic-create",
		IPAddress: "127.0.0.1",
	})
	input := CreateInvoiceInput{
		BusinessID:      businessID,
		CustomerID:      customerID,
		IdempotencyKey:  uuid.NewString(),
		InvoiceDate:     time.Time{},
		DueDate:         time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
		Notes:           "Atomic draft",
		PriceListID:     "",
		RenderProfileID: "",
		TaxProfile: TaxProfileInput{
			GSTTreatment:          models.DocumentGSTTreatmentRegular,
			PlaceOfSupply:         "Maharashtra",
			CounterpartyGSTIN:     "27BBBBB0000B1Z5",
			CounterpartyStateCode: "27",
			GenerateEInvoice:      false,
			GenerateEWayBill:      false,
			ReverseCharge:         false,
			BillOfSupply:          false,
			ReportTags:            map[string]interface{}{"channel": "test"},
			SourceLinkage:         map[string]interface{}{"origin": "manual"},
			DispatchFrom:          map[string]interface{}{"city": "Mumbai"},
			DispatchTo:            map[string]interface{}{"city": "Pune"},
			MultiVehiclePlan:      map[string]interface{}{},
			Transporter:           map[string]interface{}{},
			Vehicle:               map[string]interface{}{},
			ReverseChargeReason:   "",
		},
		Items: []CreateInvoiceItemInput{{
			Description:  "Consulting",
			HSNSACCode:   "9983",
			Unit:         "HRS",
			Quantity:     2,
			UnitPrice:    1000,
			TaxRate:      18,
			CessRate:     1,
			CustomFields: map[string]interface{}{"legal": "snapshot"},
		}},
	}
	return service, repo, input, ctx
}

func cloneInvoiceForAtomicTest(invoice *models.Invoice) *models.Invoice {
	if invoice == nil {
		return nil
	}
	cloned := *invoice
	cloned.Items = make([]*models.InvoiceItem, len(invoice.Items))
	for index, item := range invoice.Items {
		copied := *item
		cloned.Items[index] = &copied
	}
	return &cloned
}
