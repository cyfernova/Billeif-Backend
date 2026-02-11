package ap2

import (
	"encoding/json"
	"fmt"
	"time"
)

// AgentCard represents an AP2 Agent Card specification
type AgentCard struct {
	AgentID       string                 `json:"agent_id"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Endpoint      string                 `json:"endpoint"`
	Type          string                 `json:"type"` // shopping, merchant, credential_provider, payment_processor
	Capabilities  []string               `json:"capabilities"`
	PricingModel  map[string]interface{} `json:"pricing_model"`
	Currencies    []string               `json:"currencies"`
	Jurisdictions []string               `json:"jurisdictions"`
	Languages     []string               `json:"languages,omitempty"`
	PublicKey     *string                `json:"public_key,omitempty"`
	Version       string                 `json:"version"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// BuildAgentCard creates an AP2 Agent Card from agent information
func BuildAgentCard(
	agentID string,
	name string,
	description string,
	endpoint string,
	agentType string,
	capabilities []string,
	pricingModel map[string]interface{},
	currencies []string,
	jurisdictions []string,
	languages []string,
	publicKey *string,
) *AgentCard {
	return &AgentCard{
		Name:          name,
		Description:   description,
		Endpoint:      endpoint,
		Type:          agentType,
		Capabilities:  capabilities,
		PricingModel:  pricingModel,
		Currencies:    currencies,
		Jurisdictions: jurisdictions,
		Languages:     languages,
		Version:       "1.0", // AP2 spec version
		PublicKey:     publicKey,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}

// AgentCardResponse is the API response format for agent cards
type AgentCardResponse struct {
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Endpoint         string                 `json:"endpoint"`
	Type             string                 `json:"type"`
	Capabilities     []string               `json:"capabilities"`
	PricingModel     map[string]interface{} `json:"pricing_model"`
	Currencies       []string               `json:"currencies"`
	Jurisdictions    []string               `json:"jurisdictions"`
	Languages        []string               `json:"languages"`
	SupportedMethods []string               `json:"supported_methods"`
	Version          string                 `json:"version"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// MarshalAgentCard converts an agent card to JSON bytes
func MarshalAgentCard(card *AgentCard) ([]byte, error) {
	return json.MarshalIndent(card, "", "  ")
}

// UnmarshalAgentCard parses JSON bytes into an agent card
func UnmarshalAgentCard(data []byte) (*AgentCard, error) {
	var card AgentCard
	if err := json.Unmarshal(data, &card); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent card: %w", err)
	}
	return &card, nil
}

// ValidateAgentCard validates an agent card has required fields
func ValidateAgentCard(card *AgentCard) error {
	if card == nil {
		return fmt.Errorf("agent card is nil")
	}

	if card.Name == "" {
		return fmt.Errorf("agent card must have a name")
	}

	if card.Endpoint == "" {
		return fmt.Errorf("agent card must have an endpoint")
	}

	if card.Type == "" {
		return fmt.Errorf("agent card must have a type")
	}

	validTypes := map[string]bool{
		"shopping":            true,
		"merchant":            true,
		"credential_provider": true,
		"payment_processor":   true,
	}

	if !validTypes[card.Type] {
		return fmt.Errorf("invalid agent type: %s", card.Type)
	}

	if len(card.Capabilities) == 0 {
		return fmt.Errorf("agent card must have at least one capability")
	}

	if len(card.Currencies) == 0 {
		return fmt.Errorf("agent card must support at least one currency")
	}

	return nil
}

// GetCapabilitiesByType returns default capabilities for an agent type
func GetCapabilitiesByType(agentType string) []string {
	capabilitiesMap := map[string][]string{
		"shopping": {
			"search_products",
			"browse_catalog",
			"add_to_cart",
			"manage_cart",
			"checkout",
			"track_orders",
		},
		"merchant": {
			"list_products",
			"manage_products",
			"process_orders",
			"update_inventory",
			"handle_payments",
			"generate_invoices",
		},
		"credential_provider": {
			"store_credentials",
			"encrypt_credentials",
			"generate_tokens",
			"revoke_tokens",
			"verify_credentials",
		},
		"payment_processor": {
			"process_payments",
			"capture_payments",
			"refund_payments",
			"verify_payment_status",
			"handle_webhooks",
		},
	}

	if capabilities, ok := capabilitiesMap[agentType]; ok {
		return capabilities
	}

	return []string{}
}

// CreateWellKnownURI creates the well-known URI for agent discovery
func CreateWellKnownURI(domain string) string {
	return fmt.Sprintf("https://%s/.well-known/agent-card.json", domain)
}

// CreateA2AEndpoint creates the A2A endpoint URL
func CreateA2AEndpoint(domain string) string {
	return fmt.Sprintf("https://%s/api/v1/a2a", domain)
}

// AgentCardBuilder provides a fluent interface for building agent cards
type AgentCardBuilder struct {
	card *AgentCard
}

// NewAgentCardBuilder creates a new agent card builder
func NewAgentCardBuilder() *AgentCardBuilder {
	return &AgentCardBuilder{
		card: &AgentCard{
			Version:   "1.0",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
}

// WithName sets the agent name
func (b *AgentCardBuilder) WithName(name string) *AgentCardBuilder {
	b.card.Name = name
	return b
}

// WithDescription sets the description
func (b *AgentCardBuilder) WithDescription(description string) *AgentCardBuilder {
	b.card.Description = description
	return b
}

// WithEndpoint sets the endpoint
func (b *AgentCardBuilder) WithEndpoint(endpoint string) *AgentCardBuilder {
	b.card.Endpoint = endpoint
	return b
}

// WithType sets the agent type
func (b *AgentCardBuilder) WithType(agentType string) *AgentCardBuilder {
	b.card.Type = agentType
	return b
}

// WithCapabilities sets the capabilities
func (b *AgentCardBuilder) WithCapabilities(capabilities []string) *AgentCardBuilder {
	b.card.Capabilities = capabilities
	return b
}

// WithPricingModel sets the pricing model
func (b *AgentCardBuilder) WithPricingModel(pricingModel map[string]interface{}) *AgentCardBuilder {
	b.card.PricingModel = pricingModel
	return b
}

// WithCurrencies sets the supported currencies
func (b *AgentCardBuilder) WithCurrencies(currencies []string) *AgentCardBuilder {
	b.card.Currencies = currencies
	return b
}

// WithJurisdictions sets the supported jurisdictions
func (b *AgentCardBuilder) WithJurisdictions(jurisdictions []string) *AgentCardBuilder {
	b.card.Jurisdictions = jurisdictions
	return b
}

// WithLanguages sets the supported languages
func (b *AgentCardBuilder) WithLanguages(languages []string) *AgentCardBuilder {
	b.card.Languages = languages
	return b
}

// WithPublicKey sets the public key
func (b *AgentCardBuilder) WithPublicKey(publicKey string) *AgentCardBuilder {
	b.card.PublicKey = &publicKey
	return b
}

// Build creates the final agent card
func (b *AgentCardBuilder) Build() (*AgentCard, error) {
	if err := ValidateAgentCard(b.card); err != nil {
		return nil, err
	}
	return b.card, nil
}

// ToJSON converts the card to JSON
func (b *AgentCardBuilder) ToJSON() ([]byte, error) {
	if err := ValidateAgentCard(b.card); err != nil {
		return nil, err
	}
	return MarshalAgentCard(b.card)
}
