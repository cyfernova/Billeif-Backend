package services

import (
	"context"
	"encoding/json"
	"strings"

	"invoice-backend/internal/gst"
	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
)

type canonicalInvoiceCreator interface {
	CreateByBusiness(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Invoice, error)
}

type canonicalInvoiceIssuerService interface {
	IssueByBusiness(ctx context.Context, businessID, invoiceID string, input IssueInvoiceInput) (*IssueInvoiceResult, error)
}

type salesInvoiceDocumentIssuer interface {
	IssueSalesInvoiceDocument(ctx context.Context, businessID, invoiceID string, input IssueInvoiceInput) (*IssueInvoiceResult, error)
}

type salesInvoiceDocumentCreator interface {
	CreateSalesInvoiceDocument(ctx context.Context, businessID string, input CreateDocumentInput) (*models.Document, error)
	createPOSSalesInvoiceDocument(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Document, error)
}

type invoiceSalesDocumentIssuer struct {
	invoices canonicalInvoiceIssuerService
}

func newInvoiceSalesDocumentIssuer(invoices canonicalInvoiceIssuerService) salesInvoiceDocumentIssuer {
	return &invoiceSalesDocumentIssuer{invoices: invoices}
}

func (i *invoiceSalesDocumentIssuer) IssueSalesInvoiceDocument(
	ctx context.Context,
	businessID, invoiceID string,
	input IssueInvoiceInput,
) (*IssueInvoiceResult, error) {
	return i.invoices.IssueByBusiness(ctx, businessID, invoiceID, input)
}

type invoiceSalesDocumentCreator struct {
	invoices canonicalInvoiceCreator
}

func newInvoiceSalesDocumentCreator(invoices canonicalInvoiceCreator) salesInvoiceDocumentCreator {
	return &invoiceSalesDocumentCreator{invoices: invoices}
}

func (c *invoiceSalesDocumentCreator) CreateSalesInvoiceDocument(ctx context.Context, businessID string, input CreateDocumentInput) (*models.Document, error) {
	if err := validateSalesInvoiceDelegationInput(businessID, input); err != nil {
		return nil, err
	}
	invoiceInput := CreateInvoiceInput{
		BusinessID:           businessID,
		IdempotencyKey:       input.IdempotencyKey,
		CustomerID:           input.PartyID,
		ProjectID:            input.ProjectID,
		PriceListID:          input.PriceListID,
		RenderProfileID:      input.RenderProfileID,
		InvoiceDate:          input.IssueDate,
		Notes:                input.Notes,
		TermsAndConditions:   input.Terms,
		CustomFields:         input.CustomFields,
		AdditionalCharges:    input.AdditionalCharges,
		OriginSubscriptionID: input.OriginSubscriptionID,
		OriginRunID:          input.OriginRunID,
		TaxProfile: TaxProfileInput{
			GSTTreatment:          input.GSTTreatment,
			PlaceOfSupply:         input.PlaceOfSupply,
			BillOfSupply:          input.BillOfSupply,
			ExportType:            input.ExportType,
			SupplyType:            input.SupplyType,
			CounterpartyGSTIN:     input.PartyGSTIN,
			CounterpartyPAN:       input.PartyPAN,
			CounterpartyStateCode: input.PartyStateCode,
			GenerateEInvoice:      input.GenerateEInvoice,
			GenerateEWayBill:      input.GenerateEWayBill,
			ReverseCharge:         input.ReverseCharge,
			ReverseChargeReason:   input.ReverseChargeReason,
			DispatchFrom:          input.DispatchFrom,
			DispatchTo:            input.DispatchTo,
			DistanceKM:            input.DistanceKM,
			Transporter:           input.Transporter,
			Vehicle:               input.Vehicle,
			MultiVehiclePlan:      input.MultiVehiclePlan,
			TCS:                   input.Withholdings,
			SourceLinkage:         input.SourceLinkage,
			ReportTags:            input.ReportTags,
		},
		Items: make([]CreateInvoiceItemInput, 0, len(input.Lines)),
	}
	if input.DueDate != nil {
		invoiceInput.DueDate = *input.DueDate
	}
	for _, line := range input.Lines {
		invoiceInput.Items = append(invoiceInput.Items, CreateInvoiceItemInput{
			ProductID:        line.ProductID,
			VariantID:        line.VariantID,
			WarehouseID:      line.WarehouseID,
			Description:      line.Description,
			HSNSACCode:       line.HSNSACCode,
			Unit:             firstNonEmpty(line.Unit, line.UQCCode),
			Quantity:         line.Quantity,
			FreeQuantity:     line.FreeQuantity,
			UnitPrice:        line.UnitPrice,
			MRP:              line.MRP,
			TaxRate:          line.TaxRate,
			CessRate:         line.CessRate,
			CustomFields:     line.CustomFields,
			ChargeSnapshot:   line.ChargeLinkage,
			BatchAllocations: line.BatchAllocations,
			SerialIDs:        line.SerialIDs,
		})
	}
	return c.createInvoiceDocument(ctx, businessID, invoiceInput)
}

func (c *invoiceSalesDocumentCreator) createPOSSalesInvoiceDocument(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Document, error) {
	input.Origin = models.InvoiceOriginPOS
	if strings.TrimSpace(input.CustomerID) == "" && input.BuyerSnapshot.IsEmpty() {
		return nil, &idempotency.InvalidPayloadError{}
	}
	return c.createInvoiceDocument(ctx, businessID, input)
}

func (c *invoiceSalesDocumentCreator) createInvoiceDocument(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Document, error) {
	invoice, err := c.invoices.CreateByBusiness(ctx, businessID, input)
	if err != nil {
		return nil, err
	}
	return invoiceDocumentProjection(invoice), nil
}

func validateSalesInvoiceDelegationInput(businessID string, input CreateDocumentInput) error {
	unsupported := input.BranchID != "" ||
		(input.BusinessID != "" && input.BusinessID != businessID) ||
		(input.PartyType != "" && input.PartyType != models.DocumentPartyTypeCustomer) ||
		(input.Status != "" && input.Status != models.DocumentStatusDraft) ||
		(input.DraftState != "" && input.DraftState != models.DocumentDraftStateDraft) ||
		input.TaxMode != "" ||
		input.DispatchDate != nil ||
		input.Currency != "" ||
		input.ExchangeRate != 0 ||
		(input.Locale != "" && input.Locale != "en-IN") ||
		input.Declaration != "" ||
		(input.Direction != "" && input.Direction != models.DocumentDirectionOutward) ||
		len(input.ExtraFields) != 0
	if !unsupported {
		for _, line := range input.Lines {
			if line.DiscountAmount != 0 || len(line.PackingMetadata) != 0 {
				unsupported = true
				break
			}
		}
	}
	if unsupported {
		return &idempotency.InvalidPayloadError{}
	}
	return nil
}

func businessPartySnapshot(business *models.BusinessProfile) models.PartySnapshot {
	if business == nil {
		return models.PartySnapshot{}
	}
	return models.PartySnapshot{
		Name:       business.Name,
		Email:      business.Email,
		Phone:      business.Phone,
		Address:    business.Address,
		City:       business.City,
		State:      business.State,
		Country:    business.Country,
		PostalCode: business.PostalCode,
		TaxID:      business.TaxID,
		GSTIN:      business.GSTIN,
	}
}

func customerPartySnapshot(customer *models.Customer) models.PartySnapshot {
	if customer == nil {
		return models.PartySnapshot{}
	}
	return models.PartySnapshot{
		Name:       customer.Name,
		Email:      customer.Email,
		Phone:      customer.Phone,
		Address:    customer.Address,
		City:       customer.City,
		State:      customer.State,
		Country:    customer.Country,
		PostalCode: customer.PostalCode,
		TaxID:      customer.TaxID,
		GSTIN:      customer.GSTIN,
	}
}

func invoiceDocumentProjection(invoice *models.Invoice) *models.Document {
	taxProfile := unmarshalJSONMap(invoice.TaxProfile)
	editorFields := unmarshalJSONMap(invoice.CustomFields)
	terms, _ := editorFields["terms_and_conditions"].(string)
	delete(editorFields, "terms_and_conditions")
	extraFields := make(map[string]interface{})
	if len(editorFields) > 0 {
		extraFields["custom_fields"] = editorFields
	}
	var additionalCharges []map[string]interface{}
	if err := json.Unmarshal([]byte(invoice.AdditionalCharges), &additionalCharges); err == nil && len(additionalCharges) > 0 {
		extraFields["additional_charges"] = additionalCharges
	}
	sourceLinkage := nestedMap(taxProfile, "source_linkage")
	if sourceLinkage == nil {
		sourceLinkage = make(map[string]interface{})
	}
	sourceLinkage["source_invoice_id"] = invoice.ID
	sourceLinkage["invoice_origin"] = string(invoice.Origin)
	sourceLinkage["seller_snapshot"] = invoice.SellerSnapshot
	sourceLinkage["buyer_snapshot"] = invoice.BuyerSnapshot

	document := &models.Document{
		ID:                    invoice.ID,
		BusinessID:            invoice.BusinessID,
		DocumentType:          models.DocumentTypeSalesInvoice,
		PartyType:             models.DocumentPartyTypeCustomer,
		PartyID:               invoice.CustomerID,
		Status:                models.DocumentStatusDraft,
		DraftState:            models.DocumentDraftStateDraft,
		TaxMode:               defaultTaxMode(models.DocumentTypeSalesInvoice, readStringCandidate(taxProfile, "gst_treatment")),
		GSTTreatment:          firstNonEmpty(readStringCandidate(taxProfile, "gst_treatment"), models.DocumentGSTTreatmentRegular),
		PlaceOfSupply:         readStringCandidate(taxProfile, "place_of_supply"),
		PartyGSTIN:            readStringCandidate(taxProfile, "counterparty_gstin"),
		PartyPAN:              readStringCandidate(taxProfile, "counterparty_pan"),
		PartyStateCode:        readStringCandidate(taxProfile, "counterparty_state_code"),
		SupplyType:            readStringCandidate(taxProfile, "supply_type"),
		ExportType:            readStringCandidate(taxProfile, "export_type"),
		BillOfSupply:          readBoolCandidate(taxProfile, "bill_of_supply"),
		SerialNumber:          "",
		IssueDate:             invoice.InvoiceDate,
		DueDate:               &invoice.DueDate,
		Currency:              invoice.Currency,
		Locale:                "en-IN",
		SourceLinkage:         mustMarshalMap(sourceLinkage),
		RenderProfileID:       invoice.RenderProfileID,
		ProjectID:             invoice.ProjectID,
		PriceListID:           invoice.PriceListID,
		OriginSubscriptionID:  invoice.OriginSubscriptionID,
		OriginRunID:           invoice.OriginRunID,
		GenerateEInvoice:      readBoolCandidate(taxProfile, "generate_einvoice"),
		GenerateEWayBill:      readBoolCandidate(taxProfile, "generate_ewaybill"),
		ReverseCharge:         readBoolCandidate(taxProfile, "reverse_charge"),
		ReverseChargeReason:   readStringCandidate(taxProfile, "reverse_charge_reason"),
		DispatchFrom:          mustMarshalMap(nestedMap(taxProfile, "dispatch_from")),
		DispatchTo:            mustMarshalMap(nestedMap(taxProfile, "dispatch_to")),
		DistanceKM:            floatValue(taxProfile["distance_km"]),
		Transporter:           mustMarshalMap(nestedMap(taxProfile, "transporter")),
		Vehicle:               mustMarshalMap(nestedMap(taxProfile, "vehicle")),
		MultiVehiclePlan:      mustMarshalMap(nestedMap(taxProfile, "multi_vehicle_plan")),
		Notes:                 invoice.Notes,
		Terms:                 terms,
		Direction:             models.DocumentDirectionOutward,
		Subtotal:              invoice.Subtotal,
		DiscountTotal:         invoice.Discount,
		Total:                 invoice.Total,
		PaidAmount:            invoice.PaidAmount,
		BalanceDue:            invoice.BalanceDue,
		ExtraFields:           mustMarshalMap(extraFields),
		ReportTags:            mustMarshalMap(nestedMap(taxProfile, "report_tags")),
		ProfitSnapshotEnabled: true,
	}
	if document.BillOfSupply {
		document.DocumentType = models.DocumentTypeBillOfSupply
		document.TaxMode = models.DocumentTaxModeNonGST
	}

	intraState := invoiceProjectionIsIntraState(invoice, document)
	document.Lines = make([]*models.DocumentLine, 0, len(invoice.Items))
	for _, item := range invoice.Items {
		lineSubtotal := (item.Quantity * item.UnitPrice) - item.Discount
		taxAmount := roundCurrency(item.Total - lineSubtotal - item.CessAmount)
		document.TaxTotal += taxAmount
		document.CessTotal += item.CessAmount
		line := &models.DocumentLine{
			ID:                item.ID,
			DocumentID:        invoice.ID,
			ProductID:         item.ProductID,
			VariantID:         item.VariantID,
			Description:       item.Description,
			HSNSACCode:        item.HSNSACCode,
			UQCCode:           gst.CanonicalSnapshotUQC(item.Unit, ""),
			Unit:              gst.CanonicalSnapshotUQC(item.Unit, ""),
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
			TaxAmount:         taxAmount,
			LineSubtotal:      lineSubtotal,
			LineTotal:         item.Total,
			CustomFields:      item.CustomFields,
			ChargeLinkage:     item.ChargeSnapshot,
			BatchAllocations:  item.BatchAllocations,
			SerialIDs:         item.SerialIDs,
			StockEffect:       "out",
		}
		if document.TaxMode == models.DocumentTaxModeGST {
			if intraState {
				line.CGSTRate = item.TaxRate / 2
				line.SGSTRate = item.TaxRate / 2
				line.CGSTAmount = roundCurrency(taxAmount / 2)
				line.SGSTAmount = roundCurrency(taxAmount - line.CGSTAmount)
			} else {
				line.IGSTRate = item.TaxRate
				line.IGSTAmount = taxAmount
			}
		}
		document.Lines = append(document.Lines, line)
	}
	document.WithholdingTotal, document.TDSTotal, document.TCSTotal =
		summarizeWithholdings(mapSliceToWithholdings(readMapSlice(taxProfile, "tcs")))
	return document
}

func invoiceProjectionIsIntraState(invoice *models.Invoice, document *models.Document) bool {
	sellerState := strings.TrimSpace(invoice.SellerSnapshot.State)
	sellerStateCode := ""
	if gstin := strings.TrimSpace(invoice.SellerSnapshot.GSTIN); len(gstin) >= 2 {
		sellerStateCode = gstin[:2]
	}
	placeOfSupply := strings.TrimSpace(document.PlaceOfSupply)
	if placeOfSupply == "" {
		placeOfSupply = strings.TrimSpace(document.PartyStateCode)
	}
	if sellerState == "" && sellerStateCode == "" {
		return true
	}
	return strings.EqualFold(sellerState, placeOfSupply) ||
		strings.EqualFold(sellerStateCode, placeOfSupply)
}
