package services

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"invoice-backend/internal/models"
)

// IRP schema 1.1: https://einvoice6.gst.gov.in/content/notified-e-invoice-schema/
// Amounts come from the finalized document; registration must not recalculate tax.
func irpInvoicePayload(document *models.Document, seller *models.BusinessProfile, buyer *models.Customer) (map[string]interface{}, error) {
	if document == nil || seller == nil || buyer == nil {
		return nil, fmt.Errorf("invoice, business and customer details are required")
	}
	if document.BusinessID != seller.ID || buyer.BusinessID != seller.ID {
		return nil, fmt.Errorf("invoice parties must belong to the selected business")
	}
	if issues := eInvoiceDocumentIssues(document, time.Now()); len(issues) > 0 {
		return nil, fmt.Errorf("%s", issues[0].Message)
	}
	if document.Currency != "INR" {
		return nil, fmt.Errorf("IRP registration requires invoice amounts in INR")
	}
	if math.IsNaN(document.Total) || math.IsInf(document.Total, 0) || document.Total < 0 {
		return nil, fmt.Errorf("invoice total must be a finite non-negative amount")
	}
	if (document.GSTTreatment != "" && document.GSTTreatment != models.DocumentGSTTreatmentRegular) || (document.SupplyType != "" && !strings.EqualFold(document.SupplyType, "B2B")) {
		return nil, fmt.Errorf("this supply type needs its IRP export or SEZ details before registration")
	}
	typ := map[string]string{models.DocumentTypeSalesInvoice: "INV", models.DocumentTypeCreditNote: "CRN", models.DocumentTypeDebitNote: "DBN"}[document.DocumentType]
	party := func(gstin, name, address, city, pin, state string) (map[string]interface{}, error) {
		gstin = strings.ToUpper(strings.TrimSpace(gstin))
		if len(gstin) != 15 || strings.TrimSpace(name) == "" || strings.TrimSpace(address) == "" || strings.TrimSpace(city) == "" {
			return nil, fmt.Errorf("GSTIN, legal name, address and city are required")
		}
		pincode, err := strconv.Atoi(strings.TrimSpace(pin))
		if err != nil || pincode < 100000 || pincode > 999999 {
			return nil, fmt.Errorf("a six-digit Indian pincode is required")
		}
		state = strings.TrimSpace(state)
		if len(state) != 2 || state != gstin[:2] {
			return nil, fmt.Errorf("state code must match the GSTIN")
		}
		return map[string]interface{}{"Gstin": gstin, "LglNm": name, "Addr1": address, "Loc": city, "Pin": pincode, "Stcd": state}, nil
	}
	sellerDetails, err := party(seller.GSTIN, seller.Name, seller.Address, seller.City, seller.PostalCode, seller.BusinessStateCode)
	if err != nil {
		return nil, fmt.Errorf("business: %w", err)
	}
	buyerDetails, err := party(document.PartyGSTIN, firstNonEmpty(buyer.CompanyName, buyer.Name), buyer.Address, buyer.City, buyer.PostalCode, firstNonEmpty(document.PartyStateCode, buyer.StateCode))
	if err != nil {
		return nil, fmt.Errorf("customer: %w", err)
	}
	pos := strings.TrimSpace(document.PlaceOfSupply)
	if len(pos) != 2 {
		return nil, fmt.Errorf("a two-digit place-of-supply state code is required")
	}
	if _, err := strconv.Atoi(pos); err != nil {
		return nil, fmt.Errorf("place of supply must be a state code")
	}
	buyerDetails["Pos"] = pos
	items := make([]map[string]interface{}, 0, len(document.Lines))
	var taxable, cgst, sgst, igst, cess, total float64
	for index, line := range document.Lines {
		for _, amount := range []float64{line.Quantity, line.FreeQuantity, line.UnitPrice, line.DiscountAmount, line.LineSubtotal, line.TaxRate, line.CessRate, line.CGSTAmount, line.SGSTAmount, line.IGSTAmount, line.CessAmount, line.LineTotal} {
			if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
				return nil, fmt.Errorf("item %d has an invalid amount", index+1)
			}
		}
		isService := "N"
		if strings.HasPrefix(line.HSNSACCode, "99") {
			isService = "Y"
		}
		items = append(items, map[string]interface{}{
			"SlNo": strconv.Itoa(index + 1), "PrdDesc": line.Description, "IsServc": isService, "HsnCd": strings.TrimSpace(line.HSNSACCode),
			"Qty": line.Quantity, "FreeQty": line.FreeQuantity, "Unit": normalizeUQCCode(firstNonEmpty(line.UQCCode, line.Unit)), "UnitPrice": line.UnitPrice,
			"TotAmt": irpAmount(line.LineSubtotal + line.DiscountAmount), "Discount": line.DiscountAmount, "AssAmt": line.LineSubtotal,
			"GstRt": line.TaxRate, "CgstAmt": line.CGSTAmount, "SgstAmt": line.SGSTAmount, "IgstAmt": line.IGSTAmount,
			"CesRt": line.CessRate, "CesAmt": line.CessAmount, "TotItemVal": line.LineTotal,
		})
		taxable += line.LineSubtotal
		cgst += line.CGSTAmount
		sgst += line.SGSTAmount
		igst += line.IGSTAmount
		cess += line.CessAmount
		total += line.LineTotal
	}
	if math.Abs(irpAmount(total)-irpAmount(document.Total)) > 0.01 {
		return nil, fmt.Errorf("invoice total does not match its items; review charges and rounding before registration")
	}
	return map[string]interface{}{
		"Version": "1.1", "TranDtls": map[string]interface{}{"TaxSch": "GST", "SupTyp": "B2B", "RegRev": boolYN(document.ReverseCharge), "IgstOnIntra": boolYN(seller.BusinessStateCode == pos && igst > 0)},
		"DocDtls":    map[string]interface{}{"Typ": typ, "No": document.SerialNumber, "Dt": document.IssueDate.In(time.FixedZone("IST", 19800)).Format("02/01/2006")},
		"SellerDtls": sellerDetails, "BuyerDtls": buyerDetails, "ItemList": items,
		"ValDtls": map[string]interface{}{"AssVal": irpAmount(taxable), "CgstVal": irpAmount(cgst), "SgstVal": irpAmount(sgst), "IgstVal": irpAmount(igst), "CesVal": irpAmount(cess), "TotInvVal": document.Total},
	}, nil
}

func irpAmount(value float64) float64 { return math.Round(value*100) / 100 }
