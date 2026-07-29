package invoiceprojection

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"invoice-backend/internal/gst"
	"invoice-backend/internal/models"
)

// Build returns the complete canonical Document projection for an invoice draft.
func Build(invoice *models.Invoice) *models.Document {
	taxProfile := jsonMap(invoice.TaxProfile)
	editorFields := jsonMap(invoice.CustomFields)
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
		TaxMode:               defaultTaxMode(models.DocumentTypeSalesInvoice, stringCandidate(taxProfile, "gst_treatment")),
		GSTTreatment:          firstNonEmpty(stringCandidate(taxProfile, "gst_treatment"), models.DocumentGSTTreatmentRegular),
		PlaceOfSupply:         stringCandidate(taxProfile, "place_of_supply"),
		PartyGSTIN:            stringCandidate(taxProfile, "counterparty_gstin"),
		PartyPAN:              stringCandidate(taxProfile, "counterparty_pan"),
		PartyStateCode:        stringCandidate(taxProfile, "counterparty_state_code"),
		SupplyType:            stringCandidate(taxProfile, "supply_type"),
		ExportType:            stringCandidate(taxProfile, "export_type"),
		BillOfSupply:          boolCandidate(taxProfile, "bill_of_supply"),
		SerialNumber:          "",
		IssueDate:             invoice.InvoiceDate,
		DueDate:               &invoice.DueDate,
		Currency:              invoice.Currency,
		ExchangeRate:          1,
		Locale:                "en-IN",
		SourceLinkage:         marshalMap(sourceLinkage),
		RenderProfileID:       invoice.RenderProfileID,
		ProjectID:             invoice.ProjectID,
		PriceListID:           invoice.PriceListID,
		OriginSubscriptionID:  invoice.OriginSubscriptionID,
		OriginRunID:           invoice.OriginRunID,
		ProfitSnapshotEnabled: true,
		GenerateEInvoice:      boolCandidate(taxProfile, "generate_einvoice"),
		GenerateEWayBill:      boolCandidate(taxProfile, "generate_ewaybill"),
		ReverseCharge:         boolCandidate(taxProfile, "reverse_charge"),
		ReverseChargeReason:   stringCandidate(taxProfile, "reverse_charge_reason"),
		DispatchFrom:          marshalMap(nestedMap(taxProfile, "dispatch_from")),
		DispatchTo:            marshalMap(nestedMap(taxProfile, "dispatch_to")),
		DistanceKM:            numberValue(taxProfile["distance_km"]),
		Transporter:           marshalMap(nestedMap(taxProfile, "transporter")),
		Vehicle:               marshalMap(nestedMap(taxProfile, "vehicle")),
		MultiVehiclePlan:      marshalMap(nestedMap(taxProfile, "multi_vehicle_plan")),
		Notes:                 invoice.Notes,
		Terms:                 terms,
		Direction:             models.DocumentDirectionOutward,
		Subtotal:              invoice.Subtotal,
		DiscountTotal:         invoice.Discount,
		Total:                 invoice.Total,
		PaidAmount:            invoice.PaidAmount,
		BalanceDue:            invoice.BalanceDue,
		ExtraFields:           marshalMap(extraFields),
		ReportTags:            marshalMap(nestedMap(taxProfile, "report_tags")),
	}
	if document.BillOfSupply {
		document.DocumentType = models.DocumentTypeBillOfSupply
		document.TaxMode = models.DocumentTaxModeNonGST
	}

	intraState := isIntraState(invoice, document)
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
	document.WithholdingTotal, document.TDSTotal, document.TCSTotal = withholdingTotals(taxProfile["tcs"])
	return document
}

// Validate rejects any persisted Document or line semantics that differ from
// the canonical invoice projection.
func Validate(invoice *models.Invoice, document *models.Document) error {
	if invoice == nil || document == nil {
		return fmt.Errorf("invoice projection is required")
	}
	expected := Build(invoice)
	expectedDocument := normalizedDocument(expected)
	actualDocument := normalizedDocument(document)
	if !reflect.DeepEqual(expectedDocument, actualDocument) {
		return fmt.Errorf("document legal or pricing projection differs from invoice")
	}
	expectedLines := normalizedLines(expected.Lines)
	actualLines := normalizedLines(document.Lines)
	if !reflect.DeepEqual(expectedLines, actualLines) {
		return fmt.Errorf("document line projection differs from invoice items")
	}
	return nil
}

func normalizedDocument(document *models.Document) models.Document {
	normalized := *document
	normalized.Lines = nil
	normalized.CreatedAt = time.Time{}
	normalized.UpdatedAt = time.Time{}
	normalized.DeletedAt = models.Document{}.DeletedAt
	normalized.IssueDate = normalized.IssueDate.UTC()
	normalized.DueDate = normalizedTimePointer(normalized.DueDate)
	normalized.DispatchDate = normalizedTimePointer(normalized.DispatchDate)
	normalized.FXRateTimestamp = normalizedTimePointer(normalized.FXRateTimestamp)
	normalized.SignedAt = normalizedTimePointer(normalized.SignedAt)
	normalized.CancelledAt = normalizedTimePointer(normalized.CancelledAt)
	normalized.SourceLinkage = normalizedJSON(normalized.SourceLinkage, "{}")
	normalized.FXMetadata = normalizedJSON(normalized.FXMetadata, "{}")
	normalized.DispatchFrom = normalizedJSON(normalized.DispatchFrom, "{}")
	normalized.DispatchTo = normalizedJSON(normalized.DispatchTo, "{}")
	normalized.Transporter = normalizedJSON(normalized.Transporter, "{}")
	normalized.Vehicle = normalizedJSON(normalized.Vehicle, "{}")
	normalized.MultiVehiclePlan = normalizedJSON(normalized.MultiVehiclePlan, "{}")
	normalized.SignMetadata = normalizedJSON(normalized.SignMetadata, "{}")
	normalized.ExtraFields = normalizedJSON(normalized.ExtraFields, "{}")
	normalized.ReportTags = normalizedJSON(normalized.ReportTags, "{}")
	return normalized
}

func normalizedLines(lines []*models.DocumentLine) []models.DocumentLine {
	normalized := make([]models.DocumentLine, 0, len(lines))
	for _, line := range lines {
		if line == nil {
			normalized = append(normalized, models.DocumentLine{})
			continue
		}
		value := *line
		value.CreatedAt = time.Time{}
		value.UpdatedAt = time.Time{}
		value.CustomFields = normalizedJSON(value.CustomFields, "{}")
		value.ChargeLinkage = normalizedJSON(value.ChargeLinkage, "[]")
		value.PackingMetadata = normalizedJSON(value.PackingMetadata, "{}")
		value.BatchAllocations = normalizedJSON(value.BatchAllocations, "[]")
		value.SerialIDs = normalizedJSON(value.SerialIDs, "[]")
		value.ReportTags = normalizedJSON(value.ReportTags, "{}")
		normalized = append(normalized, value)
	}
	sort.Slice(normalized, func(left, right int) bool {
		return normalized[left].ID < normalized[right].ID
	})
	return normalized
}

func normalizedTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func normalizedJSON(raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		raw = fallback
	}
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return string(encoded)
}

func jsonMap(raw string) map[string]interface{} {
	if strings.TrimSpace(raw) == "" {
		return map[string]interface{}{}
	}
	var value map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return map[string]interface{}{}
	}
	return value
}

func nestedMap(value map[string]interface{}, key string) map[string]interface{} {
	nested, _ := value[key].(map[string]interface{})
	result := make(map[string]interface{}, len(nested))
	for nestedKey, nestedValue := range nested {
		result[nestedKey] = nestedValue
	}
	return result
}

func stringCandidate(value map[string]interface{}, key string) string {
	result, _ := value[key].(string)
	return result
}

func boolCandidate(value map[string]interface{}, key string) bool {
	result, _ := value[key].(bool)
	return result
}

func numberValue(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		result, _ := typed.Float64()
		return result
	default:
		return 0
	}
}

func marshalMap(value map[string]interface{}) string {
	if len(value) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func defaultTaxMode(documentType, gstTreatment string) string {
	if documentType == models.DocumentTypeBillOfSupply ||
		gstTreatment == models.DocumentGSTTreatmentComposition ||
		gstTreatment == models.DocumentGSTTreatmentExempt {
		return models.DocumentTaxModeNonGST
	}
	return models.DocumentTaxModeGST
}

func isIntraState(invoice *models.Invoice, document *models.Document) bool {
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

func roundCurrency(value float64) float64 {
	return math.Round(value*100) / 100
}

func withholdingTotals(raw interface{}) (total, tds, tcs float64) {
	items, _ := raw.([]interface{})
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]interface{})
		amount := numberValue(item["amount"])
		total += amount
		if stringCandidate(item, "withholding_type") == models.WithholdingTypeTCS {
			tcs += amount
		} else {
			tds += amount
		}
	}
	return total, tds, tcs
}
