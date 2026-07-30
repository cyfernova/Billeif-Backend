package workers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"

	"github.com/phpdave11/gofpdf"
	"github.com/skip2/go-qrcode"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

//go:embed fonts/*.ttf
var embeddedFonts embed.FS

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)
var filenameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
var logoHTTPClient = &http.Client{Timeout: 8 * time.Second}

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

type rgbColor struct {
	r int
	g int
	b int
}

type renderTheme struct {
	accent       rgbColor
	density      string
	marginTop    float64
	marginBottom float64
	marginLeft   float64
	marginRight  float64
	rowLine      float64
	rowMinHeight float64
}

type registeredLogo struct {
	name string
	opts gofpdf.ImageOptions
}

func renderDocumentPDF(ctx context.Context, svc *services.Container, document *models.Document, profile *models.RenderProfile) ([]byte, string, error) {
	return renderDocumentPDFWithLiveCompliance(ctx, svc, document, profile, true)
}

func renderFinalDocumentPDF(ctx context.Context, svc *services.Container, document *models.Document, profile *models.RenderProfile) ([]byte, string, error) {
	return renderDocumentPDFWithLiveCompliance(ctx, svc, document, profile, false)
}

func renderDocumentPDFWithLiveCompliance(
	ctx context.Context,
	svc *services.Container,
	document *models.Document,
	profile *models.RenderProfile,
	includeLiveCompliance bool,
) ([]byte, string, error) {
	if document == nil {
		return nil, "", fmt.Errorf("document is required")
	}

	labels := labelsForLocale(document.Locale)
	applyCustomLabels(labels, profile)
	theme := resolveRenderTheme(profile)
	visibility := parseVisibilityConfig(profile)

	business, party, frozenParties := frozenRenderParties(document, labels)
	if !frozenParties {
		var err error
		business, err = svc.Business.Get(ctx, document.BusinessID)
		if err != nil {
			return nil, "", err
		}
		party, err = resolveRenderParty(ctx, svc, document, labels)
		if err != nil {
			return nil, "", err
		}
	}

	pageSize := "A4"
	if profile != nil && strings.TrimSpace(profile.PageSize) != "" {
		pageSize = profile.PageSize
	}

	pdf := gofpdf.New("P", "mm", pageSize, "")
	if !includeLiveCompliance {
		snapshotTime := document.IssueDate.UTC()
		if snapshotTime.IsZero() {
			snapshotTime = time.Unix(0, 0).UTC()
		}
		pdf.SetCatalogSort(true)
		pdf.SetCreationDate(snapshotTime)
		pdf.SetModificationDate(snapshotTime)
	}
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
		ownerPassword := ""
		if !includeLiveCompliance {
			ownerKey := sha256.Sum256([]byte(strings.Join([]string{
				document.BusinessID,
				document.ID,
				document.SerialNumber,
				userPassword,
			}, "\x00")))
			ownerPassword = fmt.Sprintf("%x", ownerKey)
		}
		pdf.SetProtection(permissions, userPassword, ownerPassword)
	}

	pdf.SetMargins(theme.marginLeft, theme.marginTop, theme.marginRight)
	pdf.SetAutoPageBreak(true, theme.marginBottom)
	pdf.AliasNbPages("")
	pdf.SetTitle(document.SerialNumber, false)
	pdf.SetAuthor(business.Name, false)
	pdf.SetCreator("Cyfernova Invoice Backend", false)
	logo := registerBusinessLogo(ctx, pdf, business)

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
		renderPageHeader(pdf, fontFamily, business, document, labels, headerText, bannerText, watermarkText, visibility, theme, logo)
	}, true)
	pdf.SetFooterFunc(func() {
		renderPageFooter(pdf, fontFamily, labels, footerText, theme)
	})

	pdf.AddPage()
	renderDocumentSummary(pdf, fontFamily, document, party, labels)
	renderLineTable(pdf, fontFamily, document, labels, theme)
	renderTotalsSection(pdf, fontFamily, document, labels, theme)
	if includeLiveCompliance {
		renderComplianceSection(ctx, pdf, fontFamily, svc, document)
	}
	renderTextSections(pdf, fontFamily, document, profile, labels)
	if visibility["show_signature_line"] {
		renderSignatureLine(pdf, fontFamily, theme)
	}

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

func renderPageHeader(pdf *gofpdf.Fpdf, fontFamily string, business *models.BusinessProfile, document *models.Document, labels *localeLabels, headerText, bannerText, watermarkText string, visibility map[string]bool, theme renderTheme, logo *registeredLogo) {
	accentLight := blendColor(theme.accent, 0.85)
	accentBorder := blendColor(theme.accent, 0.65)
	pdf.SetTextColor(18, 24, 38)
	pdf.SetFillColor(accentLight.r, accentLight.g, accentLight.b)
	pdf.SetDrawColor(accentBorder.r, accentBorder.g, accentBorder.b)
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

	leftCellWidth := 118.0
	headerLeftX := 12.0
	if visibility["show_logo"] && logo != nil {
		pdf.ImageOptions(logo.name, headerLeftX, 12.5, 18, 10, false, logo.opts, 0, "")
		leftCellWidth -= 22
		pdf.SetX(headerLeftX + 22)
	}
	pdf.SetFont(fontFamily, "", 16)
	pdf.CellFormat(leftCellWidth, 8, safeValue(business.Name, "Business"), "", 0, "L", false, 0, "")
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
		pdf.SetX(headerLeftX)
		if visibility["show_logo"] && logo != nil {
			pdf.SetX(headerLeftX + 22)
		}
		pdf.CellFormat(leftCellWidth, 5, line, "", 0, "L", false, 0, "")
		pdf.CellFormat(68, 5, lineValue(labels.documentNumber, document.SerialNumber), "", 1, "R", false, 0, "")
	}
	if headerText != "" {
		pdf.SetFont(fontFamily, "", 8)
		pdf.MultiCell(186, 4.5, headerText, "", "L", false)
	}
	if bannerText != "" {
		pdf.SetFont(fontFamily, "", 9)
		bannerFill := blendColor(theme.accent, 0.78)
		pdf.SetFillColor(bannerFill.r, bannerFill.g, bannerFill.b)
		pdf.CellFormat(186, 7, bannerText, "1", 1, "C", true, 0, "")
	}
	pdf.Ln(2)
}

func renderPageFooter(pdf *gofpdf.Fpdf, fontFamily string, labels *localeLabels, footerText string, theme renderTheme) {
	pdf.SetY(-12)
	footerColor := blendColor(theme.accent, 0.45)
	pdf.SetTextColor(footerColor.r, footerColor.g, footerColor.b)
	pdf.SetFont(fontFamily, "", 8)
	footerText = safeValue(strings.TrimSpace(footerText), "Generated by Cyfernova")
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

func renderLineTable(pdf *gofpdf.Fpdf, fontFamily string, document *models.Document, labels *localeLabels, theme renderTheme) {
	widths := []float64{12, 72, 24, 28, 22, 28}
	renderLineTableHeader(pdf, fontFamily, widths, labels, theme)

	pageWidth, pageHeight := pdf.GetPageSize()
	_, _, _, bottomMargin := pdf.GetMargins()
	usableBottom := pageHeight - bottomMargin
	_ = pageWidth

	for i, line := range document.Lines {
		descLines := pdf.SplitLines([]byte(safeValue(line.Description, "-")), widths[1]-4)
		if len(descLines) == 0 {
			descLines = [][]byte{[]byte("-")}
		}
		rowHeight := float64(len(descLines))*theme.rowLine + 2
		if rowHeight < theme.rowMinHeight {
			rowHeight = theme.rowMinHeight
		}
		if pdf.GetY()+rowHeight > usableBottom {
			pdf.AddPage()
			renderLineTableHeader(pdf, fontFamily, widths, labels, theme)
		}

		x := pdf.GetX()
		y := pdf.GetY()

		drawCell(pdf, x, y, widths[0], rowHeight, strconv.Itoa(i+1), "C", fontFamily, 9, theme.rowLine)
		drawWrappedCell(pdf, x+widths[0], y, widths[1], rowHeight, safeValue(line.Description, "-"), "L", fontFamily, 9, theme.rowLine)
		drawCell(pdf, x+widths[0]+widths[1], y, widths[2], rowHeight, formatQuantity(line.Quantity), "R", fontFamily, 9, theme.rowLine)
		drawCell(pdf, x+widths[0]+widths[1]+widths[2], y, widths[3], rowHeight, formatMoney(document.Currency, line.UnitPrice), "R", fontFamily, 9, theme.rowLine)
		drawCell(pdf, x+widths[0]+widths[1]+widths[2]+widths[3], y, widths[4], rowHeight, formatMoney(document.Currency, line.TaxAmount+line.CessAmount), "R", fontFamily, 9, theme.rowLine)
		drawCell(pdf, x+widths[0]+widths[1]+widths[2]+widths[3]+widths[4], y, widths[5], rowHeight, formatMoney(document.Currency, line.LineTotal), "R", fontFamily, 9, theme.rowLine)
		pdf.SetXY(x, y+rowHeight)
	}
	pdf.Ln(4)
}

func renderLineTableHeader(pdf *gofpdf.Fpdf, fontFamily string, widths []float64, labels *localeLabels, theme renderTheme) {
	headers := []string{"#", labels.description, labels.quantity, labels.unitPrice, labels.tax, labels.total}
	pdf.SetFont(fontFamily, "", 9)
	fillColor := blendColor(theme.accent, 0.82)
	pdf.SetFillColor(fillColor.r, fillColor.g, fillColor.b)
	pdf.SetTextColor(32, 36, 42)
	for idx, header := range headers {
		pdf.CellFormat(widths[idx], 8, header, "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)
}

func renderTotalsSection(pdf *gofpdf.Fpdf, fontFamily string, document *models.Document, labels *localeLabels, theme renderTheme) {
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
		highlight := row.label == labels.total || row.label == labels.balanceDue
		if highlight {
			fillColor := blendColor(theme.accent, 0.84)
			pdf.SetFillColor(fillColor.r, fillColor.g, fillColor.b)
		}
		pdf.SetX(startX)
		pdf.CellFormat(34, 7, row.label, "1", 0, "L", highlight, 0, "")
		pdf.CellFormat(44, 7, row.value, "1", 1, "R", highlight, 0, "")
	}
	pdf.Ln(3)
}

func renderTextSections(pdf *gofpdf.Fpdf, fontFamily string, document *models.Document, profile *models.RenderProfile, labels *localeLabels) {
	visibility := parseVisibilityConfig(profile)
	renderOptionalTextBlock(pdf, fontFamily, labels.notes, document.Notes, visibility["show_notes"])
	renderOptionalTextBlock(pdf, fontFamily, labels.terms, document.Terms, visibility["show_terms"])
	renderOptionalTextBlock(pdf, fontFamily, labels.declaration, document.Declaration, visibility["show_declaration"])
}

func renderSignatureLine(pdf *gofpdf.Fpdf, fontFamily string, theme renderTheme) {
	pageWidth, pageHeight := pdf.GetPageSize()
	_, _, right, bottom := pdf.GetMargins()
	if pdf.GetY()+14 > pageHeight-bottom {
		pdf.AddPage()
	}
	startX := pageWidth - right - 65
	lineY := pdf.GetY() + 7
	pdf.SetDrawColor(theme.accent.r, theme.accent.g, theme.accent.b)
	pdf.Line(startX, lineY, startX+60, lineY)
	pdf.SetXY(startX, lineY+1)
	pdf.SetFont(fontFamily, "", 8.5)
	pdf.SetTextColor(95, 104, 117)
	pdf.CellFormat(60, 5, "Authorized Signature", "", 1, "C", false, 0, "")
	pdf.Ln(1)
}

func renderComplianceSection(ctx context.Context, pdf *gofpdf.Fpdf, fontFamily string, svc *services.Container, document *models.Document) {
	if svc == nil || svc.TaxCompliance == nil || document == nil {
		return
	}

	eInvoice, _ := svc.TaxCompliance.GetEInvoiceByDocument(ctx, document.BusinessID, document.ID)
	eWayBill, _ := svc.TaxCompliance.GetEWayBillByDocument(ctx, document.BusinessID, document.ID)
	if eInvoice == nil && eWayBill == nil {
		return
	}

	_, pageHeight := pdf.GetPageSize()
	_, _, _, bottomMargin := pdf.GetMargins()
	const boxHeight = 34.0
	if pdf.GetY()+boxHeight > pageHeight-bottomMargin {
		pdf.AddPage()
	}

	startX := pdf.GetX()
	startY := pdf.GetY()
	boxWidth := 186.0
	pdf.Rect(startX, startY, boxWidth, boxHeight, "D")

	pdf.SetXY(startX+2, startY+2)
	pdf.SetFont(fontFamily, "", 10)
	pdf.CellFormat(120, 6, "GST Compliance", "", 1, "L", false, 0, "")
	pdf.SetFont(fontFamily, "", 8.5)

	if eInvoice != nil {
		pdf.SetX(startX + 2)
		pdf.CellFormat(120, 4.5, lineValue("IRN", eInvoice.IRN), "", 1, "L", false, 0, "")
		pdf.SetX(startX + 2)
		pdf.CellFormat(120, 4.5, lineValue("Ack No", eInvoice.AckNumber), "", 1, "L", false, 0, "")
		pdf.SetX(startX + 2)
		pdf.CellFormat(120, 4.5, lineValue("Ack Date", formatDatePtr(eInvoice.AckDate)), "", 1, "L", false, 0, "")
	}
	if eWayBill != nil {
		pdf.SetX(startX + 2)
		pdf.CellFormat(120, 4.5, lineValue("E-Way Bill", eWayBill.EWayBillNumber), "", 1, "L", false, 0, "")
		pdf.SetX(startX + 2)
		pdf.CellFormat(120, 4.5, lineValue("Valid Until", formatDatePtr(eWayBill.ValidUntil)), "", 1, "L", false, 0, "")
	}

	if eInvoice != nil && strings.TrimSpace(eInvoice.SignedQRCodePayload) != "" {
		qrBytes, err := qrcode.Encode(strings.TrimSpace(eInvoice.SignedQRCodePayload), qrcode.Medium, 160)
		if err == nil {
			imageName := "gst-qr-" + sanitizeFilename(document.ID)
			imageOpts := gofpdf.ImageOptions{ImageType: "PNG", ReadDpi: true}
			pdf.RegisterImageOptionsReader(imageName, imageOpts, bytes.NewReader(qrBytes))
			pdf.ImageOptions(imageName, startX+144, startY+4, 28, 28, false, imageOpts, 0, "")
		}
	}

	pdf.SetY(startY + boxHeight + 3)
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

func drawCell(pdf *gofpdf.Fpdf, x, y, w, h float64, text, align, fontFamily string, fontSize, lineHeight float64) {
	pdf.Rect(x, y, w, h, "D")
	pdf.SetXY(x+1, y+1.5)
	pdf.SetFont(fontFamily, "", fontSize)
	pdf.MultiCell(w-2, lineHeight, text, "", align, false)
}

func drawWrappedCell(pdf *gofpdf.Fpdf, x, y, w, h float64, text, align, fontFamily string, fontSize, lineHeight float64) {
	pdf.Rect(x, y, w, h, "D")
	pdf.SetXY(x+1.5, y+1.5)
	pdf.SetFont(fontFamily, "", fontSize)
	pdf.MultiCell(w-3, lineHeight, text, "", align, false)
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

func frozenRenderParties(
	document *models.Document,
	labels *localeLabels,
) (*models.BusinessProfile, renderParty, bool) {
	if document == nil || strings.TrimSpace(document.SourceLinkage) == "" {
		return nil, renderParty{}, false
	}
	var source struct {
		Seller models.PartySnapshot `json:"seller_snapshot"`
		Buyer  models.PartySnapshot `json:"buyer_snapshot"`
	}
	if err := json.Unmarshal([]byte(document.SourceLinkage), &source); err != nil ||
		source.Seller.IsEmpty() || source.Buyer.IsEmpty() {
		return nil, renderParty{}, false
	}
	sellerTaxID := firstNonEmptyRenderValue(source.Seller.GSTIN, source.Seller.TaxID)
	buyerTaxID := firstNonEmptyRenderValue(source.Buyer.GSTIN, source.Buyer.TaxID)
	business := &models.BusinessProfile{
		ID:         document.BusinessID,
		Name:       source.Seller.Name,
		Email:      source.Seller.Email,
		Phone:      source.Seller.Phone,
		Address:    source.Seller.Address,
		City:       source.Seller.City,
		State:      source.Seller.State,
		Country:    source.Seller.Country,
		PostalCode: source.Seller.PostalCode,
		TaxID:      sellerTaxID,
		GSTIN:      source.Seller.GSTIN,
	}
	party := renderParty{
		label: labels.party,
		name:  source.Buyer.Name,
		email: source.Buyer.Email,
		phone: source.Buyer.Phone,
		taxID: buyerTaxID,
		address: compactLines([]string{
			source.Buyer.Address,
			joinCityLine(source.Buyer.City, source.Buyer.State, source.Buyer.PostalCode),
			source.Buyer.Country,
		}),
	}
	return business, party, true
}

func firstNonEmptyRenderValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
			models.DocumentTypeExpense:         "Expense",
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
	replace("invoice_number", &labels.documentNumber)
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
		"show_notes":          true,
		"show_terms":          true,
		"show_declaration":    true,
		"show_logo":           true,
		"show_signature":      false,
		"show_signature_line": false,
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
	if defaults["show_signature"] {
		defaults["show_signature_line"] = true
	}
	return defaults
}

func parseLayoutConfig(profile *models.RenderProfile) map[string]interface{} {
	if profile == nil || strings.TrimSpace(profile.LayoutConfig) == "" {
		return map[string]interface{}{}
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(profile.LayoutConfig), &config); err != nil {
		return map[string]interface{}{}
	}
	return config
}

func resolveRenderTheme(profile *models.RenderProfile) renderTheme {
	density := "classic"
	accent := rgbColor{r: 67, g: 97, b: 146}
	config := parseLayoutConfig(profile)
	if candidate := readLayoutString(config, "density", "layout_density", "layout_variant", "variant", "template"); candidate != "" {
		normalized := strings.ToLower(strings.TrimSpace(candidate))
		switch normalized {
		case "compact":
			density = "compact"
		case "minimal":
			density = "minimal"
		default:
			density = "classic"
		}
	}
	if colorValue := readLayoutString(config, "accent_color", "accentColor", "primary_color", "primaryColor"); colorValue != "" {
		if parsed, ok := parseHexColor(colorValue); ok {
			accent = parsed
		}
	}
	theme := renderTheme{
		accent:       accent,
		density:      density,
		marginTop:    16,
		marginBottom: 16,
		marginLeft:   12,
		marginRight:  12,
		rowLine:      4.5,
		rowMinHeight: 8,
	}
	switch density {
	case "compact":
		theme.marginTop = 12
		theme.marginBottom = 12
		theme.marginLeft = 10
		theme.marginRight = 10
		theme.rowLine = 4.0
		theme.rowMinHeight = 7
	case "minimal":
		theme.marginTop = 18
		theme.marginBottom = 18
		theme.marginLeft = 14
		theme.marginRight = 14
		theme.rowLine = 5.0
		theme.rowMinHeight = 9
	}
	return theme
}

func readLayoutString(config map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := config[key]; ok {
			if asString, ok := value.(string); ok {
				return strings.TrimSpace(asString)
			}
		}
	}
	return ""
}

func parseHexColor(value string) (rgbColor, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) == 3 {
		value = strings.Repeat(string(value[0]), 2) + strings.Repeat(string(value[1]), 2) + strings.Repeat(string(value[2]), 2)
	}
	if len(value) != 6 {
		return rgbColor{}, false
	}
	r, err := strconv.ParseInt(value[0:2], 16, 64)
	if err != nil {
		return rgbColor{}, false
	}
	g, err := strconv.ParseInt(value[2:4], 16, 64)
	if err != nil {
		return rgbColor{}, false
	}
	b, err := strconv.ParseInt(value[4:6], 16, 64)
	if err != nil {
		return rgbColor{}, false
	}
	return rgbColor{r: int(r), g: int(g), b: int(b)}, true
}

func blendColor(base rgbColor, blend float64) rgbColor {
	if blend < 0 {
		blend = 0
	}
	if blend > 1 {
		blend = 1
	}
	return rgbColor{
		r: int(float64(base.r)*(1-blend) + 255*blend),
		g: int(float64(base.g)*(1-blend) + 255*blend),
		b: int(float64(base.b)*(1-blend) + 255*blend),
	}
}

func registerBusinessLogo(ctx context.Context, pdf *gofpdf.Fpdf, business *models.BusinessProfile) *registeredLogo {
	if business == nil || strings.TrimSpace(business.LogoURL) == "" {
		return nil
	}
	data, imageType, err := fetchRemoteImage(ctx, business.LogoURL)
	if err != nil {
		return nil
	}
	name := "business-logo-" + sanitizeFilename(business.ID)
	opts := gofpdf.ImageOptions{ImageType: imageType, ReadDpi: true}
	pdf.RegisterImageOptionsReader(name, opts, bytes.NewReader(data))
	if pdf.Err() {
		return nil
	}
	return &registeredLogo{name: name, opts: opts}
}

func fetchRemoteImage(ctx context.Context, source string) ([]byte, string, error) {
	parsed, err := sanitizeLogoURL(source)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := logoHTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("logo fetch failed with status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil, "", err
	}
	imageType := "JPG"
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if strings.Contains(contentType, "png") || strings.HasSuffix(strings.ToLower(parsed.Path), ".png") {
		imageType = "PNG"
	}
	if len(data) >= 4 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		imageType = "PNG"
	}
	return data, imageType, nil
}

func sanitizeLogoURL(source string) (*url.URL, error) {
	trimmed := strings.TrimSpace(source)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, err
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("logo source host is required")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("logo source scheme must be http or https")
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, fmt.Errorf("logo source host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return nil, fmt.Errorf("logo source host is not allowed")
		}
	}
	return parsed, nil
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
		models.DocumentTypeExpense,
		models.DocumentTypePackingList,
		models.DocumentTypeShippingLabel,
	}
}

func documentTitle(labels *localeLabels, documentType string) string {
	if title, ok := labels.documentTitles[documentType]; ok && title != "" {
		return title
	}
	return strings.ReplaceAll(cases.Title(language.English).String(strings.ReplaceAll(documentType, "_", " ")), " ", " ")
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
