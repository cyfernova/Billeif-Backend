package services

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"invoice-backend/internal/gst"
	"invoice-backend/internal/models"

	"gorm.io/gorm"
)

type UpdateInvoiceDraftLineInput struct {
	ID               string                 `json:"id,omitempty"`
	ProductID        string                 `json:"product_id,omitempty"`
	VariantID        string                 `json:"variant_id,omitempty"`
	WarehouseID      string                 `json:"warehouse_id,omitempty"`
	ItemType         string                 `json:"item_type,omitempty"`
	Description      string                 `json:"description" binding:"required"`
	HSNSACCode       string                 `json:"hsn_sac_code,omitempty"`
	SKU              string                 `json:"sku,omitempty"`
	Quantity         float64                `json:"quantity" binding:"required,gt=0"`
	FreeQuantity     float64                `json:"free_quantity"`
	Unit             string                 `json:"unit,omitempty"`
	UnitPrice        float64                `json:"unit_price,omitempty"`
	UnitPricePaise   int64                  `json:"unit_price_paise,omitempty"`
	Discount         float64                `json:"discount,omitempty"`
	DiscountPaise    int64                  `json:"discount_paise,omitempty"`
	TaxRate          float64                `json:"tax_rate,omitempty"`
	TaxRateBps       int                    `json:"tax_rate_bps,omitempty"`
	CessRate         float64                `json:"cess_rate,omitempty"`
	CessRateBps      int                    `json:"cess_rate_bps,omitempty"`
	Batch            string                 `json:"batch,omitempty"`
	CustomFields     map[string]interface{} `json:"custom_fields,omitempty"`
	BatchAllocations []BatchAllocationInput `json:"batch_allocations,omitempty"`
	SerialIDs        []string               `json:"serial_ids,omitempty"`
}

type UpdateInvoiceDraftInput struct {
	Version            int                           `json:"version" binding:"required"`
	CustomerID         *string                       `json:"customer_id,omitempty"`
	CustomerSnapshot   map[string]interface{}        `json:"customer_snapshot,omitempty"`
	Document           map[string]interface{}        `json:"document,omitempty"`
	Details            map[string]interface{}        `json:"details,omitempty"`
	Items              []UpdateInvoiceDraftLineInput `json:"items" binding:"required,min=1,dive"`
	TemplateOverride   map[string]interface{}        `json:"template_override,omitempty"`
	PaymentDisplay     map[string]interface{}        `json:"payment_display,omitempty"`
	Terms              map[string]interface{}        `json:"terms,omitempty"`
	EWayBillDetails    map[string]interface{}        `json:"eway_bill_details,omitempty"`
	EInvoiceSettings   map[string]interface{}        `json:"einvoice_settings,omitempty"`
	TaxProfile         *TaxProfileInput              `json:"tax_profile,omitempty"`
	RenderProfileID    *string                       `json:"render_profile_id,omitempty" binding:"omitempty,uuid"`
	InvoiceDate        time.Time                     `json:"invoice_date"`
	DueDate            time.Time                     `json:"due_date"`
	PONumber           string                        `json:"po_number,omitempty"`
	Notes              string                        `json:"notes,omitempty"`
	TermsAndConditions string                        `json:"terms_and_conditions,omitempty"`
	EditReason         string                        `json:"edit_reason,omitempty"`
}

type draftLineCalculation struct {
	subtotalPaise int64
	discountPaise int64
	taxablePaise  int64
	cgstPaise     int64
	sgstPaise     int64
	igstPaise     int64
	cessPaise     int64
	totalPaise    int64
}

type draftCalculation struct {
	subtotalPaise int64
	discountPaise int64
	taxablePaise  int64
	cgstPaise     int64
	sgstPaise     int64
	igstPaise     int64
	cessPaise     int64
	totalPaise    int64
	lines         []draftLineCalculation
}

func (s *InvoiceService) UpdateDraftByBusiness(ctx context.Context, businessID, id string, input UpdateInvoiceDraftInput) (*models.Invoice, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("draft invoice requires at least one item")
	}

	var before map[string]interface{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var invoice models.Invoice
		if err := tx.Preload("Items").Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).First(&invoice).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("invoice not found")
			}
			return err
		}
		hydrateInvoiceEditorFields(&invoice)
		before = invoiceAuditSnapshot(&invoice)

		if invoice.SignedAt != nil {
			return fmt.Errorf("signed invoices are immutable")
		}
		if invoice.Status != "draft" {
			return fmt.Errorf("only draft invoices can be updated through draft endpoint")
		}
		currentVersion := invoice.Version
		if currentVersion <= 0 {
			currentVersion = 1
		}
		if input.Version != currentVersion {
			return fmt.Errorf("invoice version conflict")
		}

		customerID := invoice.CustomerID
		if input.CustomerID != nil && strings.TrimSpace(*input.CustomerID) != "" {
			customerIDValue := strings.TrimSpace(*input.CustomerID)
			customerID = &customerIDValue
			if s.customerRepo != nil {
				if _, err := s.customerRepo.GetByID(ctx, customerIDValue, businessID); err != nil {
					return fmt.Errorf("customer not found: %w", err)
				}
			}
		}

		renderProfileID := invoice.RenderProfileID
		if input.RenderProfileID != nil {
			candidate := strings.TrimSpace(*input.RenderProfileID)
			if candidate == "" {
				renderProfileID = nil
			} else {
				normalized, err := normalizeRenderProfileID(candidate)
				if err != nil {
					return err
				}
				if s.documents != nil {
					if _, err := s.documents.GetRenderProfileByBusiness(ctx, businessID, normalized); err != nil {
						return fmt.Errorf("render profile not found: %w", err)
					}
				}
				renderProfileID = &normalized
			}
		}

		documentJSON := map[string]interface{}{
			"document": input.Document,
			"details":  input.Details,
		}
		calculated := calculateDraftInvoicePaise(input.Document, input.Details, input.Items, rupeesToPaise(invoice.PaidAmount))
		nextVersion := currentVersion + 1
		invoiceDate := invoice.InvoiceDate
		if !input.InvoiceDate.IsZero() {
			invoiceDate = input.InvoiceDate
		}
		dueDate := invoice.DueDate
		if !input.DueDate.IsZero() {
			dueDate = input.DueDate
		}

		customFields := unmarshalJSONMap(invoice.CustomFields)
		customFields = mergeInvoiceEditorCustomFields(customFields, input.TermsAndConditions, input.PONumber, input.TemplateOverride)
		if input.PaymentDisplay != nil {
			customFields["payment_display"] = input.PaymentDisplay
		}
		if input.CustomerSnapshot != nil {
			customFields["customer_snapshot"] = input.CustomerSnapshot
		}
		if len(documentJSON) > 0 {
			customFields["document_json"] = documentJSON
		}

		taxProfile := invoice.TaxProfile
		if strings.TrimSpace(taxProfile) == "" {
			taxProfile = "{}"
		}
		if input.TaxProfile != nil {
			taxProfile = mustMarshalMap(taxProfileToMap(*input.TaxProfile))
		}

		updates := map[string]interface{}{
			"customer_id":            customerID,
			"version":                nextVersion,
			"invoice_date":           invoiceDate,
			"due_date":               dueDate,
			"render_profile_id":      renderProfileID,
			"notes":                  input.Notes,
			"custom_fields":          mustMarshalMap(customFields),
			"customer_snapshot":      mustMarshalMap(input.CustomerSnapshot),
			"document_json":          mustMarshalMap(documentJSON),
			"template_override":      mustMarshalMap(input.TemplateOverride),
			"payment_display":        mustMarshalMap(input.PaymentDisplay),
			"terms_json":             mustMarshalMap(input.Terms),
			"eway_details_json":      mustMarshalMap(input.EWayBillDetails),
			"einvoice_settings_json": mustMarshalMap(input.EInvoiceSettings),
			"tax_profile":            taxProfile,
			"subtotal":               paiseToRupees(calculated.subtotalPaise),
			"discount":               paiseToRupees(calculated.discountPaise),
			"tax":                    paiseToRupees(calculated.cgstPaise + calculated.sgstPaise + calculated.igstPaise + calculated.cessPaise),
			"total":                  paiseToRupees(calculated.totalPaise),
			"balance_due":            paiseToRupees(maxInt64(0, calculated.totalPaise-rupeesToPaise(invoice.PaidAmount))),
			"updated_at":             time.Now(),
		}
		result := tx.Model(&models.Invoice{}).
			Where("id = ? AND business_id = ? AND version = ?", id, businessID, currentVersion).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("invoice version conflict")
		}
		if err := tx.Where("invoice_id = ?", id).Delete(&models.InvoiceItem{}).Error; err != nil {
			return err
		}
		for index, item := range input.Items {
			row := draftInputLineToModel(id, item, calculated.lines[index])
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	invoice, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	if s.documents != nil {
		if err := s.documents.MirrorLegacyInvoice(ctx, invoice); err != nil {
			s.log.Error("failed to mirror draft invoice update", "invoice_id", invoice.ID, "error", err)
		}
	}
	_ = recordActivityLog(ctx, s.db, businessID, "invoice", invoice.ID, "draft_updated", input.EditReason, invoice, map[string]interface{}{
		"before": before,
		"after":  invoiceAuditSnapshot(invoice),
	}, map[string]interface{}{"endpoint": "PATCH /invoices/:id/draft"})
	return invoice, nil
}

func calculateDraftInvoicePaise(document, details map[string]interface{}, items []UpdateInvoiceDraftLineInput, paidPaise int64) draftCalculation {
	lines := make([]draftLineCalculation, len(items))
	var result draftCalculation
	for i, item := range items {
		line := calculateDraftLinePaise(document, details, item)
		lines[i] = line
		result.subtotalPaise += line.subtotalPaise
		result.discountPaise += line.discountPaise
		result.taxablePaise += line.taxablePaise
		result.cgstPaise += line.cgstPaise
		result.sgstPaise += line.sgstPaise
		result.igstPaise += line.igstPaise
		result.cessPaise += line.cessPaise
		result.totalPaise += line.totalPaise
	}
	_ = paidPaise
	result.lines = lines
	return result
}

func calculateDraftLinePaise(document, details map[string]interface{}, item UpdateInvoiceDraftLineInput) draftLineCalculation {
	quantity := math.Max(0, item.Quantity)
	ratePaise := item.UnitPricePaise
	if ratePaise <= 0 {
		ratePaise = rupeesToPaise(item.UnitPrice)
	}
	discountPaise := item.DiscountPaise
	if discountPaise <= 0 && item.Discount > 0 {
		discountPaise = rupeesToPaise(item.Discount)
	}
	taxRateBps := item.TaxRateBps
	if taxRateBps <= 0 && item.TaxRate > 0 {
		taxRateBps = int(math.Round(item.TaxRate * 100))
	}
	cessRateBps := item.CessRateBps
	if cessRateBps <= 0 && item.CessRate > 0 {
		cessRateBps = int(math.Round(item.CessRate * 100))
	}

	grossPaise := int64(math.Round(quantity * float64(ratePaise)))
	if discountPaise > grossPaise {
		discountPaise = grossPaise
	}
	afterDiscountPaise := maxInt64(0, grossPaise-discountPaise)
	taxablePaise := afterDiscountPaise
	taxPaise := int64(0)
	cessPaise := int64(0)
	if isGSTDraft(document) && taxRateBps > 0 {
		if strings.EqualFold(readString(details, "price_mode", "priceMode"), "TAX_INCLUSIVE") {
			taxablePaise = int64(math.Round(float64(afterDiscountPaise) * 10000 / float64(10000+taxRateBps)))
			taxPaise = afterDiscountPaise - taxablePaise
			cessPaise = int64(math.Round(float64(taxablePaise*int64(cessRateBps)) / 10000))
		} else {
			taxPaise = int64(math.Round(float64(taxablePaise*int64(taxRateBps)) / 10000))
			cessPaise = int64(math.Round(float64(taxablePaise*int64(cessRateBps)) / 10000))
		}
	}
	sameState := sameStateDraftSupply(document, details)
	cgstPaise := int64(0)
	sgstPaise := int64(0)
	igstPaise := int64(0)
	if isGSTDraft(document) {
		if sameState {
			cgstPaise = taxPaise / 2
			sgstPaise = taxPaise - cgstPaise
		} else {
			igstPaise = taxPaise
		}
	}
	return draftLineCalculation{
		subtotalPaise: grossPaise,
		discountPaise: discountPaise,
		taxablePaise:  taxablePaise,
		cgstPaise:     cgstPaise,
		sgstPaise:     sgstPaise,
		igstPaise:     igstPaise,
		cessPaise:     cessPaise,
		totalPaise:    taxablePaise + taxPaise + cessPaise,
	}
}

func draftInputLineToModel(invoiceID string, item UpdateInvoiceDraftLineInput, calc draftLineCalculation) *models.InvoiceItem {
	customFields := item.CustomFields
	if customFields == nil {
		customFields = map[string]interface{}{}
	}
	customFields["item_type"] = firstNonEmpty(item.ItemType, "GOODS")
	if item.SKU != "" {
		customFields["sku"] = item.SKU
	}
	if item.Batch != "" {
		customFields["batch"] = item.Batch
	}

	productID := stringPointer(item.ProductID)
	variantID := stringPointer(item.VariantID)
	warehouseID := stringPointer(item.WarehouseID)
	taxRateBps := item.TaxRateBps
	if taxRateBps <= 0 && item.TaxRate > 0 {
		taxRateBps = int(math.Round(item.TaxRate * 100))
	}
	cessRateBps := item.CessRateBps
	if cessRateBps <= 0 && item.CessRate > 0 {
		cessRateBps = int(math.Round(item.CessRate * 100))
	}
	ratePaise := item.UnitPricePaise
	if ratePaise <= 0 {
		ratePaise = rupeesToPaise(item.UnitPrice)
	}
	discountPaise := item.DiscountPaise
	if discountPaise <= 0 && item.Discount > 0 {
		discountPaise = rupeesToPaise(item.Discount)
	}
	return &models.InvoiceItem{
		InvoiceID:        invoiceID,
		ProductID:        productID,
		VariantID:        variantID,
		WarehouseID:      warehouseID,
		Description:      item.Description,
		HSNSACCode:       item.HSNSACCode,
		Unit:             gst.CanonicalSnapshotUQC(item.Unit, ""),
		Quantity:         item.Quantity,
		FreeQuantity:     item.FreeQuantity,
		UnitPrice:        paiseToRupees(ratePaise),
		Discount:         paiseToRupees(discountPaise),
		TaxRate:          float64(taxRateBps) / 100,
		CessRate:         float64(cessRateBps) / 100,
		CessAmount:       paiseToRupees(calc.cessPaise),
		CustomFields:     mustMarshalMap(customFields),
		BatchAllocations: mustMarshalBatchAllocations(item.BatchAllocations),
		SerialIDs:        marshalStringSlice(item.SerialIDs),
		Total:            paiseToRupees(calc.totalPaise),
	}
}

func hydrateInvoiceItemEditorFields(item *models.InvoiceItem) {
	if item == nil {
		return
	}
	fields := unmarshalJSONMap(item.CustomFields)
	item.SKU = readString(fields, "sku")
	item.Batch = readString(fields, "batch")
	item.ItemType = firstNonEmpty(readString(fields, "item_type", "itemType"), "GOODS")
	item.Amount = item.Total
}

func invoiceAuditSnapshot(invoice *models.Invoice) map[string]interface{} {
	if invoice == nil {
		return map[string]interface{}{}
	}
	items := make([]map[string]interface{}, 0, len(invoice.Items))
	for _, item := range invoice.Items {
		if item == nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"id":                item.ID,
			"product_id":        item.ProductID,
			"variant_id":        item.VariantID,
			"warehouse_id":      item.WarehouseID,
			"description":       item.Description,
			"hsn_sac_code":      item.HSNSACCode,
			"unit":              item.Unit,
			"quantity":          item.Quantity,
			"free_quantity":     item.FreeQuantity,
			"unit_price":        item.UnitPrice,
			"discount":          item.Discount,
			"tax_rate":          item.TaxRate,
			"cess_rate":         item.CessRate,
			"cess_amount":       item.CessAmount,
			"total":             item.Total,
			"custom_fields":     item.CustomFields,
			"batch_allocations": item.BatchAllocations,
			"serial_ids":        item.SerialIDs,
		})
	}
	return map[string]interface{}{
		"id":                     invoice.ID,
		"version":                invoice.Version,
		"customer_id":            invoice.CustomerID,
		"invoice_no":             invoice.InvoiceNo,
		"status":                 invoice.Status,
		"invoice_date":           invoice.InvoiceDate,
		"due_date":               invoice.DueDate,
		"subtotal":               invoice.Subtotal,
		"tax":                    invoice.Tax,
		"discount":               invoice.Discount,
		"total":                  invoice.Total,
		"balance_due":            invoice.BalanceDue,
		"notes":                  invoice.Notes,
		"customer_snapshot":      invoice.CustomerSnapshot,
		"document_json":          invoice.DocumentJSON,
		"template_override":      invoice.TemplateOverride,
		"payment_display":        invoice.PaymentDisplay,
		"terms_json":             invoice.TermsJSON,
		"eway_details_json":      invoice.EWayDetailsJSON,
		"einvoice_settings_json": invoice.EInvoiceSettingsJSON,
		"tax_profile":            invoice.TaxProfile,
		"custom_fields":          invoice.CustomFields,
		"items":                  items,
	}
}

func isGSTDraft(document map[string]interface{}) bool {
	return strings.EqualFold(readString(document, "tax_mode", "taxMode"), "GST")
}

func sameStateDraftSupply(document, details map[string]interface{}) bool {
	seller := readString(document, "supplier_state_code", "supplierStateCode")
	if seller == "" {
		if compliance, ok := document["gst_compliance"].(map[string]interface{}); ok {
			seller = readString(compliance, "state_code", "stateCode")
		}
		if compliance, ok := document["gstCompliance"].(map[string]interface{}); ok {
			seller = readString(compliance, "state_code", "stateCode")
		}
	}
	place := readString(details, "place_of_supply_state_code", "placeOfSupplyStateCode")
	return seller != "" && place != "" && seller == place
}

func readString(source map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := source[key]; ok {
			switch typed := value.(type) {
			case string:
				return strings.TrimSpace(typed)
			case fmt.Stringer:
				return strings.TrimSpace(typed.String())
			}
		}
	}
	return ""
}

func rupeesToPaise(value float64) int64 {
	return int64(math.Round(value * 100))
}

func paiseToRupees(value int64) float64 {
	return float64(value) / 100
}
