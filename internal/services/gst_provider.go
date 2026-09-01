package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"
)

type GSTProvider interface {
	ValidateCredentials(ctx context.Context, account *GSTIntegrationAccountCredentials) error
	GenerateEInvoice(ctx context.Context, req GSTEInvoiceRequest) (*GSTEInvoiceResult, error)
	CancelEInvoice(ctx context.Context, req GSTCancelEInvoiceRequest) (*GSTCancelEInvoiceResult, error)
	GenerateEWayBill(ctx context.Context, req GSTEWayBillRequest) (*GSTEWayBillResult, error)
	UpdateEWayPartB(ctx context.Context, req GSTEWayPartBRequest) (*GSTEWayBillResult, error)
	InitiateMultiVehicle(ctx context.Context, req GSTMultiVehicleRequest) (*GSTMultiVehicleResult, error)
	FetchEWayBillPDF(ctx context.Context, req GSTEWayBillPDFRequest) (*GSTEWayBillPDFResult, error)
	FetchDistance(ctx context.Context, req GSTDistanceRequest) (*GSTDistanceResult, error)
}

type GSTIntegrationAccountCredentials struct {
	PortalUsername string            `json:"portal_username,omitempty"`
	PortalPassword string            `json:"portal_password,omitempty"`
	APIUsername    string            `json:"api_username,omitempty"`
	APIPassword    string            `json:"api_password,omitempty"`
	APIKey         string            `json:"api_key,omitempty"`
	APISecret      string            `json:"api_secret,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type GSTEInvoiceRequest struct {
	BusinessID string
	DocumentID string
	SerialNo   string
	Payload    map[string]interface{}
}

type GSTEInvoiceResult struct {
	ProviderReferenceID string
	IRN                 string
	AckNumber           string
	AckDate             *time.Time
	SignedQRCodePayload string
	QRCodeURL           string
	RawResponse         map[string]interface{}
}

type GSTCancelEInvoiceRequest struct {
	BusinessID string
	DocumentID string
	IRN        string
	Reason     string
	Payload    map[string]interface{}
}

type GSTCancelEInvoiceResult struct {
	ProviderReferenceID string
	RawResponse         map[string]interface{}
}

type GSTEWayBillRequest struct {
	BusinessID string
	DocumentID string
	SerialNo   string
	Payload    map[string]interface{}
}

type GSTEWayBillResult struct {
	ProviderReferenceID string
	EWayBillNumber      string
	EWayBillDate        *time.Time
	ValidUntil          *time.Time
	PDFURL              string
	RawResponse         map[string]interface{}
}

type GSTEWayPartBRequest struct {
	BusinessID string
	DocumentID string
	EWayBillNo string
	Payload    map[string]interface{}
}

type GSTMultiVehicleRequest struct {
	BusinessID string
	DocumentID string
	EWayBillNo string
	Payload    map[string]interface{}
}

type GSTMultiVehicleResult struct {
	ProviderReferenceID string
	RawResponse         map[string]interface{}
}

type GSTEWayBillPDFRequest struct {
	BusinessID string
	DocumentID string
	EWayBillNo string
}

type GSTEWayBillPDFResult struct {
	PDFURL      string
	PDFContent  []byte
	RawResponse map[string]interface{}
}

type GSTDistanceRequest struct {
	FromPincode string
	ToPincode   string
}

type GSTDistanceResult struct {
	DistanceKM  int
	RawResponse map[string]interface{}
}

func NewConfiguredGSTProvider(cfg *config.Config, log *logger.Logger) GSTProvider {
	if cfg == nil || strings.TrimSpace(cfg.GST.BaseURL) == "" {
		return &simulatedGSTProvider{log: log}
	}
	timeout := 20 * time.Second
	if cfg.GST.Timeout > 0 {
		timeout = time.Duration(cfg.GST.Timeout) * time.Second
	}
	return &configuredGSTProvider{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout},
		log:        log,
	}
}

func NewLazyConfiguredGSTProvider(cfg *config.Config, resolver ProviderConfigResolver, log *logger.Logger) GSTProvider {
	if resolver == nil || cfg == nil || strings.TrimSpace(cfg.GST.BaseURL) == "" {
		return NewConfiguredGSTProvider(cfg, log)
	}
	return &lazyConfiguredGSTProvider{cfg: cfg, resolver: resolver, log: log}
}

type lazyConfiguredGSTProvider struct {
	cfg      *config.Config
	resolver ProviderConfigResolver
	log      *logger.Logger
}

func (p *lazyConfiguredGSTProvider) provider(ctx context.Context) (GSTProvider, error) {
	resolved, err := p.resolver.ResolveProvider(ctx, p.cfg, config.SecretGSTProvider)
	if err != nil {
		return nil, err
	}
	return NewConfiguredGSTProvider(resolved, p.log), nil
}

func (p *lazyConfiguredGSTProvider) ValidateCredentials(ctx context.Context, account *GSTIntegrationAccountCredentials) error {
	provider, err := p.provider(ctx)
	if err != nil {
		return err
	}
	return provider.ValidateCredentials(ctx, account)
}
func (p *lazyConfiguredGSTProvider) GenerateEInvoice(ctx context.Context, req GSTEInvoiceRequest) (*GSTEInvoiceResult, error) {
	provider, err := p.provider(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GenerateEInvoice(ctx, req)
}
func (p *lazyConfiguredGSTProvider) CancelEInvoice(ctx context.Context, req GSTCancelEInvoiceRequest) (*GSTCancelEInvoiceResult, error) {
	provider, err := p.provider(ctx)
	if err != nil {
		return nil, err
	}
	return provider.CancelEInvoice(ctx, req)
}
func (p *lazyConfiguredGSTProvider) GenerateEWayBill(ctx context.Context, req GSTEWayBillRequest) (*GSTEWayBillResult, error) {
	provider, err := p.provider(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GenerateEWayBill(ctx, req)
}
func (p *lazyConfiguredGSTProvider) UpdateEWayPartB(ctx context.Context, req GSTEWayPartBRequest) (*GSTEWayBillResult, error) {
	provider, err := p.provider(ctx)
	if err != nil {
		return nil, err
	}
	return provider.UpdateEWayPartB(ctx, req)
}
func (p *lazyConfiguredGSTProvider) InitiateMultiVehicle(ctx context.Context, req GSTMultiVehicleRequest) (*GSTMultiVehicleResult, error) {
	provider, err := p.provider(ctx)
	if err != nil {
		return nil, err
	}
	return provider.InitiateMultiVehicle(ctx, req)
}
func (p *lazyConfiguredGSTProvider) FetchEWayBillPDF(ctx context.Context, req GSTEWayBillPDFRequest) (*GSTEWayBillPDFResult, error) {
	provider, err := p.provider(ctx)
	if err != nil {
		return nil, err
	}
	return provider.FetchEWayBillPDF(ctx, req)
}
func (p *lazyConfiguredGSTProvider) FetchDistance(ctx context.Context, req GSTDistanceRequest) (*GSTDistanceResult, error) {
	provider, err := p.provider(ctx)
	if err != nil {
		return nil, err
	}
	return provider.FetchDistance(ctx, req)
}

type configuredGSTProvider struct {
	cfg        *config.Config
	httpClient *http.Client
	log        *logger.Logger
}

func (p *configuredGSTProvider) ValidateCredentials(ctx context.Context, account *GSTIntegrationAccountCredentials) error {
	if strings.TrimSpace(p.cfg.GST.ValidatePath) == "" {
		return nil
	}
	_, err := p.doJSON(ctx, http.MethodPost, p.cfg.GST.ValidatePath, account, account)
	return err
}

func (p *configuredGSTProvider) GenerateEInvoice(ctx context.Context, req GSTEInvoiceRequest) (*GSTEInvoiceResult, error) {
	payload, err := p.doJSON(ctx, http.MethodPost, p.cfg.GST.EInvoicePath, req.Payload, nil)
	if err != nil {
		return nil, err
	}
	return parseEInvoiceResult(payload), nil
}

func (p *configuredGSTProvider) CancelEInvoice(ctx context.Context, req GSTCancelEInvoiceRequest) (*GSTCancelEInvoiceResult, error) {
	payload, err := p.doJSON(ctx, http.MethodPost, p.cfg.GST.EInvoiceCancelPath, req.Payload, nil)
	if err != nil {
		return nil, err
	}
	return &GSTCancelEInvoiceResult{
		ProviderReferenceID: readStringCandidate(payload, "reference_id", "referenceId", "data.reference_id"),
		RawResponse:         payload,
	}, nil
}

func (p *configuredGSTProvider) GenerateEWayBill(ctx context.Context, req GSTEWayBillRequest) (*GSTEWayBillResult, error) {
	payload, err := p.doJSON(ctx, http.MethodPost, p.cfg.GST.EWayBillPath, req.Payload, nil)
	if err != nil {
		return nil, err
	}
	return parseEWayBillResult(payload), nil
}

func (p *configuredGSTProvider) UpdateEWayPartB(ctx context.Context, req GSTEWayPartBRequest) (*GSTEWayBillResult, error) {
	payload, err := p.doJSON(ctx, http.MethodPost, p.cfg.GST.EWayBillPartBPath, req.Payload, nil)
	if err != nil {
		return nil, err
	}
	return parseEWayBillResult(payload), nil
}

func (p *configuredGSTProvider) InitiateMultiVehicle(ctx context.Context, req GSTMultiVehicleRequest) (*GSTMultiVehicleResult, error) {
	payload, err := p.doJSON(ctx, http.MethodPost, p.cfg.GST.EWayBillMultiVehiclePath, req.Payload, nil)
	if err != nil {
		return nil, err
	}
	return &GSTMultiVehicleResult{
		ProviderReferenceID: readStringCandidate(payload, "reference_id", "referenceId", "data.reference_id"),
		RawResponse:         payload,
	}, nil
}

func (p *configuredGSTProvider) FetchEWayBillPDF(ctx context.Context, req GSTEWayBillPDFRequest) (*GSTEWayBillPDFResult, error) {
	payload, err := p.doJSON(ctx, http.MethodPost, p.cfg.GST.EWayBillPDFPath, map[string]interface{}{
		"eway_bill_number": req.EWayBillNo,
		"document_id":      req.DocumentID,
	}, nil)
	if err != nil {
		return nil, err
	}
	result := &GSTEWayBillPDFResult{
		PDFURL:      readStringCandidate(payload, "pdf_url", "pdfUrl", "data.pdf_url", "data.url"),
		RawResponse: payload,
	}
	if encoded := readStringCandidate(payload, "pdf_base64", "data.pdf_base64"); encoded != "" {
		if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			result.PDFContent = decoded
		}
	}
	return result, nil
}

func (p *configuredGSTProvider) FetchDistance(ctx context.Context, req GSTDistanceRequest) (*GSTDistanceResult, error) {
	payload, err := p.doJSON(ctx, http.MethodPost, p.cfg.GST.DistancePath, map[string]interface{}{
		"from_pincode": req.FromPincode,
		"to_pincode":   req.ToPincode,
	}, nil)
	if err != nil {
		return nil, err
	}
	return &GSTDistanceResult{
		DistanceKM:  int(intValue(payload["distance_km"], intValue(payload["distance"], 0))),
		RawResponse: payload,
	}, nil
}

func (p *configuredGSTProvider) doJSON(ctx context.Context, method, path string, body interface{}, credentials *GSTIntegrationAccountCredentials) (map[string]interface{}, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("gst provider path is not configured")
	}
	requestURL := joinURL(strings.TrimSpace(p.cfg.GST.BaseURL), strings.TrimSpace(path))
	rawBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(rawBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(p.cfg.GST.APIToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if p.cfg.GST.ClientID != "" {
		req.Header.Set("X-Client-Id", p.cfg.GST.ClientID)
	}
	if p.cfg.GST.ClientSecret != "" {
		req.Header.Set("X-Client-Secret", p.cfg.GST.ClientSecret)
	}
	if p.cfg.GST.GSPName != "" {
		req.Header.Set("X-GSP-Name", p.cfg.GST.GSPName)
	}
	if credentials != nil {
		if credentials.APIUsername != "" {
			req.Header.Set("X-API-Username", credentials.APIUsername)
		}
		if credentials.APIPassword != "" {
			req.Header.Set("X-API-Password", credentials.APIPassword)
		}
		if credentials.PortalUsername != "" {
			req.Header.Set("X-Portal-Username", credentials.PortalUsername)
		}
		if credentials.PortalPassword != "" {
			req.Header.Set("X-Portal-Password", credentials.PortalPassword)
		}
		if credentials.APIKey != "" {
			req.Header.Set("X-Account-API-Key", credentials.APIKey)
		}
		if credentials.APISecret != "" {
			req.Header.Set("X-Account-API-Secret", credentials.APISecret)
		}
		for key, value := range credentials.Metadata {
			req.Header.Set("X-Credential-"+key, value)
		}
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	limitedBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read GST provider response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, &providerHTTPError{status: resp.StatusCode}
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(limitedBody, &payload); err != nil {
		return nil, fmt.Errorf("decode GST provider response: %w", err)
	}
	return payload, nil
}

type simulatedGSTProvider struct {
	log *logger.Logger
}

func (p *simulatedGSTProvider) ValidateCredentials(ctx context.Context, account *GSTIntegrationAccountCredentials) error {
	return nil
}

func (p *simulatedGSTProvider) GenerateEInvoice(ctx context.Context, req GSTEInvoiceRequest) (*GSTEInvoiceResult, error) {
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte(req.BusinessID + "|" + req.DocumentID + "|" + req.SerialNo))
	irn := strings.ToUpper(hex.EncodeToString(hash[:16]))
	qrPayload := base64.StdEncoding.EncodeToString([]byte("IRN:" + irn))
	return &GSTEInvoiceResult{
		ProviderReferenceID: irn[0:12],
		IRN:                 irn,
		AckNumber:           strconv.FormatInt(now.UnixNano(), 10),
		AckDate:             &now,
		SignedQRCodePayload: qrPayload,
		RawResponse: map[string]interface{}{
			"provider": "simulated",
			"irn":      irn,
			"ack_date": now.Format(time.RFC3339),
		},
	}, nil
}

func (p *simulatedGSTProvider) CancelEInvoice(ctx context.Context, req GSTCancelEInvoiceRequest) (*GSTCancelEInvoiceResult, error) {
	return &GSTCancelEInvoiceResult{
		ProviderReferenceID: req.IRN,
		RawResponse: map[string]interface{}{
			"provider":  "simulated",
			"cancelled": true,
		},
	}, nil
}

func (p *simulatedGSTProvider) GenerateEWayBill(ctx context.Context, req GSTEWayBillRequest) (*GSTEWayBillResult, error) {
	now := time.Now().UTC()
	valid := now.Add(24 * time.Hour)
	hash := sha256.Sum256([]byte("eway|" + req.BusinessID + "|" + req.DocumentID + "|" + req.SerialNo))
	number := fmt.Sprintf("%012d", int(hash[0])<<24+int(hash[1])<<16+int(hash[2])<<8+int(hash[3]))
	return &GSTEWayBillResult{
		ProviderReferenceID: number,
		EWayBillNumber:      number,
		EWayBillDate:        &now,
		ValidUntil:          &valid,
		PDFURL:              "mock://ewaybill/" + number + ".pdf",
		RawResponse: map[string]interface{}{
			"provider":         "simulated",
			"eway_bill_number": number,
		},
	}, nil
}

func (p *simulatedGSTProvider) UpdateEWayPartB(ctx context.Context, req GSTEWayPartBRequest) (*GSTEWayBillResult, error) {
	now := time.Now().UTC()
	return &GSTEWayBillResult{
		ProviderReferenceID: req.EWayBillNo,
		EWayBillNumber:      req.EWayBillNo,
		EWayBillDate:        &now,
		ValidUntil:          timePointer(now.Add(24 * time.Hour)),
		RawResponse: map[string]interface{}{
			"provider": "simulated",
			"updated":  true,
		},
	}, nil
}

func (p *simulatedGSTProvider) InitiateMultiVehicle(ctx context.Context, req GSTMultiVehicleRequest) (*GSTMultiVehicleResult, error) {
	return &GSTMultiVehicleResult{
		ProviderReferenceID: req.EWayBillNo,
		RawResponse: map[string]interface{}{
			"provider": "simulated",
			"updated":  true,
		},
	}, nil
}

func (p *simulatedGSTProvider) FetchEWayBillPDF(ctx context.Context, req GSTEWayBillPDFRequest) (*GSTEWayBillPDFResult, error) {
	return &GSTEWayBillPDFResult{
		PDFURL:      "mock://ewaybill/" + req.EWayBillNo + ".pdf",
		RawResponse: map[string]interface{}{"provider": "simulated"},
	}, nil
}

func (p *simulatedGSTProvider) FetchDistance(ctx context.Context, req GSTDistanceRequest) (*GSTDistanceResult, error) {
	return &GSTDistanceResult{
		DistanceKM:  syntheticDistance(req.FromPincode, req.ToPincode),
		RawResponse: map[string]interface{}{"provider": "simulated"},
	}, nil
}

func parseEInvoiceResult(payload map[string]interface{}) *GSTEInvoiceResult {
	result := &GSTEInvoiceResult{
		ProviderReferenceID: readStringCandidate(payload, "reference_id", "referenceId", "data.reference_id", "data.referenceId"),
		IRN:                 readStringCandidate(payload, "irn", "Irn", "data.irn", "data.Irn"),
		AckNumber:           readStringCandidate(payload, "ack_number", "AckNo", "ack_no", "data.ack_number", "data.AckNo"),
		SignedQRCodePayload: readStringCandidate(payload, "signed_qr_code_payload", "qr_code", "QRCode", "SignedQRCode", "data.qr_code", "data.signed_qr_code_payload"),
		QRCodeURL:           readStringCandidate(payload, "qr_code_url", "data.qr_code_url"),
		RawResponse:         payload,
	}
	if ackAt := parseTimeCandidate(payload, "ack_date", "AckDt", "ack_dt", "data.ack_date", "data.AckDt"); ackAt != nil {
		result.AckDate = ackAt
	}
	return result
}

func parseEWayBillResult(payload map[string]interface{}) *GSTEWayBillResult {
	result := &GSTEWayBillResult{
		ProviderReferenceID: readStringCandidate(payload, "reference_id", "referenceId", "data.reference_id"),
		EWayBillNumber:      readStringCandidate(payload, "eway_bill_number", "ewayBillNo", "ewbNo", "data.eway_bill_number", "data.ewayBillNo"),
		PDFURL:              readStringCandidate(payload, "pdf_url", "data.pdf_url", "data.url"),
		RawResponse:         payload,
	}
	if issuedAt := parseTimeCandidate(payload, "eway_bill_date", "ewbDt", "data.eway_bill_date", "data.ewbDt"); issuedAt != nil {
		result.EWayBillDate = issuedAt
	}
	if validAt := parseTimeCandidate(payload, "valid_until", "validUpto", "data.valid_until", "data.validUpto"); validAt != nil {
		result.ValidUntil = validAt
	}
	return result
}

func parseTimeCandidate(payload map[string]interface{}, keys ...string) *time.Time {
	for _, key := range keys {
		value := readStringCandidate(payload, key)
		if value == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func syntheticDistance(fromPincode, toPincode string) int {
	from, _ := strconv.Atoi(strings.TrimSpace(fromPincode))
	to, _ := strconv.Atoi(strings.TrimSpace(toPincode))
	if from == 0 || to == 0 {
		return 100
	}
	diff := from - to
	if diff < 0 {
		diff = -diff
	}
	return (diff % 500) + 25
}
