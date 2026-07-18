package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
)

const (
	posSessionStatusOpen   = "open"
	posSessionStatusClosed = "closed"
)

type CreatePOSSessionInput struct {
	POSProfileID string                 `json:"pos_profile_id,omitempty"`
	SessionName  string                 `json:"session_name,omitempty"`
	WarehouseID  string                 `json:"warehouse_id,omitempty"`
	Currency     string                 `json:"currency,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

type POSCatalogSearchResult struct {
	ID          string                 `json:"id"`
	EntityType  string                 `json:"entity_type"`
	ProductID   string                 `json:"product_id"`
	VariantID   string                 `json:"variant_id,omitempty"`
	Name        string                 `json:"name"`
	SKU         string                 `json:"sku,omitempty"`
	Barcode     string                 `json:"barcode,omitempty"`
	Price       float64                `json:"price"`
	MRP         float64                `json:"mrp,omitempty"`
	StockLevel  float64                `json:"stock_level,omitempty"`
	HSNSACCode  string                 `json:"hsn_sac_code,omitempty"`
	UQCCode     string                 `json:"uqc_code,omitempty"`
	Unit        string                 `json:"unit,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	ProductName string                 `json:"product_name,omitempty"`
}

type ScanPOSItemInput struct {
	Code     string  `json:"code" binding:"required"`
	Quantity float64 `json:"quantity,omitempty"`
}

type POSSessionCartLine struct {
	ProductID   string                 `json:"product_id"`
	VariantID   string                 `json:"variant_id,omitempty"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	SKU         string                 `json:"sku,omitempty"`
	Barcode     string                 `json:"barcode,omitempty"`
	Quantity    float64                `json:"quantity"`
	UnitPrice   float64                `json:"unit_price"`
	MRP         float64                `json:"mrp,omitempty"`
	TaxRate     float64                `json:"tax_rate,omitempty"`
	CessRate    float64                `json:"cess_rate,omitempty"`
	HSNSACCode  string                 `json:"hsn_sac_code,omitempty"`
	UQCCode     string                 `json:"uqc_code,omitempty"`
	Unit        string                 `json:"unit,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type POSSessionCart struct {
	Items           []POSSessionCartLine `json:"items"`
	Subtotal        float64              `json:"subtotal"`
	TaxTotal        float64              `json:"tax_total"`
	CessTotal       float64              `json:"cess_total"`
	Total           float64              `json:"total"`
	ItemCount       int                  `json:"item_count"`
	LastScannedCode string               `json:"last_scanned_code,omitempty"`
}

type CheckoutPOSCartInput struct {
	PartyID             string                 `json:"party_id,omitempty"`
	PartyType           string                 `json:"party_type,omitempty"`
	Status              string                 `json:"status,omitempty"`
	TaxMode             string                 `json:"tax_mode,omitempty"`
	GSTTreatment        string                 `json:"gst_treatment,omitempty"`
	PlaceOfSupply       string                 `json:"place_of_supply,omitempty"`
	PartyGSTIN          string                 `json:"party_gstin,omitempty"`
	PartyPAN            string                 `json:"party_pan,omitempty"`
	PartyStateCode      string                 `json:"party_state_code,omitempty"`
	GenerateEInvoice    bool                   `json:"generate_einvoice"`
	GenerateEWayBill    bool                   `json:"generate_ewaybill"`
	ReverseCharge       bool                   `json:"reverse_charge"`
	ReverseChargeReason string                 `json:"reverse_charge_reason,omitempty"`
	DispatchFrom        map[string]interface{} `json:"dispatch_from,omitempty"`
	DispatchTo          map[string]interface{} `json:"dispatch_to,omitempty"`
	DistanceKM          float64                `json:"distance_km,omitempty"`
	Transporter         map[string]interface{} `json:"transporter,omitempty"`
	Vehicle             map[string]interface{} `json:"vehicle,omitempty"`
	Notes               string                 `json:"notes,omitempty"`
	Source              string                 `json:"source,omitempty"`
	Lines               []POSSessionCartLine   `json:"lines,omitempty"`
}

type POSReceiptResponse struct {
	DocumentID  string `json:"document_id"`
	Format      string `json:"format"`
	Width       string `json:"width"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
}

type POSService struct {
	db           *gorm.DB
	documents    *DocumentService
	barcode      *BarcodeService
	inventory    *InventoryService
	entitlements *EntitlementService
	log          *logger.Logger
}

func NewPOSService(db *gorm.DB, documents *DocumentService, barcode *BarcodeService, inventory *InventoryService, entitlements *EntitlementService, log *logger.Logger) *POSService {
	return &POSService{
		db:           db,
		documents:    documents,
		barcode:      barcode,
		inventory:    inventory,
		entitlements: entitlements,
		log:          log,
	}
}

func (s *POSService) CreateSession(ctx context.Context, businessID, userID string, input CreatePOSSessionInput) (*models.POSSession, error) {
	if err := s.entitlements.EnsureFeature(ctx, businessID, FeaturePOS); err != nil {
		return nil, err
	}

	session := &models.POSSession{
		BusinessID:  businessID,
		UserID:      userID,
		Status:      posSessionStatusOpen,
		SessionName: input.SessionName,
		Currency:    firstNonEmpty(input.Currency, "INR"),
		CartPayload: mustMarshalAny(POSSessionCart{Items: []POSSessionCartLine{}}, "{}"),
		Metadata:    mustMarshalMap(input.Metadata),
	}
	if input.POSProfileID != "" {
		session.POSProfileID = &input.POSProfileID
	}
	if input.WarehouseID != "" {
		session.WarehouseID = &input.WarehouseID
	}

	if input.POSProfileID != "" {
		var profile models.POSProfile
		if err := s.db.WithContext(ctx).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", input.POSProfileID, businessID).
			First(&profile).Error; err == nil {
			if session.WarehouseID == nil {
				session.WarehouseID = profile.DefaultWarehouseID
			}
			if strings.TrimSpace(session.SessionName) == "" {
				session.SessionName = profile.Name
			}
			if trimmed := strings.TrimSpace(profile.Metadata); trimmed != "" && trimmed != "{}" {
				session.Metadata = profile.Metadata
			}
		}
	}
	warehouseID := posStringValue(session.WarehouseID)
	if err := s.authorizeWarehouse(ctx, userID, businessID, warehouseID, warehousePermissionMoveStock); err != nil {
		return nil, err
	}

	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		return nil, err
	}
	return session, nil
}

func (s *POSService) ListSessions(ctx context.Context, businessID, userID string, page, limit int, status string) ([]models.POSSession, int64, error) {
	if err := s.entitlements.EnsureFeature(ctx, businessID, FeaturePOS); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	normalizedStatus := strings.ToLower(strings.TrimSpace(status))
	switch normalizedStatus {
	case "active":
		normalizedStatus = posSessionStatusOpen
	case "all":
		normalizedStatus = ""
	}

	baseQuery := s.db.WithContext(ctx).
		Model(&models.POSSession{}).
		Where("business_id = ? AND user_id = ? AND deleted_at IS NULL", businessID, userID)
	if normalizedStatus != "" {
		baseQuery = baseQuery.Where("status = ?", normalizedStatus)
	}

	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var sessions []models.POSSession
	if err := baseQuery.
		Order("opened_at DESC").
		Limit(limit).
		Offset((page - 1) * limit).
		Find(&sessions).Error; err != nil {
		return nil, 0, err
	}

	return sessions, total, nil
}

func (s *POSService) SearchCatalog(ctx context.Context, businessID, userID, query, warehouseID string, limit int) ([]POSCatalogSearchResult, error) {
	if err := s.entitlements.EnsureFeature(ctx, businessID, FeaturePOS); err != nil {
		return nil, err
	}
	if err := s.authorizeWarehouse(ctx, userID, businessID, warehouseID, warehousePermissionViewCatalog); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	q := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	results := make([]POSCatalogSearchResult, 0)

	var products []models.Product
	productQuery := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL AND is_active = TRUE", businessID)
	if strings.TrimSpace(query) != "" {
		productQuery = productQuery.Where("LOWER(name) LIKE ? OR LOWER(sku) LIKE ? OR LOWER(COALESCE(barcode, '')) LIKE ?", q, q, q)
	}
	if err := productQuery.Order("updated_at DESC").Limit(limit).Find(&products).Error; err != nil {
		return nil, err
	}
	for _, product := range products {
		results = append(results, POSCatalogSearchResult{
			ID:         product.ID,
			EntityType: "product",
			ProductID:  product.ID,
			Name:       product.Name,
			SKU:        product.SKU,
			Barcode:    product.Barcode,
			Price:      product.Price,
			MRP:        product.MRP,
			StockLevel: float64(product.StockLevel),
			HSNSACCode: product.HSNSACCode,
			UQCCode:    product.UQCCode,
			Unit:       product.Unit,
		})
	}

	if len(results) < limit {
		var variants []models.ProductVariant
		variantQuery := s.db.WithContext(ctx).
			Where("business_id = ? AND deleted_at IS NULL AND is_active = TRUE", businessID)
		if strings.TrimSpace(query) != "" {
			variantQuery = variantQuery.Where("LOWER(name) LIKE ? OR LOWER(sku) LIKE ? OR LOWER(COALESCE(barcode, '')) LIKE ?", q, q, q)
		}
		if err := variantQuery.Order("updated_at DESC").Limit(limit - len(results)).Find(&variants).Error; err != nil {
			return nil, err
		}
		for _, variant := range variants {
			var product models.Product
			if err := s.db.WithContext(ctx).
				Where("id = ? AND business_id = ? AND deleted_at IS NULL", variant.ProductID, businessID).
				First(&product).Error; err != nil {
				continue
			}
			results = append(results, POSCatalogSearchResult{
				ID:          variant.ID,
				EntityType:  "variant",
				ProductID:   product.ID,
				VariantID:   variant.ID,
				Name:        product.Name + " / " + variant.Name,
				ProductName: product.Name,
				SKU:         variant.SKU,
				Barcode:     variant.Barcode,
				Price:       coalesceFloat(variant.Price, product.Price),
				MRP:         coalesceFloat(variant.MRP, product.MRP),
				StockLevel:  variant.StockLevel,
				HSNSACCode:  product.HSNSACCode,
				UQCCode:     product.UQCCode,
				Unit:        product.Unit,
				Attributes:  unmarshalJSONMap(variant.Attributes),
			})
		}
	}

	if warehouseID != "" {
		filtered := results[:0]
		for _, item := range results {
			if s.productVisibleInWarehouse(ctx, businessID, item.ProductID, warehouseID) {
				filtered = append(filtered, item)
			}
		}
		results = filtered
	}

	return results, nil
}

func (s *POSService) ScanItem(ctx context.Context, businessID, userID, sessionID string, input ScanPOSItemInput) (*models.POSSession, POSSessionCart, error) {
	if err := s.entitlements.EnsureFeature(ctx, businessID, FeaturePOS); err != nil {
		return nil, POSSessionCart{}, err
	}
	session, err := s.getSession(ctx, businessID, userID, sessionID)
	if err != nil {
		return nil, POSSessionCart{}, err
	}
	if err := s.authorizeWarehouse(ctx, userID, businessID, posStringValue(session.WarehouseID), warehousePermissionMoveStock); err != nil {
		return nil, POSSessionCart{}, err
	}
	lookup, err := s.barcode.Lookup(ctx, businessID, input.Code)
	if err != nil {
		return nil, POSSessionCart{}, err
	}

	line, err := s.lookupToCartLine(ctx, businessID, lookup)
	if err != nil {
		return nil, POSSessionCart{}, err
	}
	if input.Quantity > 0 {
		line.Quantity = input.Quantity
	}

	cart := s.readCart(session)
	merged := false
	for idx := range cart.Items {
		if cart.Items[idx].ProductID == line.ProductID && cart.Items[idx].VariantID == line.VariantID {
			cart.Items[idx].Quantity += line.Quantity
			merged = true
			break
		}
	}
	if !merged {
		cart.Items = append(cart.Items, line)
	}
	cart.LastScannedCode = input.Code
	recalculatePOSCart(&cart)

	session.CartPayload = mustMarshalAny(cart, "{}")
	session.LastScannedCode = input.Code
	if err := s.db.WithContext(ctx).Save(session).Error; err != nil {
		return nil, POSSessionCart{}, err
	}
	return session, cart, nil
}

func (s *POSService) Checkout(ctx context.Context, businessID, userID, sessionID, idempotencyKey string, input CheckoutPOSCartInput) (*models.Document, error) {
	if err := s.entitlements.EnsureFeature(ctx, businessID, FeaturePOS); err != nil {
		return nil, err
	}
	session, err := s.getSession(ctx, businessID, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeWarehouse(ctx, userID, businessID, posStringValue(session.WarehouseID), warehousePermissionMoveStock); err != nil {
		return nil, err
	}
	if session.LastCheckedOutDocumentID != nil && idempotencyKey != "" {
		metadata := unmarshalJSONMap(session.Metadata)
		if readStringCandidate(metadata, "checkout_idempotency_key") == idempotencyKey {
			return s.documents.GetByBusiness(ctx, businessID, *session.LastCheckedOutDocumentID)
		}
	}

	cart := s.readCart(session)
	cart, err = resolveTrustedPOSCheckoutCart(cart, input)
	if err != nil {
		return nil, err
	}

	lines := make([]CreateDocumentLineInput, 0, len(cart.Items))
	for _, item := range cart.Items {
		lines = append(lines, CreateDocumentLineInput{
			ProductID:    item.ProductID,
			VariantID:    item.VariantID,
			Description:  item.Description,
			Quantity:     item.Quantity,
			UnitPrice:    item.UnitPrice,
			MRP:          item.MRP,
			TaxRate:      item.TaxRate,
			CessRate:     item.CessRate,
			HSNSACCode:   item.HSNSACCode,
			UQCCode:      item.UQCCode,
			Unit:         item.Unit,
			WarehouseID:  posStringValue(session.WarehouseID),
			CustomFields: item.Metadata,
		})
	}

	partyType := firstNonEmpty(input.PartyType, models.DocumentPartyTypeManual)
	createInput := CreateDocumentInput{
		PartyID:             input.PartyID,
		PartyType:           partyType,
		Status:              firstNonEmpty(input.Status, models.DocumentStatusIssued),
		DraftState:          models.DocumentDraftStateFinal,
		TaxMode:             firstNonEmpty(input.TaxMode, models.DocumentTaxModeNonGST),
		GSTTreatment:        firstNonEmpty(input.GSTTreatment, models.DocumentGSTTreatmentRegular),
		PlaceOfSupply:       input.PlaceOfSupply,
		PartyGSTIN:          input.PartyGSTIN,
		PartyPAN:            input.PartyPAN,
		PartyStateCode:      input.PartyStateCode,
		SupplyType:          "sale",
		IssueDate:           time.Now().UTC(),
		Currency:            firstNonEmpty(session.Currency, "INR"),
		Locale:              "en-IN",
		Direction:           models.DocumentDirectionOutward,
		GenerateEInvoice:    input.GenerateEInvoice,
		GenerateEWayBill:    input.GenerateEWayBill,
		ReverseCharge:       input.ReverseCharge,
		ReverseChargeReason: input.ReverseChargeReason,
		DispatchFrom:        input.DispatchFrom,
		DispatchTo:          input.DispatchTo,
		DistanceKM:          input.DistanceKM,
		Transporter:         input.Transporter,
		Vehicle:             input.Vehicle,
		Notes:               coalesceString(input.Notes, fmt.Sprintf("POS checkout from session %s", session.ID)),
		ExtraFields: map[string]interface{}{
			"source":      coalesceString(input.Source, "pos"),
			"pos_session": session.ID,
		},
		Lines: lines,
	}
	document, err := s.documents.CreateByType(ctx, businessID, models.DocumentTypeSalesInvoice, createInput)
	if err != nil {
		return nil, err
	}

	session.LastCheckedOutDocumentID = &document.ID
	session.CartPayload = mustMarshalAny(POSSessionCart{Items: []POSSessionCartLine{}}, "{}")
	meta := unmarshalJSONMap(session.Metadata)
	if idempotencyKey != "" {
		meta["checkout_idempotency_key"] = idempotencyKey
	}
	meta["last_checkout_document_id"] = document.ID
	session.Metadata = mustMarshalMap(meta)
	if err := s.db.WithContext(ctx).Save(session).Error; err != nil {
		return nil, err
	}
	return document, nil
}

func (s *POSService) CloseSession(ctx context.Context, businessID, userID, sessionID string) (*models.POSSession, error) {
	if err := s.entitlements.EnsureFeature(ctx, businessID, FeaturePOS); err != nil {
		return nil, err
	}
	var session models.POSSession
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND user_id = ? AND deleted_at IS NULL", sessionID, businessID, userID).
		First(&session).Error; err != nil {
		return nil, err
	}
	if err := s.authorizeWarehouse(ctx, userID, businessID, posStringValue(session.WarehouseID), warehousePermissionMoveStock); err != nil {
		return nil, err
	}
	if session.Status != posSessionStatusClosed {
		now := time.Now().UTC()
		session.Status = posSessionStatusClosed
		session.ClosedAt = &now
		if err := s.db.WithContext(ctx).Save(&session).Error; err != nil {
			return nil, err
		}
	}
	return &session, nil
}

func (s *POSService) GetThermalReceipt(ctx context.Context, businessID, documentID, format, width string) (*POSReceiptResponse, error) {
	if err := s.entitlements.EnsureFeature(ctx, businessID, FeaturePOS); err != nil {
		return nil, err
	}
	document, err := s.documents.GetByBusiness(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	if width == "" {
		width = "58mm"
	}
	if format == "" {
		format = "thermal"
	}

	lines := []string{
		document.SerialNumber,
		document.IssueDate.Format("2006-01-02 15:04"),
	}
	for _, item := range document.Lines {
		lines = append(lines, fmt.Sprintf("%s x%s @ %.2f = %.2f", item.Description, formatPOSQuantity(item.Quantity), item.UnitPrice, item.LineTotal))
	}
	lines = append(lines,
		fmt.Sprintf("Subtotal: %.2f", document.Subtotal),
		fmt.Sprintf("Tax: %.2f", document.TaxTotal+document.CessTotal),
		fmt.Sprintf("Total: %.2f", document.Total),
	)

	return &POSReceiptResponse{
		DocumentID:  document.ID,
		Format:      format,
		Width:       width,
		ContentType: "text/plain",
		Content:     strings.Join(lines, "\n"),
	}, nil
}

func (s *POSService) getSession(ctx context.Context, businessID, userID, sessionID string) (*models.POSSession, error) {
	var session models.POSSession
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND user_id = ? AND deleted_at IS NULL", sessionID, businessID, userID).
		First(&session).Error; err != nil {
		return nil, err
	}
	if session.Status == posSessionStatusClosed {
		return nil, fmt.Errorf("pos session is closed")
	}
	return &session, nil
}

func (s *POSService) authorizeWarehouse(ctx context.Context, userID, businessID, warehouseID, permission string) error {
	if strings.TrimSpace(warehouseID) == "" {
		return fmt.Errorf("POS warehouse is required")
	}
	if s.inventory == nil || !s.inventory.UserHasWarehouseAccess(ctx, userID, businessID, warehouseID, permission) {
		return fmt.Errorf("access denied to POS warehouse")
	}
	return nil
}

func (s *POSService) lookupToCartLine(_ context.Context, _ string, lookup map[string]interface{}) (POSSessionCartLine, error) {
	product, _ := lookup["product"].(models.Product)
	variant, hasVariant := lookup["variant"].(models.ProductVariant)

	line := POSSessionCartLine{
		ProductID:   product.ID,
		Name:        product.Name,
		Description: product.Name,
		SKU:         product.SKU,
		Barcode:     product.Barcode,
		Quantity:    1,
		UnitPrice:   product.Price,
		MRP:         product.MRP,
		HSNSACCode:  product.HSNSACCode,
		UQCCode:     product.UQCCode,
		Unit:        product.Unit,
		Metadata:    map[string]interface{}{},
	}
	if hasVariant {
		line.VariantID = variant.ID
		line.Name = product.Name + " / " + variant.Name
		line.Description = line.Name
		line.SKU = firstNonEmpty(variant.SKU, product.SKU)
		line.Barcode = firstNonEmpty(variant.Barcode, product.Barcode)
		line.UnitPrice = coalesceFloat(variant.Price, product.Price)
		line.MRP = coalesceFloat(variant.MRP, product.MRP)
		line.CessRate = variant.DefaultCessRate
		line.Metadata = unmarshalJSONMap(variant.Attributes)
	}

	line.TaxRate = floatValue(unmarshalJSONMap(product.GSTMetadata)["tax_rate"])
	if line.CessRate <= 0 {
		line.CessRate = product.DefaultCessRate
	}
	return line, nil
}

func (s *POSService) readCart(session *models.POSSession) POSSessionCart {
	var cart POSSessionCart
	if err := jsonUnmarshalString(session.CartPayload, &cart); err != nil || cart.Items == nil {
		cart.Items = []POSSessionCartLine{}
	}
	return cart
}

func resolveTrustedPOSCheckoutCart(cart POSSessionCart, input CheckoutPOSCartInput) (POSSessionCart, error) {
	if len(cart.Items) == 0 && len(input.Lines) > 0 {
		return POSSessionCart{}, fmt.Errorf("pos checkout requires server-side cart items")
	}
	if len(cart.Items) == 0 {
		return POSSessionCart{}, fmt.Errorf("pos cart is empty")
	}
	recalculatePOSCart(&cart)
	return cart, nil
}

func (s *POSService) productVisibleInWarehouse(ctx context.Context, businessID, productID, warehouseID string) bool {
	if warehouseID == "" {
		return true
	}
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&models.ProductWarehouseCatalog{}).
		Where("business_id = ? AND product_id = ? AND warehouse_id = ? AND deleted_at IS NULL AND COALESCE(is_visible, TRUE) = TRUE", businessID, productID, warehouseID).
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func recalculatePOSCart(cart *POSSessionCart) {
	cart.Subtotal = 0
	cart.TaxTotal = 0
	cart.CessTotal = 0
	cart.Total = 0
	cart.ItemCount = 0
	for _, item := range cart.Items {
		lineSubtotal := item.Quantity * item.UnitPrice
		tax := lineSubtotal * (item.TaxRate / 100)
		cess := lineSubtotal * (item.CessRate / 100)
		cart.Subtotal += lineSubtotal
		cart.TaxTotal += tax
		cart.CessTotal += cess
		cart.Total += lineSubtotal + tax + cess
		cart.ItemCount++
	}
}

func jsonUnmarshalString[T any](raw string, dest *T) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("empty payload")
	}
	return json.Unmarshal([]byte(raw), dest)
}

func posStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func coalesceFloat(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

func formatPOSQuantity(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d", int64(value))
	}
	return fmt.Sprintf("%.3f", value)
}
