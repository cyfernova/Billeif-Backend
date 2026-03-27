package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type ShippingService struct {
	cfg          *config.Config
	repo         interfaces.ShippingRepository
	customerRepo interfaces.CustomerRepository
	vendorRepo   interfaces.VendorRepository
	httpClient   *http.Client
	log          *logger.Logger
}

type ShippingRequestInput struct {
	Provider          string                 `json:"provider"`
	Courier           string                 `json:"courier"`
	PackageCount      int                    `json:"package_count"`
	WeightKG          float64                `json:"weight_kg"`
	PackageDimensions map[string]interface{} `json:"package_dimensions,omitempty"`
	AddressSnapshot   map[string]interface{} `json:"address_snapshot,omitempty"`
	ProviderPayload   map[string]interface{} `json:"provider_payload,omitempty"`
}

type ShipmentResult struct {
	Shipment *models.Shipment      `json:"shipment"`
	Label    *models.ShippingLabel `json:"label,omitempty"`
}

type shippingProvider interface {
	Name() string
	CreateLabel(ctx context.Context, req shippingProviderRequest) (*shippingProviderResult, error)
}

type shippingProviderRequest struct {
	Document          *models.Document
	Shipment          *models.Shipment
	AddressSnapshot   map[string]interface{}
	PackageDimensions map[string]interface{}
	ProviderPayload   map[string]interface{}
}

type shippingProviderResult struct {
	Status          string
	Courier         string
	TrackingNumber  string
	TrackingURL     string
	LabelURL        string
	LabelZPL        string
	ProviderLabelID string
	Payload         map[string]interface{}
}

func NewShippingService(cfg *config.Config, repo interfaces.ShippingRepository, customerRepo interfaces.CustomerRepository, vendorRepo interfaces.VendorRepository, log *logger.Logger) *ShippingService {
	timeout := 30 * time.Second
	if cfg != nil && cfg.Shipping.Timeout > 0 {
		timeout = time.Duration(cfg.Shipping.Timeout) * time.Second
	}
	return &ShippingService{
		cfg:          cfg,
		repo:         repo,
		customerRepo: customerRepo,
		vendorRepo:   vendorRepo,
		httpClient:   &http.Client{Timeout: timeout},
		log:          log,
	}
}

func (s *ShippingService) GetShipmentByDocument(ctx context.Context, businessID, documentID string) (*models.Shipment, error) {
	return s.repo.GetShipmentByDocument(ctx, businessID, documentID)
}

func (s *ShippingService) GetShippingLabelByDocument(ctx context.Context, businessID, documentID string) (*models.ShippingLabel, error) {
	return s.repo.GetShippingLabelByDocument(ctx, businessID, documentID)
}

func (s *ShippingService) EnsureShipmentForDocument(ctx context.Context, document *models.Document, provider string) (*models.Shipment, error) {
	result, err := s.CreateOrUpdateShipmentForDocument(ctx, document, ShippingRequestInput{Provider: provider})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ShippingService) CreateOrUpdateShipmentForDocument(ctx context.Context, document *models.Document, input ShippingRequestInput) (*models.Shipment, error) {
	if document == nil {
		return nil, fmt.Errorf("document is required")
	}

	extraFields := unmarshalJSONMap(document.ExtraFields)
	if input.PackageCount == 0 {
		input.PackageCount = intValue(extraFields["package_count"], 1)
	}
	if input.WeightKG == 0 {
		input.WeightKG = floatValue(extraFields["weight_kg"])
	}
	if input.PackageDimensions == nil {
		input.PackageDimensions = nestedMap(extraFields, "package_dimensions")
	}
	if input.AddressSnapshot == nil {
		input.AddressSnapshot = nestedMap(extraFields, "address_snapshot")
	}
	if input.ProviderPayload == nil {
		input.ProviderPayload = nestedMap(extraFields, "provider_payload")
	}

	providerName := coalesceString(input.Provider, stringValue(extraFields["provider"]), defaultShippingProvider(s.cfg))
	if providerName == "" {
		providerName = "manual"
	}

	shipment, err := s.repo.GetShipmentByDocument(ctx, document.BusinessID, document.ID)
	if err != nil {
		shipment = &models.Shipment{
			BusinessID: document.BusinessID,
			DocumentID: document.ID,
			Status:     "draft",
		}
	}

	shipment.Provider = providerName
	shipment.Courier = coalesceString(input.Courier, shipment.Courier)
	shipment.PackageCount = intValueWithFallback(input.PackageCount, shipment.PackageCount, 1)
	shipment.WeightKG = floatValueWithFallback(input.WeightKG, shipment.WeightKG)

	addressSnapshot, err := s.resolveAddressSnapshot(ctx, document, input.AddressSnapshot)
	if err != nil {
		return nil, err
	}
	shipment.AddressSnapshot = mustMarshalMap(addressSnapshot)
	shipment.PackageDimensions = mustMarshalMap(input.PackageDimensions)
	shipment.ProviderPayload = mustMarshalMap(input.ProviderPayload)

	if shipment.ID == "" {
		if err := s.repo.CreateShipment(ctx, shipment); err != nil {
			return nil, err
		}
	} else if err := s.repo.UpdateShipment(ctx, shipment); err != nil {
		return nil, err
	}

	return shipment, nil
}

func (s *ShippingService) EnsureLabelForDocument(ctx context.Context, document *models.Document) (*models.ShippingLabel, error) {
	result, err := s.CreateLabelForDocument(ctx, document, ShippingRequestInput{})
	if err != nil {
		return nil, err
	}
	return result.Label, nil
}

func (s *ShippingService) CreateLabelForDocument(ctx context.Context, document *models.Document, input ShippingRequestInput) (*ShipmentResult, error) {
	shipment, err := s.CreateOrUpdateShipmentForDocument(ctx, document, input)
	if err != nil {
		return nil, err
	}

	providerName := shipment.Provider
	provider := s.resolveProvider(providerName)
	if provider == nil {
		return nil, fmt.Errorf("unsupported shipping provider: %s", providerName)
	}

	addressSnapshot := unmarshalJSONMap(shipment.AddressSnapshot)
	packageDimensions := unmarshalJSONMap(shipment.PackageDimensions)
	providerPayload := mergeMaps(unmarshalJSONMap(shipment.ProviderPayload), input.ProviderPayload)

	result, err := provider.CreateLabel(ctx, shippingProviderRequest{
		Document:          document,
		Shipment:          shipment,
		AddressSnapshot:   addressSnapshot,
		PackageDimensions: packageDimensions,
		ProviderPayload:   providerPayload,
	})
	if err != nil {
		if providerName == "shiprocket" && !shiprocketConfigured(s.cfg) {
			s.log.Warn("shiprocket not configured; falling back to manual label", "document_id", document.ID)
			shipment.Provider = "manual"
			if updateErr := s.repo.UpdateShipment(ctx, shipment); updateErr != nil {
				return nil, updateErr
			}
			result, err = (&manualShippingProvider{}).CreateLabel(ctx, shippingProviderRequest{
				Document:          document,
				Shipment:          shipment,
				AddressSnapshot:   addressSnapshot,
				PackageDimensions: packageDimensions,
				ProviderPayload:   providerPayload,
			})
		}
		if err != nil {
			return nil, err
		}
	}

	shipment.Status = coalesceString(result.Status, "label_generated")
	shipment.Courier = coalesceString(result.Courier, shipment.Courier)
	shipment.TrackingNumber = coalesceString(result.TrackingNumber, shipment.TrackingNumber)
	shipment.TrackingURL = coalesceString(result.TrackingURL, shipment.TrackingURL)
	shipment.ProviderPayload = mustMarshalMap(mergeMaps(providerPayload, result.Payload))
	if err := s.repo.UpdateShipment(ctx, shipment); err != nil {
		return nil, err
	}

	label, err := s.repo.GetShippingLabelByDocument(ctx, document.BusinessID, document.ID)
	if err != nil {
		label = &models.ShippingLabel{
			BusinessID:  document.BusinessID,
			ShipmentID:  shipment.ID,
			DocumentID:  document.ID,
			LabelFormat: "pdf",
		}
	}
	label.ShipmentID = shipment.ID
	label.LabelURL = result.LabelURL
	label.LabelZPL = result.LabelZPL
	label.ProviderLabelID = result.ProviderLabelID
	label.ProviderPayload = mustMarshalMap(result.Payload)
	if label.ID == "" {
		if err := s.repo.CreateShippingLabel(ctx, label); err != nil {
			return nil, err
		}
	} else if err := s.repo.UpdateShippingLabel(ctx, label); err != nil {
		return nil, err
	}

	return &ShipmentResult{Shipment: shipment, Label: label}, nil
}

func (s *ShippingService) resolveProvider(providerName string) shippingProvider {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "", "manual":
		return &manualShippingProvider{}
	case "shiprocket":
		if !shiprocketConfigured(s.cfg) {
			return &manualShippingProvider{}
		}
		return &shiprocketProvider{
			cfg:        s.cfg,
			httpClient: s.httpClient,
			log:        s.log,
		}
	default:
		return nil
	}
}

func (s *ShippingService) resolveAddressSnapshot(ctx context.Context, document *models.Document, input map[string]interface{}) (map[string]interface{}, error) {
	if len(input) > 0 {
		return input, nil
	}
	if document == nil || document.PartyID == nil || *document.PartyID == "" {
		return map[string]interface{}{}, nil
	}

	switch document.PartyType {
	case models.DocumentPartyTypeVendor:
		vendor, err := s.vendorRepo.GetByID(ctx, *document.PartyID, document.BusinessID)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"name":        vendor.Name,
			"email":       vendor.Email,
			"phone":       vendor.Phone,
			"address":     vendor.Address,
			"city":        vendor.City,
			"state":       vendor.State,
			"country":     vendor.Country,
			"postal_code": vendor.PostalCode,
			"tax_id":      vendor.TaxID,
		}, nil
	default:
		customer, err := s.customerRepo.GetByID(ctx, *document.PartyID, document.BusinessID)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"name":        customer.Name,
			"email":       customer.Email,
			"phone":       customer.Phone,
			"address":     customer.Address,
			"city":        customer.City,
			"state":       customer.State,
			"country":     customer.Country,
			"postal_code": customer.PostalCode,
			"tax_id":      customer.TaxID,
		}, nil
	}
}

type manualShippingProvider struct{}

func (p *manualShippingProvider) Name() string { return "manual" }

func (p *manualShippingProvider) CreateLabel(_ context.Context, req shippingProviderRequest) (*shippingProviderResult, error) {
	trackingNumber := fmt.Sprintf("MANUAL-%s", strings.ToUpper(strings.ReplaceAll(req.Document.SerialNumber, " ", "-")))
	return &shippingProviderResult{
		Status:          "label_generated",
		Courier:         coalesceString(req.Shipment.Courier, "manual"),
		TrackingNumber:  trackingNumber,
		TrackingURL:     "",
		LabelURL:        fmt.Sprintf("manual://shipping-label/%s", req.Document.ID),
		ProviderLabelID: req.Document.ID,
		Payload: map[string]interface{}{
			"provider":        "manual",
			"tracking_number": trackingNumber,
		},
	}, nil
}

type shiprocketProvider struct {
	cfg        *config.Config
	httpClient *http.Client
	log        *logger.Logger
}

func (p *shiprocketProvider) Name() string { return "shiprocket" }

func (p *shiprocketProvider) CreateLabel(ctx context.Context, req shippingProviderRequest) (*shippingProviderResult, error) {
	token, err := p.authenticate(ctx)
	if err != nil {
		return nil, err
	}

	orderPayload := nestedMap(req.ProviderPayload, "order_payload")
	if len(orderPayload) == 0 {
		orderPayload = p.defaultOrderPayload(req)
	}
	orderResponse, err := p.doJSONRequest(ctx, http.MethodPost, p.cfg.Shipping.ShiprocketOrderPath, token, orderPayload)
	if err != nil {
		return nil, err
	}

	shipmentID := readStringCandidate(orderResponse, "shipment_id", "data.shipment_id", "data.order.shipment_id")
	trackingNumber := readStringCandidate(orderResponse, "awb_code", "data.awb_code", "data.tracking_number")
	courier := readStringCandidate(orderResponse, "courier_name", "data.courier_name")

	if shipmentID != "" && trackingNumber == "" && strings.TrimSpace(p.cfg.Shipping.ShiprocketAssignAWBPath) != "" {
		assignPayload := nestedMap(req.ProviderPayload, "assign_awb_payload")
		if len(assignPayload) == 0 {
			assignPayload = map[string]interface{}{"shipment_id": shipmentID}
			if courierID := readStringCandidate(orderResponse, "recommended_courier_company_id", "data.recommended_courier_company_id"); courierID != "" {
				assignPayload["courier_id"] = courierID
			}
		}
		assignResp, assignErr := p.doJSONRequest(ctx, http.MethodPost, p.cfg.Shipping.ShiprocketAssignAWBPath, token, assignPayload)
		if assignErr == nil {
			trackingNumber = coalesceString(trackingNumber, readStringCandidate(assignResp, "awb_code", "data.awb_code"))
			courier = coalesceString(courier, readStringCandidate(assignResp, "courier_name", "data.courier_name"))
			orderResponse = mergeMaps(orderResponse, map[string]interface{}{"assign_awb_response": assignResp})
		}
	}

	labelURL := readStringCandidate(orderResponse, "label_url", "data.label_url", "data.label.download_url")
	providerLabelID := readStringCandidate(orderResponse, "label_id", "data.label_id")
	if shipmentID != "" && labelURL == "" && strings.TrimSpace(p.cfg.Shipping.ShiprocketLabelPath) != "" {
		labelPayload := nestedMap(req.ProviderPayload, "label_payload")
		if len(labelPayload) == 0 {
			labelPayload = map[string]interface{}{"shipment_id": shipmentID}
		}
		labelResp, labelErr := p.doJSONRequest(ctx, http.MethodPost, p.cfg.Shipping.ShiprocketLabelPath, token, labelPayload)
		if labelErr == nil {
			labelURL = coalesceString(labelURL, readStringCandidate(labelResp, "label_url", "data.label_url", "data.label.download_url"))
			providerLabelID = coalesceString(providerLabelID, readStringCandidate(labelResp, "label_id", "data.label_id"))
			orderResponse = mergeMaps(orderResponse, map[string]interface{}{"label_response": labelResp})
		}
	}

	trackingURL := readStringCandidate(orderResponse, "tracking_url", "data.tracking_url")
	if trackingURL == "" && trackingNumber != "" && strings.TrimSpace(p.cfg.Shipping.ShiprocketTrackingBaseURL) != "" {
		trackingURL = strings.TrimRight(p.cfg.Shipping.ShiprocketTrackingBaseURL, "/") + "/" + trackingNumber
	}

	return &shippingProviderResult{
		Status:          "label_generated",
		Courier:         coalesceString(courier, req.Shipment.Courier),
		TrackingNumber:  trackingNumber,
		TrackingURL:     trackingURL,
		LabelURL:        labelURL,
		ProviderLabelID: coalesceString(providerLabelID, shipmentID),
		Payload:         orderResponse,
	}, nil
}

func (p *shiprocketProvider) authenticate(ctx context.Context) (string, error) {
	response, err := p.doJSONRequest(ctx, http.MethodPost, p.cfg.Shipping.ShiprocketAuthPath, "", map[string]interface{}{
		"email":    p.cfg.Shipping.ShiprocketEmail,
		"password": p.cfg.Shipping.ShiprocketPassword,
	})
	if err != nil {
		return "", err
	}
	token := readStringCandidate(response, "token", "data.token")
	if token == "" {
		return "", fmt.Errorf("shiprocket auth response did not include a token")
	}
	return token, nil
}

func (p *shiprocketProvider) defaultOrderPayload(req shippingProviderRequest) map[string]interface{} {
	address := req.AddressSnapshot
	orderItems := make([]map[string]interface{}, 0, len(req.Document.Lines))
	for _, line := range req.Document.Lines {
		orderItems = append(orderItems, map[string]interface{}{
			"name":          line.Description,
			"sku":           stringPointerValue(line.ProductID),
			"units":         line.Quantity,
			"selling_price": line.UnitPrice,
			"discount":      line.DiscountAmount,
			"tax":           line.TaxAmount + line.CessAmount,
		})
	}
	return map[string]interface{}{
		"order_id":              req.Document.SerialNumber,
		"order_date":            req.Document.IssueDate.Format("2006-01-02 15:04"),
		"pickup_location":       "Primary",
		"channel_id":            "",
		"comment":               req.Document.Notes,
		"billing_customer_name": stringValue(address["name"]),
		"billing_last_name":     "",
		"billing_address":       stringValue(address["address"]),
		"billing_city":          stringValue(address["city"]),
		"billing_pincode":       stringValue(address["postal_code"]),
		"billing_state":         stringValue(address["state"]),
		"billing_country":       coalesceString(stringValue(address["country"]), "India"),
		"billing_email":         stringValue(address["email"]),
		"billing_phone":         stringValue(address["phone"]),
		"shipping_is_billing":   true,
		"order_items":           orderItems,
		"payment_method":        "Prepaid",
		"shipping_charges":      0,
		"giftwrap_charges":      0,
		"transaction_charges":   0,
		"total_discount":        req.Document.DiscountTotal,
		"sub_total":             req.Document.Total,
		"length":                floatValue(req.PackageDimensions["length"]),
		"breadth":               floatValue(req.PackageDimensions["breadth"]),
		"height":                floatValue(req.PackageDimensions["height"]),
		"weight":                floatValueWithFallback(req.Shipment.WeightKG, 0),
	}
}

func (p *shiprocketProvider) doJSONRequest(ctx context.Context, method, endpointPath, token string, payload map[string]interface{}) (map[string]interface{}, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	requestURL := joinURL(p.cfg.Shipping.ShiprocketBaseURL, endpointPath)
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed map[string]interface{}
	if len(responseBody) > 0 {
		_ = json.Unmarshal(responseBody, &parsed)
	}
	if parsed == nil {
		parsed = map[string]interface{}{}
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("shiprocket request failed with status %d", resp.StatusCode)
	}
	return parsed, nil
}

func shiprocketConfigured(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return strings.TrimSpace(cfg.Shipping.ShiprocketEmail) != "" && strings.TrimSpace(cfg.Shipping.ShiprocketPassword) != ""
}

func defaultShippingProvider(cfg *config.Config) string {
	if cfg == nil {
		return "manual"
	}
	return coalesceString(cfg.Shipping.DefaultProvider, "manual")
}

func unmarshalJSONMap(raw string) map[string]interface{} {
	if strings.TrimSpace(raw) == "" {
		return map[string]interface{}{}
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return map[string]interface{}{}
	}
	return data
}

func nestedMap(data map[string]interface{}, key string) map[string]interface{} {
	if data == nil {
		return map[string]interface{}{}
	}
	if value, ok := data[key]; ok {
		if nested, ok := value.(map[string]interface{}); ok {
			return nested
		}
	}
	return map[string]interface{}{}
}

func mergeMaps(base map[string]interface{}, overrides map[string]interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overrides {
		merged[key] = value
	}
	return merged
}

func joinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	path = strings.TrimSpace(path)
	if path == "" {
		return base
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return base + "/" + strings.TrimLeft(path, "/")
}

func readStringCandidate(data map[string]interface{}, paths ...string) string {
	for _, candidate := range paths {
		if value := readStringPath(data, candidate); value != "" {
			return value
		}
	}
	return ""
}

func readStringPath(data map[string]interface{}, path string) string {
	current := interface{}(data)
	for _, part := range strings.Split(path, ".") {
		asMap, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current = asMap[part]
	}
	return stringValue(current)
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%.0f", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	default:
		return ""
	}
}

func intValue(value interface{}, fallback int) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return fallback
	}
}

func intValueWithFallback(primary int, fallback int, defaultValue int) int {
	if primary > 0 {
		return primary
	}
	if fallback > 0 {
		return fallback
	}
	return defaultValue
}

func floatValue(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	default:
		return 0
	}
}

func floatValueWithFallback(primary, fallback float64) float64 {
	if primary > 0 {
		return primary
	}
	return fallback
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
