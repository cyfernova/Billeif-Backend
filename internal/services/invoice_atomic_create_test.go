package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoicecursor"
	"invoice-backend/internal/invoiceresolution"
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

	replayChecks        int
	resolutionCalls     int
	lastResolution      invoiceresolution.Request
	resolutionSnapshots []invoiceresolution.LineSnapshot
	resolutionErr       error
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

func (r *atomicInvoiceRepositoryFake) ReplayCompletedDraft(
	ctx context.Context,
	businessID, command, idempotencyKey, requestHash string,
) (*interfaces.AtomicInvoiceDraftResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replayChecks++
	scope := businessID + "/" + command + "/" + idempotencyKey
	existing, ok := r.entries[scope]
	if !ok {
		return nil, nil
	}
	if existing.hash != requestHash {
		return nil, &idempotency.ConflictError{}
	}
	return &interfaces.AtomicInvoiceDraftResult{
		Invoice:  cloneInvoiceForAtomicTest(existing.invoice),
		Replayed: true,
	}, nil
}

func (r *atomicInvoiceRepositoryFake) ResolveInvoiceLines(
	ctx context.Context,
	request invoiceresolution.Request,
) ([]invoiceresolution.LineSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolutionCalls++
	r.lastResolution = request
	if r.resolutionErr != nil {
		return nil, r.resolutionErr
	}
	if r.resolutionSnapshots == nil {
		return make([]invoiceresolution.LineSnapshot, len(request.Lines)), nil
	}
	return append([]invoiceresolution.LineSnapshot(nil), r.resolutionSnapshots...), nil
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
func (r *atomicInvoiceRepositoryFake) ListByCursor(context.Context, string, *invoicecursor.Position, int) ([]*models.Invoice, bool, error) {
	return nil, false, errors.New("not implemented")
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
	if repo.replayChecks != 1 || repo.resolutionCalls != 1 || repo.executions != 1 {
		t.Fatalf(
			"first execution replay/resolution/atomic counts = %d/%d/%d, want 1/1/1",
			repo.replayChecks,
			repo.resolutionCalls,
			repo.executions,
		)
	}
	if invoice.SellerSnapshot.Name != "Acme Seller" || invoice.BuyerSnapshot.Name != "Buyer Ltd" {
		t.Fatalf("legal party snapshots = seller %#v buyer %#v", invoice.SellerSnapshot, invoice.BuyerSnapshot)
	}
	command := repo.last
	if command.RequestHash == "" {
		t.Fatal("first atomic execution did not carry the raw canonical request hash")
	}
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

func TestInvoiceServiceCreateReusesOneResolvedSnapshotForInvoiceAndDocument(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	productID := uuid.NewString()
	variantID := uuid.NewString()
	warehouseID := uuid.NewString()
	catalogueID := uuid.NewString()
	priceListID := uuid.NewString()
	input.PriceListID = priceListID
	input.Items[0].ProductID = productID
	input.Items[0].VariantID = variantID
	input.Items[0].WarehouseID = warehouseID
	input.Items[0].HSNSACCode = ""
	input.Items[0].Unit = ""
	input.Items[0].UnitPrice = 0
	input.Items[0].MRP = 0
	input.Items[0].CessRate = 0
	repo.resolutionSnapshots = []invoiceresolution.LineSnapshot{{
		ProductID: productID, VariantID: variantID, WarehouseID: warehouseID,
		CatalogueID: catalogueID, PriceListID: priceListID,
		ProductName: "Resolved product", SKU: "VAR-1", HSNSACCode: "1001",
		UQCCode: "KGS", Unit: "KGS", UnitPrice: 125, MRP: 140, CessRate: 2.5,
	}}

	invoice, err := service.Create(ctx, input)

	if err != nil {
		t.Fatalf("create resolved invoice: %v", err)
	}
	if repo.resolutionCalls != 1 {
		t.Fatalf("resolution calls = %d, want 1", repo.resolutionCalls)
	}
	if repo.lastResolution.BusinessID != input.BusinessID ||
		repo.lastResolution.PriceListID != priceListID ||
		len(repo.lastResolution.Lines) != 1 ||
		repo.lastResolution.Lines[0].ProductID != productID ||
		repo.lastResolution.Lines[0].VariantID != variantID ||
		repo.lastResolution.Lines[0].WarehouseID != warehouseID {
		t.Fatalf("resolution request = %#v", repo.lastResolution)
	}
	item := invoice.Items[0]
	line := repo.last.Document.Lines[0]
	if models.StringValue(item.ProductID) != productID ||
		models.StringValue(item.VariantID) != variantID ||
		models.StringValue(item.WarehouseID) != warehouseID ||
		item.SKU != "VAR-1" ||
		item.HSNSACCode != "1001" ||
		item.Unit != "KGS" ||
		item.UnitPrice != 125 ||
		item.MRP != 140 ||
		item.CessRate != 2.5 {
		t.Fatalf("resolved invoice item = %#v", item)
	}
	if models.StringValue(line.ProductID) != models.StringValue(item.ProductID) ||
		models.StringValue(line.VariantID) != models.StringValue(item.VariantID) ||
		models.StringValue(line.WarehouseID) != models.StringValue(item.WarehouseID) ||
		line.Description != item.Description ||
		line.HSNSACCode != item.HSNSACCode ||
		line.Unit != item.Unit ||
		line.UnitPrice != item.UnitPrice ||
		line.MRP != item.MRP ||
		line.CessRate != item.CessRate ||
		line.CessAmount != item.CessAmount ||
		line.LineTotal != item.Total {
		t.Fatalf("invoice/document resolved snapshots diverged:\nitem=%#v\nline=%#v", item, line)
	}
	itemFields := unmarshalJSONMap(item.CustomFields)
	lineFields := unmarshalJSONMap(line.CustomFields)
	itemProvenance, _ := itemFields["pricing_provenance"].(map[string]interface{})
	lineProvenance, _ := lineFields["pricing_provenance"].(map[string]interface{})
	if itemProvenance["catalogue_id"] != catalogueID ||
		itemProvenance["price_list_id"] != priceListID ||
		lineProvenance["catalogue_id"] != catalogueID ||
		lineProvenance["price_list_id"] != priceListID {
		t.Fatalf("invoice/document pricing provenance diverged: item=%#v line=%#v", itemFields, lineFields)
	}
}

func TestInvoiceServiceCreateUsesResolvedSKUInPersistedProjectionSnapshots(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	input.Items[0].ProductID = uuid.NewString()
	input.Items[0].VariantID = uuid.NewString()
	input.Items[0].CustomFields = map[string]interface{}{"sku": "caller-supplied"}
	repo.resolutionSnapshots = []invoiceresolution.LineSnapshot{{
		ProductID: input.Items[0].ProductID,
		VariantID: input.Items[0].VariantID,
		SKU:       "RESOLVED-SKU",
	}}

	invoice, err := service.Create(ctx, input)

	if err != nil {
		t.Fatalf("create resolved invoice: %v", err)
	}
	itemFields := unmarshalJSONMap(invoice.Items[0].CustomFields)
	lineFields := unmarshalJSONMap(repo.last.Document.Lines[0].CustomFields)
	if itemFields["sku"] != "RESOLVED-SKU" || lineFields["sku"] != "RESOLVED-SKU" {
		t.Fatalf("persisted SKU snapshots = item:%#v line:%#v, want resolver-owned SKU", itemFields, lineFields)
	}
}

func TestInvoiceServiceCreateUsesOnlyResolvedPricingProvenance(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	input.Items[0].ProductID = uuid.NewString()
	input.Items[0].CustomFields = map[string]interface{}{
		"pricing_provenance": map[string]interface{}{
			"catalogue_id":  "caller-catalogue",
			"price_list_id": "caller-price-list",
			"source":        "caller",
		},
	}
	resolvedPriceListID := uuid.NewString()
	repo.resolutionSnapshots = []invoiceresolution.LineSnapshot{{
		ProductID:   input.Items[0].ProductID,
		PriceListID: resolvedPriceListID,
	}}

	invoice, err := service.Create(ctx, input)

	if err != nil {
		t.Fatalf("create resolved invoice: %v", err)
	}
	itemFields := unmarshalJSONMap(invoice.Items[0].CustomFields)
	lineFields := unmarshalJSONMap(repo.last.Document.Lines[0].CustomFields)
	itemProvenance, _ := itemFields["pricing_provenance"].(map[string]interface{})
	lineProvenance, _ := lineFields["pricing_provenance"].(map[string]interface{})
	if len(itemProvenance) != 1 || itemProvenance["price_list_id"] != resolvedPriceListID {
		t.Fatalf("invoice pricing provenance = %#v, want only resolved price list", itemProvenance)
	}
	if len(lineProvenance) != 1 || lineProvenance["price_list_id"] != resolvedPriceListID {
		t.Fatalf("document pricing provenance = %#v, want only resolved price list", lineProvenance)
	}
}

func TestInvoiceServiceCreateRejectsResolutionFailureBeforeAtomicCreate(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	productID := uuid.NewString()
	input.Items[0].ProductID = productID
	repo.resolutionErr = &invoiceresolution.MissingReferenceError{
		Kind: invoiceresolution.ReferenceProduct,
		ID:   productID,
	}

	invoice, err := service.Create(ctx, input)

	if invoice != nil {
		t.Fatalf("invoice = %#v, want nil", invoice)
	}
	var missing *invoiceresolution.MissingReferenceError
	if !errors.As(err, &missing) || missing.ID != productID {
		t.Fatalf("error = %T %v, want typed missing product", err, err)
	}
	if repo.executions != 0 {
		t.Fatalf("atomic executions = %d, want 0", repo.executions)
	}
}

func TestInvoiceServiceCreateSanitizesResolverInfrastructureFailure(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	repo.resolutionErr = errors.New("dial tcp db.internal:5432: credential detail")

	invoice, err := service.Create(ctx, input)

	if invoice != nil {
		t.Fatalf("invoice = %#v, want nil", invoice)
	}
	var unavailable *invoiceresolution.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %T %v, want typed resolver unavailable", err, err)
	}
	if strings.Contains(err.Error(), "db.internal") || strings.Contains(err.Error(), "credential") {
		t.Fatalf("resolver infrastructure detail leaked: %v", err)
	}
	if repo.executions != 0 {
		t.Fatalf("atomic executions = %d, want 0", repo.executions)
	}
}

func TestInvoiceServiceCreateSanitizesResolverContractFailure(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	repo.resolutionSnapshots = []invoiceresolution.LineSnapshot{}

	invoice, err := service.Create(ctx, input)

	if invoice != nil {
		t.Fatalf("invoice = %#v, want nil", invoice)
	}
	var unavailable *invoiceresolution.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %T %v, want typed resolver unavailable", err, err)
	}
	if strings.Contains(err.Error(), "0 snapshots") {
		t.Fatalf("resolver contract detail leaked: %v", err)
	}
	if repo.executions != 0 {
		t.Fatalf("atomic executions = %d, want 0", repo.executions)
	}
}

func TestInvoiceServiceCreateKeepsExplicitLinePricingOverResolvedDefaults(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	input.Items[0].UnitPrice = 77
	input.Items[0].MRP = 88
	input.Items[0].CessRate = 4
	repo.resolutionSnapshots = []invoiceresolution.LineSnapshot{
		{UnitPrice: 125, MRP: 140, CessRate: 2.5},
	}

	invoice, err := service.Create(ctx, input)

	if err != nil {
		t.Fatalf("create explicitly priced invoice: %v", err)
	}
	item := invoice.Items[0]
	if item.UnitPrice != 77 || item.MRP != 88 || item.CessRate != 4 {
		t.Fatalf("explicit pricing was overridden: %#v", item)
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

func TestInvoiceServiceCreateSnapshotsDiscountInInvoiceAndDocument(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	input.Items[0].Discount = 10

	invoice, err := service.Create(ctx, input)

	if err != nil {
		t.Fatalf("create discounted invoice: %v", err)
	}
	item := invoice.Items[0]
	line := repo.last.Document.Lines[0]
	if item.Discount != 10 || line.DiscountAmount != 10 {
		t.Fatalf("discount snapshot item/line = %.2f/%.2f, want 10/10", item.Discount, line.DiscountAmount)
	}
	if item.Total != 2368.1 || line.LineSubtotal != 1990 || line.LineTotal != 2368.1 {
		t.Fatalf("discounted totals item=%#v line=%#v", item, line)
	}
	if invoice.Subtotal != 1990 || invoice.Discount != 10 || invoice.Total != 2368.1 {
		t.Fatalf("discounted invoice totals = %#v", invoice)
	}
}

func TestInvoiceServiceCreateRejectsDiscountAboveResolvedGrossAtSharedCanonicalBoundary(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	input.Items[0].ProductID = uuid.NewString()
	input.Items[0].UnitPrice = 0
	input.Items[0].Quantity = 2
	input.Items[0].Discount = 201
	repo.resolutionSnapshots = []invoiceresolution.LineSnapshot{{
		ProductID: input.Items[0].ProductID,
		UnitPrice: 100,
	}}

	invoice, err := service.Create(ctx, input)

	if invoice != nil {
		t.Fatalf("invoice = %#v, want nil", invoice)
	}
	var invalid *idempotency.InvalidPayloadError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %T %v, want sanitized invalid payload", err, err)
	}
	if repo.executions != 0 {
		t.Fatalf("atomic executions = %d, want 0", repo.executions)
	}
}

func TestInvoiceServiceCreateReplaysSameKeyAndConflictsOnChangedPayload(t *testing.T) {
	service, repo, input, ctx := newAtomicInvoiceServiceFixture(t)
	first, err := service.Create(ctx, input)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	initialResolutionCalls := repo.resolutionCalls
	repo.resolutionErr = errors.New("catalog changed after completed request")
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
	if repo.resolutionCalls != initialResolutionCalls {
		t.Fatalf("resolution calls = %d, want unchanged %d on completed replay", repo.resolutionCalls, initialResolutionCalls)
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
	if repo.resolutionCalls != initialResolutionCalls {
		t.Fatalf("resolution calls after conflict = %d, want unchanged %d", repo.resolutionCalls, initialResolutionCalls)
	}
}

func TestInvoiceServiceCreateRejectsCompletedReplayWithoutValidActor(t *testing.T) {
	service, repo, input, validContext := newAtomicInvoiceServiceFixture(t)
	_, err := service.Create(validContext, input)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	initialReplayChecks := repo.replayChecks
	initialResolutionCalls := repo.resolutionCalls
	repo.resolutionErr = errors.New("resolver must not run for actor validation")

	fixtures := []struct {
		name string
		ctx  context.Context
	}{
		{name: "missing", ctx: context.Background()},
		{
			name: "invalid",
			ctx: ContextWithActor(context.Background(), ActorContext{
				UserID: "not-a-uuid",
			}),
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			invoice, err := service.Create(fixture.ctx, input)

			if invoice != nil {
				t.Fatalf("invoice = %#v, want nil", invoice)
			}
			if err == nil || err.Error() != "invoice create actor is required" {
				t.Fatalf("error = %T %v, want required actor error", err, err)
			}
			if repo.replayChecks != initialReplayChecks {
				t.Fatalf(
					"replay checks = %d, want unchanged %d before actor validation",
					repo.replayChecks,
					initialReplayChecks,
				)
			}
			if repo.resolutionCalls != initialResolutionCalls {
				t.Fatalf(
					"resolution calls = %d, want unchanged %d",
					repo.resolutionCalls,
					initialResolutionCalls,
				)
			}
		})
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
	origin := input.Origin
	if origin == "" {
		origin = models.InvoiceOriginManual
	}
	return &models.Invoice{
		ID:            uuid.NewString(),
		BusinessID:    businessID,
		CustomerID:    models.StringPointer(input.CustomerID),
		Status:        models.InvoiceStatusDraft,
		Origin:        origin,
		Version:       1,
		BuyerSnapshot: input.BuyerSnapshot,
		InvoiceDate:   input.InvoiceDate,
		DueDate:       input.DueDate,
		Currency:      firstNonEmpty(input.Currency, "INR"),
		Subtotal:      100,
		Total:         100,
		BalanceDue:    100,
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
			Description:    "Canonical line",
			Quantity:       1,
			UnitPrice:      100,
			DiscountAmount: 10,
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
		creator.input.Items[0].Description != "Canonical line" ||
		creator.input.Items[0].Discount != 10 {
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

func TestInvoiceDocumentProjectionSplitsOddPaiseGSTWithoutLosingRemainder(t *testing.T) {
	customerID := uuid.NewString()
	invoice := &models.Invoice{
		ID:             uuid.NewString(),
		BusinessID:     uuid.NewString(),
		CustomerID:     &customerID,
		Status:         models.InvoiceStatusDraft,
		Origin:         models.InvoiceOriginManual,
		Version:        1,
		SellerSnapshot: models.PartySnapshot{GSTIN: "27AAAAA0000A1Z5"},
		TaxProfile:     `{"gst_treatment":"regular","place_of_supply":"27"}`,
		Items: []*models.InvoiceItem{{
			ID:          uuid.NewString(),
			Description: "Odd paise tax",
			Quantity:    1,
			UnitPrice:   1,
			TaxRate:     5,
			Total:       1.05,
		}},
	}

	document := invoiceDocumentProjection(invoice)
	if len(document.Lines) != 1 {
		t.Fatalf("projected lines = %d, want 1", len(document.Lines))
	}
	line := document.Lines[0]
	if line.TaxAmount != 0.05 || line.CGSTAmount != 0.03 || line.SGSTAmount != 0.02 {
		t.Fatalf("tax/CGST/SGST = %.2f/%.2f/%.2f, want 0.05/0.03/0.02",
			line.TaxAmount, line.CGSTAmount, line.SGSTAmount)
	}
	if line.CGSTAmount+line.SGSTAmount != line.TaxAmount {
		t.Fatalf("CGST %.2f + SGST %.2f != tax amount %.2f",
			line.CGSTAmount, line.SGSTAmount, line.TaxAmount)
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
