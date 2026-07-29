package invoiceprojection

import (
	"encoding/json"
	"fmt"
	"math"
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
	if legalPricingDocument(expected) != legalPricingDocument(document) {
		return fmt.Errorf("document legal or pricing projection differs from invoice")
	}
	expectedLines := legalPricingLines(expected.Lines)
	actualLines := legalPricingLines(document.Lines)
	if len(expectedLines) != len(actualLines) {
		return fmt.Errorf("document line projection differs from invoice items")
	}
	for index := range expectedLines {
		if expectedLines[index] != actualLines[index] {
			return fmt.Errorf("document line projection differs from invoice items")
		}
	}
	return nil
}

type optionalString struct {
	Set   bool
	Value string
}

type optionalTime struct {
	Set   bool
	Value time.Time
}

type documentProjection struct {
	ID                    string
	BusinessID            string
	DocumentType          string
	PartyType             string
	PartyID               optionalString
	Status                string
	DraftState            string
	TaxMode               string
	GSTTreatment          string
	PlaceOfSupply         string
	PartyGSTIN            string
	PartyPAN              string
	PartyStateCode        string
	SupplyType            string
	ExportType            string
	BillOfSupply          bool
	SerialNumber          string
	IssueDate             time.Time
	DueDate               optionalTime
	Currency              string
	ExchangeRate          float64
	Locale                string
	SourceLinkage         string
	RenderProfileID       optionalString
	ProjectID             optionalString
	PriceListID           optionalString
	OriginSubscriptionID  optionalString
	OriginRunID           optionalString
	ProfitSnapshotEnabled bool
	GenerateEInvoice      bool
	GenerateEWayBill      bool
	ReverseCharge         bool
	ReverseChargeReason   string
	DispatchFrom          string
	DispatchTo            string
	DistanceKM            float64
	Transporter           string
	Vehicle               string
	MultiVehiclePlan      string
	Notes                 string
	Terms                 string
	Direction             string
	Subtotal              float64
	DiscountTotal         float64
	TaxTotal              float64
	CessTotal             float64
	WithholdingTotal      float64
	TDSTotal              float64
	TCSTotal              float64
	Total                 float64
	PaidAmount            float64
	BalanceDue            float64
	ExtraFields           string
	ReportTags            string
}

func legalPricingDocument(document *models.Document) documentProjection {
	return documentProjection{
		ID:                    document.ID,
		BusinessID:            document.BusinessID,
		DocumentType:          document.DocumentType,
		PartyType:             document.PartyType,
		PartyID:               normalizedStringPointer(document.PartyID),
		Status:                document.Status,
		DraftState:            document.DraftState,
		TaxMode:               document.TaxMode,
		GSTTreatment:          document.GSTTreatment,
		PlaceOfSupply:         document.PlaceOfSupply,
		PartyGSTIN:            document.PartyGSTIN,
		PartyPAN:              document.PartyPAN,
		PartyStateCode:        document.PartyStateCode,
		SupplyType:            document.SupplyType,
		ExportType:            document.ExportType,
		BillOfSupply:          document.BillOfSupply,
		SerialNumber:          document.SerialNumber,
		IssueDate:             document.IssueDate.UTC(),
		DueDate:               normalizedTimePointer(document.DueDate),
		Currency:              document.Currency,
		ExchangeRate:          roundScale(document.ExchangeRate, 6),
		Locale:                document.Locale,
		SourceLinkage:         normalizedJSON(document.SourceLinkage, "{}"),
		RenderProfileID:       normalizedStringPointer(document.RenderProfileID),
		ProjectID:             normalizedStringPointer(document.ProjectID),
		PriceListID:           normalizedStringPointer(document.PriceListID),
		OriginSubscriptionID:  normalizedStringPointer(document.OriginSubscriptionID),
		OriginRunID:           normalizedStringPointer(document.OriginRunID),
		ProfitSnapshotEnabled: document.ProfitSnapshotEnabled,
		GenerateEInvoice:      document.GenerateEInvoice,
		GenerateEWayBill:      document.GenerateEWayBill,
		ReverseCharge:         document.ReverseCharge,
		ReverseChargeReason:   document.ReverseChargeReason,
		DispatchFrom:          normalizedJSON(document.DispatchFrom, "{}"),
		DispatchTo:            normalizedJSON(document.DispatchTo, "{}"),
		DistanceKM:            roundScale(document.DistanceKM, 2),
		Transporter:           normalizedJSON(document.Transporter, "{}"),
		Vehicle:               normalizedJSON(document.Vehicle, "{}"),
		MultiVehiclePlan:      normalizedJSON(document.MultiVehiclePlan, "{}"),
		Notes:                 document.Notes,
		Terms:                 document.Terms,
		Direction:             document.Direction,
		Subtotal:              roundScale(document.Subtotal, 2),
		DiscountTotal:         roundScale(document.DiscountTotal, 2),
		TaxTotal:              roundScale(document.TaxTotal, 2),
		CessTotal:             roundScale(document.CessTotal, 2),
		WithholdingTotal:      roundScale(document.WithholdingTotal, 2),
		TDSTotal:              roundScale(document.TDSTotal, 2),
		TCSTotal:              roundScale(document.TCSTotal, 2),
		Total:                 roundScale(document.Total, 2),
		PaidAmount:            roundScale(document.PaidAmount, 2),
		BalanceDue:            roundScale(document.BalanceDue, 2),
		ExtraFields:           normalizedJSON(document.ExtraFields, "{}"),
		ReportTags:            normalizedJSON(document.ReportTags, "{}"),
	}
}

type lineProjection struct {
	ID                string
	DocumentID        string
	ProductID         optionalString
	VariantID         optionalString
	Description       string
	HSNSACCode        string
	UQCCode           string
	Unit              string
	WarehouseID       optionalString
	Quantity          float64
	FreeQuantity      float64
	RemainingQuantity float64
	UnitPrice         float64
	MRP               float64
	DiscountAmount    float64
	TaxRate           float64
	CGSTRate          float64
	SGSTRate          float64
	IGSTRate          float64
	CessRate          float64
	CGSTAmount        float64
	SGSTAmount        float64
	IGSTAmount        float64
	CessAmount        float64
	TaxAmount         float64
	LineSubtotal      float64
	LineTotal         float64
	CustomFields      string
	ChargeLinkage     string
	BatchAllocations  string
	SerialIDs         string
	StockEffect       string
}

func legalPricingLines(lines []*models.DocumentLine) []lineProjection {
	normalized := make([]lineProjection, 0, len(lines))
	for _, line := range lines {
		if line == nil {
			normalized = append(normalized, lineProjection{})
			continue
		}
		normalized = append(normalized, lineProjection{
			ID:                line.ID,
			DocumentID:        line.DocumentID,
			ProductID:         normalizedStringPointer(line.ProductID),
			VariantID:         normalizedStringPointer(line.VariantID),
			Description:       line.Description,
			HSNSACCode:        line.HSNSACCode,
			UQCCode:           line.UQCCode,
			Unit:              line.Unit,
			WarehouseID:       normalizedStringPointer(line.WarehouseID),
			Quantity:          roundScale(line.Quantity, 3),
			FreeQuantity:      roundScale(line.FreeQuantity, 3),
			RemainingQuantity: roundScale(line.RemainingQuantity, 3),
			UnitPrice:         roundScale(line.UnitPrice, 2),
			MRP:               roundScale(line.MRP, 2),
			DiscountAmount:    roundScale(line.DiscountAmount, 2),
			TaxRate:           roundScale(line.TaxRate, 3),
			CGSTRate:          roundScale(line.CGSTRate, 3),
			SGSTRate:          roundScale(line.SGSTRate, 3),
			IGSTRate:          roundScale(line.IGSTRate, 3),
			CessRate:          roundScale(line.CessRate, 3),
			CGSTAmount:        roundScale(line.CGSTAmount, 2),
			SGSTAmount:        roundScale(line.SGSTAmount, 2),
			IGSTAmount:        roundScale(line.IGSTAmount, 2),
			CessAmount:        roundScale(line.CessAmount, 2),
			TaxAmount:         roundScale(line.TaxAmount, 2),
			LineSubtotal:      roundScale(line.LineSubtotal, 2),
			LineTotal:         roundScale(line.LineTotal, 2),
			CustomFields:      normalizedJSON(line.CustomFields, "{}"),
			ChargeLinkage:     normalizedJSON(line.ChargeLinkage, "[]"),
			BatchAllocations:  normalizedJSON(line.BatchAllocations, "[]"),
			SerialIDs:         normalizedJSON(line.SerialIDs, "[]"),
			StockEffect:       line.StockEffect,
		})
	}
	sort.Slice(normalized, func(left, right int) bool {
		return normalized[left].ID < normalized[right].ID
	})
	return normalized
}

func normalizedStringPointer(value *string) optionalString {
	if value == nil {
		return optionalString{}
	}
	return optionalString{Set: true, Value: *value}
}

func normalizedTimePointer(value *time.Time) optionalTime {
	if value == nil {
		return optionalTime{}
	}
	return optionalTime{Set: true, Value: value.UTC()}
}

func roundScale(value float64, scale int) float64 {
	factor := math.Pow10(scale)
	return math.Round(value*factor) / factor
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
