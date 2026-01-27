package handlers

import (
	"fmt"
	"net/http"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// WellKnownHandler handles well-known endpoints including agent.json
type WellKnownHandler struct {
	config *config.Config
	log    *logger.Logger
}

// NewWellKnownHandler creates a new well-known handler
func NewWellKnownHandler(cfg *config.Config, log *logger.Logger) *WellKnownHandler {
	return &WellKnownHandler{
		config: cfg,
		log:    log,
	}
}

// GetAgentCard returns the Agent Card per A2A v0.3 specification
// GET /.well-known/agent.json
func (h *WellKnownHandler) GetAgentCard(c *gin.Context) {
	baseURL := h.config.Server.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", h.config.Server.Port)
	}

	// Build the agent card
	agentCard := a2a.NewAgentCardV03(
		"Invoice Backend Agent",
		"A multi-purpose agent for invoice management, marketplace operations, and payment processing. Supports shopping, merchant, and payment workflows via the A2A protocol.",
		baseURL,
	)

	// Set provider information
	agentCard.WithProvider(
		"Invoice Backend",
		baseURL,
		baseURL+"/assets/logo.png",
	)

	// Set version
	agentCard.Version = "1.0.0"

	// Set capabilities
	agentCard.WithCapabilities(a2a.AgentCapabilitiesV03{
		Streaming:              true,
		PushNotifications:      true,
		ExtendedAgentCard:      true,
		StateTransitionHistory: true,
		MultiTurn:              true,
		FileUpload:             false,
		FileDownload:           true,
		SupportedInputTypes:    []string{"application/json", "text/plain"},
		SupportedOutputTypes:   []string{"application/json", "text/plain"},
		RateLimits: &a2a.RateLimits{
			RequestsPerMinute: 100,
			RequestsPerHour:   1000,
			MaxConcurrent:     10,
		},
	})

	// Add skills
	agentCard.AddSkill(a2a.AgentSkill{
		ID:          "search_products",
		Name:        "Search Products",
		Description: "Search for products in the marketplace by keyword, category, or filters",
		Endpoint:    baseURL + "/a2a/v0.3/tasks:send",
		InputSchema: &a2a.JSONSchema{
			Type: "object",
			Properties: map[string]*a2a.JSONSchema{
				"query": {
					Type:        "string",
					Description: "Search query string",
				},
				"category": {
					Type:        "string",
					Description: "Product category filter",
				},
				"page": {
					Type:        "integer",
					Description: "Page number for pagination",
				},
				"limit": {
					Type:        "integer",
					Description: "Number of results per page",
				},
			},
			Required: []string{"query"},
		},
		OutputSchema: &a2a.JSONSchema{
			Type: "object",
			Properties: map[string]*a2a.JSONSchema{
				"products": {
					Type:        "array",
					Description: "Array of matching products",
				},
				"total": {
					Type:        "integer",
					Description: "Total number of matching products",
				},
			},
		},
		Tags: []string{"marketplace", "search", "products"},
	})

	agentCard.AddSkill(a2a.AgentSkill{
		ID:          "create_cart",
		Name:        "Create Shopping Cart",
		Description: "Create a new shopping cart with products",
		Endpoint:    baseURL + "/a2a/v0.3/tasks:send",
		InputSchema: &a2a.JSONSchema{
			Type: "object",
			Properties: map[string]*a2a.JSONSchema{
				"user_id": {
					Type:        "string",
					Description: "User ID for the cart",
				},
				"items": {
					Type:        "array",
					Description: "Array of cart items with product_id and quantity",
				},
			},
			Required: []string{"user_id"},
		},
		Tags: []string{"shopping", "cart"},
	})

	agentCard.AddSkill(a2a.AgentSkill{
		ID:          "process_payment",
		Name:        "Process Payment",
		Description: "Process a payment for a cart or order",
		Endpoint:    baseURL + "/a2a/v0.3/tasks:send",
		InputSchema: &a2a.JSONSchema{
			Type: "object",
			Properties: map[string]*a2a.JSONSchema{
				"cart_id": {
					Type:        "string",
					Description: "Cart ID to process payment for",
				},
				"payment_method": {
					Type:        "string",
					Description: "Payment method (card, upi, netbanking)",
				},
				"credential_id": {
					Type:        "string",
					Description: "Stored payment credential ID",
				},
			},
			Required: []string{"cart_id", "payment_method"},
		},
		Tags: []string{"payment", "checkout"},
	})

	agentCard.AddSkill(a2a.AgentSkill{
		ID:          "workflow_automation",
		Name:        "Workflow Automation",
		Description: "Create and manage automated buy/sell workflows with time or price triggers",
		Endpoint:    baseURL + "/a2a/v0.3/tasks:send",
		InputSchema: &a2a.JSONSchema{
			Type: "object",
			Properties: map[string]*a2a.JSONSchema{
				"action": {
					Type:        "string",
					Description: "Workflow action (create, pause, resume, delete)",
				},
				"workflow": {
					Type:        "object",
					Description: "Workflow configuration with trigger and action",
				},
			},
			Required: []string{"action"},
		},
		Tags: []string{"automation", "workflow"},
	})

	// Add security schemes
	agentCard.WithAPIKeySecurity(
		"api_key",
		"X-API-Key",
		"API Key authentication via X-API-Key header",
	)

	agentCard.AddSecurityScheme(a2a.SecurityScheme{
		ID:           "bearer_auth",
		Type:         a2a.SecurityTypeBearer,
		Description:  "Bearer token authentication (JWT)",
		BearerFormat: "JWT",
	})

	// Set default security
	agentCard.DefaultSecurity = []string{"api_key", "bearer_auth"}

	// Add HTTP interface
	agentCard.WithHTTPInterface(baseURL + "/a2a/v0.3")

	// Add metadata
	agentCard.DocumentationURL = baseURL + "/docs"
	agentCard.ContactEmail = "support@invoice-backend.com"
	agentCard.Tags = []string{"invoice", "marketplace", "payments", "automation"}

	h.log.Info("serving agent card", "url", c.Request.URL.String())

	c.JSON(http.StatusOK, agentCard)
}
