package handlers

import (
	"fmt"
	"net/http"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type WellKnownHandler struct {
	config *config.Config
	log    *logger.Logger
}

func NewWellKnownHandler(cfg *config.Config, log *logger.Logger) *WellKnownHandler {
	return &WellKnownHandler{
		config: cfg,
		log:    log,
	}
}

func (h *WellKnownHandler) GetAgentCard(c *gin.Context) {
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
