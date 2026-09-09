package services

import (
	"testing"
	"time"

	"invoice-backend/internal/models"

	"github.com/stretchr/testify/require"
)

func TestIRPInvoicePayloadPreservesTaxAndDiscounts(t *testing.T) {
	seller := &models.BusinessProfile{ID: "business", Name: "Seller", GSTIN: "29ABCDE1234F1Z5", Address: "12 Main Road", City: "Bengaluru", PostalCode: "560001", BusinessStateCode: "29"}
	buyer := &models.Customer{BusinessID: seller.ID, Name: "Buyer", Address: "14 Main Road", City: "Bengaluru", PostalCode: "560002", StateCode: "29"}
	document := &models.Document{BusinessID: seller.ID, DocumentType: models.DocumentTypeSalesInvoice, DraftState: models.DocumentDraftStateFinal, Status: models.DocumentStatusIssued, TaxMode: models.DocumentTaxModeGST, Currency: "INR", SerialNumber: "INV-1", IssueDate: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC), PartyGSTIN: "29ABCDE1234F1Z5", PartyStateCode: "29", PlaceOfSupply: "29", Total: 212.4,
		Lines: []*models.DocumentLine{{Description: "Office chairs", HSNSACCode: "9403", Quantity: 2, UnitPrice: 100, DiscountAmount: 20, LineSubtotal: 180, TaxRate: 18, CGSTAmount: 16.2, SGSTAmount: 16.2, LineTotal: 212.4, UQCCode: "NOS", Unit: "pieces"}}}
	payload, err := irpInvoicePayload(document, seller, buyer)
	require.NoError(t, err)
	require.Equal(t, "1.1", payload["Version"])
	require.Equal(t, "02/01/2026", payload["DocDtls"].(map[string]interface{})["Dt"])
	item := payload["ItemList"].([]map[string]interface{})[0]
	require.Equal(t, float64(200), item["TotAmt"])
	require.Equal(t, float64(180), item["AssAmt"])
	require.Equal(t, "NOS", item["Unit"])
	require.Equal(t, float64(16.2), item["CgstAmt"])
	values := payload["ValDtls"].(map[string]interface{})
	require.NotContains(t, values, "Discount", "line discount must not be deducted again at invoice level")
	require.Equal(t, float64(212.4), values["TotInvVal"])
	for _, kind := range []struct{ doc, irp string }{{models.DocumentTypeCreditNote, "CRN"}, {models.DocumentTypeDebitNote, "DBN"}} {
		document.DocumentType = kind.doc
		payload, err = irpInvoicePayload(document, seller, buyer)
		require.NoError(t, err)
		require.Equal(t, kind.irp, payload["DocDtls"].(map[string]interface{})["Typ"])
	}
	buyer.BusinessID = "another-business"
	_, err = irpInvoicePayload(document, seller, buyer)
	require.ErrorContains(t, err, "selected business")
	buyer.BusinessID = seller.ID
	buyer.PostalCode = "invalid"
	_, err = irpInvoicePayload(document, seller, buyer)
	require.ErrorContains(t, err, "pincode")
	buyer.PostalCode = "560002"
	document.Total = 200
	_, err = irpInvoicePayload(document, seller, buyer)
	require.ErrorContains(t, err, "total")
}
