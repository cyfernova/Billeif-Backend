package services

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/gst"
	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceresolution"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type InvoiceEmailSender interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}

type InvoiceService struct {
	db           *gorm.DB
	cfg          *config.Config
	repo         interfaces.CanonicalInvoiceRepository
	businessRepo interfaces.BusinessRepository
	productRepo  interfaces.ProductRepository
	customerRepo interfaces.CustomerRepository
	documents    *DocumentService
	s3           *S3Service
	email        InvoiceEmailSender
	log          *logger.Logger
}

func NewInvoiceService(
	db *gorm.DB,
	cfg *config.Config,
	repo interfaces.CanonicalInvoiceRepository,
	businessRepo interfaces.BusinessRepository,
	productRepo interfaces.ProductRepository,
	customerRepo interfaces.CustomerRepository,
	documents *DocumentService,
	aws *awsclients.Config,
	s3 *S3Service,
	email InvoiceEmailSender,
	log *logger.Logger,
) *InvoiceService {
	return &InvoiceService{
		db:           db,
		cfg:          cfg,
		repo:         repo,
		businessRepo: businessRepo,
		productRepo:  productRepo,
		customerRepo: customerRepo,
		documents:    documents,
		s3:           s3,
		email:        email,
		log:          log,
	}
}

type CreateInvoiceItemInput struct {
	ProductID        string                   `json:"product_id"`
	VariantID        string                   `json:"variant_id,omitempty"`
	WarehouseID      string                   `json:"warehouse_id,omitempty"`
	Description      string                   `json:"description" binding:"required"`
	HSNSACCode       string                   `json:"hsn_sac_code,omitempty"`
	Unit             string                   `json:"unit,omitempty"`
	Quantity         float64                  `json:"quantity" binding:"required,gt=0"`
	FreeQuantity     float64                  `json:"free_quantity"`
	UnitPrice        float64                  `json:"unit_price" binding:"gte=0"`
	MRP              float64                  `json:"mrp"`
	Discount         float64                  `json:"discount" binding:"gte=0"`
	TaxRate          float64                  `json:"tax_rate"`
	CessRate         float64                  `json:"cess_rate"`
	CustomFields     map[string]interface{}   `json:"custom_fields,omitempty"`
	ChargeSnapshot   []map[string]interface{} `json:"charge_snapshot,omitempty"`
	BatchAllocations []BatchAllocationInput   `json:"batch_allocations,omitempty"`
	SerialIDs        []string                 `json:"serial_ids,omitempty"`
}

type CreateInvoiceInput struct {
	BusinessID           string                   `json:"business_id,omitempty"`
	IdempotencyKey       string                   `json:"-"`
	Origin               models.InvoiceOrigin     `json:"-"`
	CustomerID           string                   `json:"customer_id" binding:"required,uuid"`
	BuyerSnapshot        models.PartySnapshot     `json:"-"`
	Currency             string                   `json:"-"`
	ProjectID            string                   `json:"project_id,omitempty" binding:"omitempty,uuid"`
	PriceListID          string                   `json:"price_list_id,omitempty"`
	RenderProfileID      string                   `json:"render_profile_id,omitempty" binding:"omitempty,uuid"`
	InvoiceDate          time.Time                `json:"invoice_date"`
	DueDate              time.Time                `json:"due_date" binding:"required"`
	Notes                string                   `json:"notes"`
	TermsAndConditions   string                   `json:"terms_and_conditions"`
	PONumber             string                   `json:"po_number"`
	TemplateOverride     map[string]interface{}   `json:"template_override,omitempty"`
	CustomFields         map[string]interface{}   `json:"custom_fields,omitempty"`
	AdditionalCharges    []map[string]interface{} `json:"additional_charges,omitempty"`
	OriginSubscriptionID string                   `json:"-"`
	OriginRunID          string                   `json:"-"`
	TaxProfile           TaxProfileInput          `json:"tax_profile"`
	Items                []CreateInvoiceItemInput `json:"items" binding:"required,min=1,dive"`
}

type canonicalInvoiceCreatePayload struct {
	BusinessID           string                   `json:"business_id"`
	Origin               models.InvoiceOrigin     `json:"origin"`
	CustomerID           string                   `json:"customer_id"`
	BuyerSnapshot        models.PartySnapshot     `json:"buyer_snapshot"`
	Currency             string                   `json:"currency"`
	ProjectID            string                   `json:"project_id,omitempty"`
	PriceListID          string                   `json:"price_list_id,omitempty"`
	RenderProfileID      string                   `json:"render_profile_id,omitempty"`
	InvoiceDate          time.Time                `json:"invoice_date"`
	DueDate              time.Time                `json:"due_date"`
	Notes                string                   `json:"notes"`
	TermsAndConditions   string                   `json:"terms_and_conditions"`
	PONumber             string                   `json:"po_number"`
	TemplateOverride     map[string]interface{}   `json:"template_override,omitempty"`
	CustomFields         map[string]interface{}   `json:"custom_fields,omitempty"`
	AdditionalCharges    []map[string]interface{} `json:"additional_charges,omitempty"`
	OriginSubscriptionID string                   `json:"origin_subscription_id"`
	OriginRunID          string                   `json:"origin_run_id"`
	TaxProfile           TaxProfileInput          `json:"tax_profile"`
	Items                []CreateInvoiceItemInput `json:"items"`
}

func canonicalInvoiceCreateRequest(input CreateInvoiceInput) canonicalInvoiceCreatePayload {
	return canonicalInvoiceCreatePayload{
		BusinessID:           input.BusinessID,
		Origin:               input.Origin,
		CustomerID:           input.CustomerID,
		BuyerSnapshot:        input.BuyerSnapshot,
		Currency:             input.Currency,
		ProjectID:            input.ProjectID,
		PriceListID:          input.PriceListID,
		RenderProfileID:      input.RenderProfileID,
		InvoiceDate:          input.InvoiceDate,
		DueDate:              input.DueDate,
		Notes:                input.Notes,
		TermsAndConditions:   input.TermsAndConditions,
		PONumber:             input.PONumber,
		TemplateOverride:     input.TemplateOverride,
		CustomFields:         input.CustomFields,
		AdditionalCharges:    input.AdditionalCharges,
		OriginSubscriptionID: input.OriginSubscriptionID,
		OriginRunID:          input.OriginRunID,
		TaxProfile:           input.TaxProfile,
		Items:                input.Items,
	}
}

func normalizedInvoiceCreateOrigin(input CreateInvoiceInput) (models.InvoiceOrigin, error) {
	hasSubscriptionOrigin := strings.TrimSpace(input.OriginSubscriptionID) != "" ||
		strings.TrimSpace(input.OriginRunID) != ""
	switch input.Origin {
	case "":
		if hasSubscriptionOrigin {
			return models.InvoiceOriginSubscription, nil
		}
		return models.InvoiceOriginManual, nil
	case models.InvoiceOriginManual:
		if hasSubscriptionOrigin || !input.BuyerSnapshot.IsEmpty() {
			return "", &idempotency.InvalidPayloadError{}
		}
		return input.Origin, nil
	case models.InvoiceOriginPOS:
		if hasSubscriptionOrigin ||
			(strings.TrimSpace(input.CustomerID) != "" && !input.BuyerSnapshot.IsEmpty()) {
			return "", &idempotency.InvalidPayloadError{}
		}
		return input.Origin, nil
	case models.InvoiceOriginSubscription:
		if !hasSubscriptionOrigin || !input.BuyerSnapshot.IsEmpty() {
			return "", &idempotency.InvalidPayloadError{}
		}
		return input.Origin, nil
	default:
		return "", &idempotency.InvalidPayloadError{}
	}
}

func firstInvoiceBuyerSnapshot(customer *models.Customer, trusted models.PartySnapshot) models.PartySnapshot {
	if customer != nil {
		return customerPartySnapshot(customer)
	}
	return trusted
}

func (s *InvoiceService) Create(ctx context.Context, input CreateInvoiceInput) (*models.Invoice, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if _, err := uuid.Parse(input.IdempotencyKey); err != nil {
		return nil, &idempotency.InvalidKeyError{}
	}
	origin, err := normalizedInvoiceCreateOrigin(input)
	if err != nil {
		return nil, err
	}
	input.Origin = origin
	if s.repo == nil {
		return nil, fmt.Errorf("canonical invoice repository is not configured")
	}
	if input.RenderProfileID != "" {
		normalized, err := normalizeRenderProfileID(input.RenderProfileID)
		if err != nil {
			return nil, err
		}
		input.RenderProfileID = normalized
	}
	requestHash, err := idempotency.CanonicalHash(canonicalInvoiceCreateRequest(input))
	if err != nil {
		return nil, err
	}
	replay, err := s.repo.ReplayCompletedDraft(
		ctx,
		input.BusinessID,
		"invoice.create",
		input.IdempotencyKey,
		requestHash,
	)
	if err != nil {
		return nil, err
	}
	if replay != nil && replay.Invoice != nil {
		hydrateInvoiceEditorFields(replay.Invoice)
		return replay.Invoice, nil
	}
	if s.businessRepo == nil {
		return nil, fmt.Errorf("business repository is not configured")
	}
	business, err := s.businessRepo.GetByID(ctx, input.BusinessID)
	if err != nil {
		return nil, fmt.Errorf("business not found: %w", err)
	}
	var customer *models.Customer
	if strings.TrimSpace(input.CustomerID) != "" {
		customer, err = s.customerRepo.GetByID(ctx, input.CustomerID, input.BusinessID)
		if err != nil {
			return nil, fmt.Errorf("customer not found: %w", err)
		}
	} else if input.Origin != models.InvoiceOriginPOS || input.BuyerSnapshot.IsEmpty() {
		return nil, &idempotency.InvalidPayloadError{}
	}
	if input.RenderProfileID != "" && s.documents != nil {
		if _, err := s.documents.GetRenderProfileByBusiness(ctx, input.BusinessID, input.RenderProfileID); err != nil {
			return nil, fmt.Errorf("render profile not found: %w", err)
		}
	}
	priceListID, err := resolvePriceListID(
		ctx,
		s.db,
		s.customerRepo,
		nil,
		input.BusinessID,
		models.DocumentPartyTypeCustomer,
		input.CustomerID,
		stringPointer(input.PriceListID),
		nil,
	)
	if err != nil {
		return nil, err
	}

	projectID := syncProjectIDFromTags(input.ProjectID, input.TaxProfile.ReportTags)
	input.TaxProfile.ReportTags = mergeProjectIntoTags(input.TaxProfile.ReportTags, projectID)
	customFields := mergeInvoiceEditorCustomFields(input.CustomFields, input.TermsAndConditions, input.PONumber, input.TemplateOverride)
	lineReferences := make([]invoiceresolution.LineReference, len(input.Items))
	for index, item := range input.Items {
		lineReferences[index] = invoiceresolution.LineReference{
			ProductID:   item.ProductID,
			VariantID:   item.VariantID,
			WarehouseID: item.WarehouseID,
		}
	}
	resolvedLines, err := s.repo.ResolveInvoiceLines(ctx, invoiceresolution.Request{
		BusinessID:  input.BusinessID,
		PriceListID: pointerStringValue(priceListID),
		Lines:       lineReferences,
	})
	if err != nil {
		var missing *invoiceresolution.MissingReferenceError
		var invalid *invoiceresolution.InvalidReferenceError
		if errors.As(err, &missing) || errors.As(err, &invalid) {
			return nil, err
		}
		return nil, &invoiceresolution.UnavailableError{}
	}
	if len(resolvedLines) != len(input.Items) {
		return nil, &invoiceresolution.UnavailableError{}
	}

	var subtotal, discountTotal, taxTotal float64
	var cessTotal float64
	items := make([]*models.InvoiceItem, len(input.Items))

	for i, item := range input.Items {
		resolved := resolvedLines[i]
		unitPrice := item.UnitPrice
		if unitPrice <= 0 {
			unitPrice = resolved.UnitPrice
		}
		mrp := item.MRP
		if mrp <= 0 {
			mrp = resolved.MRP
		}
		cessRate := item.CessRate
		if cessRate <= 0 {
			cessRate = resolved.CessRate
		}
		itemGross := item.Quantity * unitPrice
		if item.Discount < 0 || item.Discount > itemGross {
			return nil, &idempotency.InvalidPayloadError{}
		}
		itemSubtotal := itemGross - item.Discount
		itemTax := itemSubtotal * (item.TaxRate / 100)
		itemCess := itemSubtotal * (cessRate / 100)
		subtotal += itemSubtotal
		discountTotal += item.Discount
		taxTotal += itemTax
		cessTotal += itemCess

		var productID *string
		if resolved.ProductID != "" {
			productID = &resolved.ProductID
		}
		var variantID *string
		if resolved.VariantID != "" {
			variantID = &resolved.VariantID
		}
		var warehouseID *string
		if resolved.WarehouseID != "" {
			warehouseID = &resolved.WarehouseID
		}
		hsnCode := item.HSNSACCode
		if hsnCode == "" {
			hsnCode = resolved.HSNSACCode
		}
		unit := gst.CanonicalSnapshotUQC(item.Unit, firstNonEmpty(resolved.Unit, resolved.UQCCode))
		lineCustomFields := make(map[string]interface{}, len(item.CustomFields)+1)
		for key, value := range item.CustomFields {
			lineCustomFields[key] = value
		}
		if resolved.SKU != "" {
			lineCustomFields["sku"] = resolved.SKU
		}
		delete(lineCustomFields, "pricing_provenance")
		if resolved.CatalogueID != "" || resolved.PriceListID != "" {
			provenance := map[string]interface{}{}
			if resolved.CatalogueID != "" {
				provenance["catalogue_id"] = resolved.CatalogueID
			}
			if resolved.PriceListID != "" {
				provenance["price_list_id"] = resolved.PriceListID
			}
			lineCustomFields["pricing_provenance"] = provenance
		}

		items[i] = &models.InvoiceItem{
			ProductID:        productID,
			VariantID:        variantID,
			WarehouseID:      warehouseID,
			Description:      firstNonEmpty(item.Description, resolved.ProductName),
			HSNSACCode:       hsnCode,
			Unit:             unit,
			SKU:              resolved.SKU,
			Quantity:         item.Quantity,
			FreeQuantity:     item.FreeQuantity,
			UnitPrice:        unitPrice,
			MRP:              mrp,
			Discount:         item.Discount,
			TaxRate:          item.TaxRate,
			CessRate:         cessRate,
			CessAmount:       itemCess,
			CustomFields:     mustMarshalMap(lineCustomFields),
			ChargeSnapshot:   mustMarshalAny(item.ChargeSnapshot, "[]"),
			BatchAllocations: mustMarshalBatchAllocations(item.BatchAllocations),
			SerialIDs:        marshalStringSlice(item.SerialIDs),
			Total:            itemSubtotal + itemTax + itemCess,
		}
	}

	invoiceDate := input.InvoiceDate
	if invoiceDate.IsZero() {
		invoiceDate = time.Now()
	}
	invoice := &models.Invoice{
		ID:                uuid.NewString(),
		BusinessID:        input.BusinessID,
		CustomerID:        stringPointer(input.CustomerID),
		ProjectID:         projectIDPointer(projectID),
		PriceListID:       priceListID,
		RenderProfileID:   stringPointer(input.RenderProfileID),
		Status:            models.InvoiceStatusDraft,
		Origin:            input.Origin,
		Version:           1,
		SellerSnapshot:    businessPartySnapshot(business),
		BuyerSnapshot:     firstInvoiceBuyerSnapshot(customer, input.BuyerSnapshot),
		InvoiceDate:       invoiceDate,
		DueDate:           input.DueDate,
		Subtotal:          subtotal,
		Discount:          discountTotal,
		Tax:               taxTotal + cessTotal,
		Total:             subtotal + taxTotal + cessTotal,
		BalanceDue:        subtotal + taxTotal + cessTotal,
		Notes:             input.Notes,
		CustomFields:      mustMarshalMap(customFields),
		AdditionalCharges: mustMarshalAny(input.AdditionalCharges, "[]"),
		TaxProfile:        mustMarshalMap(taxProfileToMap(input.TaxProfile)),
		Currency:          defaultCurrency(firstNonEmpty(input.Currency, business.Currency)),
		Items:             items,
	}
	if input.OriginSubscriptionID != "" {
		invoice.OriginSubscriptionID = &input.OriginSubscriptionID
	}
	if input.OriginRunID != "" {
		invoice.OriginRunID = &input.OriginRunID
	}

	for _, item := range invoice.Items {
		item.ID = uuid.NewString()
		item.InvoiceID = invoice.ID
	}
	actor := actorFromContext(ctx)
	if _, err := uuid.Parse(actor.UserID); err != nil {
		return nil, fmt.Errorf("invoice create actor is required")
	}
	document := invoiceDocumentProjection(invoice)
	activity := &models.ActivityLog{
		BusinessID: invoice.BusinessID,
		ActorID:    actor.UserID,
		ActorRole:  actor.Role,
		RequestID:  actor.RequestID,
		IPAddress:  actor.IPAddress,
		EntityType: "invoice",
		EntityID:   invoice.ID,
		Action:     "created",
		Snapshot:   mustMarshalAny(invoice, "{}"),
		Diff:       "{}",
		Metadata:   "{}",
	}
	result, err := s.repo.CreateDraftAtomic(ctx, interfaces.AtomicInvoiceDraft{
		BusinessID:     invoice.BusinessID,
		Command:        "invoice.create",
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    requestHash,
		Invoice:        invoice,
		Document:       document,
		Activity:       activity,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create invoice: %w", err)
	}
	if result == nil || result.Invoice == nil {
		return nil, fmt.Errorf("failed to create invoice: atomic repository returned no result")
	}
	hydrateInvoiceEditorFields(result.Invoice)
	return result.Invoice, nil
}

func (s *InvoiceService) CreateByBusiness(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Invoice, error) {
	input.BusinessID = businessID
	return s.Create(ctx, input)
}

func (s *InvoiceService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Invoice, error) {
	invoice, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return nil, err
	}
	hydrateInvoiceEditorFields(invoice)
	return invoice, nil
}

// GetForWorker fetches an invoice without tenant scoping. Only for trusted internal callers (workers).
func (s *InvoiceService) GetForWorker(ctx context.Context, id string) (*models.Invoice, error) {
	invoice, err := s.repo.GetByIDInternal(ctx, id)
	if err != nil {
		return nil, err
	}
	hydrateInvoiceEditorFields(invoice)
	return invoice, nil
}

func (s *InvoiceService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error) {
	invoices, total, err := s.repo.GetByBusinessID(ctx, businessID, page, limit)
	if err != nil {
		return nil, 0, err
	}
	for _, invoice := range invoices {
		hydrateInvoiceEditorFields(invoice)
	}
	return invoices, total, nil
}

type UpdateInvoiceInput struct {
	DueDate            time.Time                `json:"due_date"`
	Notes              string                   `json:"notes"`
	ProjectID          *string                  `json:"project_id,omitempty"`
	PriceListID        *string                  `json:"price_list_id,omitempty"`
	RenderProfileID    *string                  `json:"render_profile_id,omitempty" binding:"omitempty,uuid"`
	InvoiceDate        time.Time                `json:"invoice_date"`
	TermsAndConditions string                   `json:"terms_and_conditions"`
	PONumber           string                   `json:"po_number"`
	TemplateOverride   map[string]interface{}   `json:"template_override,omitempty"`
	CustomFields       map[string]interface{}   `json:"custom_fields,omitempty"`
	AdditionalCharges  []map[string]interface{} `json:"additional_charges,omitempty"`
	EditReason         string                   `json:"edit_reason,omitempty"`
	TaxProfile         *TaxProfileInput         `json:"tax_profile,omitempty"`
}

func (s *InvoiceService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateInvoiceInput) (*models.Invoice, error) {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}

	if invoice.SignedAt != nil {
		return nil, fmt.Errorf("signed invoices are immutable")
	}
	if !isInvoiceEditableStatus(invoice.Status) {
		return nil, fmt.Errorf("cannot update invoice with status: %s", invoice.Status)
	}

	financialsLocked := !isInvoiceFinancialEditableStatus(invoice.Status)
	if !financialsLocked && !input.InvoiceDate.IsZero() {
		invoice.InvoiceDate = input.InvoiceDate
	}
	if !financialsLocked && !input.DueDate.IsZero() {
		invoice.DueDate = input.DueDate
	}
	if input.Notes != "" {
		invoice.Notes = input.Notes
	}
	if !financialsLocked && input.PriceListID != nil {
		invoice.PriceListID = input.PriceListID
	}
	if input.RenderProfileID != nil {
		candidate := strings.TrimSpace(*input.RenderProfileID)
		if candidate == "" {
			invoice.RenderProfileID = nil
		} else {
			normalized, err := normalizeRenderProfileID(candidate)
			if err != nil {
				return nil, err
			}
			if s.documents != nil {
				if _, err := s.documents.GetRenderProfileByBusiness(ctx, businessID, normalized); err != nil {
					return nil, fmt.Errorf("render profile not found: %w", err)
				}
			}
			invoice.RenderProfileID = &normalized
		}
	}
	customFields := unmarshalJSONMap(invoice.CustomFields)
	for key, value := range input.CustomFields {
		customFields[key] = value
	}
	customFields = mergeInvoiceEditorCustomFields(customFields, input.TermsAndConditions, input.PONumber, input.TemplateOverride)
	invoice.CustomFields = mustMarshalMap(customFields)
	if input.AdditionalCharges != nil {
		invoice.AdditionalCharges = mustMarshalAny(input.AdditionalCharges, "[]")
	}
	if !financialsLocked && input.TaxProfile != nil {
		projectID := ""
		if invoice.ProjectID != nil {
			projectID = *invoice.ProjectID
		}
		if input.ProjectID != nil {
			projectID = normalizeProjectID(*input.ProjectID)
		}
		projectID = syncProjectIDFromTags(projectID, input.TaxProfile.ReportTags)
		input.TaxProfile.ReportTags = mergeProjectIntoTags(input.TaxProfile.ReportTags, projectID)
		invoice.TaxProfile = mustMarshalMap(taxProfileToMap(*input.TaxProfile))
		invoice.ProjectID = projectIDPointer(projectID)
	} else if !financialsLocked && input.ProjectID != nil {
		profile := unmarshalJSONMap(invoice.TaxProfile)
		profile["report_tags"] = mergeProjectIntoTags(nestedMap(profile, "report_tags"), normalizeProjectID(*input.ProjectID))
		invoice.TaxProfile = mustMarshalMap(profile)
		invoice.ProjectID = projectIDPointer(*input.ProjectID)
	}
	hydrateInvoiceEditorFields(invoice)

	if err := s.repo.Update(ctx, invoice); err != nil {
		return nil, err
	}
	if s.documents != nil {
		if err := s.documents.MirrorLegacyInvoice(ctx, invoice); err != nil {
			s.log.Error("failed to mirror updated invoice", "invoice_id", invoice.ID, "error", err)
		}
	}
	_ = recordActivityLog(ctx, s.db, invoice.BusinessID, "invoice", invoice.ID, "updated", input.EditReason, invoice, nil, nil)
	return invoice, nil
}

func isInvoiceEditableStatus(status string) bool {
	switch status {
	case "draft", "pending", "sent", "viewed", "overdue", "partially_paid":
		return true
	default:
		return false
	}
}

func isInvoiceFinancialEditableStatus(status string) bool {
	switch status {
	case "draft", "pending":
		return true
	default:
		return false
	}
}

func mergeInvoiceEditorCustomFields(base map[string]interface{}, terms, poNumber string, templateOverride map[string]interface{}) map[string]interface{} {
	if base == nil {
		base = map[string]interface{}{}
	}
	if terms != "" {
		base["terms_and_conditions"] = terms
	}
	if poNumber != "" {
		base["po_number"] = poNumber
	}
	if templateOverride != nil {
		base["template_override"] = templateOverride
	}
	return base
}

func hydrateInvoiceEditorFields(invoice *models.Invoice) {
	if invoice == nil {
		return
	}
	if invoice.Version <= 0 {
		invoice.Version = 1
	}
	for _, item := range invoice.Items {
		hydrateInvoiceItemEditorFields(item)
	}
	if customerSnapshot := unmarshalJSONMap(invoice.CustomerSnapshotRaw); len(customerSnapshot) > 0 {
		invoice.CustomerSnapshot = customerSnapshot
	}
	if documentJSON := unmarshalJSONMap(invoice.DocumentJSONRaw); len(documentJSON) > 0 {
		invoice.DocumentJSON = documentJSON
	}
	if templateOverride := unmarshalJSONMap(invoice.TemplateOverrideRaw); len(templateOverride) > 0 {
		invoice.TemplateOverride = templateOverride
	}
	if paymentDisplay := unmarshalJSONMap(invoice.PaymentDisplayRaw); len(paymentDisplay) > 0 {
		invoice.PaymentDisplay = paymentDisplay
	}
	if termsJSON := unmarshalJSONMap(invoice.TermsJSONRaw); len(termsJSON) > 0 {
		invoice.TermsJSON = termsJSON
	}
	if ewayDetails := unmarshalJSONMap(invoice.EWayDetailsJSONRaw); len(ewayDetails) > 0 {
		invoice.EWayDetailsJSON = ewayDetails
	}
	if einvoiceSettings := unmarshalJSONMap(invoice.EInvoiceSettingsRaw); len(einvoiceSettings) > 0 {
		invoice.EInvoiceSettingsJSON = einvoiceSettings
	}
	customFields := unmarshalJSONMap(invoice.CustomFields)
	if terms, ok := customFields["terms_and_conditions"].(string); ok {
		invoice.TermsAndConditions = terms
	}
	if poNumber, ok := customFields["po_number"].(string); ok {
		invoice.PONumber = poNumber
	}
	if templateOverride, ok := customFields["template_override"].(map[string]interface{}); ok && len(invoice.TemplateOverride) == 0 {
		invoice.TemplateOverride = templateOverride
	}
	if customerSnapshot, ok := customFields["customer_snapshot"].(map[string]interface{}); ok && len(invoice.CustomerSnapshot) == 0 {
		invoice.CustomerSnapshot = customerSnapshot
	}
	if paymentDisplay, ok := customFields["payment_display"].(map[string]interface{}); ok && len(invoice.PaymentDisplay) == 0 {
		invoice.PaymentDisplay = paymentDisplay
	}
}

func taxProfileToMap(input TaxProfileInput) map[string]interface{} {
	return map[string]interface{}{
		"gst_treatment":           input.GSTTreatment,
		"place_of_supply":         input.PlaceOfSupply,
		"bill_of_supply":          input.BillOfSupply,
		"export_type":             input.ExportType,
		"supply_type":             input.SupplyType,
		"counterparty_gstin":      input.CounterpartyGSTIN,
		"counterparty_pan":        input.CounterpartyPAN,
		"counterparty_state_code": input.CounterpartyStateCode,
		"generate_einvoice":       input.GenerateEInvoice,
		"generate_ewaybill":       input.GenerateEWayBill,
		"reverse_charge":          input.ReverseCharge,
		"reverse_charge_reason":   input.ReverseChargeReason,
		"dispatch_from":           input.DispatchFrom,
		"dispatch_to":             input.DispatchTo,
		"distance_km":             input.DistanceKM,
		"transporter":             input.Transporter,
		"vehicle":                 input.Vehicle,
		"multi_vehicle_plan":      input.MultiVehiclePlan,
		"tcs":                     withholdingsToList(input.TCS),
		"source_linkage":          input.SourceLinkage,
		"report_tags":             input.ReportTags,
	}
}

func normalizeRenderProfileID(value string) (string, error) {
	candidate := strings.TrimSpace(value)
	if candidate == "" {
		return "", nil
	}
	if _, err := uuid.Parse(candidate); err != nil {
		return "", fmt.Errorf("invalid render profile id: %w", err)
	}
	return candidate, nil
}

func withholdingsToList(inputs []WithholdingInput) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(inputs))
	for _, input := range inputs {
		items = append(items, withholdingToMap(&input))
	}
	return items
}

func (s *InvoiceService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}
	if invoice.SignedAt != nil {
		return fmt.Errorf("signed invoices are immutable")
	}
	if invoice.Status != "draft" {
		return fmt.Errorf("cannot delete invoice with status: %s", invoice.Status)
	}
	if err := s.repo.Delete(ctx, invoice.ID); err != nil {
		return err
	}
	if s.documents != nil {
		if err := s.documents.DeleteByType(ctx, businessID, invoice.ID, models.DocumentTypeSalesInvoice); err != nil && err.Error() != "document not found" {
			if altErr := s.documents.DeleteByType(ctx, businessID, invoice.ID, models.DocumentTypeBillOfSupply); altErr != nil && altErr.Error() != "document not found" {
				s.log.Error("failed to delete mirrored invoice document", "invoice_id", invoice.ID, "error", altErr)
			}
		}
	}
	_ = recordActivityLog(ctx, s.db, businessID, "invoice", invoice.ID, "deleted", "", invoice, nil, nil)
	return nil
}

func (s *InvoiceService) UpdateStatus(ctx context.Context, id, status string) error {
	return s.repo.UpdateStatus(ctx, id, status)
}

func (s *InvoiceService) SendByBusiness(ctx context.Context, businessID, id string) error {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}

	if invoice.Status == models.InvoiceStatusDraft || invoice.InvoiceNo == nil || invoice.IssuedAt == nil {
		return models.ErrInvalidInvoiceLifecycle
	}
	if invoice.CustomerID == nil {
		return models.ErrInvoiceCustomerRequired
	}
	customer, err := s.customerRepo.GetByID(ctx, *invoice.CustomerID, businessID)
	if err != nil {
		return err
	}

	if err := s.repo.UpdateStatus(ctx, id, "sent"); err != nil {
		return err
	}
	invoice.Status = "sent"
	now := time.Now()
	invoice.SentAt = &now
	if s.documents != nil {
		if err := s.documents.MirrorLegacyInvoice(ctx, invoice); err != nil {
			s.log.Error("failed to mirror sent invoice", "invoice_id", invoice.ID, "error", err)
		}
	}
	_ = recordActivityLog(ctx, s.db, businessID, "invoice", invoice.ID, "sent", "", invoice, nil, nil)

	subject := fmt.Sprintf("Invoice %s", models.StringValue(invoice.InvoiceNo))
	body := fmt.Sprintf("Please find attached invoice %s for amount %s%.2f", models.StringValue(invoice.InvoiceNo), invoice.Currency, invoice.Total)
	return s.email.SendEmail(ctx, customer.Email, subject, body)
}

func (s *InvoiceService) GetPDFURLByBusiness(ctx context.Context, businessID, invoiceID string) (string, error) {
	invoice, err := s.GetByBusiness(ctx, businessID, invoiceID)
	if err != nil {
		return "", err
	}
	if invoice.PDFURL != "" {
		return invoice.PDFURL, nil
	}
	return "", fmt.Errorf("PDF not yet generated")
}

// UpdatePDFUrl is called by the internal worker (no tenant context).
// Uses direct column update rather than tenant-scoped GetByID.
func (s *InvoiceService) UpdatePDFUrl(ctx context.Context, invoiceID, pdfURL string) error {
	if err := s.repo.UpdatePDFURL(ctx, invoiceID, pdfURL); err != nil {
		return err
	}
	if s.documents != nil {
		if err := s.documents.SyncLegacyInvoicePDF(ctx, invoiceID, pdfURL, path.Base(pdfURL)); err != nil {
			s.log.Error("failed to sync invoice pdf to mirrored document", "invoice_id", invoiceID, "error", err)
		}
	}
	return nil
}

type Invoice = models.Invoice
