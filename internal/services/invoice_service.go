package services

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/gst"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type InvoiceService struct {
	db           *gorm.DB
	cfg          *config.Config
	repo         interfaces.InvoiceRepository
	productRepo  interfaces.ProductRepository
	customerRepo interfaces.CustomerRepository
	documents    *DocumentService
	sqs          *sqs.Client
	s3           *S3Service
	email        *EmailService
	log          *logger.Logger
}

func NewInvoiceService(
	db *gorm.DB,
	cfg *config.Config,
	repo interfaces.InvoiceRepository,
	productRepo interfaces.ProductRepository,
	customerRepo interfaces.CustomerRepository,
	documents *DocumentService,
	aws *awsclients.Config,
	s3 *S3Service,
	email *EmailService,
	log *logger.Logger,
) *InvoiceService {
	return &InvoiceService{
		db:           db,
		cfg:          cfg,
		repo:         repo,
		productRepo:  productRepo,
		customerRepo: customerRepo,
		documents:    documents,
		sqs:          aws.SQS,
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
	TaxRate          float64                  `json:"tax_rate"`
	CessRate         float64                  `json:"cess_rate"`
	CustomFields     map[string]interface{}   `json:"custom_fields,omitempty"`
	ChargeSnapshot   []map[string]interface{} `json:"charge_snapshot,omitempty"`
	BatchAllocations []BatchAllocationInput   `json:"batch_allocations,omitempty"`
	SerialIDs        []string                 `json:"serial_ids,omitempty"`
}

type CreateInvoiceInput struct {
	BusinessID           string                   `json:"business_id,omitempty"`
	CustomerID           string                   `json:"customer_id" binding:"required,uuid"`
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
	OriginSubscriptionID string                   `json:"origin_subscription_id,omitempty"`
	OriginRunID          string                   `json:"origin_run_id,omitempty"`
	TaxProfile           TaxProfileInput          `json:"tax_profile"`
	Items                []CreateInvoiceItemInput `json:"items" binding:"required,min=1,dive"`
}

func (s *InvoiceService) Create(ctx context.Context, input CreateInvoiceInput) (*models.Invoice, error) {
	if input.RenderProfileID != "" {
		normalized, err := normalizeRenderProfileID(input.RenderProfileID)
		if err != nil {
			return nil, err
		}
		input.RenderProfileID = normalized
	}
	_, err := s.customerRepo.GetByID(ctx, input.CustomerID, input.BusinessID)
	if err != nil {
		return nil, fmt.Errorf("customer not found: %w", err)
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

	invoiceNo, err := s.generateInvoiceNumber(ctx, input.BusinessID)
	if err != nil {
		return nil, err
	}
	projectID := syncProjectIDFromTags(input.ProjectID, input.TaxProfile.ReportTags)
	input.TaxProfile.ReportTags = mergeProjectIntoTags(input.TaxProfile.ReportTags, projectID)
	customFields := mergeInvoiceEditorCustomFields(input.CustomFields, input.TermsAndConditions, input.PONumber, input.TemplateOverride)

	var subtotal, taxTotal float64
	var cessTotal float64
	items := make([]*models.InvoiceItem, len(input.Items))

	for i, item := range input.Items {
		pricing, err := resolveLinePricing(ctx, s.db, s.productRepo, input.BusinessID, priceListID, item.ProductID, item.VariantID, stringPointer(item.WarehouseID))
		if err != nil {
			return nil, err
		}
		var product *models.Product
		if item.ProductID != "" {
			product, _ = s.productRepo.GetByID(ctx, item.ProductID, input.BusinessID)
		}
		unitPrice := item.UnitPrice
		if unitPrice <= 0 {
			unitPrice = pricing.UnitPrice
		}
		mrp := item.MRP
		if mrp <= 0 {
			mrp = pricing.MRP
		}
		cessRate := item.CessRate
		if cessRate <= 0 {
			cessRate = pricing.CessRate
		}
		itemSubtotal := item.Quantity * unitPrice
		itemTax := itemSubtotal * (item.TaxRate / 100)
		itemCess := itemSubtotal * (cessRate / 100)
		subtotal += itemSubtotal
		taxTotal += itemTax
		cessTotal += itemCess

		var productID *string
		if item.ProductID != "" {
			productID = &item.ProductID
		}
		var variantID *string
		if item.VariantID != "" {
			variantID = &item.VariantID
		}
		var warehouseID *string
		if item.WarehouseID != "" {
			warehouseID = &item.WarehouseID
		}
		legacyUnit := ""
		if product != nil {
			legacyUnit = firstNonEmpty(product.Unit, product.UQCCode)
		}
		hsnCode := item.HSNSACCode
		if hsnCode == "" && product != nil {
			hsnCode = product.HSNSACCode
		}
		unit := gst.CanonicalSnapshotUQC(item.Unit, legacyUnit)

		items[i] = &models.InvoiceItem{
			ProductID:        productID,
			VariantID:        variantID,
			WarehouseID:      warehouseID,
			Description:      item.Description,
			HSNSACCode:       hsnCode,
			Unit:             unit,
			Quantity:         item.Quantity,
			FreeQuantity:     item.FreeQuantity,
			UnitPrice:        unitPrice,
			MRP:              mrp,
			TaxRate:          item.TaxRate,
			CessRate:         cessRate,
			CessAmount:       itemCess,
			CustomFields:     mustMarshalMap(item.CustomFields),
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
		BusinessID:        input.BusinessID,
		CustomerID:        models.StringPointer(input.CustomerID),
		ProjectID:         projectIDPointer(projectID),
		PriceListID:       priceListID,
		RenderProfileID:   stringPointer(input.RenderProfileID),
		InvoiceNo:         models.StringPointer(invoiceNo),
		Status:            "draft",
		InvoiceDate:       invoiceDate,
		DueDate:           input.DueDate,
		Subtotal:          subtotal,
		Tax:               taxTotal + cessTotal,
		Total:             subtotal + taxTotal + cessTotal,
		BalanceDue:        subtotal + taxTotal + cessTotal,
		Notes:             input.Notes,
		CustomFields:      mustMarshalMap(customFields),
		AdditionalCharges: mustMarshalAny(input.AdditionalCharges, "[]"),
		TaxProfile:        mustMarshalMap(taxProfileToMap(input.TaxProfile)),
		Currency:          "USD",
		Items:             items,
	}
	if input.OriginSubscriptionID != "" {
		invoice.OriginSubscriptionID = &input.OriginSubscriptionID
	}
	if input.OriginRunID != "" {
		invoice.OriginRunID = &input.OriginRunID
	}

	if err := s.repo.Create(ctx, invoice); err != nil {
		return nil, fmt.Errorf("failed to create invoice: %w", err)
	}
	hydrateInvoiceEditorFields(invoice)
	if s.documents != nil {
		if err := s.documents.MirrorLegacyInvoice(ctx, invoice); err != nil {
			s.log.Error("failed to mirror invoice into documents", "invoice_id", invoice.ID, "error", err)
		}
	}
	_ = recordActivityLog(ctx, s.db, invoice.BusinessID, "invoice", invoice.ID, "created", "", invoice, nil, nil)

	go s.queuePDFGeneration(invoice.ID)

	return invoice, nil
}

func (s *InvoiceService) CreateByBusiness(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Invoice, error) {
	input.BusinessID = businessID
	return s.Create(ctx, input)
}

func (s *InvoiceService) generateInvoiceNumber(ctx context.Context, businessID string) (string, error) {
	year := time.Now().Year()
	prefix := fmt.Sprintf("INV-%d-", year)
	return prefix + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000), nil
}

func (s *InvoiceService) queuePDFGeneration(invoiceID string) {
	ctx := context.Background()
	message := map[string]string{
		"type":       "generate_pdf",
		"invoice_id": invoiceID,
	}
	body, err := json.Marshal(message)
	if err != nil {
		s.log.Error("failed to marshal PDF generation message", "invoice_id", invoiceID, "error", err)
		return
	}

	_, err = s.sqs.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(s.cfg.SQS.InvoiceQueue),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		s.log.Error("failed to queue PDF generation", "invoice_id", invoiceID, "error", err)
	}
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

func (s *InvoiceService) GetNextNumber(ctx context.Context, businessID string) (string, error) {
	return s.generateInvoiceNumber(ctx, businessID)
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

// InvoiceServiceTestable is a test-friendly version of InvoiceService
type InvoiceServiceTestable struct {
	cfg          *config.Config
	repo         InvoiceRepositoryTestable
	productRepo  interfaces.ProductRepository
	customerRepo CustomerRepositoryTestable
	sqs          SQSServiceTestable
	s3           *S3Service
	email        EmailServiceTestable
	log          *logger.Logger
}

// InvoiceRepositoryTestable is the testable interface for InvoiceRepository
type InvoiceRepositoryTestable interface {
	Create(ctx context.Context, invoice *models.Invoice) error
	GetByID(ctx context.Context, id, businessID string) (*models.Invoice, error)
	GetByIDInternal(ctx context.Context, id string) (*models.Invoice, error)
	GetByInvoiceNo(ctx context.Context, businessID, invoiceNo string) (*models.Invoice, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error)
	GetItems(ctx context.Context, invoiceID string) ([]*models.InvoiceItem, error)
	Update(ctx context.Context, invoice *models.Invoice) error
	UpdateStatus(ctx context.Context, invoiceID string, status string) error
	UpdatePDFURL(ctx context.Context, invoiceID, pdfURL string) error
	Delete(ctx context.Context, id string) error
}

// CustomerRepositoryTestable is the testable interface for CustomerRepository
type CustomerRepositoryTestable interface {
	Create(ctx context.Context, customer *models.Customer) error
	GetByID(ctx context.Context, id, businessID string) (*models.Customer, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Customer, int64, error)
	Update(ctx context.Context, customer *models.Customer) error
	Delete(ctx context.Context, id string) error
}

// SQSServiceTestable is the testable interface for SQS operations
type SQSServiceTestable interface {
	SendMessage(ctx context.Context, queueUrl string, message interface{}) error
}

// EmailServiceTestable is the testable interface for EmailService
type EmailServiceTestable interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}

// NewInvoiceServiceForTesting creates an InvoiceServiceTestable for unit testing
func NewInvoiceServiceForTesting(
	repo InvoiceRepositoryTestable,
	productRepo interfaces.ProductRepository,
	customerRepo CustomerRepositoryTestable,
	sqs SQSServiceTestable,
	s3 *S3Service,
	email EmailServiceTestable,
	log *logger.Logger,
) *InvoiceServiceTestable {
	return &InvoiceServiceTestable{
		cfg:          nil,
		repo:         repo,
		productRepo:  productRepo,
		customerRepo: customerRepo,
		sqs:          sqs,
		s3:           s3,
		email:        email,
		log:          log,
	}
}

// Create creates an invoice (testable version)
func (s *InvoiceServiceTestable) Create(ctx context.Context, input CreateInvoiceInput) (*models.Invoice, error) {
	if input.RenderProfileID != "" {
		normalized, err := normalizeRenderProfileID(input.RenderProfileID)
		if err != nil {
			return nil, err
		}
		input.RenderProfileID = normalized
	}
	_, err := s.customerRepo.GetByID(ctx, input.CustomerID, input.BusinessID)
	if err != nil {
		return nil, fmt.Errorf("customer not found: %w", err)
	}

	invoiceNo, err := s.generateInvoiceNumber(ctx, input.BusinessID)
	if err != nil {
		return nil, err
	}

	var subtotal, taxTotal float64
	items := make([]*models.InvoiceItem, len(input.Items))

	for i, item := range input.Items {
		itemSubtotal := item.Quantity * item.UnitPrice
		itemTax := itemSubtotal * (item.TaxRate / 100)
		subtotal += itemSubtotal
		taxTotal += itemTax

		var productID *string
		if item.ProductID != "" {
			productID = &item.ProductID
		}

		items[i] = &models.InvoiceItem{
			ProductID:   productID,
			Description: item.Description,
			HSNSACCode:  item.HSNSACCode,
			Unit:        gst.CanonicalSnapshotUQC(item.Unit, ""),
			Quantity:    item.Quantity,
			UnitPrice:   item.UnitPrice,
			TaxRate:     item.TaxRate,
			Total:       itemSubtotal + itemTax,
		}
	}

	invoice := &models.Invoice{
		BusinessID:      input.BusinessID,
		CustomerID:      models.StringPointer(input.CustomerID),
		RenderProfileID: stringPointer(input.RenderProfileID),
		InvoiceNo:       models.StringPointer(invoiceNo),
		Status:          "draft",
		InvoiceDate:     time.Now(),
		DueDate:         input.DueDate,
		Subtotal:        subtotal,
		Tax:             taxTotal,
		Total:           subtotal + taxTotal,
		BalanceDue:      subtotal + taxTotal,
		Notes:           input.Notes,
		Currency:        "USD",
		Items:           items,
	}

	if err := s.repo.Create(ctx, invoice); err != nil {
		return nil, fmt.Errorf("failed to create invoice: %w", err)
	}

	if s.sqs != nil {
		_ = s.queuePDFGeneration(ctx, invoice.ID)
	}

	return invoice, nil
}

// GetByBusiness retrieves an invoice by business ID and invoice ID
func (s *InvoiceServiceTestable) GetByBusiness(ctx context.Context, businessID, id string) (*models.Invoice, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

// List retrieves invoices for a business with pagination
func (s *InvoiceServiceTestable) List(ctx context.Context, businessID string, page, limit int) ([]*models.Invoice, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

// UpdateByBusiness updates an invoice (testable version)
func (s *InvoiceServiceTestable) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateInvoiceInput) (*models.Invoice, error) {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}

	if !isInvoiceEditableStatus(invoice.Status) {
		return nil, fmt.Errorf("cannot update invoice with status: %s", invoice.Status)
	}

	if !input.DueDate.IsZero() {
		invoice.DueDate = input.DueDate
	}
	if input.Notes != "" {
		invoice.Notes = input.Notes
	}
	if input.RenderProfileID != nil {
		if strings.TrimSpace(*input.RenderProfileID) == "" {
			invoice.RenderProfileID = nil
		} else {
			value, err := normalizeRenderProfileID(*input.RenderProfileID)
			if err != nil {
				return nil, err
			}
			invoice.RenderProfileID = &value
		}
	}

	if err := s.repo.Update(ctx, invoice); err != nil {
		return nil, err
	}
	return invoice, nil
}

// DeleteByBusiness deletes an invoice (testable version)
func (s *InvoiceServiceTestable) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}
	if invoice.Status != "draft" {
		return fmt.Errorf("cannot delete invoice with status: %s", invoice.Status)
	}
	return s.repo.Delete(ctx, invoice.ID)
}

// SendByBusiness sends an invoice email (testable version)
func (s *InvoiceServiceTestable) SendByBusiness(ctx context.Context, businessID, id string) error {
	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
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

	subject := fmt.Sprintf("Invoice %s", models.StringValue(invoice.InvoiceNo))
	body := fmt.Sprintf("Please find attached invoice %s for amount %s%.2f", models.StringValue(invoice.InvoiceNo), invoice.Currency, invoice.Total)
	return s.email.SendEmail(ctx, customer.Email, subject, body)
}

// GetPDFURLByBusiness gets the PDF URL for an invoice
func (s *InvoiceServiceTestable) GetPDFURLByBusiness(ctx context.Context, businessID, invoiceID string) (string, error) {
	invoice, err := s.GetByBusiness(ctx, businessID, invoiceID)
	if err != nil {
		return "", err
	}
	if invoice.PDFURL != "" {
		return invoice.PDFURL, nil
	}
	return "", fmt.Errorf("PDF not yet generated")
}

// GetNextNumber generates the next invoice number
func (s *InvoiceServiceTestable) GetNextNumber(ctx context.Context, businessID string) (string, error) {
	return s.generateInvoiceNumber(ctx, businessID)
}

// queuePDFGeneration queues PDF generation (testable version)
func (s *InvoiceServiceTestable) queuePDFGeneration(ctx context.Context, invoiceID string) error {
	message := map[string]string{
		"type":       "generate_pdf",
		"invoice_id": invoiceID,
	}
	return s.sqs.SendMessage(ctx, "invoice-queue", message)
}

// generateInvoiceNumber generates a unique invoice number
func (s *InvoiceServiceTestable) generateInvoiceNumber(ctx context.Context, businessID string) (string, error) {
	year := time.Now().Year()
	prefix := fmt.Sprintf("INV-%d-", year)
	return prefix + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000), nil
}
