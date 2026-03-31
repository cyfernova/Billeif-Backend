package unit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// WellKnownHandlerTestable is a testable version of the WellKnown handler
type WellKnownHandlerTestable struct {
	config *config.Config
	log    *logger.Logger
}

func NewWellKnownHandlerTestable(cfg *config.Config, log *logger.Logger) *WellKnownHandlerTestable {
	return &WellKnownHandlerTestable{
		config: cfg,
		log:    log,
	}
}

func (h *WellKnownHandlerTestable) GetAgentCard(c *gin.Context) {
	baseURL := h.config.Server.ResolveBaseURL()
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", h.config.Server.Port)
	}

	a2aBaseURL := baseURL + "/api/v1/a2a"
	card := a2a.NewAgentCard(
		"Invoice Backend Agent",
		"A2A-enabled agent for marketplace, payment, and invoice workflows.",
		"1.0.0",
	)

	card.Provider = &a2a.AgentProvider{
		Organization: "Invoice Backend",
		URL:          baseURL,
	}
	card.SupportedInterfaces = []a2a.AgentInterface{
		{
			URL:             a2aBaseURL,
			ProtocolBinding: a2a.ProtocolBindingHTTPJSON,
			ProtocolVersion: a2a.SupportedVersion,
		},
	}
	card.Capabilities = a2a.AgentCapabilities{
		Streaming:         true,
		PushNotifications: true,
	}
	card.DefaultInputModes = []string{"text/plain", "application/json"}
	card.DefaultOutputModes = []string{"text/plain", "application/json"}
	card.DocumentationURL = baseURL + "/swagger/index.html"
	card.Skills = []a2a.AgentSkill{
		{
			ID:          "shopping.search",
			Name:        "Shopping Search",
			Description: "Find products, evaluate carts, and coordinate shopping flows.",
			Tags:        []string{"shopping", "marketplace", "products"},
			Examples: []string{
				"Find the best invoice printer under $300",
				"Process a cart with merchant-side validation",
			},
			InputModes:  []string{"text/plain", "application/json"},
			OutputModes: []string{"text/plain", "application/json"},
		},
		{
			ID:          "shopping.procurement",
			Name:        "Shopping Procurement",
			Description: "Run buyer-side procurement across listings, seller discovery, and checkout handoff.",
			Tags:        []string{"shopping", "procurement", "marketplace"},
			Examples: []string{
				"Negotiate with registered sellers for 100 invoice printers under a fixed budget",
			},
			InputModes:  []string{"text/plain", "application/json"},
			OutputModes: []string{"text/plain", "application/json"},
		},
		{
			ID:          "procurement.quote.request",
			Name:        "Procurement Quote Request",
			Description: "Evaluate a buyer procurement request and reply with quote, counteroffer, accept, reject, or unavailable.",
			Tags:        []string{"merchant", "procurement", "quote"},
			InputModes:  []string{"text/plain", "application/json"},
			OutputModes: []string{"text/plain", "application/json"},
		},
		{
			ID:          "procurement.negotiation.counteroffer",
			Name:        "Procurement Counteroffer",
			Description: "Respond to buyer counteroffers during a procurement negotiation.",
			Tags:        []string{"merchant", "procurement", "negotiation"},
			InputModes:  []string{"text/plain", "application/json"},
			OutputModes: []string{"text/plain", "application/json"},
		},
		{
			ID:          "procurement.negotiation.accept",
			Name:        "Procurement Accept",
			Description: "Finalize a buyer acceptance and reserve inventory for checkout.",
			Tags:        []string{"merchant", "procurement", "negotiation"},
			InputModes:  []string{"text/plain", "application/json"},
			OutputModes: []string{"text/plain", "application/json"},
		},
		{
			ID:          "procurement.negotiation.reject",
			Name:        "Procurement Reject",
			Description: "Acknowledge the end of a procurement negotiation.",
			Tags:        []string{"merchant", "procurement", "negotiation"},
			InputModes:  []string{"text/plain", "application/json"},
			OutputModes: []string{"text/plain", "application/json"},
		},
		{
			ID:          "payment.process",
			Name:        "Payment Processing",
			Description: "Coordinate payment intents and payment-mandate processing.",
			Tags:        []string{"payments", "checkout", "mandates"},
			Examples: []string{
				"Process a payment mandate for an approved cart",
			},
			InputModes:  []string{"text/plain", "application/json"},
			OutputModes: []string{"text/plain", "application/json"},
		},
		{
			ID:          "invoice.workflow",
			Name:        "Invoice Workflow",
			Description: "Create, update, and coordinate invoice-focused workflows.",
			Tags:        []string{"invoice", "workflow", "automation"},
			Examples: []string{
				"Create an invoice workflow for a recurring customer",
			},
			InputModes:  []string{"text/plain", "application/json"},
			OutputModes: []string{"text/plain", "application/json"},
		},
	}

	oidcURL := fmt.Sprintf(
		"https://cognito-idp.%s.amazonaws.com/%s/.well-known/openid-configuration",
		h.config.Cognito.Region,
		h.config.Cognito.UserPoolID,
	)
	card.SecuritySchemes["bearerAuth"] = a2a.SecurityScheme{
		HTTPAuthSecurityScheme: &a2a.HTTPAuthSecurityScheme{
			Description:  "Cognito bearer token authentication",
			Scheme:       "Bearer",
			BearerFormat: "JWT",
		},
	}
	card.SecuritySchemes["oidc"] = a2a.SecurityScheme{
		OpenIDConnectSecurityScheme: &a2a.OpenIDConnectSecurityScheme{
			Description:      "Amazon Cognito OpenID Connect metadata",
			OpenIDConnectURL: oidcURL,
		},
	}
	card.Security = []map[string][]string{
		{"bearerAuth": {}},
		{"oidc": {}},
	}

	h.log.Info("serving latest A2A agent card", "url", c.Request.URL.String(), "a2a_url", a2aBaseURL)
	c.Header("Content-Type", a2a.ContentTypeA2AJSON)
	c.JSON(http.StatusOK, card)
}

func newWellKnownTestRouter(cfg *config.Config) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewWellKnownHandlerTestable(cfg, logger.New())
	router.GET("/.well-known/agent-card.json", handler.GetAgentCard)
	return router
}

func TestGetAgentCard_SuccessWithBaseURL(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	if ct := res.Header().Get("Content-Type"); ct != a2a.ContentTypeA2AJSON {
		t.Fatalf("expected Content-Type %s, got %s", a2a.ContentTypeA2AJSON, ct)
	}

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	if card.Name != "Invoice Backend Agent" {
		t.Fatalf("expected name 'Invoice Backend Agent', got %q", card.Name)
	}
	if card.Version != "1.0.0" {
		t.Fatalf("expected version '1.0.0', got %q", card.Version)
	}
	if len(card.ProtocolVersions) != 1 || card.ProtocolVersions[0] != a2a.SupportedVersion {
		t.Fatalf("unexpected protocol versions: %#v", card.ProtocolVersions)
	}
}

func TestGetAgentCard_SuccessWithoutBaseURLFallsBackToLocalhost(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{Port: 8080},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	if card.Provider == nil {
		t.Fatal("expected provider to be set")
	}
	if card.Provider.URL != "http://localhost:8080" {
		t.Fatalf("expected provider URL 'http://localhost:8080', got %q", card.Provider.URL)
	}
	if len(card.SupportedInterfaces) == 0 {
		t.Fatal("expected supported interfaces to be set")
	}
	expectedA2AURL := "http://localhost:8080/api/v1/a2a"
	if card.SupportedInterfaces[0].URL != expectedA2AURL {
		t.Fatalf("expected A2A URL %q, got %q", expectedA2AURL, card.SupportedInterfaces[0].URL)
	}
}

func TestGetAgentCard_A2AEndpointUsesBaseURL(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com/v1"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	expected := "https://api.example.com/v1/api/v1/a2a"
	if card.SupportedInterfaces[0].URL != expected {
		t.Fatalf("expected A2A URL %q, got %q", expected, card.SupportedInterfaces[0].URL)
	}
}

func TestGetAgentCard_SkillsPopulated(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	if len(card.Skills) == 0 {
		t.Fatal("expected skills to be populated")
	}
	skillIDs := make(map[string]bool)
	for _, s := range card.Skills {
		skillIDs[s.ID] = true
	}

	expectedSkills := []string{
		"shopping.search",
		"shopping.procurement",
		"procurement.quote.request",
		"procurement.negotiation.counteroffer",
		"procurement.negotiation.accept",
		"procurement.negotiation.reject",
		"payment.process",
		"invoice.workflow",
	}
	for _, id := range expectedSkills {
		if !skillIDs[id] {
			t.Errorf("expected skill %q to be present", id)
		}
	}
}

func TestGetAgentCard_Capabilities(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	if !card.Capabilities.Streaming {
		t.Error("expected streaming capability to be true")
	}
	if !card.Capabilities.PushNotifications {
		t.Error("expected push notifications capability to be true")
	}
}

func TestGetAgentCard_DefaultInputOutputModes(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	expected := []string{"text/plain", "application/json"}
	if len(card.DefaultInputModes) != len(expected) {
		t.Fatalf("expected input modes %v, got %v", expected, card.DefaultInputModes)
	}
	if len(card.DefaultOutputModes) != len(expected) {
		t.Fatalf("expected output modes %v, got %v", expected, card.DefaultOutputModes)
	}
}

func TestGetAgentCard_DocumentationURL(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	expected := "https://api.example.com/swagger/index.html"
	if card.DocumentationURL != expected {
		t.Fatalf("expected documentation URL %q, got %q", expected, card.DocumentationURL)
	}
}

func TestGetAgentCard_SecuritySchemes(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	if len(card.SecuritySchemes) == 0 {
		t.Fatal("expected security schemes to be set")
	}

	bearerScheme, ok := card.SecuritySchemes["bearerAuth"]
	if !ok {
		t.Fatal("expected bearerAuth security scheme")
	}
	if bearerScheme.HTTPAuthSecurityScheme == nil {
		t.Fatal("expected HTTP auth security scheme for bearerAuth")
	}
	if bearerScheme.HTTPAuthSecurityScheme.Scheme != "Bearer" {
		t.Errorf("expected bearer scheme 'Bearer', got %q", bearerScheme.HTTPAuthSecurityScheme.Scheme)
	}

	oidcScheme, ok := card.SecuritySchemes["oidc"]
	if !ok {
		t.Fatal("expected oidc security scheme")
	}
	if oidcScheme.OpenIDConnectSecurityScheme == nil {
		t.Fatal("expected OpenID Connect security scheme for oidc")
	}
	expectedOIDCURL := "https://cognito-idp.us-east-1.amazonaws.com/pool-123/.well-known/openid-configuration"
	if oidcScheme.OpenIDConnectSecurityScheme.OpenIDConnectURL != expectedOIDCURL {
		t.Errorf("expected OIDC URL %q, got %q", expectedOIDCURL, oidcScheme.OpenIDConnectSecurityScheme.OpenIDConnectURL)
	}
}

func TestGetAgentCard_Security(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	if len(card.Security) != 2 {
		t.Fatalf("expected 2 security entries, got %d", len(card.Security))
	}

	foundBearer := false
	foundOIDC := false
	for _, sec := range card.Security {
		if _, ok := sec["bearerAuth"]; ok {
			foundBearer = true
		}
		if _, ok := sec["oidc"]; ok {
			foundOIDC = true
		}
	}
	if !foundBearer {
		t.Error("expected bearerAuth in security list")
	}
	if !foundOIDC {
		t.Error("expected oidc in security list")
	}
}

func TestGetAgentCard_Provider(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	if card.Provider == nil {
		t.Fatal("expected provider to be set")
	}
	if card.Provider.Organization != "Invoice Backend" {
		t.Errorf("expected organization 'Invoice Backend', got %q", card.Provider.Organization)
	}
	if card.Provider.URL != "https://api.example.com" {
		t.Errorf("expected provider URL 'https://api.example.com', got %q", card.Provider.URL)
	}
}

func TestGetAgentCard_SupportedInterfaceProtocolBinding(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	if len(card.SupportedInterfaces) == 0 {
		t.Fatal("expected supported interfaces to be set")
	}
	iface := card.SupportedInterfaces[0]
	if iface.ProtocolBinding != a2a.ProtocolBindingHTTPJSON {
		t.Errorf("expected protocol binding %q, got %q", a2a.ProtocolBindingHTTPJSON, iface.ProtocolBinding)
	}
	if iface.ProtocolVersion != a2a.SupportedVersion {
		t.Errorf("expected protocol version %q, got %q", a2a.SupportedVersion, iface.ProtocolVersion)
	}
}

func TestGetAgentCard_SkillExamples(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	shoppingSkill := findSkillByID(t, card.Skills, "shopping.search")
	if shoppingSkill == nil {
		t.Fatal("shopping.search skill not found")
	}
	if len(shoppingSkill.Examples) == 0 {
		t.Error("expected shopping.search to have examples")
	}
	if len(shoppingSkill.Tags) == 0 {
		t.Error("expected shopping.search to have tags")
	}
}

func TestGetAgentCard_SkillInputOutputModes(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	skill := findSkillByID(t, card.Skills, "shopping.search")
	if skill == nil {
		t.Fatal("shopping.search skill not found")
	}

	expectedModes := []string{"text/plain", "application/json"}
	if len(skill.InputModes) != len(expectedModes) {
		t.Errorf("expected input modes %v, got %v", expectedModes, skill.InputModes)
	}
	if len(skill.OutputModes) != len(expectedModes) {
		t.Errorf("expected output modes %v, got %v", expectedModes, skill.OutputModes)
	}
}

func TestGetAgentCard_CognitoPoolIDConstructsCorrectOIDCURL(t *testing.T) {
	tests := []struct {
		name     string
		region   string
		poolID   string
		expected string
	}{
		{
			name:     "standard pool ID",
			region:   "us-east-1",
			poolID:   "pool-123",
			expected: "https://cognito-idp.us-east-1.amazonaws.com/pool-123/.well-known/openid-configuration",
		},
		{
			name:     "complex pool ID",
			region:   "ap-south-1",
			poolID:   "ap-south-1_abc123XYZ",
			expected: "https://cognito-idp.ap-south-1.amazonaws.com/ap-south-1_abc123XYZ/.well-known/openid-configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
				Cognito: config.CognitoConfig{Region: tt.region, UserPoolID: tt.poolID},
			}
			router := newWellKnownTestRouter(cfg)

			req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			var card a2a.AgentCard
			if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
				t.Fatalf("decode agent card: %v", err)
			}

			oidcScheme := card.SecuritySchemes["oidc"]
			if oidcScheme.OpenIDConnectSecurityScheme.OpenIDConnectURL != tt.expected {
				t.Errorf("expected OIDC URL %q, got %q", tt.expected, oidcScheme.OpenIDConnectSecurityScheme.OpenIDConnectURL)
			}
		})
	}
}

func TestGetAgentCard_NoAuthMiddlewareRequired(t *testing.T) {
	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	router := newWellKnownTestRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 without auth, got %d: %s", res.Code, res.Body.String())
	}
}

func findSkillByID(t *testing.T, skills []a2a.AgentSkill, id string) *a2a.AgentSkill {
	t.Helper()
	for i := range skills {
		if skills[i].ID == id {
			return &skills[i]
		}
	}
	return nil
}
