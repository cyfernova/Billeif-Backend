package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type DocumentService struct {
	db                 *gorm.DB
	cfg                *config.Config
	resolver           ProviderConfigResolver
	repo               interfaces.DocumentRepository
	businessRepo       interfaces.BusinessRepository
	customerRepo       interfaces.CustomerRepository
	vendorRepo         interfaces.VendorRepository
	productRepo        interfaces.ProductRepository
	inventory          *InventoryService
	journals           *JournalService
	shipping           *ShippingService
	taxCompliance      *TaxComplianceService
	salesInvoices      salesInvoiceDocumentCreator
	salesInvoiceIssuer salesInvoiceDocumentIssuer
	sqs                *sqs.Client
	permissions        PermissionChecker
	log                *logger.Logger
}

func (s *DocumentService) IssueSalesDocumentByBusiness(
	ctx context.Context,
	businessID, documentType, documentID string,
	input IssueInvoiceInput,
) (*IssueInvoiceResult, error) {
	if documentType != models.DocumentTypeSalesInvoice {
		return nil, fmt.Errorf("generic issuance is not supported for document type %q", documentType)
	}
	if s.salesInvoiceIssuer == nil {
		return nil, fmt.Errorf("canonical sales invoice issuer is not configured")
	}
	return s.salesInvoiceIssuer.IssueSalesInvoiceDocument(ctx, businessID, documentID, input)
}

type CreateDocumentLineInput struct {
	ProductID        string                   `json:"product_id"`
	VariantID        string                   `json:"variant_id,omitempty"`
	Description      string                   `json:"description" binding:"required"`
	HSNSACCode       string                   `json:"hsn_sac_code"`
	UQCCode          string                   `json:"uqc_code"`
	Unit             string                   `json:"unit"`
	WarehouseID      string                   `json:"warehouse_id"`
	Quantity         float64                  `json:"quantity" binding:"required,gt=0"`
	FreeQuantity     float64                  `json:"free_quantity"`
	UnitPrice        float64                  `json:"unit_price" binding:"required,gte=0"`
	MRP              float64                  `json:"mrp"`
	DiscountAmount   float64                  `json:"discount_amount"`
	TaxRate          float64                  `json:"tax_rate"`
	CessRate         float64                  `json:"cess_rate"`
	CustomFields     map[string]interface{}   `json:"custom_fields,omitempty"`
	ChargeLinkage    []map[string]interface{} `json:"charge_linkage,omitempty"`
	PackingMetadata  map[string]interface{}   `json:"packing_metadata,omitempty"`
	BatchAllocations []BatchAllocationInput   `json:"batch_allocations,omitempty"`
	SerialIDs        []string                 `json:"serial_ids,omitempty"`
}

type CreateDocumentInput struct {
	BusinessID           string                    `json:"business_id,omitempty"`
	IdempotencyKey       string                    `json:"-"`
	BranchID             string                    `json:"branch_id,omitempty" binding:"omitempty,uuid"`
	PartyID              string                    `json:"party_id"`
	PartyType            string                    `json:"party_type"`
	ProjectID            string                    `json:"project_id,omitempty" binding:"omitempty,uuid"`
	PriceListID          string                    `json:"price_list_id,omitempty"`
	Status               string                    `json:"status"`
	DraftState           string                    `json:"draft_state"`
	TaxMode              string                    `json:"tax_mode"`
	GSTTreatment         string                    `json:"gst_treatment"`
	PlaceOfSupply        string                    `json:"place_of_supply"`
	PartyGSTIN           string                    `json:"party_gstin"`
	PartyPAN             string                    `json:"party_pan"`
	PartyStateCode       string                    `json:"party_state_code"`
	SupplyType           string                    `json:"supply_type"`
	ExportType           string                    `json:"export_type"`
	BillOfSupply         bool                      `json:"bill_of_supply"`
	IssueDate            time.Time                 `json:"issue_date"`
	DueDate              *time.Time                `json:"due_date"`
	DispatchDate         *time.Time                `json:"dispatch_date"`
	Currency             string                    `json:"currency"`
	ExchangeRate         float64                   `json:"exchange_rate"`
	Locale               string                    `json:"locale"`
	SourceLinkage        map[string]interface{}    `json:"source_linkage,omitempty"`
	RenderProfileID      string                    `json:"render_profile_id"`
	Notes                string                    `json:"notes"`
	Terms                string                    `json:"terms"`
	Declaration          string                    `json:"declaration"`
	Direction            string                    `json:"direction"`
	GenerateEInvoice     bool                      `json:"generate_einvoice"`
	GenerateEWayBill     bool                      `json:"generate_ewaybill"`
	ReverseCharge        bool                      `json:"reverse_charge"`
	ReverseChargeReason  string                    `json:"reverse_charge_reason"`
	DispatchFrom         map[string]interface{}    `json:"dispatch_from,omitempty"`
	DispatchTo           map[string]interface{}    `json:"dispatch_to,omitempty"`
	DistanceKM           float64                   `json:"distance_km"`
	Transporter          map[string]interface{}    `json:"transporter,omitempty"`
	Vehicle              map[string]interface{}    `json:"vehicle,omitempty"`
	MultiVehiclePlan     map[string]interface{}    `json:"multi_vehicle_plan,omitempty"`
	ExtraFields          map[string]interface{}    `json:"extra_fields,omitempty"`
	CustomFields         map[string]interface{}    `json:"custom_fields,omitempty"`
	AdditionalCharges    []map[string]interface{}  `json:"additional_charges,omitempty"`
	OriginSubscriptionID string                    `json:"origin_subscription_id,omitempty"`
	OriginRunID          string                    `json:"origin_run_id,omitempty"`
	ReportTags           map[string]interface{}    `json:"report_tags,omitempty"`
	Withholdings         []WithholdingInput        `json:"withholdings,omitempty"`
	Lines                []CreateDocumentLineInput `json:"lines" binding:"required,min=1,dive"`
}

type ConvertDocumentLineQuantity struct {
	SourceLineID string  `json:"source_line_id" binding:"required"`
	Quantity     float64 `json:"quantity" binding:"required,gt=0"`
}

type ConvertDocumentInput struct {
	TargetDocumentType string                        `json:"target_document_type" binding:"required"`
	Status             string                        `json:"status"`
	IssueDate          time.Time                     `json:"issue_date"`
	LineQuantities     []ConvertDocumentLineQuantity `json:"line_quantities,omitempty"`
}

type MergeDocumentsInput struct {
	DocumentIDs []string `json:"document_ids" binding:"required,min=2"`
}

type DocumentHistory struct {
	Document  *models.Document           `json:"document"`
	Links     []*models.DocumentLink     `json:"links"`
	Revisions []*models.DocumentRevision `json:"revisions,omitempty"`
	RenderJob *models.DocumentRenderJob  `json:"render_job,omitempty"`
}

type RenderDocumentInput struct {
	RenderProfileID string `json:"render_profile_id"`
	Locale          string `json:"locale"`
}

type CreateRenderProfileInput struct {
	Name              string                 `json:"name" binding:"required"`
	HeaderHTML        string                 `json:"header_html"`
	FooterHTML        string                 `json:"footer_html"`
	WatermarkText     string                 `json:"watermark_text"`
	BannerText        string                 `json:"banner_text"`
	FontFamily        string                 `json:"font_family"`
	PageSize          string                 `json:"page_size"`
	LayoutConfig      map[string]interface{} `json:"layout_config,omitempty"`
	PasswordProtected bool                   `json:"password_protected"`
	Password          string                 `json:"password" extensions:"x-writeOnly"`
	CopyAllowed       *bool                  `json:"copy_allowed"`
	PrintAllowed      *bool                  `json:"print_allowed"`
	CustomLabels      map[string]interface{} `json:"custom_labels,omitempty"`
	VisibilityConfig  map[string]interface{} `json:"visibility_config,omitempty"`
	IsDefault         bool                   `json:"is_default"`
}

type UpdateRenderProfileInput struct {
	Name              string                 `json:"name"`
	HeaderHTML        string                 `json:"header_html"`
	FooterHTML        string                 `json:"footer_html"`
	WatermarkText     string                 `json:"watermark_text"`
	BannerText        string                 `json:"banner_text"`
	FontFamily        string                 `json:"font_family"`
	PageSize          string                 `json:"page_size"`
	LayoutConfig      map[string]interface{} `json:"layout_config,omitempty"`
	PasswordProtected *bool                  `json:"password_protected"`
	Password          string                 `json:"password" extensions:"x-writeOnly"`
	CopyAllowed       *bool                  `json:"copy_allowed"`
	PrintAllowed      *bool                  `json:"print_allowed"`
	CustomLabels      map[string]interface{} `json:"custom_labels,omitempty"`
	VisibilityConfig  map[string]interface{} `json:"visibility_config,omitempty"`
	IsDefault         *bool                  `json:"is_default"`
}

func NewDocumentService(
	db *gorm.DB,
	cfg *config.Config,
	resolver ProviderConfigResolver,
	repo interfaces.DocumentRepository,
	businessRepo interfaces.BusinessRepository,
	customerRepo interfaces.CustomerRepository,
	vendorRepo interfaces.VendorRepository,
	productRepo interfaces.ProductRepository,
	inventory *InventoryService,
	journals *JournalService,
	shipping *ShippingService,
	awsCfg *awsclients.Config,
	permissions PermissionChecker,
	log *logger.Logger,
) *DocumentService {
	return &DocumentService{
		db:           db,
		cfg:          cfg,
		resolver:     resolver,
		repo:         repo,
		businessRepo: businessRepo,
		customerRepo: customerRepo,
		vendorRepo:   vendorRepo,
		productRepo:  productRepo,
		inventory:    inventory,
		journals:     journals,
		shipping:     shipping,
		sqs:          awsCfg.SQS,
		permissions:  permissions,
		log:          log,
	}
}

func (s *DocumentService) AttachTaxComplianceService(taxCompliance *TaxComplianceService) {
	s.taxCompliance = taxCompliance
}

func (s *DocumentService) CreateByType(ctx context.Context, businessID, documentType string, input CreateDocumentInput) (*models.Document, error) {
	if documentType == models.DocumentTypeSalesInvoice {
		if s.salesInvoices == nil {
			return nil, fmt.Errorf("canonical sales invoice creator is not configured")
		}
		return s.salesInvoices.CreateSalesInvoiceDocument(ctx, businessID, input)
	}
	document, err := s.buildDocument(ctx, businessID, documentType, input)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, document); err != nil {
		return nil, err
	}
	if err := s.syncDocumentWithholdings(ctx, document, input.Withholdings); err != nil {
		return nil, err
	}
	if document.Status != models.DocumentStatusDraft {
		if err := s.applyPostCreateSideEffects(ctx, document); err != nil {
			return nil, err
		}
		if s.taxCompliance != nil {
			if err := s.taxCompliance.EnqueueDocumentCompliance(ctx, document, "document_create"); err != nil {
				return nil, err
			}
		}
	}
	s.recordRevision(ctx, document, "created", nil)
	_ = recordActivityLog(ctx, s.db, businessID, "document", document.ID, "created", "", document, nil, nil)
	return document, nil
}

func (s *DocumentService) createPOSSalesInvoice(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Document, error) {
	if s.salesInvoices == nil {
		return nil, fmt.Errorf("canonical sales invoice creator is not configured")
	}
	return s.salesInvoices.createPOSSalesInvoiceDocument(ctx, businessID, input)
}

func (s *DocumentService) buildDocument(ctx context.Context, businessID, documentType string, input CreateDocumentInput) (*models.Document, error) {
	if !isSupportedDocumentType(documentType) {
		return nil, fmt.Errorf("unsupported document type: %s", documentType)
	}
	business, err := s.businessRepo.GetByID(ctx, businessID)
	if err != nil {
		return nil, err
	}
	partyType := resolvePartyType(documentType, input.PartyType, input.Direction)
	if err := s.validateParty(ctx, businessID, partyType, input.PartyID); err != nil {
		return nil, err
	}
	projectID := syncProjectIDFromTags(input.ProjectID, input.ReportTags)
	input.ReportTags = mergeProjectIntoTags(input.ReportTags, projectID)
	priceListID, err := resolvePriceListID(
		ctx,
		s.db,
		s.customerRepo,
		s.vendorRepo,
		businessID,
		partyType,
		input.PartyID,
		stringPointer(input.PriceListID),
		nil,
	)
	if err != nil {
		return nil, err
	}
	extraFields := input.ExtraFields
	if extraFields == nil {
		extraFields = map[string]interface{}{}
	}
	if input.CustomFields != nil {
		extraFields["custom_fields"] = input.CustomFields
	}
	if input.AdditionalCharges != nil {
		extraFields["additional_charges"] = input.AdditionalCharges
	}
	if input.DistanceKM > 0 {
		extraFields["distance_source"] = models.DistanceSourceManual
	}

	document := &models.Document{
		BusinessID:            businessID,
		BranchID:              stringPointer(input.BranchID),
		DocumentType:          documentType,
		PartyType:             partyType,
		Status:                coalesceString(input.Status, models.DocumentStatusDraft),
		DraftState:            coalesceString(input.DraftState, models.DocumentDraftStateDraft),
		TaxMode:               coalesceString(input.TaxMode, defaultTaxMode(documentType, input.GSTTreatment)),
		GSTTreatment:          coalesceString(input.GSTTreatment, business.DefaultGSTTreatment),
		PlaceOfSupply:         coalesceString(input.PlaceOfSupply, business.BusinessStateCode),
		PartyGSTIN:            input.PartyGSTIN,
		PartyPAN:              input.PartyPAN,
		PartyStateCode:        input.PartyStateCode,
		SupplyType:            input.SupplyType,
		ExportType:            input.ExportType,
		BillOfSupply:          input.BillOfSupply,
		IssueDate:             input.IssueDate,
		DueDate:               input.DueDate,
		DispatchDate:          input.DispatchDate,
		Currency:              defaultCurrency(coalesceString(input.Currency, business.Currency)),
		ExchangeRate:          defaultExchangeRate(input.ExchangeRate),
		Locale:                coalesceString(input.Locale, "en-IN"),
		SourceLinkage:         mustMarshalMap(input.SourceLinkage),
		Notes:                 input.Notes,
		Terms:                 input.Terms,
		Declaration:           input.Declaration,
		Direction:             input.Direction,
		GenerateEInvoice:      input.GenerateEInvoice,
		GenerateEWayBill:      input.GenerateEWayBill,
		ReverseCharge:         input.ReverseCharge,
		ReverseChargeReason:   input.ReverseChargeReason,
		DispatchFrom:          mustMarshalMap(input.DispatchFrom),
		DispatchTo:            mustMarshalMap(input.DispatchTo),
		DistanceKM:            input.DistanceKM,
		Transporter:           mustMarshalMap(input.Transporter),
		Vehicle:               mustMarshalMap(input.Vehicle),
		MultiVehiclePlan:      mustMarshalMap(input.MultiVehiclePlan),
		ExtraFields:           mustMarshalMap(extraFields),
		ReportTags:            mustMarshalMap(input.ReportTags),
		RenderProfileID:       nil,
		ProfitSnapshotEnabled: true,
		ProjectID:             projectIDPointer(projectID),
		PriceListID:           priceListID,
	}
	if input.OriginSubscriptionID != "" {
		document.OriginSubscriptionID = &input.OriginSubscriptionID
	}
	if input.OriginRunID != "" {
		document.OriginRunID = &input.OriginRunID
	}
	if input.PartyID != "" {
		document.PartyID = &input.PartyID
	}
	if input.RenderProfileID != "" {
		document.RenderProfileID = &input.RenderProfileID
	}
	if err := s.validateBranchAccess(ctx, businessID, input.BranchID); err != nil {
		return nil, err
	}
	if document.IssueDate.IsZero() {
		document.IssueDate = time.Now()
	}
	if document.GSTTreatment == "" {
		document.GSTTreatment = models.DocumentGSTTreatmentRegular
	}
	if documentType == models.DocumentTypeBillOfSupply {
		document.TaxMode = models.DocumentTaxModeNonGST
		document.GSTTreatment = models.DocumentGSTTreatmentComposition
		document.BillOfSupply = true
		if document.Declaration == "" {
			document.Declaration = "Tax not payable under Section 10 or exempt supply as per GST Law."
		}
	}
	if documentType == models.DocumentTypeExpense && document.Direction == "" {
		document.Direction = models.DocumentDirectionInward
	}
	if err := s.applyPartyTaxSnapshot(ctx, businessID, document); err != nil {
		return nil, err
	}
	s.applyDocumentFX(ctx, business, document)

	document.SerialNumber = s.generateSerialNumber(documentType)
	lines, totals, err := s.buildDocumentLines(ctx, business, document, input.Lines)
	if err != nil {
		return nil, err
	}
	document.Lines = lines
	document.Subtotal = totals.Subtotal
	document.DiscountTotal = totals.DiscountTotal
	document.TaxTotal = totals.TaxTotal
	document.CessTotal = totals.CessTotal
	document.Total = totals.Total
	document.BalanceDue = totals.Total
	if documentType == models.DocumentTypeCreditNote || documentType == models.DocumentTypeDebitNote {
		document.BalanceDue = 0
	}
	document.WithholdingTotal, document.TDSTotal, document.TCSTotal = summarizeWithholdings(input.Withholdings)

	return document, nil
}

func (s *DocumentService) validateBranchAccess(ctx context.Context, businessID, branchID string) error {
	if branchID == "" || s.db == nil {
		return nil
	}
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&models.Branch{}).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", branchID, businessID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("branch not found")
	}
	return nil
}

func (s *DocumentService) applyDocumentFX(ctx context.Context, business *models.BusinessProfile, document *models.Document) {
	if business == nil || document == nil {
		return
	}
	baseCurrency := defaultCurrency(business.Currency)
	quoteCurrency := defaultCurrency(document.Currency)
	document.FXBaseCurrency = baseCurrency
	document.FXQuoteCurrency = quoteCurrency
	if strings.EqualFold(baseCurrency, quoteCurrency) {
		document.ExchangeRate = 1
		document.FXProvider = ""
		document.FXMetadata = mustMarshalMap(map[string]interface{}{
			"mode": "base_currency",
		})
		return
	}
	if document.ExchangeRate <= 0 {
		var fxRate models.FXRate
		err := s.db.WithContext(ctx).
			Where("base_currency = ? AND quote_currency = ? AND deleted_at IS NULL", baseCurrency, quoteCurrency).
			Order("fetched_at DESC").
			First(&fxRate).Error
		if err == nil && fxRate.Rate > 0 {
			document.ExchangeRate = fxRate.Rate
			document.FXProvider = fxRate.Provider
			document.FXRateTimestamp = &fxRate.FetchedAt
			document.FXMetadata = fxRate.Metadata
		}
	}
	if document.ExchangeRate <= 0 {
		document.ExchangeRate = 1
	}
	if document.FXMetadata == "" {
		document.FXMetadata = mustMarshalMap(map[string]interface{}{
			"manual_override": document.ExchangeRate > 0,
		})
	}
}

type documentTotals struct {
	Subtotal      float64
	DiscountTotal float64
	TaxTotal      float64
	CessTotal     float64
	Total         float64
}

func (s *DocumentService) buildDocumentLines(ctx context.Context, business *models.BusinessProfile, document *models.Document, inputs []CreateDocumentLineInput) ([]*models.DocumentLine, documentTotals, error) {
	lines := make([]*models.DocumentLine, 0, len(inputs))
	var totals documentTotals
	intraState := business.BusinessStateCode == "" || business.BusinessStateCode == document.PlaceOfSupply
	for _, input := range inputs {
		pricing, err := resolveLinePricing(ctx, s.db, s.productRepo, business.ID, document.PriceListID, input.ProductID, input.VariantID, stringPointer(input.WarehouseID))
		if err != nil {
			return nil, totals, err
		}
		unitPrice := input.UnitPrice
		if unitPrice <= 0 {
			unitPrice = pricing.UnitPrice
		}
		mrp := input.MRP
		if mrp <= 0 {
			mrp = pricing.MRP
		}
		cessRate := input.CessRate
		if cessRate <= 0 {
			cessRate = pricing.CessRate
		}
		line := &models.DocumentLine{
			Description:       input.Description,
			HSNSACCode:        input.HSNSACCode,
			UQCCode:           input.UQCCode,
			Unit:              input.Unit,
			Quantity:          input.Quantity,
			FreeQuantity:      input.FreeQuantity,
			RemainingQuantity: input.Quantity,
			UnitPrice:         unitPrice,
			MRP:               mrp,
			DiscountAmount:    input.DiscountAmount,
			TaxRate:           input.TaxRate,
			CessRate:          cessRate,
			CustomFields:      mustMarshalMap(input.CustomFields),
			ChargeLinkage:     mustMarshalAny(input.ChargeLinkage, "[]"),
			PackingMetadata:   mustMarshalMap(input.PackingMetadata),
			BatchAllocations:  mustMarshalBatchAllocations(input.BatchAllocations),
			SerialIDs:         marshalStringSlice(input.SerialIDs),
			StockEffect:       stockEffectForDocument(document.DocumentType, document.Direction),
		}
		if input.ProductID != "" {
			product, err := s.productRepo.GetByID(ctx, input.ProductID, business.ID)
			if err != nil {
				return nil, totals, err
			}
			line.ProductID = &product.ID
			if line.HSNSACCode == "" {
				line.HSNSACCode = product.HSNSACCode
			}
			if line.Unit == "" {
				line.Unit = firstNonEmpty(product.Unit, product.UQCCode)
			}
			line.CostSnapshot = product.CostPrice
			if product.IsService {
				line.StockEffect = "none"
				line.WarehouseID = nil
			}
		}
		if input.VariantID != "" {
			line.VariantID = &input.VariantID
		}
		if input.WarehouseID != "" {
			line.WarehouseID = &input.WarehouseID
		}
		line.Unit = gst.CanonicalSnapshotUQC(line.Unit, line.UQCCode)
		line.UQCCode = line.Unit

		line.LineSubtotal = (line.Quantity * line.UnitPrice) - line.DiscountAmount
		if line.LineSubtotal < 0 {
			line.LineSubtotal = 0
		}
		if document.TaxMode == models.DocumentTaxModeGST && document.DocumentType != models.DocumentTypeBillOfSupply && document.GSTTreatment != models.DocumentGSTTreatmentComposition && document.GSTTreatment != models.DocumentGSTTreatmentExempt {
			line.TaxAmount = line.LineSubtotal * (line.TaxRate / 100.0)
			if intraState {
				line.CGSTRate = line.TaxRate / 2.0
				line.SGSTRate = line.TaxRate / 2.0
				line.CGSTAmount = line.TaxAmount / 2.0
				line.SGSTAmount = line.TaxAmount / 2.0
			} else {
				line.IGSTRate = line.TaxRate
				line.IGSTAmount = line.TaxAmount
			}
			line.CessAmount = line.LineSubtotal * (line.CessRate / 100.0)
		}
		line.LineTotal = line.LineSubtotal + line.TaxAmount + line.CessAmount
		line.MarginSnapshot = line.LineSubtotal - (line.CostSnapshot * line.Quantity)

		totals.Subtotal += line.LineSubtotal
		totals.DiscountTotal += line.DiscountAmount
		totals.TaxTotal += line.TaxAmount
		totals.CessTotal += line.CessAmount
		totals.Total += line.LineTotal

		lines = append(lines, line)
	}
	return lines, totals, nil
}

func (s *DocumentService) ListByType(ctx context.Context, businessID, documentType string, page, limit int) ([]*models.Document, int64, error) {
	return s.repo.ListByType(ctx, businessID, documentType, page, limit)
}

func (s *DocumentService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Document, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

func (s *DocumentService) GetForWorker(ctx context.Context, id string) (*models.Document, error) {
	return s.repo.GetByIDInternal(ctx, id)
}

func (s *DocumentService) UpdateByType(ctx context.Context, businessID, id, documentType string, input CreateDocumentInput) (*models.Document, error) {
	existing, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return nil, err
	}
	if existing.DocumentType != documentType {
		return nil, fmt.Errorf("document type mismatch")
	}
	if existing.SignedAt != nil {
		return nil, fmt.Errorf("signed documents are immutable")
	}
	if existing.Status != models.DocumentStatusDraft {
		return nil, fmt.Errorf("only draft documents can be updated")
	}
	rebuilt, err := s.buildDocument(ctx, businessID, documentType, input)
	if err != nil {
		return nil, err
	}
	existing.PartyType = rebuilt.PartyType
	existing.PartyID = rebuilt.PartyID
	existing.BranchID = rebuilt.BranchID
	existing.Status = rebuilt.Status
	existing.DraftState = rebuilt.DraftState
	existing.TaxMode = rebuilt.TaxMode
	existing.GSTTreatment = rebuilt.GSTTreatment
	existing.PlaceOfSupply = rebuilt.PlaceOfSupply
	existing.IssueDate = rebuilt.IssueDate
	existing.DueDate = rebuilt.DueDate
	existing.DispatchDate = rebuilt.DispatchDate
	existing.Currency = rebuilt.Currency
	existing.ExchangeRate = rebuilt.ExchangeRate
	existing.FXProvider = rebuilt.FXProvider
	existing.FXBaseCurrency = rebuilt.FXBaseCurrency
	existing.FXQuoteCurrency = rebuilt.FXQuoteCurrency
	existing.FXRateTimestamp = rebuilt.FXRateTimestamp
	existing.FXMetadata = rebuilt.FXMetadata
	existing.Locale = rebuilt.Locale
	existing.SourceLinkage = rebuilt.SourceLinkage
	existing.RenderProfileID = rebuilt.RenderProfileID
	existing.Notes = rebuilt.Notes
	existing.Terms = rebuilt.Terms
	existing.Declaration = rebuilt.Declaration
	existing.Direction = rebuilt.Direction
	existing.GenerateEInvoice = rebuilt.GenerateEInvoice
	existing.GenerateEWayBill = rebuilt.GenerateEWayBill
	existing.ReverseCharge = rebuilt.ReverseCharge
	existing.ReverseChargeReason = rebuilt.ReverseChargeReason
	existing.DispatchFrom = rebuilt.DispatchFrom
	existing.DispatchTo = rebuilt.DispatchTo
	existing.DistanceKM = rebuilt.DistanceKM
	existing.Transporter = rebuilt.Transporter
	existing.Vehicle = rebuilt.Vehicle
	existing.MultiVehiclePlan = rebuilt.MultiVehiclePlan
	existing.ExtraFields = rebuilt.ExtraFields
	existing.ReportTags = rebuilt.ReportTags
	existing.ProjectID = rebuilt.ProjectID
	existing.PriceListID = rebuilt.PriceListID
	existing.OriginSubscriptionID = rebuilt.OriginSubscriptionID
	existing.OriginRunID = rebuilt.OriginRunID
	existing.PartyGSTIN = rebuilt.PartyGSTIN
	existing.PartyPAN = rebuilt.PartyPAN
	existing.PartyStateCode = rebuilt.PartyStateCode
	existing.SupplyType = rebuilt.SupplyType
	existing.ExportType = rebuilt.ExportType
	existing.BillOfSupply = rebuilt.BillOfSupply
	existing.Lines = rebuilt.Lines
	existing.Subtotal = rebuilt.Subtotal
	existing.DiscountTotal = rebuilt.DiscountTotal
	existing.TaxTotal = rebuilt.TaxTotal
	existing.CessTotal = rebuilt.CessTotal
	existing.Total = rebuilt.Total
	existing.BalanceDue = rebuilt.Total - existing.PaidAmount
	if err := s.repo.UpdateDraft(ctx, existing); err != nil {
		return nil, err
	}
	if err := s.syncDocumentWithholdings(ctx, existing, input.Withholdings); err != nil {
		return nil, err
	}
	if existing.Status != models.DocumentStatusDraft {
		if err := s.applyPostCreateSideEffects(ctx, existing); err != nil {
			return nil, err
		}
		if s.taxCompliance != nil {
			if err := s.taxCompliance.EnqueueDocumentCompliance(ctx, existing, "document_update"); err != nil {
				return nil, err
			}
		}
	}
	s.recordRevision(ctx, existing, "updated", nil)
	_ = recordActivityLog(ctx, s.db, businessID, "document", existing.ID, "updated", "", existing, nil, nil)
	return existing, nil
}

func (s *DocumentService) DeleteByType(ctx context.Context, businessID, id, documentType string) error {
	existing, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return err
	}
	if existing.DocumentType != documentType {
		return fmt.Errorf("document type mismatch")
	}
	if existing.SignedAt != nil {
		return fmt.Errorf("signed documents are immutable")
	}
	if existing.Status != models.DocumentStatusDraft {
		return fmt.Errorf("only draft documents can be deleted")
	}
	if err := s.repo.Delete(ctx, existing.ID); err != nil {
		return err
	}
	_ = recordActivityLog(ctx, s.db, businessID, "document", existing.ID, "deleted", "", existing, nil, nil)
	return nil
}

func (s *DocumentService) CancelByType(ctx context.Context, businessID, id, documentType, reason string) (*models.Document, error) {
	document, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return nil, err
	}
	if document.DocumentType != documentType {
		return nil, fmt.Errorf("document type mismatch")
	}
	now := time.Now()
	document.Status = models.DocumentStatusCancelled
	document.CancellationReason = &reason
	document.CancelledAt = &now
	if err := s.repo.Update(ctx, document); err != nil {
		return nil, err
	}
	if err := s.inventory.ReleaseReservations(ctx, document.ID); err != nil {
		return nil, err
	}
	s.recordRevision(ctx, document, "cancelled", map[string]interface{}{"reason": reason})
	_ = recordActivityLog(ctx, s.db, businessID, "document", document.ID, "cancelled", reason, document, nil, nil)
	return document, nil
}

func (s *DocumentService) GetHistory(ctx context.Context, businessID, id string) (*DocumentHistory, error) {
	document, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return nil, err
	}
	links, err := s.repo.ListLinks(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	revisions, _, err := s.repo.ListRevisions(ctx, businessID, id, 1, 100)
	if err != nil {
		return nil, err
	}
	job, _ := s.repo.GetLatestRenderJob(ctx, id)
	return &DocumentHistory{Document: document, Links: links, Revisions: revisions, RenderJob: job}, nil
}

func (s *DocumentService) DuplicateByBusiness(ctx context.Context, businessID, id string) (*models.Document, error) {
	document, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return nil, err
	}
	duplicate := cloneDocument(document)
	duplicate.ID = ""
	duplicate.SerialNumber = s.generateSerialNumber(document.DocumentType)
	duplicate.Status = models.DocumentStatusDraft
	duplicate.DraftState = models.DocumentDraftStateDraft
	duplicate.PDFURL = ""
	duplicate.PDFFilename = ""
	if err := s.repo.Create(ctx, duplicate); err != nil {
		return nil, err
	}
	if err := s.repo.CreateLink(ctx, &models.DocumentLink{
		BusinessID:       businessID,
		SourceDocumentID: document.ID,
		TargetDocumentID: duplicate.ID,
		LinkType:         models.DocumentLinkTypeDuplicateOf,
	}); err != nil {
		return nil, err
	}
	s.recordRevision(ctx, duplicate, "duplicated", map[string]interface{}{"source_document_id": document.ID})
	return duplicate, nil
}

func (s *DocumentService) MergeByBusiness(ctx context.Context, businessID string, input MergeDocumentsInput) (*models.Document, error) {
	if len(input.DocumentIDs) < 2 {
		return nil, fmt.Errorf("at least two documents are required")
	}
	var sourceDocuments []*models.Document
	for _, id := range input.DocumentIDs {
		doc, err := s.repo.GetByID(ctx, id, businessID)
		if err != nil {
			return nil, err
		}
		sourceDocuments = append(sourceDocuments, doc)
	}

	base := sourceDocuments[0]
	for _, doc := range sourceDocuments[1:] {
		if doc.DocumentType != base.DocumentType || doc.Currency != base.Currency || doc.TaxMode != base.TaxMode || doc.PartyType != base.PartyType || pointerStringValue(doc.PartyID) != pointerStringValue(base.PartyID) {
			return nil, fmt.Errorf("documents are not merge-compatible")
		}
	}

	merged := cloneDocument(base)
	merged.ID = ""
	merged.SerialNumber = s.generateSerialNumber(base.DocumentType)
	merged.Status = models.DocumentStatusDraft
	merged.DraftState = models.DocumentDraftStateDraft
	merged.IssueDate = time.Now()
	merged.Lines = nil
	merged.Subtotal = 0
	merged.DiscountTotal = 0
	merged.TaxTotal = 0
	merged.CessTotal = 0
	merged.Total = 0
	merged.PaidAmount = 0
	merged.BalanceDue = 0

	for _, doc := range sourceDocuments {
		for _, line := range doc.Lines {
			cloned := *line
			cloned.ID = ""
			cloned.DocumentID = ""
			merged.Lines = append(merged.Lines, &cloned)
			merged.Subtotal += cloned.LineSubtotal
			merged.DiscountTotal += cloned.DiscountAmount
			merged.TaxTotal += cloned.TaxAmount
			merged.CessTotal += cloned.CessAmount
			merged.Total += cloned.LineTotal
		}
	}
	merged.BalanceDue = merged.Total
	if err := s.repo.Create(ctx, merged); err != nil {
		return nil, err
	}
	for _, doc := range sourceDocuments {
		if err := s.repo.CreateLink(ctx, &models.DocumentLink{
			BusinessID:       businessID,
			SourceDocumentID: doc.ID,
			TargetDocumentID: merged.ID,
			LinkType:         models.DocumentLinkTypeMergedFrom,
		}); err != nil {
			return nil, err
		}
	}
	s.recordRevision(ctx, merged, "merged", map[string]interface{}{"source_document_ids": input.DocumentIDs})
	_ = recordActivityLog(ctx, s.db, businessID, "document", merged.ID, "merged", "", merged, nil, map[string]interface{}{"source_document_ids": input.DocumentIDs})
	return merged, nil
}

func (s *DocumentService) ConvertByBusiness(ctx context.Context, businessID, id string, input ConvertDocumentInput) (*models.Document, error) {
	source, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return nil, err
	}
	if !isAllowedConversion(source.DocumentType, input.TargetDocumentType) {
		return nil, fmt.Errorf("conversion from %s to %s is not allowed", source.DocumentType, input.TargetDocumentType)
	}

	qtyByLine := make(map[string]float64)
	for _, requested := range input.LineQuantities {
		qtyByLine[requested.SourceLineID] = requested.Quantity
	}

	converted := cloneDocument(source)
	converted.ID = ""
	converted.DocumentType = input.TargetDocumentType
	converted.Status = coalesceString(input.Status, models.DocumentStatusDraft)
	converted.DraftState = models.DocumentDraftStateDraft
	converted.SerialNumber = s.generateSerialNumber(input.TargetDocumentType)
	converted.IssueDate = input.IssueDate
	if converted.IssueDate.IsZero() {
		converted.IssueDate = time.Now()
	}
	converted.Lines = nil
	converted.Subtotal = 0
	converted.DiscountTotal = 0
	converted.TaxTotal = 0
	converted.CessTotal = 0
	converted.Total = 0
	converted.PaidAmount = 0
	converted.BalanceDue = 0
	converted.PDFURL = ""
	converted.PDFFilename = ""

	type linkCandidate struct {
		sourceLineID      string
		targetLineIndex   int
		quantityUsed      float64
		quantityRemaining float64
	}
	linkCandidates := make([]linkCandidate, 0, len(source.Lines))
	allUsed := true
	for _, line := range source.Lines {
		requestedQty := line.RemainingQuantity
		if qty, ok := qtyByLine[line.ID]; ok {
			requestedQty = qty
		}
		if requestedQty <= 0 {
			allUsed = false
			continue
		}
		if requestedQty > line.RemainingQuantity {
			requestedQty = line.RemainingQuantity
		}
		cloned := *line
		cloned.ID = ""
		cloned.DocumentID = ""
		cloned.Quantity = requestedQty
		cloned.RemainingQuantity = requestedQty
		if line.Quantity > 0 {
			ratio := requestedQty / line.Quantity
			cloned.LineSubtotal = line.LineSubtotal * ratio
			cloned.DiscountAmount = line.DiscountAmount * ratio
			cloned.TaxAmount = line.TaxAmount * ratio
			cloned.CessAmount = line.CessAmount * ratio
			cloned.LineTotal = line.LineTotal * ratio
			cloned.CostSnapshot = line.CostSnapshot
			cloned.MarginSnapshot = line.MarginSnapshot * ratio
		}
		converted.Lines = append(converted.Lines, &cloned)
		linkCandidates = append(linkCandidates, linkCandidate{
			sourceLineID:    line.ID,
			targetLineIndex: len(converted.Lines) - 1,
			quantityUsed:    requestedQty,
		})
		converted.Subtotal += cloned.LineSubtotal
		converted.DiscountTotal += cloned.DiscountAmount
		converted.TaxTotal += cloned.TaxAmount
		converted.CessTotal += cloned.CessAmount
		converted.Total += cloned.LineTotal

		line.RemainingQuantity = line.RemainingQuantity - requestedQty
		linkCandidates[len(linkCandidates)-1].quantityRemaining = line.RemainingQuantity
		if line.RemainingQuantity > 0 {
			allUsed = false
		}
	}
	if len(converted.Lines) == 0 {
		return nil, fmt.Errorf("no convertible line items found")
	}
	converted.BalanceDue = converted.Total
	if err := s.repo.Create(ctx, converted); err != nil {
		return nil, err
	}

	if allUsed {
		source.Status = models.DocumentStatusFullyConverted
	} else {
		source.Status = models.DocumentStatusPartiallyConverted
	}
	if err := s.repo.Update(ctx, source); err != nil {
		return nil, err
	}
	for _, candidate := range linkCandidates {
		targetLine := converted.Lines[candidate.targetLineIndex]
		sourceLineID := candidate.sourceLineID
		targetLineID := targetLine.ID
		if err := s.repo.CreateLink(ctx, &models.DocumentLink{
			BusinessID:        businessID,
			SourceDocumentID:  source.ID,
			SourceLineID:      &sourceLineID,
			TargetDocumentID:  converted.ID,
			TargetLineID:      &targetLineID,
			LinkType:          models.DocumentLinkTypeConvertedTo,
			QuantityUsed:      candidate.quantityUsed,
			QuantityRemaining: candidate.quantityRemaining,
		}); err != nil {
			return nil, err
		}
	}
	if converted.Status != models.DocumentStatusDraft {
		if err := s.applyPostCreateSideEffects(ctx, converted); err != nil {
			return nil, err
		}
	}
	s.recordRevision(ctx, source, "converted_source", map[string]interface{}{"target_document_id": converted.ID, "target_document_type": input.TargetDocumentType})
	s.recordRevision(ctx, converted, "converted_target", map[string]interface{}{"source_document_id": source.ID, "source_document_type": source.DocumentType})
	_ = recordActivityLog(ctx, s.db, businessID, "document", converted.ID, "converted", "", converted, nil, map[string]interface{}{"source_document_id": source.ID, "target_document_type": input.TargetDocumentType})
	return converted, nil
}

func (s *DocumentService) RequestRenderByBusiness(ctx context.Context, businessID, documentID string, input RenderDocumentInput) (*models.DocumentRenderJob, error) {
	document, err := s.repo.GetByID(ctx, documentID, businessID)
	if err != nil {
		return nil, err
	}
	job := &models.DocumentRenderJob{
		ID:              uuid.NewString(),
		DocumentID:      models.StringPointer(document.ID),
		BusinessID:      businessID,
		Status:          models.RenderJobStatusQueued,
		Locale:          coalesceString(input.Locale, document.Locale),
		TemplateVersion: "v1",
	}
	job.ObjectKey = fmt.Sprintf("documents/%s/%s/%s.pdf", businessID, document.ID, job.ID)
	if input.RenderProfileID != "" {
		job.RenderProfileID = &input.RenderProfileID
	} else if document.RenderProfileID != nil {
		job.RenderProfileID = document.RenderProfileID
	}
	if err := s.repo.CreateRenderJob(ctx, job); err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]string{
		"type":          "generate_document_pdf",
		"document_id":   document.ID,
		"render_job_id": job.ID,
	})
	if err != nil {
		return nil, err
	}
	_, err = s.sqs.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(s.cfg.SQS.InvoiceQueue),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return nil, err
	}
	s.recordRevision(ctx, document, "render_requested", map[string]interface{}{"render_job_id": job.ID, "locale": job.Locale})
	return job, nil
}

func (s *DocumentService) GetRenderJobByBusiness(ctx context.Context, businessID, jobID string) (*models.DocumentRenderJob, error) {
	return s.repo.GetRenderJob(ctx, businessID, jobID)
}

func (s *DocumentService) GetRenderProfileByBusiness(ctx context.Context, businessID, profileID string) (*models.RenderProfile, error) {
	profile, err := s.repo.GetRenderProfile(ctx, businessID, profileID)
	if err != nil {
		return nil, err
	}
	if err := s.migrateLegacyRenderProfilePassword(ctx, profile); err != nil {
		return nil, err
	}
	return publicRenderProfile(profile), nil
}

func (s *DocumentService) GetDefaultRenderProfileByBusiness(ctx context.Context, businessID string) (*models.RenderProfile, error) {
	profile, err := s.repo.GetDefaultRenderProfile(ctx, businessID)
	if err != nil {
		return nil, err
	}
	if err := s.migrateLegacyRenderProfilePassword(ctx, profile); err != nil {
		return nil, err
	}
	return publicRenderProfile(profile), nil
}

// GetRenderProfileForRenderingByBusiness is the trusted renderer boundary. It
// returns plaintext password material only in the transient, non-serializable
// Password field of the returned copy.
func (s *DocumentService) GetRenderProfileForRenderingByBusiness(
	ctx context.Context,
	businessID, profileID string,
) (*models.RenderProfile, error) {
	profile, err := s.repo.GetRenderProfile(ctx, businessID, profileID)
	if err != nil {
		return nil, err
	}
	return s.resolveRenderProfilePassword(ctx, profile)
}

// GetDefaultRenderProfileForRenderingByBusiness is the default-profile form
// of the trusted renderer boundary.
func (s *DocumentService) GetDefaultRenderProfileForRenderingByBusiness(
	ctx context.Context,
	businessID string,
) (*models.RenderProfile, error) {
	profile, err := s.repo.GetDefaultRenderProfile(ctx, businessID)
	if err != nil {
		return nil, err
	}
	return s.resolveRenderProfilePassword(ctx, profile)
}

// BackfillLegacyRenderProfilePasswords encrypts all released plaintext rows in
// bounded batches. It returns only a count so secret material cannot reach
// startup logs or metrics.
func (s *DocumentService) BackfillLegacyRenderProfilePasswords(ctx context.Context) (int, error) {
	const batchSize = 100
	migrated := 0
	for {
		profiles, err := s.repo.ListLegacyRenderProfiles(ctx, batchSize)
		if err != nil {
			return migrated, fmt.Errorf("list legacy render profile passwords: %w", err)
		}
		if len(profiles) == 0 {
			return migrated, nil
		}
		for _, profile := range profiles {
			if err := s.migrateLegacyRenderProfilePassword(ctx, profile); err != nil {
				return migrated, err
			}
			migrated++
		}
	}
}

func (s *DocumentService) CreateRenderProfileByBusiness(ctx context.Context, businessID string, input CreateRenderProfileInput) (*models.RenderProfile, error) {
	if err := requireMutationPermission(ctx, s.permissions, businessID, PermissionRenderProfilesCreate); err != nil {
		return nil, err
	}
	profile := &models.RenderProfile{
		ID:                uuid.NewString(),
		BusinessID:        businessID,
		Name:              input.Name,
		HeaderHTML:        input.HeaderHTML,
		FooterHTML:        input.FooterHTML,
		WatermarkText:     input.WatermarkText,
		BannerText:        input.BannerText,
		FontFamily:        coalesceString(input.FontFamily, "Noto Sans"),
		PageSize:          coalesceString(input.PageSize, "A4"),
		LayoutConfig:      mustMarshalMap(input.LayoutConfig),
		PasswordProtected: input.PasswordProtected,
		CopyAllowed:       boolValueOrDefault(input.CopyAllowed, true),
		PrintAllowed:      boolValueOrDefault(input.PrintAllowed, true),
		CustomLabels:      mustMarshalMap(input.CustomLabels),
		VisibilityConfig:  mustMarshalMap(input.VisibilityConfig),
		IsDefault:         input.IsDefault,
	}
	if input.Password != "" {
		ciphertext, err := s.encryptRenderProfilePassword(ctx, businessID, profile.ID, input.Password)
		if err != nil {
			return nil, err
		}
		profile.PasswordCiphertext = &ciphertext
	}
	if err := s.repo.CreateRenderProfile(ctx, profile); err != nil {
		return nil, err
	}
	return publicRenderProfile(profile), nil
}

func (s *DocumentService) ListRenderProfilesByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.RenderProfile, int64, error) {
	profiles, total, err := s.repo.ListRenderProfiles(ctx, businessID, page, limit)
	if err != nil {
		return nil, 0, err
	}
	result := make([]*models.RenderProfile, len(profiles))
	for i, profile := range profiles {
		if err := s.migrateLegacyRenderProfilePassword(ctx, profile); err != nil {
			return nil, 0, err
		}
		result[i] = publicRenderProfile(profile)
	}
	return result, total, nil
}

func (s *DocumentService) UpdateRenderProfileByBusiness(ctx context.Context, businessID, profileID string, input UpdateRenderProfileInput) (*models.RenderProfile, error) {
	if err := requireMutationPermission(ctx, s.permissions, businessID, PermissionRenderProfilesUpdate); err != nil {
		return nil, err
	}
	profile, err := s.repo.GetRenderProfile(ctx, businessID, profileID)
	if err != nil {
		return nil, err
	}
	if input.Password == "" {
		if err := s.migrateLegacyRenderProfilePassword(ctx, profile); err != nil {
			return nil, err
		}
	}
	if input.Name != "" {
		profile.Name = input.Name
	}
	if input.HeaderHTML != "" {
		profile.HeaderHTML = input.HeaderHTML
	}
	if input.FooterHTML != "" {
		profile.FooterHTML = input.FooterHTML
	}
	if input.WatermarkText != "" {
		profile.WatermarkText = input.WatermarkText
	}
	if input.BannerText != "" {
		profile.BannerText = input.BannerText
	}
	if input.FontFamily != "" {
		profile.FontFamily = input.FontFamily
	}
	if input.PageSize != "" {
		profile.PageSize = input.PageSize
	}
	if input.LayoutConfig != nil {
		profile.LayoutConfig = mustMarshalMap(input.LayoutConfig)
	}
	if input.PasswordProtected != nil {
		profile.PasswordProtected = *input.PasswordProtected
	}
	if input.Password != "" {
		ciphertext, err := s.encryptRenderProfilePassword(ctx, businessID, profile.ID, input.Password)
		if err != nil {
			return nil, err
		}
		profile.PasswordCiphertext = &ciphertext
		profile.LegacyPassword = nil
	}
	if input.CopyAllowed != nil {
		profile.CopyAllowed = *input.CopyAllowed
	}
	if input.PrintAllowed != nil {
		profile.PrintAllowed = *input.PrintAllowed
	}
	if input.CustomLabels != nil {
		profile.CustomLabels = mustMarshalMap(input.CustomLabels)
	}
	if input.VisibilityConfig != nil {
		profile.VisibilityConfig = mustMarshalMap(input.VisibilityConfig)
	}
	if input.IsDefault != nil {
		profile.IsDefault = *input.IsDefault
	}
	if err := s.repo.UpdateRenderProfile(ctx, profile); err != nil {
		return nil, err
	}
	return publicRenderProfile(profile), nil
}

func publicRenderProfile(profile *models.RenderProfile) *models.RenderProfile {
	if profile == nil {
		return nil
	}
	result := *profile
	result.PasswordConfigured = renderProfilePasswordConfigured(profile)
	result.Password = ""
	result.LegacyPassword = nil
	result.PasswordCiphertext = nil
	return &result
}

func renderProfilePasswordConfigured(profile *models.RenderProfile) bool {
	return profile != nil &&
		((profile.PasswordCiphertext != nil && *profile.PasswordCiphertext != "") ||
			(profile.LegacyPassword != nil && *profile.LegacyPassword != "") ||
			profile.Password != "")
}

func (s *DocumentService) migrateLegacyRenderProfilePassword(
	ctx context.Context,
	profile *models.RenderProfile,
) error {
	if profile == nil || profile.LegacyPassword == nil || *profile.LegacyPassword == "" {
		return nil
	}
	ciphertext, err := s.encryptRenderProfilePassword(
		ctx,
		profile.BusinessID,
		profile.ID,
		*profile.LegacyPassword,
	)
	if err != nil {
		return fmt.Errorf("migrate legacy render profile password: %w", err)
	}
	migrated, err := s.repo.MigrateRenderProfilePassword(
		ctx,
		profile.BusinessID,
		profile.ID,
		*profile.LegacyPassword,
		ciphertext,
	)
	if err != nil {
		return fmt.Errorf("store migrated render profile password: %w", err)
	}
	if !migrated {
		return errors.New("render profile password changed during migration")
	}
	profile.PasswordCiphertext = &ciphertext
	profile.LegacyPassword = nil
	return nil
}

func (s *DocumentService) resolveRenderProfilePassword(
	ctx context.Context,
	profile *models.RenderProfile,
) (*models.RenderProfile, error) {
	if profile == nil {
		return nil, nil
	}
	plaintext := ""
	if profile.LegacyPassword != nil && *profile.LegacyPassword != "" {
		plaintext = *profile.LegacyPassword
	} else if profile.PasswordCiphertext != nil && *profile.PasswordCiphertext != "" {
		var err error
		plaintext, err = s.decryptRenderProfilePassword(
			ctx,
			profile.BusinessID,
			profile.ID,
			*profile.PasswordCiphertext,
		)
		if err != nil {
			return nil, fmt.Errorf("decrypt render profile password: %w", err)
		}
	}
	if err := s.migrateLegacyRenderProfilePassword(ctx, profile); err != nil {
		return nil, err
	}
	result := publicRenderProfile(profile)
	result.Password = plaintext
	return result, nil
}

func (s *DocumentService) DeleteRenderProfileByBusiness(ctx context.Context, businessID, profileID string) error {
	if err := requireMutationPermission(ctx, s.permissions, businessID, PermissionRenderProfilesDelete); err != nil {
		return err
	}
	return s.repo.DeleteRenderProfile(ctx, businessID, profileID)
}

func (s *DocumentService) GetPDFURLByBusiness(ctx context.Context, businessID, documentID string) (string, error) {
	document, err := s.repo.GetByID(ctx, documentID, businessID)
	if err != nil {
		return "", err
	}
	if document.PDFURL != "" {
		return document.PDFURL, nil
	}
	job, err := s.repo.GetLatestRenderJob(ctx, documentID)
	if err != nil || job.OutputURL == "" {
		return "", fmt.Errorf("PDF not yet generated")
	}
	return job.OutputURL, nil
}

func (s *DocumentService) UpdateRenderedPDF(ctx context.Context, documentID, jobID, pdfURL, filename string) error {
	if err := s.repo.UpdatePDF(ctx, documentID, pdfURL, filename); err != nil {
		return err
	}
	if jobID == "" {
		return nil
	}
	job, err := s.repo.GetRenderJob(ctx, "", jobID)
	if err != nil {
		return nil
	}
	now := time.Now()
	job.Status = models.RenderJobStatusCompleted
	job.OutputURL = pdfURL
	job.OutputFilename = filename
	job.CompletedAt = &now
	job.ErrorMessage = ""
	if err := s.repo.UpdateRenderJob(ctx, job); err != nil {
		return err
	}
	if document, err := s.repo.GetByIDInternal(ctx, documentID); err == nil {
		document.PDFURL = pdfURL
		document.PDFFilename = filename
		s.recordRevision(ctx, document, "render_completed", map[string]interface{}{"render_job_id": jobID, "filename": filename})
	}
	return nil
}

func (s *DocumentService) MirrorLegacyInvoice(ctx context.Context, invoice *models.Invoice) error {
	if invoice == nil {
		return nil
	}
	doc, err := s.repo.GetByIDInternal(ctx, invoice.ID)
	if err != nil && err.Error() != "document not found" {
		return err
	}
	if doc == nil {
		doc = &models.Document{ID: invoice.ID}
	}
	doc.BusinessID = invoice.BusinessID
	doc.DocumentType = models.DocumentTypeSalesInvoice
	doc.PartyType = models.DocumentPartyTypeCustomer
	doc.PartyID = invoice.CustomerID
	doc.Status = legacyInvoiceStatusToDocument(invoice.Status)
	doc.DraftState = models.DocumentDraftStateFinal
	if invoice.Status == "draft" {
		doc.DraftState = models.DocumentDraftStateDraft
	}
	doc.TaxMode = models.DocumentTaxModeNonGST
	if invoice.Tax > 0 {
		doc.TaxMode = models.DocumentTaxModeGST
	}
	doc.GSTTreatment = models.DocumentGSTTreatmentRegular
	doc.SerialNumber = models.StringValue(invoice.InvoiceNo)
	doc.IssueDate = invoice.InvoiceDate
	doc.DueDate = &invoice.DueDate
	doc.Currency = defaultCurrency(invoice.Currency)
	doc.Locale = "en-IN"
	doc.ProjectID = invoice.ProjectID
	doc.PriceListID = invoice.PriceListID
	doc.RenderProfileID = invoice.RenderProfileID
	doc.OriginSubscriptionID = invoice.OriginSubscriptionID
	doc.OriginRunID = invoice.OriginRunID
	doc.SignedAt = invoice.SignedAt
	doc.SignedByProfileID = invoice.SignedByProfileID
	doc.SignMetadata = invoice.SignMetadata
	doc.PDFURL = invoice.PDFURL
	doc.PDFFilename = invoice.PDFFilename
	doc.Notes = invoice.Notes
	doc.Subtotal = invoice.Subtotal
	doc.TaxTotal = invoice.Tax
	doc.Total = invoice.Total
	doc.PaidAmount = invoice.PaidAmount
	doc.BalanceDue = invoice.BalanceDue
	doc.ProfitSnapshotEnabled = true
	taxProfile := unmarshalJSONMap(invoice.TaxProfile)
	if value := readStringCandidate(taxProfile, "gst_treatment"); value != "" {
		doc.GSTTreatment = value
	}
	if value := readStringCandidate(taxProfile, "place_of_supply"); value != "" {
		doc.PlaceOfSupply = value
	}
	doc.BillOfSupply = readBoolCandidate(taxProfile, "bill_of_supply")
	doc.ExportType = readStringCandidate(taxProfile, "export_type")
	doc.SupplyType = readStringCandidate(taxProfile, "supply_type")
	doc.PartyGSTIN = readStringCandidate(taxProfile, "counterparty_gstin")
	doc.PartyPAN = coalesceString(readStringCandidate(taxProfile, "counterparty_pan"), parsePANFromGSTIN(doc.PartyGSTIN))
	doc.PartyStateCode = readStringCandidate(taxProfile, "counterparty_state_code")
	doc.GenerateEInvoice = readBoolCandidate(taxProfile, "generate_einvoice")
	doc.GenerateEWayBill = readBoolCandidate(taxProfile, "generate_ewaybill")
	doc.ReverseCharge = readBoolCandidate(taxProfile, "reverse_charge")
	doc.ReverseChargeReason = readStringCandidate(taxProfile, "reverse_charge_reason")
	doc.DispatchFrom = mustMarshalMap(nestedMap(taxProfile, "dispatch_from"))
	doc.DispatchTo = mustMarshalMap(nestedMap(taxProfile, "dispatch_to"))
	doc.DistanceKM = floatValue(taxProfile["distance_km"])
	doc.Transporter = mustMarshalMap(nestedMap(taxProfile, "transporter"))
	doc.Vehicle = mustMarshalMap(nestedMap(taxProfile, "vehicle"))
	doc.MultiVehiclePlan = mustMarshalMap(nestedMap(taxProfile, "multi_vehicle_plan"))
	reportTags := nestedMap(taxProfile, "report_tags")
	projectID := normalizeProjectID(extractProjectIDFromTags(reportTags))
	if invoice.ProjectID != nil && *invoice.ProjectID != "" {
		projectID = *invoice.ProjectID
	}
	reportTags = mergeProjectIntoTags(reportTags, projectID)
	doc.ReportTags = mustMarshalMap(reportTags)
	doc.ProjectID = projectIDPointer(projectID)
	if sourceLinkage := nestedMap(taxProfile, "source_linkage"); len(sourceLinkage) > 0 {
		doc.SourceLinkage = mustMarshalMap(sourceLinkage)
	}
	if doc.BillOfSupply {
		doc.DocumentType = models.DocumentTypeBillOfSupply
		doc.TaxMode = models.DocumentTaxModeNonGST
	}
	doc.Lines = nil
	for _, item := range invoice.Items {
		line := &models.DocumentLine{
			ID:                item.ID,
			DocumentID:        invoice.ID,
			ProductID:         item.ProductID,
			VariantID:         item.VariantID,
			Description:       item.Description,
			HSNSACCode:        item.HSNSACCode,
			Unit:              item.Unit,
			WarehouseID:       item.WarehouseID,
			Quantity:          item.Quantity,
			FreeQuantity:      item.FreeQuantity,
			RemainingQuantity: item.Quantity,
			UnitPrice:         item.UnitPrice,
			MRP:               item.MRP,
			DiscountAmount:    item.Discount,
			TaxRate:           item.TaxRate,
			CessRate:          item.CessRate,
			CessAmount:        item.CessAmount,
			CustomFields:      item.CustomFields,
			ChargeLinkage:     item.ChargeSnapshot,
			TaxAmount:         item.Total - ((item.Quantity * item.UnitPrice) - item.Discount) - item.CessAmount,
			LineSubtotal:      (item.Quantity * item.UnitPrice) - item.Discount,
			LineTotal:         item.Total,
			BatchAllocations:  item.BatchAllocations,
			SerialIDs:         item.SerialIDs,
			StockEffect:       "out",
		}
		if item.ProductID != nil {
			if product, productErr := s.productRepo.GetByID(ctx, *item.ProductID, invoice.BusinessID); productErr == nil {
				line.CostSnapshot = product.CostPrice
				line.MarginSnapshot = line.LineSubtotal - (product.CostPrice * item.Quantity)
				if line.HSNSACCode == "" {
					line.HSNSACCode = product.HSNSACCode
				}
				if line.Unit == "" {
					line.Unit = firstNonEmpty(product.Unit, product.UQCCode)
				}
			}
		}
		line.Unit = gst.CanonicalSnapshotUQC(line.Unit, item.Unit)
		line.UQCCode = line.Unit
		doc.Lines = append(doc.Lines, line)
	}
	doc.WithholdingTotal, doc.TDSTotal, doc.TCSTotal = summarizeWithholdings(mapSliceToWithholdings(readMapSlice(taxProfile, "tcs")))
	if doc.CreatedAt.IsZero() {
		if err := s.repo.Create(ctx, doc); err != nil {
			return err
		}
		if err := s.syncDocumentWithholdings(ctx, doc, mapSliceToWithholdings(readMapSlice(taxProfile, "tcs"))); err != nil {
			return err
		}
		s.recordRevision(ctx, doc, "legacy_sync_created", nil)
		return nil
	}
	if err := s.repo.Update(ctx, doc); err != nil {
		return err
	}
	if err := s.syncDocumentWithholdings(ctx, doc, mapSliceToWithholdings(readMapSlice(taxProfile, "tcs"))); err != nil {
		return err
	}
	s.recordRevision(ctx, doc, "legacy_sync_updated", nil)
	return nil
}

func (s *DocumentService) SyncLegacyInvoicePayment(ctx context.Context, invoiceID string, paidAmount, balanceDue float64, status string) error {
	doc, err := s.repo.GetByIDInternal(ctx, invoiceID)
	if err != nil {
		return nil
	}
	doc.PaidAmount = paidAmount
	doc.BalanceDue = balanceDue
	doc.Status = legacyInvoiceStatusToDocument(status)
	if err := s.repo.Update(ctx, doc); err != nil {
		return err
	}
	s.recordRevision(ctx, doc, "legacy_payment_synced", map[string]interface{}{"paid_amount": paidAmount, "balance_due": balanceDue})
	return nil
}

func (s *DocumentService) SyncLegacyInvoicePDF(ctx context.Context, invoiceID, pdfURL, filename string) error {
	if err := s.repo.UpdatePDF(ctx, invoiceID, pdfURL, filename); err != nil {
		return err
	}
	if document, err := s.repo.GetByIDInternal(ctx, invoiceID); err == nil {
		document.PDFURL = pdfURL
		document.PDFFilename = filename
		s.recordRevision(ctx, document, "legacy_pdf_synced", map[string]interface{}{"filename": filename})
	}
	return nil
}

func (s *DocumentService) MarkRenderJobProcessing(ctx context.Context, businessID, jobID string) error {
	if jobID == "" {
		return nil
	}
	job, err := s.repo.GetRenderJob(ctx, businessID, jobID)
	if err != nil {
		return err
	}
	job.Status = models.RenderJobStatusProcessing
	job.ErrorMessage = ""
	job.CompletedAt = nil
	return s.repo.UpdateRenderJob(ctx, job)
}

func (s *DocumentService) ClaimGenericRender(
	ctx context.Context,
	businessID, jobID, owner string,
	now, leaseUntil time.Time,
) (interfaces.GenericRenderClaimState, error) {
	repository, ok := s.repo.(interfaces.GenericRenderRepository)
	if !ok {
		return "", errors.New("generic render repository is not configured")
	}
	return repository.ClaimGenericRender(ctx, businessID, jobID, owner, now, leaseUntil)
}

func (s *DocumentService) FailGenericRender(
	ctx context.Context,
	businessID, jobID, owner, errorMessage string,
) error {
	repository, ok := s.repo.(interfaces.GenericRenderRepository)
	if !ok {
		return errors.New("generic render repository is not configured")
	}
	return repository.FailGenericRender(ctx, businessID, jobID, owner, errorMessage)
}

func (s *DocumentService) CompleteGenericRender(
	ctx context.Context,
	businessID, documentID, jobID, owner, objectKey, pdfURL, filename string,
	now time.Time,
) (bool, error) {
	repository, ok := s.repo.(interfaces.GenericRenderRepository)
	if !ok {
		return false, errors.New("generic render repository is not configured")
	}
	return repository.CompleteGenericRender(ctx, businessID, documentID, jobID, owner, objectKey, pdfURL, filename, now)
}

func (s *DocumentService) FailRenderJob(ctx context.Context, businessID, jobID, errorMessage string) error {
	if jobID == "" {
		return nil
	}
	job, err := s.repo.GetRenderJob(ctx, businessID, jobID)
	if err != nil {
		return err
	}
	job.Status = models.RenderJobStatusFailed
	job.ErrorMessage = errorMessage
	job.CompletedAt = nil
	return s.repo.UpdateRenderJob(ctx, job)
}

func (s *DocumentService) ObsoletePreviewRender(
	ctx context.Context,
	businessID, jobID, owner string,
) error {
	repository, ok := s.repo.(interfaces.PreviewRenderRepository)
	if !ok {
		return errors.New("preview render repository is not configured")
	}
	return repository.ObsoletePreviewRender(ctx, businessID, jobID, owner)
}

func (s *DocumentService) ClaimPreviewRender(
	ctx context.Context,
	businessID, jobID string,
	sourceVersion int,
	owner string,
	now, leaseUntil time.Time,
) (interfaces.PreviewRenderClaimState, error) {
	repository, ok := s.repo.(interfaces.PreviewRenderRepository)
	if !ok {
		return "", errors.New("preview render repository is not configured")
	}
	return repository.ClaimPreviewRender(ctx, businessID, jobID, sourceVersion, owner, now, leaseUntil)
}

func (s *DocumentService) FailPreviewRender(
	ctx context.Context,
	businessID, jobID, owner, errorMessage string,
) error {
	repository, ok := s.repo.(interfaces.PreviewRenderRepository)
	if !ok {
		return errors.New("preview render repository is not configured")
	}
	return repository.FailPreviewRender(ctx, businessID, jobID, owner, errorMessage)
}

func (s *DocumentService) CompletePreviewRender(
	ctx context.Context,
	businessID, jobID string,
	sourceVersion int,
	owner string,
	claimedObjectKey, selectedObjectKey, filename string,
) (bool, error) {
	repository, ok := s.repo.(interfaces.PreviewRenderRepository)
	if !ok {
		return false, errors.New("preview render repository is not configured")
	}
	return repository.CompletePreviewRender(
		ctx,
		businessID,
		jobID,
		sourceVersion,
		owner,
		claimedObjectKey,
		selectedObjectKey,
		filename,
	)
}

func (s *DocumentService) VerifyRenderLease(
	ctx context.Context,
	businessID, jobID string,
	kind models.RenderKind,
	owner string,
	now time.Time,
) error {
	repository, ok := s.repo.(interfaces.RenderLeaseRepository)
	if !ok {
		return errors.New("render lease repository is not configured")
	}
	return repository.VerifyRenderLease(ctx, businessID, jobID, kind, owner, now)
}

func (s *DocumentService) ClaimFinalRender(
	ctx context.Context,
	businessID, jobID string,
	sourceVersion int,
	owner string,
	now, leaseUntil time.Time,
) (interfaces.FinalRenderClaimState, error) {
	repository, ok := s.repo.(interfaces.FinalRenderRepository)
	if !ok {
		return "", errors.New("final render repository is not configured")
	}
	return repository.ClaimFinalRender(ctx, businessID, jobID, sourceVersion, owner, now, leaseUntil)
}

func (s *DocumentService) LoadFinalRenderSnapshot(
	ctx context.Context,
	businessID, invoiceID, jobID string,
	sourceVersion int,
) (*models.Document, error) {
	repository, ok := s.repo.(interfaces.FinalRenderRepository)
	if !ok {
		return nil, errors.New("final render repository is not configured")
	}
	return repository.LoadFinalRenderSnapshot(
		ctx,
		businessID,
		invoiceID,
		jobID,
		sourceVersion,
	)
}

func (s *DocumentService) FailFinalRender(
	ctx context.Context,
	businessID, jobID, owner, errorMessage string,
) error {
	repository, ok := s.repo.(interfaces.FinalRenderRepository)
	if !ok {
		return errors.New("final render repository is not configured")
	}
	return repository.FailFinalRender(ctx, businessID, jobID, owner, errorMessage)
}

func (s *DocumentService) CompleteFinalRender(
	ctx context.Context,
	businessID, invoiceID, jobID string,
	sourceVersion int,
	owner string,
	objectKey, filename string,
) (bool, error) {
	repository, ok := s.repo.(interfaces.FinalRenderRepository)
	if !ok {
		return false, errors.New("final render repository is not configured")
	}
	return repository.CompleteFinalRender(
		ctx,
		businessID,
		invoiceID,
		jobID,
		sourceVersion,
		owner,
		objectKey,
		filename,
	)
}

func (s *DocumentService) applyPostCreateSideEffects(ctx context.Context, document *models.Document) error {
	if err := s.inventory.ApplyDocument(ctx, document); err != nil {
		return err
	}
	if _, err := s.journals.CreateAutoJournalForDocument(ctx, document); err != nil {
		return err
	}
	if document.DocumentType == models.DocumentTypeShippingLabel {
		if _, err := s.shipping.EnsureLabelForDocument(ctx, document); err != nil {
			return err
		}
	}
	return nil
}

func (s *DocumentService) validateParty(ctx context.Context, businessID, partyType, partyID string) error {
	if partyType == models.DocumentPartyTypeManual {
		return nil
	}
	if partyID == "" {
		return fmt.Errorf("party_id is required")
	}
	switch partyType {
	case models.DocumentPartyTypeCustomer:
		_, err := s.customerRepo.GetByID(ctx, partyID, businessID)
		return err
	case models.DocumentPartyTypeVendor:
		_, err := s.vendorRepo.GetByID(ctx, partyID, businessID)
		return err
	default:
		return fmt.Errorf("unsupported party type: %s", partyType)
	}
}

func (s *DocumentService) generateSerialNumber(documentType string) string {
	prefix := map[string]string{
		models.DocumentTypeSalesInvoice:    "SI",
		models.DocumentTypePurchaseInvoice: "PI",
		models.DocumentTypePurchaseOrder:   "PO",
		models.DocumentTypeSalesOrder:      "SO",
		models.DocumentTypeQuotation:       "QT",
		models.DocumentTypeProformaInvoice: "PF",
		models.DocumentTypeDeliveryChallan: "DC",
		models.DocumentTypeCreditNote:      "CN",
		models.DocumentTypeDebitNote:       "DN",
		models.DocumentTypeBillOfSupply:    "BS",
		models.DocumentTypePackingList:     "PK",
		models.DocumentTypeShippingLabel:   "SL",
		models.DocumentTypeExpense:         "EX",
	}[documentType]
	if prefix == "" {
		prefix = "DOC"
	}
	return fmt.Sprintf("%s-%d-%06d", prefix, time.Now().Year(), time.Now().UnixNano()%1000000)
}

func cloneDocument(document *models.Document) *models.Document {
	cloned := *document
	cloned.Lines = nil
	for _, line := range document.Lines {
		lineClone := *line
		cloned.Lines = append(cloned.Lines, &lineClone)
	}
	return &cloned
}

func pointerStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func legacyInvoiceStatusToDocument(status string) string {
	switch status {
	case "draft":
		return models.DocumentStatusDraft
	case "sent":
		return models.DocumentStatusSent
	case "paid":
		return models.DocumentStatusCompleted
	case "overdue":
		return models.DocumentStatusIssued
	case "void", "canceled":
		return models.DocumentStatusCancelled
	default:
		return models.DocumentStatusIssued
	}
}

func defaultExchangeRate(rate float64) float64 {
	if rate <= 0 {
		return 1
	}
	return rate
}

func defaultCurrency(currency string) string {
	if currency == "" {
		return "INR"
	}
	return currency
}

func defaultTaxMode(documentType, gstTreatment string) string {
	if documentType == models.DocumentTypeBillOfSupply || gstTreatment == models.DocumentGSTTreatmentComposition || gstTreatment == models.DocumentGSTTreatmentExempt {
		return models.DocumentTaxModeNonGST
	}
	return models.DocumentTaxModeGST
}

func resolvePartyType(documentType, requested, direction string) string {
	if requested != "" {
		return requested
	}
	switch documentType {
	case models.DocumentTypePurchaseInvoice, models.DocumentTypePurchaseOrder, models.DocumentTypeDebitNote, models.DocumentTypeExpense:
		return models.DocumentPartyTypeVendor
	case models.DocumentTypeDeliveryChallan:
		if direction == models.DocumentDirectionInward {
			return models.DocumentPartyTypeVendor
		}
		return models.DocumentPartyTypeCustomer
	default:
		return models.DocumentPartyTypeCustomer
	}
}

func stockEffectForDocument(documentType, direction string) string {
	switch documentType {
	case models.DocumentTypePurchaseInvoice, models.DocumentTypeCreditNote, models.DocumentTypeExpense:
		return "in"
	case models.DocumentTypeDeliveryChallan:
		if direction == models.DocumentDirectionInward {
			return "in"
		}
		return "out"
	case models.DocumentTypeDebitNote, models.DocumentTypeSalesInvoice, models.DocumentTypeBillOfSupply:
		return "out"
	case models.DocumentTypeSalesOrder:
		return "reserve"
	default:
		return "none"
	}
}

func isSupportedDocumentType(documentType string) bool {
	switch documentType {
	case models.DocumentTypeSalesInvoice,
		models.DocumentTypePurchaseInvoice,
		models.DocumentTypePurchaseOrder,
		models.DocumentTypeSalesOrder,
		models.DocumentTypeQuotation,
		models.DocumentTypeProformaInvoice,
		models.DocumentTypeDeliveryChallan,
		models.DocumentTypeCreditNote,
		models.DocumentTypeDebitNote,
		models.DocumentTypeBillOfSupply,
		models.DocumentTypePackingList,
		models.DocumentTypeShippingLabel,
		models.DocumentTypeExpense:
		return true
	default:
		return false
	}
}

func isAllowedConversion(sourceType, targetType string) bool {
	allowed := map[string]map[string]bool{
		models.DocumentTypeQuotation: {
			models.DocumentTypeSalesOrder:      true,
			models.DocumentTypeProformaInvoice: true,
			models.DocumentTypeSalesInvoice:    true,
		},
		models.DocumentTypeProformaInvoice: {
			models.DocumentTypeSalesOrder:   true,
			models.DocumentTypeSalesInvoice: true,
		},
		models.DocumentTypeSalesOrder: {
			models.DocumentTypeDeliveryChallan: true,
			models.DocumentTypeSalesInvoice:    true,
		},
		models.DocumentTypeSalesInvoice: {
			models.DocumentTypeCreditNote:    true,
			models.DocumentTypePackingList:   true,
			models.DocumentTypeShippingLabel: true,
		},
		models.DocumentTypePurchaseOrder: {
			models.DocumentTypePurchaseInvoice: true,
		},
		models.DocumentTypePurchaseInvoice: {
			models.DocumentTypeDebitNote: true,
		},
		models.DocumentTypeDeliveryChallan: {
			models.DocumentTypeSalesInvoice: true,
			models.DocumentTypeCreditNote:   true,
		},
		models.DocumentTypeBillOfSupply: {
			models.DocumentTypeCreditNote: true,
		},
	}
	return allowed[sourceType][targetType]
}

func coalesceString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func mustMarshalMap(value map[string]interface{}) string {
	if len(value) == 0 {
		return "{}"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func boolValueOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func (s *DocumentService) recordRevision(ctx context.Context, document *models.Document, action string, metadata map[string]interface{}) {
	if document == nil || document.ID == "" || document.BusinessID == "" {
		return
	}
	snapshot, err := json.Marshal(document)
	if err != nil {
		s.log.Warn("failed to marshal document revision snapshot", "document_id", document.ID, "action", action, "error", err)
		return
	}
	revision := &models.DocumentRevision{
		DocumentID: document.ID,
		BusinessID: document.BusinessID,
		Action:     action,
		Snapshot:   string(snapshot),
		Metadata:   mustMarshalMap(metadata),
	}
	if err := s.repo.CreateRevision(ctx, revision); err != nil {
		s.log.Warn("failed to create document revision", "document_id", document.ID, "action", action, "error", err)
	}
}

func (s *DocumentService) applyPartyTaxSnapshot(ctx context.Context, businessID string, document *models.Document) error {
	if document == nil || document.PartyID == nil || *document.PartyID == "" {
		return nil
	}
	switch document.PartyType {
	case models.DocumentPartyTypeCustomer:
		customer, err := s.customerRepo.GetByID(ctx, *document.PartyID, businessID)
		if err != nil {
			return err
		}
		if document.PartyGSTIN == "" {
			document.PartyGSTIN = customer.GSTIN
		}
		if document.PartyPAN == "" {
			document.PartyPAN = customer.PAN
		}
		if document.PartyStateCode == "" {
			document.PartyStateCode = customer.StateCode
		}
	case models.DocumentPartyTypeVendor:
		vendor, err := s.vendorRepo.GetByID(ctx, *document.PartyID, businessID)
		if err != nil {
			return err
		}
		if document.PartyGSTIN == "" {
			document.PartyGSTIN = vendor.GSTIN
		}
		if document.PartyPAN == "" {
			document.PartyPAN = vendor.PAN
		}
		if document.PartyStateCode == "" {
			document.PartyStateCode = vendor.StateCode
		}
	}
	return nil
}

func (s *DocumentService) syncDocumentWithholdings(ctx context.Context, document *models.Document, inputs []WithholdingInput) error {
	if s.db == nil || document == nil || document.ID == "" {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", document.ID).Delete(&models.DocumentWithholding{}).Error; err != nil {
			return err
		}
		if len(inputs) == 0 {
			return nil
		}
		records := make([]models.DocumentWithholding, 0, len(inputs))
		for _, input := range inputs {
			if input.SectionCode == "" || input.Amount == 0 {
				continue
			}
			records = append(records, models.DocumentWithholding{
				BusinessID:      document.BusinessID,
				DocumentID:      document.ID,
				SectionCode:     input.SectionCode,
				WithholdingType: coalesceString(input.WithholdingType, models.WithholdingTypeTDS),
				Rate:            input.Rate,
				TaxableAmount:   input.TaxableAmount,
				Amount:          input.Amount,
				Metadata:        mustMarshalMap(input.Metadata),
			})
		}
		if len(records) == 0 {
			return nil
		}
		return tx.Create(&records).Error
	})
}

func summarizeWithholdings(inputs []WithholdingInput) (total, tdsTotal, tcsTotal float64) {
	for _, input := range inputs {
		total += input.Amount
		switch input.WithholdingType {
		case models.WithholdingTypeTCS:
			tcsTotal += input.Amount
		default:
			tdsTotal += input.Amount
		}
	}
	return total, tdsTotal, tcsTotal
}
