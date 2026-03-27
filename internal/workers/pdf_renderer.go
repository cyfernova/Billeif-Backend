package workers

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"

	"github.com/phpdave11/gofpdf"
)

//go:embed fonts/*.ttf
var embeddedFonts embed.FS

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)
var filenameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type fontAsset struct {
	family string
	file   string
}

type localeLabels struct {
	documentTitles map[string]string
	documentNumber string
	issueDate      string
	dueDate        string
	dispatchDate   string
	party          string
	description    string
	quantity       string
	unitPrice      string
	tax            string
	total          string
	subtotal       string
	discount       string
	cess           string
	paid           string
	balanceDue     string
	notes          string
	terms          string
	declaration    string
	page           string
	hsnSac         string
}

type renderParty struct {
	label   string
	name    string
	email   string
	phone   string
	taxID   string
	address []string
}

func renderDocumentPDF(ctx context.Context, svc *services.Container, document *models.Document, profile *models.RenderProfile) ([]byte, string, error) {
	if document == nil {
		return nil, "", fmt.Errorf("document is required")
	}

	labels := labelsForLocale(document.Locale)
	applyCustomLabels(labels, profile)

	business, err := svc.Business.Get(ctx, document.BusinessID)
	if err != nil {
		return nil, "", err
	}
	party, err := resolveRenderParty(ctx, svc, document, labels)
	if err != nil {
		return nil, "", err
	}

	pageSize := "A4"
	if profile != nil && strings.TrimSpace(profile.PageSize) != "" {
		pageSize = profile.PageSize
	}

	pdf := gofpdf.New("P", "mm", pageSize, "")
	fontFamily := loadDocumentFont(pdf, profile, document.Locale)
	if profile != nil && profile.PasswordProtected {
		var permissions byte
		if profile.PrintAllowed {
			permissions |= gofpdf.CnProtectPrint
		}
		if profile.CopyAllowed {
			permissions |= gofpdf.CnProtectCopy
		}
		userPassword := strings.TrimSpace(profile.Password)
		if userPassword == "" {
			userPassword = sanitizeFilename(document.SerialNumber)
		}
		pdf.SetProtection(permissions, userPassword, "")
	}

	pdf.SetMargins(12, 16, 12)
	pdf.SetAutoPageBreak(true, 16)
	pdf.AliasNbPages("")
	pdf.SetTitle(document.SerialNumber, false)
	pdf.SetAuthor(business.Name, false)
	pdf.SetCreator("Cyfernova Invoice Backend", false)

	headerText := ""
	footerText := ""
	bannerText := ""
	watermarkText := ""
	if profile != nil {
		headerText = stripHTML(profile.HeaderHTML)
		footerText = stripHTML(profile.FooterHTML)
		bannerText = strings.TrimSpace(profile.BannerText)
		watermarkText = strings.TrimSpace(profile.WatermarkText)
	}

	pdf.SetHeaderFuncMode(func() {
		renderPageHeader(pdf, fontFamily, business, document, labels, headerText, bannerText, watermarkText)
	}, true)
	pdf.SetFooterFunc(func() {
		renderPageFooter(pdf, fontFamily, labels, footerText)
	})

	pdf.AddPage()
	renderDocumentSummary(pdf, fontFamily, document, party, labels)
	renderLineTable(pdf, fontFamily, document, labels)
	renderTotalsSection(pdf, fontFamily, document, labels)
	renderTextSections(pdf, fontFamily, document, profile, labels)

	if pdf.Err() {
		return nil, "", fmt.Errorf("pdf rendering failed")
	}

	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, "", err
	}

	filename := sanitizeFilename(document.SerialNumber)
	if filename == "" {
		filename = fmt.Sprintf("%s-%d", document.DocumentType, time.Now().Unix())
	}
	return out.Bytes(), filename + ".pdf", nil
}

func renderPageHeader(pdf *gofpdf.Fpdf, fontFamily string, business *models.BusinessProfile, document *models.Document, labels *localeLabels, headerText, bannerText, watermarkText string) {
	pdf.SetTextColor(18, 24, 38)
	pdf.SetFillColor(244, 247, 251)
	pdf.SetDrawColor(220, 227, 235)
	pdf.SetY(12)

	if watermarkText != "" {
		pageWidth, pageHeight := pdf.GetPageSize()
		pdf.TransformBegin()
		pdf.TransformRotate(45, pageWidth/2, pageHeight/2)
		pdf.SetTextColor(238, 240, 244)
		pdf.SetFont(fontFamily, "", 30)
		pdf.Text(pageWidth/2-35, pageHeight/2, watermarkText)
		pdf.TransformEnd()
		pdf.SetTextColor(18, 24, 38)
	}

	pdf.SetFont(fontFamily, "", 16)
	pdf.CellFormat(118, 8, safeValue(business.Name, "Business"), "", 0, "L", false, 0, "")
	pdf.SetFont(fontFamily, "", 13)
	pdf.CellFormat(68, 8, documentTitle(labels, document.DocumentType), "", 1, "R", false, 0, "")

	addressLines := compactLines([]string{
		business.Address,
		joinCityLine(business.City, business.State, business.PostalCode),
		business.Country,
		prefixedValue("GSTIN", business.GSTIN),
	})
	pdf.SetFont(fontFamily, "", 9)
	for _, line := range addressLines {
		pdf.CellFormat(118, 5, line, "", 0, "L", false, 0, "")
		pdf.CellFormat(68, 5, lineValue(labels.documentNumber, document.SerialNumber), "", 1, "R", false, 0, "")
	}
	if headerText != "" {
		pdf.SetFont(fontFamily, "", 8)
		pdf.MultiCell(186, 4.5, headerText, "", "L", false)
	}
	if bannerText != "" {
		pdf.SetFont(fontFamily, "", 9)
		pdf.SetFillColor(228, 236, 248)
		pdf.CellFormat(186, 7, bannerText, "1", 1, "C", true, 0, "")
	}
	pdf.Ln(2)
}

func renderPageFooter(pdf *gofpdf.Fpdf, fontFamily string, labels *localeLabels, footerText string) {
	pdf.SetY(-12)
	pdf.SetTextColor(110, 118, 129)
	pdf.SetFont(fontFamily, "", 8)
	pdf.CellFormat(120, 5, footerText, "", 0, "L", false, 0, "")
	pdf.CellFormat(66, 5, fmt.Sprintf("%s %d/{nb}", labels.page, pdf.PageNo()), "", 0, "R", false, 0, "")
}

func renderDocumentSummary(pdf *gofpdf.Fpdf, fontFamily string, document *models.Document, party renderParty, labels *localeLabels) {
	pdf.SetFont(fontFamily, "", 10)
	pdf.SetTextColor(32, 36, 42)

	leftX := pdf.GetX()
	startY := pdf.GetY()
	boxWidth := 90.0
	boxHeight := 30.0
	rightX := leftX + 96.0

	pdf.Rect(leftX, startY, boxWidth, boxHeight, "D")
	pdf.Rect(rightX, startY, boxWidth, boxHeight, "D")

	pdf.SetXY(leftX+2, startY+2)
	pdf.SetFont(fontFamily, "", 11)
	pdf.CellFormat(boxWidth-4, 6, safeValue(party.label, labels.party), "", 1, "L", false, 0, "")
	pdf.SetFont(fontFamily, "", 10)
	for _, line := range compactLines(append([]string{
		party.name,
	}, append(party.address, prefixedValue("Tax ID", party.taxID), party.email, party.phone)...)) {
		pdf.SetX(leftX + 2)
		pdf.CellFormat(boxWidth-4, 4.8, line, "", 1, "L", false, 0, "")
	}

	metaX := rightX + 2
	metaY := startY + 3
	pdf.SetXY(metaX, metaY)
	renderKeyValue(pdf, fontFamily, labels.issueDate, formatDate(document.IssueDate))
	renderKeyValue(pdf, fontFamily, labels.dueDate, formatDatePtr(document.DueDate))
	if document.DispatchDate != nil {
		renderKeyValue(pdf, fontFamily, labels.dispatchDate, formatDatePtr(document.DispatchDate))
	}
	renderKeyValue(pdf, fontFamily, labels.party, safeValue(party.name, "-"))

	pdf.SetY(startY + boxHeight + 6)
}

func renderLineTable(pdf *gofpdf.Fpdf, fontFamily string, document *models.Document, labels *localeLabels) {
	widths := []float64{12, 72, 24, 28, 22, 28}
	renderLineTableHeader(pdf, fontFamily, widths, labels)

	pageWidth, pageHeight := pdf.GetPageSize()
	_, _, _, bottomMargin := pdf.GetMargins()
	usableBottom := pageHeight - bottomMargin
	_ = pageWidth

	for i, line := range document.Lines {
		descLines := pdf.SplitLines([]byte(safeValue(line.Description, "-")), widths[1]-4)
		if len(descLines) == 0 {
			descLines = [][]byte{[]byte("-")}
		}
		rowHeight := float64(len(descLines))*5 + 2
		if rowHeight < 8 {
			rowHeight = 8
		}
		if pdf.GetY()+rowHeight > usableBottom {
			pdf.AddPage()
			renderLineTableHeader(pdf, fontFamily, widths, labels)
		}

		x := pdf.GetX()
		y := pdf.GetY()

		drawCell(pdf, x, y, widths[0], rowHeight, strconv.Itoa(i+1), "C", fontFamily, 9)
		drawWrappedCell(pdf, x+widths[0], y, widths[1], rowHeight, safeValue(line.Description, "-"), "L", fontFamily, 9)
		drawCell(pdf, x+widths[0]+widths[1], y, widths[2], rowHeight, formatQuantity(line.Quantity), "R", fontFamily, 9)
		drawCell(pdf, x+widths[0]+widths[1]+widths[2], y, widths[3], rowHeight, formatMoney(document.Currency, line.UnitPrice), "R", fontFamily, 9)
		drawCell(pdf, x+widths[0]+widths[1]+widths[2]+widths[3], y, widths[4], rowHeight, formatMoney(document.Currency, line.TaxAmount+line.CessAmount), "R", fontFamily, 9)
		drawCell(pdf, x+widths[0]+widths[1]+widths[2]+widths[3]+widths[4], y, widths[5], rowHeight, formatMoney(document.Currency, line.LineTotal), "R", fontFamily, 9)
		pdf.SetXY(x, y+rowHeight)
	}
	pdf.Ln(4)
}

func renderLineTableHeader(pdf *gofpdf.Fpdf, fontFamily string, widths []float64, labels *localeLabels) {
	headers := []string{"#", labels.description, labels.quantity, labels.unitPrice, labels.tax, labels.total}
	pdf.SetFont(fontFamily, "", 9)
	pdf.SetFillColor(241, 245, 249)
	pdf.SetTextColor(32, 36, 42)
	for idx, header := range headers {
		pdf.CellFormat(widths[idx], 8, header, "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)
}

func renderTotalsSection(pdf *gofpdf.Fpdf, fontFamily string, document *models.Document, labels *localeLabels) {
	pageWidth, _ := pdf.GetPageSize()
	left, _, right, _ := pdf.GetMargins()
	tableWidth := 78.0
	startX := pageWidth - right - tableWidth

	rows := []struct {
		label string
		value string
	}{
		{labels.subtotal, formatMoney(document.Currency, document.Subtotal)},
		{labels.discount, formatMoney(document.Currency, document.DiscountTotal)},
		{labels.tax, formatMoney(document.Currency, document.TaxTotal)},
		{labels.cess, formatMoney(document.Currency, document.CessTotal)},
		{labels.total, formatMoney(document.Currency, document.Total)},
	}
	if document.PaidAmount > 0 {
		rows = append(rows, struct {
			label string
			value string
		}{labels.paid, formatMoney(document.Currency, document.PaidAmount)})
	}
	rows = append(rows, struct {
		label string
		value string
	}{labels.balanceDue, formatMoney(document.Currency, document.BalanceDue)})

	pdf.SetX(left)
	pdf.SetFont(fontFamily, "", 9)
	for _, row := range rows {
		pdf.SetX(startX)
		pdf.CellFormat(34, 7, row.label, "1", 0, "L", false, 0, "")
		pdf.CellFormat(44, 7, row.value, "1", 1, "R", false, 0, "")
	}
	pdf.Ln(3)
}

func renderTextSections(pdf *gofpdf.Fpdf, fontFamily string, document *models.Document, profile *models.RenderProfile, labels *localeLabels) {
	visibility := parseVisibilityConfig(profile)
	renderOptionalTextBlock(pdf, fontFamily, labels.notes, document.Notes, visibility["show_notes"])
	renderOptionalTextBlock(pdf, fontFamily, labels.terms, document.Terms, visibility["show_terms"])
	renderOptionalTextBlock(pdf, fontFamily, labels.declaration, document.Declaration, visibility["show_declaration"])
}

func renderOptionalTextBlock(pdf *gofpdf.Fpdf, fontFamily, title, content string, visible bool) {
	content = strings.TrimSpace(content)
	if !visible || content == "" {
		return
	}
	pdf.SetFont(fontFamily, "", 10)
	pdf.CellFormat(186, 6, title, "", 1, "L", false, 0, "")
	pdf.SetFont(fontFamily, "", 9)
	pdf.MultiCell(186, 5, content, "1", "L", false)
	pdf.Ln(2)
}

func renderKeyValue(pdf *gofpdf.Fpdf, fontFamily, key, value string) {
	pdf.SetFont(fontFamily, "", 9)
	pdf.CellFormat(28, 5, key, "", 0, "L", false, 0, "")
	pdf.CellFormat(58, 5, safeValue(value, "-"), "", 1, "R", false, 0, "")
}

func drawCell(pdf *gofpdf.Fpdf, x, y, w, h float64, text, align, fontFamily string, fontSize float64) {
	pdf.Rect(x, y, w, h, "D")
	pdf.SetXY(x+1, y+1.5)
	pdf.SetFont(fontFamily, "", fontSize)
	pdf.MultiCell(w-2, 4.5, text, "", align, false)
}

func drawWrappedCell(pdf *gofpdf.Fpdf, x, y, w, h float64, text, align, fontFamily string, fontSize float64) {
	pdf.Rect(x, y, w, h, "D")
	pdf.SetXY(x+1.5, y+1.5)
	pdf.SetFont(fontFamily, "", fontSize)
	pdf.MultiCell(w-3, 4.5, text, "", align, false)
}

func resolveRenderParty(ctx context.Context, svc *services.Container, document *models.Document, labels *localeLabels) (renderParty, error) {
	party := renderParty{label: labels.party}
	if document.PartyID == nil || *document.PartyID == "" {
		return party, nil
	}

	switch document.PartyType {
	case models.DocumentPartyTypeVendor:
		vendor, err := svc.Vendor.GetByBusiness(ctx, document.BusinessID, *document.PartyID)
		if err != nil {
			return party, err
		}
		party.label = labels.party
		party.name = vendor.Name
		party.email = vendor.Email
		party.phone = vendor.Phone
		party.taxID = vendor.TaxID
		party.address = compactLines([]string{vendor.Address, joinCityLine(vendor.City, vendor.State, vendor.PostalCode), vendor.Country})
	default:
		customer, err := svc.Customer.GetByBusiness(ctx, document.BusinessID, *document.PartyID)
		if err != nil {
			return party, err
		}
		party.label = labels.party
		party.name = customer.Name
		party.email = customer.Email
		party.phone = customer.Phone
		party.taxID = customer.TaxID
		party.address = compactLines([]string{customer.Address, joinCityLine(customer.City, customer.State, customer.PostalCode), customer.Country})
	}

	return party, nil
}

func loadDocumentFont(pdf *gofpdf.Fpdf, profile *models.RenderProfile, locale string) string {
	asset := selectFontAsset(profile, locale)
	fontBytes, err := embeddedFonts.ReadFile(filepath.Join("fonts", asset.file))
	if err != nil {
		return "Helvetica"
	}
	pdf.AddUTF8FontFromBytes(asset.family, "", fontBytes)
	if pdf.Err() {
		return "Helvetica"
	}
	return asset.family
}

func selectFontAsset(profile *models.RenderProfile, locale string) fontAsset {
	if profile != nil {
		switch normalizeFontFamily(profile.FontFamily) {
		case "devanagari":
			return fontAsset{family: "NotoSansDevanagari", file: "NotoSansDevanagari-Regular.ttf"}
		case "tamil":
			return fontAsset{family: "NotoSansTamil", file: "NotoSansTamil-Regular.ttf"}
		case "telugu":
			return fontAsset{family: "NotoSansTelugu", file: "NotoSansTelugu-Regular.ttf"}
		case "kannada":
			return fontAsset{family: "NotoSansKannada", file: "NotoSansKannada-Regular.ttf"}
		case "malayalam":
			return fontAsset{family: "NotoSansMalayalam", file: "NotoSansMalayalam-Regular.ttf"}
		case "bengali":
			return fontAsset{family: "NotoSansBengali", file: "NotoSansBengali-Regular.ttf"}
		case "gujarati":
			return fontAsset{family: "NotoSansGujarati", file: "NotoSansGujarati-Regular.ttf"}
		case "gurmukhi":
			return fontAsset{family: "NotoSansGurmukhi", file: "NotoSansGurmukhi-Regular.ttf"}
		}
	}

	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "hi-in", "mr-in":
		return fontAsset{family: "NotoSansDevanagari", file: "NotoSansDevanagari-Regular.ttf"}
	case "ta-in":
		return fontAsset{family: "NotoSansTamil", file: "NotoSansTamil-Regular.ttf"}
	case "te-in":
		return fontAsset{family: "NotoSansTelugu", file: "NotoSansTelugu-Regular.ttf"}
	case "kn-in":
		return fontAsset{family: "NotoSansKannada", file: "NotoSansKannada-Regular.ttf"}
	case "ml-in":
		return fontAsset{family: "NotoSansMalayalam", file: "NotoSansMalayalam-Regular.ttf"}
	case "bn-in":
		return fontAsset{family: "NotoSansBengali", file: "NotoSansBengali-Regular.ttf"}
	case "gu-in":
		return fontAsset{family: "NotoSansGujarati", file: "NotoSansGujarati-Regular.ttf"}
	case "pa-in":
		return fontAsset{family: "NotoSansGurmukhi", file: "NotoSansGurmukhi-Regular.ttf"}
	default:
		return fontAsset{family: "NotoSans", file: "NotoSans-Regular.ttf"}
	}
}

func labelsForLocale(locale string) *localeLabels {
	labels := &localeLabels{
		documentTitles: map[string]string{
			models.DocumentTypeSalesInvoice:    "Sales Invoice",
			models.DocumentTypePurchaseInvoice: "Purchase Invoice",
			models.DocumentTypePurchaseOrder:   "Purchase Order",
			models.DocumentTypeSalesOrder:      "Sales Order",
			models.DocumentTypeQuotation:       "Quotation",
			models.DocumentTypeProformaInvoice: "Proforma Invoice",
			models.DocumentTypeDeliveryChallan: "Delivery Challan",
			models.DocumentTypeCreditNote:      "Credit Note",
			models.DocumentTypeDebitNote:       "Debit Note",
			models.DocumentTypeBillOfSupply:    "Bill of Supply",
			models.DocumentTypePackingList:     "Packing List",
			models.DocumentTypeShippingLabel:   "Shipping Label",
		},
		documentNumber: "Document No",
		issueDate:      "Issue Date",
		dueDate:        "Due Date",
		dispatchDate:   "Dispatch Date",
		party:          "Party",
		description:    "Description",
		quantity:       "Qty",
		unitPrice:      "Unit Price",
		tax:            "Tax",
		total:          "Total",
		subtotal:       "Subtotal",
		discount:       "Discount",
		cess:           "Cess",
		paid:           "Paid",
		balanceDue:     "Balance Due",
		notes:          "Notes",
		terms:          "Terms",
		declaration:    "Declaration",
		page:           "Page",
		hsnSac:         "HSN/SAC",
	}

	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "hi-in", "mr-in":
		labels.documentNumber = "दस्तावेज़ संख्या"
		labels.issueDate = "जारी तिथि"
		labels.dueDate = "नियत तिथि"
		labels.dispatchDate = "प्रेषण तिथि"
		labels.party = "पक्ष"
		labels.description = "विवरण"
		labels.quantity = "मात्रा"
		labels.unitPrice = "दर"
		labels.tax = "कर"
		labels.total = "कुल"
		labels.subtotal = "उप-योग"
		labels.discount = "छूट"
		labels.cess = "सेस"
		labels.paid = "भुगतान"
		labels.balanceDue = "शेष राशि"
		labels.notes = "टिप्पणियाँ"
		labels.terms = "शर्तें"
		labels.declaration = "घोषणा"
		labels.page = "पृष्ठ"
	case "ta-in":
		labels.documentNumber = "ஆவண எண்"
		labels.issueDate = "வெளியீட்டு தேதி"
		labels.dueDate = "கடைசி தேதி"
		labels.dispatchDate = "அனுப்பும் தேதி"
		labels.party = "தரப்பு"
		labels.description = "விளக்கம்"
		labels.quantity = "அளவு"
		labels.unitPrice = "ஒற்றை விலை"
		labels.tax = "வரி"
		labels.total = "மொத்தம்"
		labels.subtotal = "இடைத் தொகை"
		labels.discount = "தள்ளுபடி"
		labels.cess = "செஸ்"
		labels.paid = "செலுத்தியது"
		labels.balanceDue = "நிலுவை"
		labels.notes = "குறிப்புகள்"
		labels.terms = "விதிமுறைகள்"
		labels.declaration = "அறிக்கை"
		labels.page = "பக்கம்"
	case "te-in":
		labels.documentNumber = "పత్రం సంఖ్య"
		labels.issueDate = "జారీ తేదీ"
		labels.dueDate = "గడువు తేదీ"
		labels.dispatchDate = "రవాణా తేదీ"
		labels.party = "పార్టీ"
		labels.description = "వివరణ"
		labels.quantity = "పరిమాణం"
		labels.unitPrice = "ఒక్కొక్క ధర"
		labels.tax = "పన్ను"
		labels.total = "మొత్తం"
		labels.subtotal = "ఉప మొత్తం"
		labels.discount = "డిస్కౌంట్"
		labels.cess = "సెస్"
		labels.paid = "చెల్లించినది"
		labels.balanceDue = "బాకీ"
		labels.notes = "గమనికలు"
		labels.terms = "నిబంధనలు"
		labels.declaration = "ప్రకటన"
		labels.page = "పేజీ"
	case "kn-in":
		labels.documentNumber = "ದಾಖಲೆ ಸಂಖ್ಯೆ"
		labels.issueDate = "ಜಾರಿ ದಿನಾಂಕ"
		labels.dueDate = "ಪಾವತಿ ದಿನಾಂಕ"
		labels.dispatchDate = "ರವಾನೆ ದಿನಾಂಕ"
		labels.party = "ಪಕ್ಷ"
		labels.description = "ವಿವರಣೆ"
		labels.quantity = "ಪ್ರಮಾಣ"
		labels.unitPrice = "ಏಕಕ ದರ"
		labels.tax = "ತೆರಿಗೆ"
		labels.total = "ಒಟ್ಟು"
		labels.subtotal = "ಉಪ ಒಟ್ಟು"
		labels.discount = "ರಿಯಾಯಿತಿ"
		labels.cess = "ಸೆಸ್"
		labels.paid = "ಪಾವತಿಸಿದುದು"
		labels.balanceDue = "ಬಾಕಿ"
		labels.notes = "ಟಿಪ್ಪಣಿಗಳು"
		labels.terms = "ನಿಯಮಗಳು"
		labels.declaration = "ಘೋಷಣೆ"
		labels.page = "ಪುಟ"
	case "ml-in":
		labels.documentNumber = "രേഖ നമ്പർ"
		labels.issueDate = "ഇഷ്യൂ തീയതി"
		labels.dueDate = "കാലാവധി തീയതി"
		labels.dispatchDate = "അയയ്ക്കുന്ന തീയതി"
		labels.party = "കക്ഷി"
		labels.description = "വിവരണം"
		labels.quantity = "അളവ്"
		labels.unitPrice = "ഒറ്റ വില"
		labels.tax = "നികുതി"
		labels.total = "ആകെ"
		labels.subtotal = "ഉപ ആകെ"
		labels.discount = "ഡിസ്കൗണ്ട്"
		labels.cess = "സെസ്"
		labels.paid = "അടച്ചത്"
		labels.balanceDue = "ബാക്കി"
		labels.notes = "കുറിപ്പുകൾ"
		labels.terms = "നിബന്ധനകൾ"
		labels.declaration = "പ്രഖ്യാപനം"
		labels.page = "പേജ്"
	case "bn-in":
		labels.documentNumber = "নথি নম্বর"
		labels.issueDate = "ইস্যু তারিখ"
		labels.dueDate = "পরিশোধের তারিখ"
		labels.dispatchDate = "প্রেরণের তারিখ"
		labels.party = "পক্ষ"
		labels.description = "বিবরণ"
		labels.quantity = "পরিমাণ"
		labels.unitPrice = "একক মূল্য"
		labels.tax = "কর"
		labels.total = "মোট"
		labels.subtotal = "উপমোট"
		labels.discount = "ছাড়"
		labels.cess = "সেস"
		labels.paid = "পরিশোধিত"
		labels.balanceDue = "বকেয়া"
		labels.notes = "নোট"
		labels.terms = "শর্তাবলী"
		labels.declaration = "ঘোষণা"
		labels.page = "পৃষ্ঠা"
	case "gu-in":
		labels.documentNumber = "દસ્તાવેજ નંબર"
		labels.issueDate = "જારી તારીખ"
		labels.dueDate = "ચુકવણી તારીખ"
		labels.dispatchDate = "ડિસ્પેચ તારીખ"
		labels.party = "પક્ષ"
		labels.description = "વર્ણન"
		labels.quantity = "જથ્થો"
		labels.unitPrice = "એકમ દર"
		labels.tax = "કર"
		labels.total = "કુલ"
		labels.subtotal = "ઉપ કુલ"
		labels.discount = "ડિસ્કાઉન્ટ"
		labels.cess = "સેસ"
		labels.paid = "ચૂકવેલ"
		labels.balanceDue = "બાકી"
		labels.notes = "નોંધો"
		labels.terms = "શરતો"
		labels.declaration = "ઘોષણા"
		labels.page = "પાનું"
	case "pa-in":
		labels.documentNumber = "ਦਸਤਾਵੇਜ਼ ਨੰਬਰ"
		labels.issueDate = "ਜਾਰੀ ਮਿਤੀ"
		labels.dueDate = "ਅਦਾਇਗੀ ਮਿਤੀ"
		labels.dispatchDate = "ਡਿਸਪੈਚ ਮਿਤੀ"
		labels.party = "ਪੱਖ"
		labels.description = "ਵੇਰਵਾ"
		labels.quantity = "ਮਾਤਰਾ"
		labels.unitPrice = "ਇਕਾਈ ਕੀਮਤ"
		labels.tax = "ਟੈਕਸ"
		labels.total = "ਕੁੱਲ"
		labels.subtotal = "ਉਪ-ਕੁੱਲ"
		labels.discount = "ਛੂਟ"
		labels.cess = "ਸੈਸ"
		labels.paid = "ਭੁਗਤਾਨ"
		labels.balanceDue = "ਬਾਕੀ"
		labels.notes = "ਨੋਟ"
		labels.terms = "ਸ਼ਰਤਾਂ"
		labels.declaration = "ਘੋਸ਼ਣਾ"
		labels.page = "ਸਫ਼ਾ"
	}

	return labels
}

func applyCustomLabels(labels *localeLabels, profile *models.RenderProfile) {
	if profile == nil || strings.TrimSpace(profile.CustomLabels) == "" {
		return
	}
	var custom map[string]string
	if err := json.Unmarshal([]byte(profile.CustomLabels), &custom); err != nil {
		return
	}
	replace := func(key string, dest *string) {
		if value := strings.TrimSpace(custom[key]); value != "" {
			*dest = value
		}
	}
	replace("document_number", &labels.documentNumber)
	replace("issue_date", &labels.issueDate)
	replace("due_date", &labels.dueDate)
	replace("dispatch_date", &labels.dispatchDate)
	replace("party", &labels.party)
	replace("description", &labels.description)
	replace("quantity", &labels.quantity)
	replace("unit_price", &labels.unitPrice)
	replace("tax", &labels.tax)
	replace("total", &labels.total)
	replace("subtotal", &labels.subtotal)
	replace("discount", &labels.discount)
	replace("cess", &labels.cess)
	replace("paid", &labels.paid)
	replace("balance_due", &labels.balanceDue)
	replace("notes", &labels.notes)
	replace("terms", &labels.terms)
	replace("declaration", &labels.declaration)
	if value := strings.TrimSpace(custom["title"]); value != "" {
		labels.documentTitles = map[string]string{}
		for _, documentType := range supportedDocumentTypes() {
			labels.documentTitles[documentType] = value
		}
	}
}

func parseVisibilityConfig(profile *models.RenderProfile) map[string]bool {
	defaults := map[string]bool{
		"show_notes":       true,
		"show_terms":       true,
		"show_declaration": true,
	}
	if profile == nil || strings.TrimSpace(profile.VisibilityConfig) == "" {
		return defaults
	}
	var raw map[string]bool
	if err := json.Unmarshal([]byte(profile.VisibilityConfig), &raw); err != nil {
		return defaults
	}
	for key, value := range raw {
		defaults[key] = value
	}
	return defaults
}

func supportedDocumentTypes() []string {
	return []string{
		models.DocumentTypeSalesInvoice,
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
	}
}

func documentTitle(labels *localeLabels, documentType string) string {
	if title, ok := labels.documentTitles[documentType]; ok && title != "" {
		return title
	}
	return strings.ReplaceAll(strings.Title(strings.ReplaceAll(documentType, "_", " ")), " ", " ")
}

func stripHTML(value string) string {
	value = html.UnescapeString(value)
	value = htmlTagPattern.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func normalizeFontFamily(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(value, "devanagari"):
		return "devanagari"
	case strings.Contains(value, "tamil"):
		return "tamil"
	case strings.Contains(value, "telugu"):
		return "telugu"
	case strings.Contains(value, "kannada"):
		return "kannada"
	case strings.Contains(value, "malayalam"):
		return "malayalam"
	case strings.Contains(value, "bengali"):
		return "bengali"
	case strings.Contains(value, "gujarati"):
		return "gujarati"
	case strings.Contains(value, "gurmukhi"), strings.Contains(value, "punjabi"):
		return "gurmukhi"
	default:
		return ""
	}
}

func compactLines(lines []string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func joinCityLine(city, state, postalCode string) string {
	return strings.Trim(strings.Join(compactLines([]string{city, state, postalCode}), ", "), ", ")
}

func prefixedValue(prefix, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return prefix + ": " + value
}

func lineValue(label, value string) string {
	value = safeValue(value, "-")
	return label + ": " + value
}

func safeValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func formatMoney(currency string, amount float64) string {
	return fmt.Sprintf("%s %.2f", safeValue(currency, "INR"), amount)
}

func formatQuantity(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return fmt.Sprintf("%.3f", value)
}

func formatDate(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format("02 Jan 2006")
}

func formatDatePtr(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return formatDate(*value)
}

func sanitizeFilename(value string) string {
	value = strings.TrimSpace(value)
	value = filenameSanitizer.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-_.")
	if value == "" {
		return "document"
	}
	return value
}
