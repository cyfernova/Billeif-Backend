package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
)

const (
	maxProbeResponseBytes  = 1 << 20
	maxJourneyRequestBytes = 64 << 10
)

// HTTPJourneyProbe runs one explicitly configured synthetic API journey. It
// never derives endpoints from production configuration, never records a
// response body, and marks created resources preserved when no cleanup endpoint
// exists (for example a provider test order that cannot be deleted).
type HTTPJourneyProbe struct {
	ID             string
	Description    string
	Endpoint       string
	Method         string
	BearerToken    string
	Body           string
	IdempotencyKey string
	ResourceKind   string
	ReferenceField string
	CleanupURL     string
	CleanupMethod  string
	HTTPClient     *http.Client
}

func (p *HTTPJourneyProbe) Metadata() Metadata {
	return Metadata{ID: p.ID, Provider: "billeif-api", Description: p.Description, Impact: ImpactExternalWrite}
}

func (p *HTTPJourneyProbe) Probe(ctx context.Context) Outcome {
	endpoint := strings.TrimSpace(p.Endpoint)
	if endpoint == "" {
		return NotConfigured("journey endpoint")
	}
	if len(p.Body) > maxJourneyRequestBytes {
		return Blocked("journey_request_too_large")
	}
	if err := validateProbeURL(endpoint); err != nil {
		return Blocked("unsafe_journey_endpoint")
	}
	if err := p.validateCleanupTemplate(); err != nil {
		return Blocked("unsafe_cleanup_endpoint")
	}
	resourceKind := strings.TrimSpace(p.ResourceKind)
	if resourceKind == "" {
		resourceKind = "synthetic-journey"
	}
	pendingRef := strings.TrimSpace(p.IdempotencyKey)
	if pendingRef == "" {
		pendingRef = "unknown-outcome"
	}
	pendingResources := []Resource{{Kind: resourceKind, Reference: pendingRef}}
	method := strings.ToUpper(strings.TrimSpace(p.Method))
	if method == "" {
		method = http.MethodPost
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(p.Body))
	if err != nil {
		return Failed(err, "journey_request_invalid", false)
	}
	request.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(p.BearerToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if key := strings.TrimSpace(p.IdempotencyKey); key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := probeHTTPClient(p.HTTPClient).Do(request)
	if err != nil {
		return Outcome{Status: StatusFailed, Err: err, ErrCode: "journey_unknown_outcome", Resources: pendingResources}
	}
	defer response.Body.Close()
	raw, readErr := readBounded(response.Body)
	if readErr != nil {
		return Outcome{Status: StatusFailed, Err: readErr, ErrCode: "journey_response_invalid", Resources: pendingResources}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Outcome{Status: StatusFailed, Err: fmt.Errorf("synthetic journey returned status %d", response.StatusCode), ErrCode: "journey_unknown_outcome", Resources: pendingResources}
	}
	reference, err := extractStringField(raw, p.ReferenceField)
	if err != nil {
		return Outcome{Status: StatusFailed, Err: err, ErrCode: "journey_reference_missing", Resources: pendingResources}
	}
	cleanup, err := p.cleanup(reference)
	if err != nil {
		return Outcome{Status: StatusFailed, Err: err, ErrCode: "journey_cleanup_binding_invalid", Resources: []Resource{{Kind: resourceKind, Reference: reference}}}
	}
	outcome := Outcome{
		Status:    StatusPassed,
		Evidence:  []Evidence{{Key: "status_code", Value: response.StatusCode}, {Key: "synthetic", Value: true}, {Key: "resource_reference_received", Value: true}},
		Resources: []Resource{{Kind: resourceKind, Reference: reference}},
		Cleanup:   cleanup,
	}
	return outcome
}

func (p *HTTPJourneyProbe) validateCleanupTemplate() error {
	cleanupURL := strings.TrimSpace(p.CleanupURL)
	if cleanupURL == "" {
		return nil
	}
	if !strings.Contains(cleanupURL, "{reference}") {
		return errors.New("cleanup URL must contain {reference}")
	}
	return validateProbeURL(strings.ReplaceAll(cleanupURL, "{reference}", "synthetic-reference"))
}

func (p *HTTPJourneyProbe) cleanup(reference string) (CleanupFunc, error) {
	cleanupURL := strings.TrimSpace(p.CleanupURL)
	if cleanupURL == "" {
		return nil, nil
	}
	cleanupURL = strings.ReplaceAll(cleanupURL, "{reference}", url.PathEscape(reference))
	if err := validateProbeURL(cleanupURL); err != nil {
		return nil, err
	}
	return func(cleanupCtx context.Context) ([]Evidence, error) {
		cleanupMethod := strings.ToUpper(strings.TrimSpace(p.CleanupMethod))
		if cleanupMethod == "" {
			cleanupMethod = http.MethodDelete
		}
		cleanupRequest, err := http.NewRequestWithContext(cleanupCtx, cleanupMethod, cleanupURL, nil)
		if err != nil {
			return nil, err
		}
		if token := strings.TrimSpace(p.BearerToken); token != "" {
			cleanupRequest.Header.Set("Authorization", "Bearer "+token)
		}
		if key := strings.TrimSpace(p.IdempotencyKey); key != "" {
			cleanupRequest.Header.Set("Idempotency-Key", key+":cleanup")
		}
		cleanupResponse, err := probeHTTPClient(p.HTTPClient).Do(cleanupRequest)
		if err != nil {
			return nil, err
		}
		defer cleanupResponse.Body.Close()
		drainBounded(cleanupResponse.Body)
		if cleanupResponse.StatusCode < 200 || cleanupResponse.StatusCode >= 300 {
			return nil, fmt.Errorf("synthetic cleanup returned status %d", cleanupResponse.StatusCode)
		}
		return []Evidence{{Key: "cleanup_status_code", Value: cleanupResponse.StatusCode}}, nil
	}, nil
}

func extractStringField(raw []byte, field string) (string, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		field = "id"
	}
	var current any
	if err := json.Unmarshal(raw, &current); err != nil {
		return "", err
	}
	for _, part := range strings.Split(field, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return "", errors.New("journey reference field is not an object path")
		}
		current, ok = object[part]
		if !ok {
			return "", errors.New("journey reference field is missing")
		}
	}
	reference, ok := current.(string)
	if !ok || strings.TrimSpace(reference) == "" || len(reference) > 512 {
		return "", errors.New("journey reference field is invalid")
	}
	return strings.TrimSpace(reference), nil
}

type RazorpayTestModeProbe struct {
	KeyID      string
	KeySecret  string
	Mode       string
	BaseURL    string
	HTTPClient *http.Client
}

func (p *RazorpayTestModeProbe) Metadata() Metadata {
	return Metadata{ID: "razorpay.test", Provider: "razorpay", Description: "Razorpay test-mode plans endpoint is reachable", Impact: ImpactReadOnly}
}

func (p *RazorpayTestModeProbe) Probe(ctx context.Context) Outcome {
	keyID, secret := strings.TrimSpace(p.KeyID), strings.TrimSpace(p.KeySecret)
	if keyID == "" || secret == "" {
		return NotConfigured("RAZORPAY_KEY_ID", "RAZORPAY_KEY_SECRET")
	}
	if !strings.EqualFold(strings.TrimSpace(p.Mode), "test") || !strings.HasPrefix(keyID, "rzp_test_") {
		return Blocked(ReasonTestModeRequired, EvidenceItem("configured_mode", safeMode(p.Mode)))
	}
	baseURL := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.razorpay.com/v1"
	}
	if err := validateProbeURL(baseURL); err != nil {
		return Blocked("unsafe_provider_endpoint")
	}
	if err := validateProviderHost(baseURL, "api.razorpay.com"); err != nil {
		return Blocked("unexpected_provider_host")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/plans", nil)
	if err != nil {
		return Failed(err, "razorpay_request_invalid", false)
	}
	request.SetBasicAuth(keyID, secret)
	response, err := probeHTTPClient(p.HTTPClient).Do(request)
	if err != nil {
		return Failed(err, "razorpay_test_unreachable", true)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		drainBounded(response.Body)
		return Failed(fmt.Errorf("Razorpay plans returned status %d", response.StatusCode), "razorpay_test_unavailable", response.StatusCode >= 500)
	}
	var result struct {
		Count int `json:"count"`
	}
	if err := decodeBoundedJSON(response.Body, &result); err != nil {
		return Failed(err, "razorpay_response_invalid", false)
	}
	return Outcome{Status: StatusPassed, Evidence: []Evidence{{Key: "mode", Value: "test"}, {Key: "key_prefix", Value: "rzp_test_"}, {Key: "plans_visible", Value: result.Count}}}
}

type GSTSandboxProbe struct {
	BaseURL      string
	ValidatePath string
	Sandbox      bool
	APIToken     string
	ClientID     string
	ClientSecret string
	Username     string
	Password     string
	GSPName      string
	HTTPClient   *http.Client
}

func (p *GSTSandboxProbe) Metadata() Metadata {
	return Metadata{ID: "gst.sandbox", Provider: "gst", Description: "GST sandbox credential validation", Impact: ImpactExternalWrite}
}

func (p *GSTSandboxProbe) Probe(ctx context.Context) Outcome {
	if strings.TrimSpace(p.BaseURL) == "" {
		return NotConfigured("GST_BASE_URL")
	}
	if !p.Sandbox {
		return Blocked(ReasonSandboxRequired)
	}
	if err := validateProbeURL(p.BaseURL); err != nil {
		return Blocked("unsafe_provider_endpoint")
	}
	if strings.TrimSpace(p.APIToken) == "" && (strings.TrimSpace(p.Username) == "" || strings.TrimSpace(p.Password) == "") {
		return NotConfigured("GST_SANDBOX_CREDENTIALS")
	}
	body, err := json.Marshal(map[string]string{"gsp_name": strings.TrimSpace(p.GSPName), "username": strings.TrimSpace(p.Username), "password": strings.TrimSpace(p.Password)})
	if err != nil {
		return Failed(err, "gst_request_invalid", false)
	}
	path := strings.TrimSpace(p.ValidatePath)
	if path == "" {
		path = "/validate"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/"+strings.TrimLeft(path, "/"), bytes.NewReader(body))
	if err != nil {
		return Failed(err, "gst_request_invalid", false)
	}
	request.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(p.APIToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if value := strings.TrimSpace(p.ClientID); value != "" {
		request.Header.Set("X-Client-Id", value)
	}
	if value := strings.TrimSpace(p.ClientSecret); value != "" {
		request.Header.Set("X-Client-Secret", value)
	}
	response, err := probeHTTPClient(p.HTTPClient).Do(request)
	if err != nil {
		return Failed(err, "gst_sandbox_unreachable", true)
	}
	defer response.Body.Close()
	drainBounded(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Failed(fmt.Errorf("GST sandbox returned status %d", response.StatusCode), "gst_sandbox_validation_failed", response.StatusCode >= 500)
	}
	return Outcome{Status: StatusPassed, Evidence: []Evidence{{Key: "sandbox", Value: true}, {Key: "credentials_validated", Value: true}}}
}

type LLMGatewayProbe struct {
	Family     string
	Model      string
	APIURL     string
	APIKey     string
	HTTPClient *http.Client
}

func (p *LLMGatewayProbe) Metadata() Metadata {
	family := strings.ToLower(strings.TrimSpace(p.Family))
	return Metadata{ID: "llm." + family, Provider: family, Description: "Configured LLM gateway model family is discoverable", Impact: ImpactReadOnly}
}

func (p *LLMGatewayProbe) Probe(ctx context.Context) Outcome {
	family := strings.ToLower(strings.TrimSpace(p.Family))
	if strings.TrimSpace(p.APIURL) == "" || strings.TrimSpace(p.APIKey) == "" || strings.TrimSpace(p.Model) == "" || family == "" {
		return NotConfigured("LLM_API_URL", "LLM_API_KEY", "LLM_MODEL", "LLM_FAMILY")
	}
	if !modelMatchesFamily(p.Model, family) {
		if family == "gemini" {
			return NotConfigured("gateway model family " + family)
		}
		return Failed(errors.New("configured model does not match required family"), ReasonFamilyNotOffered, false)
	}
	modelsURL, err := llmModelsURL(p.APIURL)
	if err != nil {
		return Blocked(ReasonEndpointUnsupported)
	}
	if err := validateProbeURL(modelsURL); err != nil {
		return Blocked("unsafe_provider_endpoint")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return Failed(err, "llm_models_request_invalid", false)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.APIKey))
	response, err := probeHTTPClient(p.HTTPClient).Do(request)
	if err != nil {
		return Failed(err, "llm_gateway_unreachable", true)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		drainBounded(response.Body)
		return Failed(fmt.Errorf("LLM models endpoint returned status %d", response.StatusCode), "llm_models_unavailable", response.StatusCode >= 500)
	}
	var listing struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := decodeBoundedJSON(response.Body, &listing); err != nil || listing.Object != "list" {
		if err == nil {
			err = errors.New("unrecognized models response")
		}
		return Failed(err, "llm_models_response_invalid", false)
	}
	offered := false
	for _, model := range listing.Data {
		if modelMatchesFamily(model.ID, family) {
			offered = true
			break
		}
	}
	if !offered {
		if family == "gemini" {
			return NotConfigured("gateway model family " + family)
		}
		return Failed(errors.New("configured model family is unavailable"), ReasonFamilyNotOffered, false)
	}
	return Outcome{Status: StatusPassed, Evidence: []Evidence{{Key: "family", Value: family}, {Key: "model_family_offered", Value: true}}}
}

type WhatsAppTestEnvProbe struct {
	BaseURL        string
	PhoneNumberID  string
	AccessTokenEnv string
	PhoneNumberEnv string
	EnvLookup      func(string) string
	HTTPClient     *http.Client
}

func (p *WhatsAppTestEnvProbe) Metadata() Metadata {
	return Metadata{ID: "whatsapp.test", Provider: "whatsapp", Description: "WhatsApp test phone metadata is readable", Impact: ImpactReadOnly}
}

func (p *WhatsAppTestEnvProbe) Probe(ctx context.Context) Outcome {
	if strings.TrimSpace(p.BaseURL) == "" || strings.TrimSpace(p.PhoneNumberID) == "" {
		return NotConfigured("WHATSAPP_BASE_URL", "WHATSAPP_PHONE_NUMBER_ID")
	}
	lookup := p.EnvLookup
	if lookup == nil {
		lookup = os.Getenv
	}
	tokenEnv, phoneEnv := strings.TrimSpace(p.AccessTokenEnv), strings.TrimSpace(p.PhoneNumberEnv)
	if tokenEnv == "" || phoneEnv == "" {
		return NotConfigured("WHATSAPP_TEST_TOKEN_ENV", "WHATSAPP_TEST_PHONE_ENV")
	}
	token, phone := strings.TrimSpace(lookup(tokenEnv)), strings.TrimSpace(lookup(phoneEnv))
	if token == "" || phone == "" {
		return NotConfigured(tokenEnv, phoneEnv)
	}
	if phone != strings.TrimSpace(p.PhoneNumberID) {
		return Blocked("test_phone_mismatch")
	}
	if err := validateProbeURL(p.BaseURL); err != nil {
		return Blocked("unsafe_provider_endpoint")
	}
	if err := validateProviderHost(p.BaseURL, "graph.facebook.com"); err != nil {
		return Blocked("unexpected_provider_host")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.BaseURL, "/")+"/"+url.PathEscape(phone), nil)
	if err != nil {
		return Failed(err, "whatsapp_request_invalid", false)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := probeHTTPClient(p.HTTPClient).Do(request)
	if err != nil {
		return Failed(err, "whatsapp_test_unreachable", true)
	}
	defer response.Body.Close()
	drainBounded(response.Body)
	if response.StatusCode != http.StatusOK {
		return Failed(fmt.Errorf("WhatsApp phone metadata returned status %d", response.StatusCode), "whatsapp_test_unavailable", response.StatusCode >= 500)
	}
	return Outcome{Status: StatusPassed, Evidence: []Evidence{{Key: "test_phone_confirmed", Value: true}}}
}

type SarvamAPIProbe struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func (p *SarvamAPIProbe) Metadata() Metadata {
	return Metadata{ID: "sarvam.controlled", Provider: "sarvam", Description: "Controlled Sarvam streaming request", Impact: ImpactExternalWrite}
}

func (p *SarvamAPIProbe) Probe(ctx context.Context) Outcome {
	if strings.TrimSpace(p.APIKey) == "" || strings.TrimSpace(p.BaseURL) == "" {
		return NotConfigured("SARVAM_API_KEY", "SARVAM_BASE_URL")
	}
	if err := validateProbeURL(p.BaseURL); err != nil {
		return Blocked("unsafe_provider_endpoint")
	}
	if err := validateProviderHost(p.BaseURL, "api.sarvam.ai"); err != nil {
		return Blocked("unexpected_provider_host")
	}
	body := []byte(`{"messages":[{"role":"user","content":"Reply only: ok"}],"stream":true,"max_tokens":4}`)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/"), bytes.NewReader(body))
	if err != nil {
		return Failed(err, "sarvam_request_invalid", false)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("api-subscription-key", strings.TrimSpace(p.APIKey))
	response, err := probeHTTPClient(p.HTTPClient).Do(request)
	if err != nil {
		return Failed(err, "sarvam_unreachable", true)
	}
	defer response.Body.Close()
	raw, readErr := readBounded(response.Body)
	if readErr != nil {
		return Failed(readErr, "sarvam_response_invalid", false)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Failed(fmt.Errorf("Sarvam returned status %d", response.StatusCode), "sarvam_unavailable", response.StatusCode >= 500)
	}
	if !bytes.Contains(raw, []byte(`"finish_reason":"stop"`)) {
		return Failed(errors.New("Sarvam stream did not complete"), "sarvam_stream_incomplete", false)
	}
	return Outcome{Status: StatusPassed, Evidence: []Evidence{{Key: "controlled_response_complete", Value: true}}}
}

func probeHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	clone := *client
	// Verification probes carry credentials. Refuse redirects instead of
	// relying on header-copy behavior across origins.
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

func safeMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "test" || mode == "live" {
		return mode
	}
	return "unknown"
}

func llmModelsURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("invalid LLM API URL")
	}
	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		path = strings.TrimSuffix(path, "/chat/completions") + "/models"
	case strings.HasSuffix(path, "/models"):
	default:
		return "", errors.New("unsupported LLM API URL")
	}
	parsed.Path, parsed.RawQuery, parsed.Fragment = path, "", ""
	return parsed.String(), nil
}

func modelMatchesFamily(model, family string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	family = strings.ToLower(strings.TrimSpace(family))
	return strings.HasPrefix(model, family+"-") || model == family || strings.Contains(model, "/"+family+"-")
}

func decodeBoundedJSON(reader io.Reader, target any) error {
	raw, err := readBounded(reader)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateProbeURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return errors.New("invalid probe URL")
	}
	if parsed.Scheme == "https" {
		if address, parseErr := netip.ParseAddr(parsed.Hostname()); parseErr == nil &&
			(address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified()) && !address.IsLoopback() {
			return errors.New("private and link-local literal probe addresses are refused")
		}
		return nil
	}
	if parsed.Scheme != "http" {
		return errors.New("probe URL must use HTTPS")
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	address, err := netip.ParseAddr(host)
	if err == nil && address.IsLoopback() {
		return nil
	}
	return errors.New("plain HTTP is allowed only for loopback tests")
}

func validateProviderHost(raw string, allowed ...string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" {
		return nil
	}
	if address, parseErr := netip.ParseAddr(host); parseErr == nil && address.IsLoopback() {
		return nil
	}
	for _, candidate := range allowed {
		if host == strings.ToLower(candidate) {
			return nil
		}
	}
	return errors.New("unexpected provider host")
}

func readBounded(reader io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maxProbeResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxProbeResponseBytes {
		return nil, errors.New("probe response exceeded limit")
	}
	return raw, nil
}

func drainBounded(reader io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(reader, maxProbeResponseBytes))
}
