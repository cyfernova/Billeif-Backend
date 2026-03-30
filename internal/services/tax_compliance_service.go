package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"github.com/phpdave11/gofpdf"
	"gorm.io/gorm"
)

type TaxComplianceService struct {
	cfg              *config.Config
	db               *gorm.DB
	businessRepo     interfaces.BusinessRepository
	customerRepo     interfaces.CustomerRepository
	vendorRepo       interfaces.VendorRepository
	subscriptionRepo interfaces.SubscriptionRepository
	httpClient       *http.Client
	sqs              *sqs.Client
	s3               *S3Service
	webhooks         *WebhookService
	documents        *DocumentService
	entitlements     *EntitlementService
	provider         GSTProvider
	log              *logger.Logger
}

type GSTINLookupResult struct {
	GSTIN       string                 `json:"gstin"`
	PAN         string                 `json:"pan,omitempty"`
	LegalName   string                 `json:"legal_name,omitempty"`
	TradeName   string                 `json:"trade_name,omitempty"`
	Address     string                 `json:"address,omitempty"`
	StateCode   string                 `json:"state_code,omitempty"`
	Source      string                 `json:"source"`
	IsValid     bool                   `json:"is_valid"`
	RawMetadata map[string]interface{} `json:"raw_metadata,omitempty"`
}

type GSTReportOptions struct {
	PeriodStart     time.Time `json:"period_start"`
	PeriodEnd       time.Time `json:"period_end"`
	FilingFrequency string    `json:"filing_frequency"`
	ExportFormat    string    `json:"export_format"`
	CreatedBy       string    `json:"created_by"`
}

type GSTR2BImportLineInput struct {
	SupplierGSTIN  string                 `json:"supplier_gstin"`
	SupplierName   string                 `json:"supplier_name"`
	DocumentNumber string                 `json:"document_number"`
	DocumentDate   *time.Time             `json:"document_date"`
	DocumentType   string                 `json:"document_type"`
	TaxableAmount  float64                `json:"taxable_amount"`
	TaxAmount      float64                `json:"tax_amount"`
	CGSTAmount     float64                `json:"cgst_amount"`
	SGSTAmount     float64                `json:"sgst_amount"`
	IGSTAmount     float64                `json:"igst_amount"`
	CessAmount     float64                `json:"cess_amount"`
	PlaceOfSupply  string                 `json:"place_of_supply"`
	RawPayload     map[string]interface{} `json:"raw_payload,omitempty"`
}

type ImportGSTR2BInput struct {
	PeriodStart time.Time               `json:"period_start" binding:"required"`
	PeriodEnd   time.Time               `json:"period_end" binding:"required"`
	Source      string                  `json:"source"`
	Notes       string                  `json:"notes"`
	Lines       []GSTR2BImportLineInput `json:"lines" binding:"required,min=1,dive"`
}

func NewTaxComplianceService(
	cfg *config.Config,
	db *gorm.DB,
	businessRepo interfaces.BusinessRepository,
	customerRepo interfaces.CustomerRepository,
	vendorRepo interfaces.VendorRepository,
	subscriptionRepo interfaces.SubscriptionRepository,
	awsCfg *awsclients.Config,
	s3 *S3Service,
	webhooks *WebhookService,
	log *logger.Logger,
) *TaxComplianceService {
	timeout := 15 * time.Second
	if cfg != nil && cfg.GSTLookup.Timeout > 0 {
		timeout = time.Duration(cfg.GSTLookup.Timeout) * time.Second
	}
	var sqsClient *sqs.Client
	if awsCfg != nil {
		sqsClient = awsCfg.SQS
	}
	svc := &TaxComplianceService{
		cfg:              cfg,
		db:               db,
		businessRepo:     businessRepo,
		customerRepo:     customerRepo,
		vendorRepo:       vendorRepo,
		subscriptionRepo: subscriptionRepo,
		httpClient:       &http.Client{Timeout: timeout},
		sqs:              sqsClient,
		s3:               s3,
		webhooks:         webhooks,
		log:              log,
	}
	svc.entitlements = NewEntitlementService(cfg, db, subscriptionRepo, log)
	svc.provider = NewConfiguredGSTProvider(cfg, log)
	return svc
}

func (s *TaxComplianceService) AttachDocumentService(documents *DocumentService) {
	s.documents = documents
}

func (s *TaxComplianceService) FetchGSTIN(ctx context.Context, gstin string) (*GSTINLookupResult, error) {
	result := &GSTINLookupResult{
		GSTIN:   strings.ToUpper(strings.TrimSpace(gstin)),
		PAN:     parsePANFromGSTIN(gstin),
		Source:  "local_fallback",
		IsValid: len(strings.TrimSpace(gstin)) == 15,
	}
	if s.cfg == nil || strings.TrimSpace(s.cfg.GSTLookup.BaseURL) == "" {
		return result, nil
	}

	requestURL := strings.TrimSpace(s.cfg.GSTLookup.BaseURL)
	if strings.Contains(requestURL, "{gstin}") {
		requestURL = strings.ReplaceAll(requestURL, "{gstin}", result.GSTIN)
	} else {
		requestURL = joinURL(requestURL, result.GSTIN)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return result, nil
	}
	if apiKey := strings.TrimSpace(s.cfg.GSTLookup.APIKey); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return result, nil
	}
	defer resp.Body.Close()

	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return result, nil
	}

	result.Source = "external"
	result.LegalName = readStringCandidate(payload, "legal_name", "lgnm", "data.legal_name")
	result.TradeName = readStringCandidate(payload, "trade_name", "tradeNam", "data.trade_name")
	result.Address = coalesceString(
		readStringCandidate(payload, "address", "pradr.addr", "data.address"),
		readStringCandidate(nestedMap(payload, "pradr"), "addr"),
	)
	defaultStateCode := ""
	if len(result.GSTIN) >= 2 {
		defaultStateCode = result.GSTIN[0:2]
	}
	result.StateCode = coalesceString(readStringCandidate(payload, "state_code", "stcd", "data.state_code"), defaultStateCode)
	result.RawMetadata = payload
	return result, nil
}

func (s *TaxComplianceService) GetReport(ctx context.Context, businessID, reportType string, opts GSTReportOptions) (map[string]interface{}, error) {
	if opts.PeriodStart.IsZero() || opts.PeriodEnd.IsZero() {
		return nil, fmt.Errorf("period_start and period_end are required")
	}
	switch strings.ToLower(strings.TrimSpace(reportType)) {
	case models.GSTReportTypeGSTR1:
		return s.buildGSTR1Report(ctx, businessID, opts, false)
	case models.GSTReportTypeHSNSummary:
		return s.buildHSNSummaryReport(ctx, businessID, opts)
	case models.GSTReportTypeCMP08:
		return s.buildCompositionReport(ctx, businessID, opts, true)
	case models.GSTReportTypeGSTR4:
		return s.buildCompositionReport(ctx, businessID, opts, false)
	case models.GSTReportTypeGSTR7:
		return s.buildGSTR7Report(ctx, businessID, opts)
	case models.GSTReportTypeGSTR2B:
		return s.buildLatestGSTR2BReport(ctx, businessID, opts)
	default:
		return nil, fmt.Errorf("unsupported report type: %s", reportType)
	}
}

func (s *TaxComplianceService) ExportReport(ctx context.Context, businessID, reportType string, opts GSTReportOptions) (*models.GSTReportRun, error) {
	var (
		data map[string]interface{}
		err  error
	)
	if strings.EqualFold(reportType, models.GSTReportTypeGSTR1) {
		data, err = s.buildGSTR1Report(ctx, businessID, opts, true)
	} else {
		data, err = s.GetReport(ctx, businessID, reportType, opts)
	}
	if err != nil {
		return nil, err
	}
	payload, warnings, err := s.renderExportPayload(reportType, opts.ExportFormat, data)
	if err != nil {
		return nil, err
	}
	run := &models.GSTReportRun{
		BusinessID:      businessID,
		ReportType:      reportType,
		PeriodStart:     opts.PeriodStart,
		PeriodEnd:       opts.PeriodEnd,
		FilingFrequency: coalesceString(opts.FilingFrequency, "monthly"),
		ExportFormat:    coalesceString(opts.ExportFormat, "json"),
		Status:          models.GSTReportStatusCompleted,
		Warnings:        mustMarshalJSONArray(warnings),
		Payload:         mustMarshalMap(payload),
		CreatedBy:       opts.CreatedBy,
	}
	if strings.TrimSpace(run.ID) == "" {
		run.ID = uuid.NewString()
	}
	if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, err
	}
	return run, nil
}

func (s *TaxComplianceService) GetReportRun(ctx context.Context, businessID, id string) (*models.GSTReportRun, error) {
	var run models.GSTReportRun
	err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&run).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *TaxComplianceService) ImportGSTR2B(ctx context.Context, businessID string, input ImportGSTR2BInput) (*models.GSTR2BImport, []models.GSTR2BMatchResult, error) {
	rawPayload := map[string]interface{}{
		"source": input.Source,
		"notes":  input.Notes,
		"lines":  input.Lines,
	}
	rawPayloadJSON := mustMarshalMap(rawPayload)

	var existing models.GSTR2BImport
	err := s.db.WithContext(ctx).
		Where("business_id = ? AND period_start = ? AND period_end = ? AND source = ? AND raw_payload = ? AND deleted_at IS NULL",
			businessID, input.PeriodStart, input.PeriodEnd, coalesceString(input.Source, "api"), rawPayloadJSON).
		First(&existing).Error
	if err == nil {
		var results []models.GSTR2BMatchResult
		if err := s.db.WithContext(ctx).Where("import_id = ? AND deleted_at IS NULL", existing.ID).Find(&results).Error; err != nil {
			return nil, nil, err
		}
		return &existing, results, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, nil, err
	}

	gstrImport := &models.GSTR2BImport{
		BusinessID:  businessID,
		PeriodStart: input.PeriodStart,
		PeriodEnd:   input.PeriodEnd,
		Source:      coalesceString(input.Source, "api"),
		Status:      "processed",
		Notes:       input.Notes,
		RawPayload:  rawPayloadJSON,
	}
	if strings.TrimSpace(gstrImport.ID) == "" {
		gstrImport.ID = uuid.NewString()
	}

	results := make([]models.GSTR2BMatchResult, 0, len(input.Lines))
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(gstrImport).Error; err != nil {
			return err
		}

		lines := make([]models.GSTR2BImportLine, 0, len(input.Lines))
		for _, line := range input.Lines {
			lines = append(lines, models.GSTR2BImportLine{
				ID:             uuid.NewString(),
				ImportID:       gstrImport.ID,
				BusinessID:     businessID,
				SupplierGSTIN:  strings.ToUpper(strings.TrimSpace(line.SupplierGSTIN)),
				SupplierName:   line.SupplierName,
				DocumentNumber: strings.TrimSpace(line.DocumentNumber),
				DocumentDate:   line.DocumentDate,
				DocumentType:   line.DocumentType,
				TaxableAmount:  line.TaxableAmount,
				TaxAmount:      line.TaxAmount,
				CGSTAmount:     line.CGSTAmount,
				SGSTAmount:     line.SGSTAmount,
				IGSTAmount:     line.IGSTAmount,
				CessAmount:     line.CessAmount,
				PlaceOfSupply:  line.PlaceOfSupply,
				RawPayload:     mustMarshalMap(line.RawPayload),
			})
		}
		if len(lines) > 0 {
			if err := tx.Create(&lines).Error; err != nil {
				return err
			}
		}

		bookDocs, err := s.listDocumentsByTypes(ctx, businessID, input.PeriodStart, input.PeriodEnd, []string{
			models.DocumentTypePurchaseInvoice,
			models.DocumentTypeExpense,
		})
		if err != nil {
			return err
		}

		bookMap := make(map[string]*models.Document, len(bookDocs))
		for _, doc := range bookDocs {
			key := gstr2bBookKey(doc.PartyGSTIN, doc.SerialNumber)
			bookMap[key] = doc
		}

		usedDocuments := map[string]bool{}
		for _, line := range lines {
			match := models.GSTR2BMatchResult{
				ID:                  uuid.NewString(),
				ImportID:            gstrImport.ID,
				BusinessID:          businessID,
				Status:              models.GSTR2BMatchStatusMissingInBooks,
				ImportLineID:        &line.ID,
				ImportTaxableAmount: line.TaxableAmount,
				ImportTaxAmount:     line.TaxAmount,
			}
			if doc, ok := bookMap[gstr2bBookKey(line.SupplierGSTIN, line.DocumentNumber)]; ok {
				usedDocuments[doc.ID] = true
				match.DocumentID = &doc.ID
				match.BooksTaxableAmount = doc.Subtotal
				match.BooksTaxAmount = doc.TaxTotal + doc.CessTotal
				switch {
				case !almostEqualFloat(match.BooksTaxableAmount, match.ImportTaxableAmount):
					match.Status = models.GSTR2BMatchStatusValueMismatch
					match.MismatchReason = "taxable value mismatch"
				case !almostEqualFloat(match.BooksTaxAmount, match.ImportTaxAmount):
					match.Status = models.GSTR2BMatchStatusTaxMismatch
					match.MismatchReason = "tax amount mismatch"
				default:
					match.Status = models.GSTR2BMatchStatusMatched
				}
			}
			results = append(results, match)
		}

		for _, doc := range bookDocs {
			if usedDocuments[doc.ID] {
				continue
			}
			docID := doc.ID
			results = append(results, models.GSTR2BMatchResult{
				ID:                  uuid.NewString(),
				ImportID:            gstrImport.ID,
				BusinessID:          businessID,
				DocumentID:          &docID,
				Status:              models.GSTR2BMatchStatusMissingInPortal,
				BooksTaxableAmount:  doc.Subtotal,
				BooksTaxAmount:      doc.TaxTotal + doc.CessTotal,
				ImportTaxableAmount: 0,
				ImportTaxAmount:     0,
			})
		}

		if len(results) > 0 {
			if err := tx.Create(&results).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return gstrImport, results, nil
}

func (s *TaxComplianceService) buildGSTR1Report(ctx context.Context, businessID string, opts GSTReportOptions, exportMode bool) (map[string]interface{}, error) {
	business, err := s.businessRepo.GetByID(ctx, businessID)
	if err != nil {
		return nil, err
	}
	docs, err := s.listDocumentsByTypes(ctx, businessID, opts.PeriodStart, opts.PeriodEnd, []string{
		models.DocumentTypeSalesInvoice,
		models.DocumentTypeBillOfSupply,
		models.DocumentTypeCreditNote,
	})
	if err != nil {
		return nil, err
	}

	b2b := make([]map[string]interface{}, 0)
	b2cl := make([]map[string]interface{}, 0)
	cdnr := make([]map[string]interface{}, 0)
	cdnur := make([]map[string]interface{}, 0)
	exports := make([]map[string]interface{}, 0)
	b2cs := map[string]map[string]interface{}{}
	hsnRows := map[string]map[string]interface{}{}
	warnings := []string{}

	for _, doc := range docs {
		partyGSTIN := strings.ToUpper(strings.TrimSpace(doc.PartyGSTIN))
		interstate := business.BusinessStateCode != "" && doc.PlaceOfSupply != "" && business.BusinessStateCode != doc.PlaceOfSupply
		signedTaxable := doc.Subtotal
		signedTax := doc.TaxTotal + doc.CessTotal
		signedTotal := doc.Total
		if doc.DocumentType == models.DocumentTypeCreditNote {
			signedTaxable *= -1
			signedTax *= -1
			signedTotal *= -1
		}

		row := map[string]interface{}{
			"document_id":     doc.ID,
			"serial_number":   doc.SerialNumber,
			"issue_date":      doc.IssueDate.Format("2006-01-02"),
			"party_gstin":     partyGSTIN,
			"place_of_supply": doc.PlaceOfSupply,
			"taxable_value":   round2(signedTaxable),
			"tax_amount":      round2(signedTax),
			"total":           round2(signedTotal),
			"gst_treatment":   doc.GSTTreatment,
			"document_type":   doc.DocumentType,
		}

		switch {
		case doc.ExportType != "" || doc.GSTTreatment == models.DocumentGSTTreatmentExportIGST || doc.GSTTreatment == models.DocumentGSTTreatmentExportLUT || doc.GSTTreatment == models.DocumentGSTTreatmentSEZ:
			exports = append(exports, row)
		case doc.DocumentType == models.DocumentTypeCreditNote && partyGSTIN != "":
			cdnr = append(cdnr, row)
		case doc.DocumentType == models.DocumentTypeCreditNote && interstate:
			cdnur = append(cdnur, row)
		case partyGSTIN != "":
			b2b = append(b2b, row)
		case interstate && mathAbs(signedTotal) >= 250000:
			b2cl = append(b2cl, row)
		default:
			key := fmt.Sprintf("%s|%.3f|%s", doc.PlaceOfSupply, doc.TaxTotal, doc.SupplyType)
			if _, ok := b2cs[key]; !ok {
				b2cs[key] = map[string]interface{}{
					"place_of_supply": doc.PlaceOfSupply,
					"supply_type":     doc.SupplyType,
					"taxable_value":   0.0,
					"tax_amount":      0.0,
					"total":           0.0,
				}
			}
			b2cs[key]["taxable_value"] = round2(b2cs[key]["taxable_value"].(float64) + signedTaxable)
			b2cs[key]["tax_amount"] = round2(b2cs[key]["tax_amount"].(float64) + signedTax)
			b2cs[key]["total"] = round2(b2cs[key]["total"].(float64) + signedTotal)
		}

		for _, line := range doc.Lines {
			uqc := normalizeUQCCode(line.UQCCode)
			if strings.TrimSpace(line.UQCCode) != "" && uqc == "OTH" && strings.ToUpper(strings.TrimSpace(line.UQCCode)) != "OTH" {
				warnings = append(warnings, fmt.Sprintf("invalid UQC %q downgraded to OTH for document %s", line.UQCCode, doc.SerialNumber))
			}
			if !isValidHSNCode(line.HSNSACCode) {
				warnings = append(warnings, fmt.Sprintf("excluded invalid HSN for document %s line %s", doc.SerialNumber, line.Description))
				continue
			}
			key := line.HSNSACCode + "|" + uqc + "|" + boolBucket(partyGSTIN != "")
			if _, ok := hsnRows[key]; !ok {
				hsnRows[key] = map[string]interface{}{
					"hsn_sac_code":  line.HSNSACCode,
					"uqc_code":      uqc,
					"b2b":           partyGSTIN != "",
					"quantity":      0.0,
					"taxable_value": 0.0,
					"tax_amount":    0.0,
					"total":         0.0,
				}
			}
			sign := 1.0
			if doc.DocumentType == models.DocumentTypeCreditNote {
				sign = -1
			}
			hsnRows[key]["quantity"] = round2(hsnRows[key]["quantity"].(float64) + sign*line.Quantity)
			hsnRows[key]["taxable_value"] = round2(hsnRows[key]["taxable_value"].(float64) + sign*line.LineSubtotal)
			hsnRows[key]["tax_amount"] = round2(hsnRows[key]["tax_amount"].(float64) + sign*(line.TaxAmount+line.CessAmount))
			hsnRows[key]["total"] = round2(hsnRows[key]["total"].(float64) + sign*line.LineTotal)
		}

		if len(doc.SerialNumber) > 16 {
			warnings = append(warnings, fmt.Sprintf("document %s exceeds 16 characters and will be blocked from JSON export", doc.SerialNumber))
		}
	}

	b2csList := mapsToSortedSlice(b2cs)
	hsnList := mapsToSortedSlice(hsnRows)

	report := map[string]interface{}{
		"report_type":      models.GSTReportTypeGSTR1,
		"period_start":     opts.PeriodStart.Format("2006-01-02"),
		"period_end":       opts.PeriodEnd.Format("2006-01-02"),
		"filing_frequency": coalesceString(opts.FilingFrequency, business.GSTFilingFrequency),
		"sections": map[string]interface{}{
			"b2b":         b2b,
			"b2cl":        b2cl,
			"b2cs":        b2csList,
			"cdnr":        cdnr,
			"cdnur":       cdnur,
			"exports":     exports,
			"hsn_summary": hsnList,
		},
		"warnings": warnings,
		"note":     "Indicative dataset prepared from document records.",
	}

	if exportMode && strings.EqualFold(coalesceString(opts.FilingFrequency, business.GSTFilingFrequency), "quarterly") {
		report["sections"] = map[string]interface{}{
			"b2b":  b2b,
			"cdnr": cdnr,
		}
		report["warnings"] = append(warnings,
			"Quarterly JSON export includes only B2B and CDNR sections. B2CS and HSN summary require manual filing entry, mirroring Swipe behavior.",
		)
	}
	return report, nil
}

func (s *TaxComplianceService) buildHSNSummaryReport(ctx context.Context, businessID string, opts GSTReportOptions) (map[string]interface{}, error) {
	gstr1, err := s.buildGSTR1Report(ctx, businessID, opts, false)
	if err != nil {
		return nil, err
	}
	sections := nestedMap(gstr1, "sections")
	return map[string]interface{}{
		"report_type":  models.GSTReportTypeHSNSummary,
		"period_start": opts.PeriodStart.Format("2006-01-02"),
		"period_end":   opts.PeriodEnd.Format("2006-01-02"),
		"data":         sections["hsn_summary"],
		"warnings":     gstr1["warnings"],
	}, nil
}

func (s *TaxComplianceService) buildCompositionReport(ctx context.Context, businessID string, opts GSTReportOptions, cmp08 bool) (map[string]interface{}, error) {
	business, err := s.businessRepo.GetByID(ctx, businessID)
	if err != nil {
		return nil, err
	}
	if !business.CompositionEnabled {
		return nil, fmt.Errorf("composition reports are available only for composition-enabled businesses")
	}
	outward, err := s.listDocumentsByTypes(ctx, businessID, opts.PeriodStart, opts.PeriodEnd, []string{
		models.DocumentTypeBillOfSupply,
		models.DocumentTypeSalesInvoice,
	})
	if err != nil {
		return nil, err
	}
	inward, err := s.listDocumentsByTypes(ctx, businessID, opts.PeriodStart, opts.PeriodEnd, []string{
		models.DocumentTypePurchaseInvoice,
		models.DocumentTypeExpense,
	})
	if err != nil {
		return nil, err
	}

	outwardTotal := 0.0
	inwardTotal := 0.0
	for _, doc := range outward {
		if doc.BillOfSupply || doc.GSTTreatment == models.DocumentGSTTreatmentComposition {
			outwardTotal += doc.Total
		}
	}
	for _, doc := range inward {
		inwardTotal += doc.Total
	}

	reportType := models.GSTReportTypeGSTR4
	if cmp08 {
		reportType = models.GSTReportTypeCMP08
	}
	return map[string]interface{}{
		"report_type":  reportType,
		"period_start": opts.PeriodStart.Format("2006-01-02"),
		"period_end":   opts.PeriodEnd.Format("2006-01-02"),
		"summary": map[string]interface{}{
			"outward_turnover": round2(outwardTotal),
			"inward_supplies":  round2(inwardTotal),
		},
		"note": "Indicative composition summary prepared from bill of supply, sales, purchase, and expense documents.",
	}, nil
}

func (s *TaxComplianceService) buildGSTR7Report(ctx context.Context, businessID string, opts GSTReportOptions) (map[string]interface{}, error) {
	var withholdings []models.DocumentWithholding
	if err := s.db.WithContext(ctx).
		Joins("JOIN documents ON documents.id = document_withholdings.document_id").
		Where("document_withholdings.business_id = ? AND document_withholdings.withholding_type = ? AND documents.issue_date BETWEEN ? AND ? AND document_withholdings.deleted_at IS NULL AND documents.deleted_at IS NULL",
			businessID, models.WithholdingTypeGSTTDS, opts.PeriodStart, opts.PeriodEnd).
		Find(&withholdings).Error; err != nil {
		return nil, err
	}
	rows := make([]map[string]interface{}, 0, len(withholdings))
	total := 0.0
	for _, item := range withholdings {
		var doc models.Document
		if err := s.db.WithContext(ctx).Where("id = ?", item.DocumentID).First(&doc).Error; err != nil {
			continue
		}
		total += item.Amount
		rows = append(rows, map[string]interface{}{
			"document_id":     doc.ID,
			"serial_number":   doc.SerialNumber,
			"party_gstin":     doc.PartyGSTIN,
			"section_code":    item.SectionCode,
			"taxable_amount":  round2(item.TaxableAmount),
			"withholding_amt": round2(item.Amount),
			"rate":            item.Rate,
			"issue_date":      doc.IssueDate.Format("2006-01-02"),
		})
	}
	return map[string]interface{}{
		"report_type":  models.GSTReportTypeGSTR7,
		"period_start": opts.PeriodStart.Format("2006-01-02"),
		"period_end":   opts.PeriodEnd.Format("2006-01-02"),
		"summary": map[string]interface{}{
			"entries":             len(rows),
			"gst_tds_total":       round2(total),
			"metal_scrap_support": true,
		},
		"data": rows,
	}, nil
}

func (s *TaxComplianceService) buildLatestGSTR2BReport(ctx context.Context, businessID string, opts GSTReportOptions) (map[string]interface{}, error) {
	var gstrImport models.GSTR2BImport
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND period_start = ? AND period_end = ? AND deleted_at IS NULL", businessID, opts.PeriodStart, opts.PeriodEnd).
		Order("created_at DESC").
		First(&gstrImport).Error; err != nil {
		return nil, err
	}
	var results []models.GSTR2BMatchResult
	if err := s.db.WithContext(ctx).
		Where("import_id = ? AND deleted_at IS NULL", gstrImport.ID).
		Find(&results).Error; err != nil {
		return nil, err
	}
	summary := map[string]int{}
	rows := make([]map[string]interface{}, 0, len(results))
	for _, item := range results {
		summary[item.Status]++
		rows = append(rows, map[string]interface{}{
			"status":                item.Status,
			"mismatch_reason":       item.MismatchReason,
			"document_id":           pointerStringValue(item.DocumentID),
			"import_line_id":        pointerStringValue(item.ImportLineID),
			"books_taxable_amount":  round2(item.BooksTaxableAmount),
			"import_taxable_amount": round2(item.ImportTaxableAmount),
			"books_tax_amount":      round2(item.BooksTaxAmount),
			"import_tax_amount":     round2(item.ImportTaxAmount),
		})
	}
	return map[string]interface{}{
		"report_type": models.GSTReportTypeGSTR2B,
		"import_id":   gstrImport.ID,
		"summary":     summary,
		"data":        rows,
		"note":        "Indicative reconciliation only, mirroring Swipe's guidance.",
	}, nil
}

func (s *TaxComplianceService) renderExportPayload(reportType, format string, data map[string]interface{}) (map[string]interface{}, []string, error) {
	warnings := []string{}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		return map[string]interface{}{
			"content_type": "application/json",
			"filename":     reportType + ".json",
			"data":         data,
		}, warnings, nil
	case "pdf":
		content, err := generateReportPDF(reportType, data)
		if err != nil {
			return nil, nil, err
		}
		return map[string]interface{}{
			"content_type":   "application/pdf",
			"filename":       reportType + ".pdf",
			"content_base64": base64.StdEncoding.EncodeToString(content),
		}, warnings, nil
	case "xlsx":
		csvContent, err := generateReportCSV(data)
		if err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, "XLSX export is currently emitted as CSV content for backend compatibility.")
		return map[string]interface{}{
			"content_type":   "text/csv",
			"filename":       reportType + ".csv",
			"content_base64": base64.StdEncoding.EncodeToString(csvContent),
		}, warnings, nil
	default:
		return nil, nil, fmt.Errorf("unsupported export format: %s", format)
	}
}

func (s *TaxComplianceService) listDocumentsByTypes(ctx context.Context, businessID string, start, end time.Time, types []string) ([]*models.Document, error) {
	var docs []models.Document
	query := s.db.WithContext(ctx).
		Preload("Lines").
		Where("business_id = ? AND deleted_at IS NULL AND issue_date BETWEEN ? AND ?", businessID, start, end)
	if len(types) > 0 {
		query = query.Where("document_type IN ?", types)
	}
	if err := query.Order("issue_date ASC, created_at ASC").Find(&docs).Error; err != nil {
		return nil, err
	}
	results := make([]*models.Document, 0, len(docs))
	for i := range docs {
		results = append(results, &docs[i])
	}
	return results, nil
}

func gstr2bBookKey(gstin, docNo string) string {
	return strings.ToUpper(strings.TrimSpace(gstin)) + "|" + strings.ToUpper(strings.TrimSpace(docNo))
}

func mapsToSortedSlice(items map[string]map[string]interface{}) []map[string]interface{} {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]map[string]interface{}, 0, len(keys))
	for _, key := range keys {
		result = append(result, items[key])
	}
	return result
}

func mustMarshalJSONArray(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	data, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func round2(value float64) float64 {
	return mathRound(value*100) / 100
}

func boolBucket(value bool) string {
	if value {
		return "b2b"
	}
	return "b2c"
}

func mathAbs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func mathRound(value float64) float64 {
	if value < 0 {
		return float64(int(value - 0.5))
	}
	return float64(int(value + 0.5))
}

func generateReportPDF(title string, data map[string]interface{}) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 14)
	pdf.CellFormat(190, 8, strings.ToUpper(title)+" REPORT", "", 1, "L", false, 0, "")
	pdf.SetFont("Arial", "", 9)
	pretty, _ := json.MarshalIndent(data, "", "  ")
	pdf.MultiCell(190, 4.5, string(pretty), "", "L", false)
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func generateReportCSV(data map[string]interface{}) ([]byte, error) {
	pretty, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return pretty, nil
}
