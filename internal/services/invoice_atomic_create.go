package services

import (
	"context"

	"invoice-backend/internal/gst"
	"invoice-backend/internal/models"
)

type canonicalInvoiceCreator interface {
	CreateByBusiness(ctx context.Context, businessID string, input CreateInvoiceInput) (*models.Invoice, error)
}

type salesInvoiceDocumentCreator interface {
	CreateSalesInvoiceDocument(ctx context.Context, businessID string, input CreateDocumentInput) (*models.Document, error)
}

type invoiceSalesDocumentCreator struct {
	invoices canonicalInvoiceCreator
}

func newInvoiceSalesDocumentCreator(invoices canonicalInvoiceCreator) salesInvoiceDocumentCreator {
	return &invoiceSalesDocumentCreator{invoices: invoices}
}

func (c *invoiceSalesDocumentCreator) CreateSalesInvoiceDocument(ctx context.Context, businessID string, input CreateDocumentInput) (*models.Document, error) {
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
	invoice, err := c.invoices.CreateByBusiness(ctx, businessID, invoiceInput)
	if err != nil {
		return nil, err
	}
	return invoiceDocumentProjection(invoice), nil
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
		TaxMode:               models.DocumentTaxModeNonGST,
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
		Direction:             models.DocumentDirectionOutward,
		Subtotal:              invoice.Subtotal,
		DiscountTotal:         invoice.Discount,
		TaxTotal:              invoice.Tax,
		Total:                 invoice.Total,
		PaidAmount:            invoice.PaidAmount,
		BalanceDue:            invoice.BalanceDue,
		ReportTags:            mustMarshalMap(nestedMap(taxProfile, "report_tags")),
		ProfitSnapshotEnabled: true,
	}
	if invoice.Tax > 0 {
		document.TaxMode = models.DocumentTaxModeGST
	}
	if document.BillOfSupply {
		document.DocumentType = models.DocumentTypeBillOfSupply
		document.TaxMode = models.DocumentTaxModeNonGST
	}

	document.Lines = make([]*models.DocumentLine, 0, len(invoice.Items))
	for _, item := range invoice.Items {
		lineSubtotal := (item.Quantity * item.UnitPrice) - item.Discount
		taxAmount := item.Total - lineSubtotal - item.CessAmount
		document.CessTotal += item.CessAmount
		document.Lines = append(document.Lines, &models.DocumentLine{
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
		})
	}
	document.WithholdingTotal, document.TDSTotal, document.TCSTotal =
		summarizeWithholdings(mapSliceToWithholdings(readMapSlice(taxProfile, "tcs")))
	return document
}
