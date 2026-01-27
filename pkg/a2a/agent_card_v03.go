package a2a

import (
	"encoding/json"
	"time"
)

// A2A Protocol v0.3 Agent Card Types
// Based on Google's Agent2Agent Protocol specification

// AgentCardV03 represents an agent's metadata and capabilities per A2A v0.3 spec
type AgentCardV03 struct {
	// Required fields
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"` // Agent's base URL

	// Agent provider information
	Provider *AgentProvider `json:"provider,omitempty"`

	// Version information
	Version          string `json:"version,omitempty"` // Agent version
	ProtocolVersion  string `json:"protocolVersion"`   // A2A protocol version (e.g., "0.3")

	// Capabilities
	Capabilities AgentCapabilitiesV03 `json:"capabilities"`

	// Skills the agent can perform
	Skills []AgentSkill `json:"skills,omitempty"`

	// Security and authentication
	SecuritySchemes []SecurityScheme `json:"securitySchemes,omitempty"`
	DefaultSecurity []string         `json:"defaultSecurity,omitempty"` // References to security scheme IDs

	// Protocol interfaces/bindings
	Interfaces []AgentInterface `json:"interfaces,omitempty"`

	// Documentation and metadata
	DocumentationURL string                 `json:"documentationUrl,omitempty"`
	ContactEmail     string                 `json:"contactEmail,omitempty"`
	TermsOfService   string                 `json:"termsOfService,omitempty"`
	PrivacyPolicy    string                 `json:"privacyPolicy,omitempty"`
	Tags             []string               `json:"tags,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// AgentProvider represents the organization providing the agent
type AgentProvider struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	LogoURL string `json:"logoUrl,omitempty"`
}

// AgentCapabilitiesV03 represents what an agent can do per A2A v0.3
type AgentCapabilitiesV03 struct {
	// Core streaming capability
	Streaming bool `json:"streaming"`

	// Push notification support
	PushNotifications bool `json:"pushNotifications"`

	// Extended agent card with additional metadata
	ExtendedAgentCard bool `json:"extendedAgentCard,omitempty"`

	// State reporting during task execution
	StateTransitionHistory bool `json:"stateTransitionHistory,omitempty"`

	// Multi-turn conversations
	MultiTurn bool `json:"multiTurn,omitempty"`

	// File handling
	FileUpload   bool `json:"fileUpload,omitempty"`
	FileDownload bool `json:"fileDownload,omitempty"`

	// Supported input/output types
	SupportedInputTypes  []string `json:"supportedInputTypes,omitempty"`  // MIME types
	SupportedOutputTypes []string `json:"supportedOutputTypes,omitempty"` // MIME types

	// Rate limits
	RateLimits *RateLimits `json:"rateLimits,omitempty"`

	// Custom capabilities
	Custom map[string]interface{} `json:"custom,omitempty"`
}

// RateLimits defines rate limiting for the agent
type RateLimits struct {
	RequestsPerMinute int `json:"requestsPerMinute,omitempty"`
	RequestsPerHour   int `json:"requestsPerHour,omitempty"`
	RequestsPerDay    int `json:"requestsPerDay,omitempty"`
	MaxConcurrent     int `json:"maxConcurrent,omitempty"`
}

// AgentSkill represents a specific capability or function an agent can perform
type AgentSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`

	// Optional endpoint override for this skill
	Endpoint string `json:"endpoint,omitempty"`

	// Input/output schemas (JSON Schema format)
	InputSchema  *JSONSchema `json:"inputSchema,omitempty"`
	OutputSchema *JSONSchema `json:"outputSchema,omitempty"`

	// Examples for the skill
	Examples []SkillExample `json:"examples,omitempty"`

	// Skill-specific metadata
	Tags     []string               `json:"tags,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// JSONSchema represents a JSON Schema definition
type JSONSchema struct {
	Type        string                 `json:"type,omitempty"`
	Description string                 `json:"description,omitempty"`
	Properties  map[string]*JSONSchema `json:"properties,omitempty"`
	Required    []string               `json:"required,omitempty"`
	Items       *JSONSchema            `json:"items,omitempty"`
	Enum        []interface{}          `json:"enum,omitempty"`
	Default     interface{}            `json:"default,omitempty"`
	Format      string                 `json:"format,omitempty"`
	Minimum     *float64               `json:"minimum,omitempty"`
	Maximum     *float64               `json:"maximum,omitempty"`
	MinLength   *int                   `json:"minLength,omitempty"`
	MaxLength   *int                   `json:"maxLength,omitempty"`
	Pattern     string                 `json:"pattern,omitempty"`
	AdditionalProperties interface{}   `json:"additionalProperties,omitempty"`
}

// SkillExample provides example inputs/outputs for a skill
type SkillExample struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Input       json.RawMessage `json:"input"`
	Output      json.RawMessage `json:"output,omitempty"`
}

// SecurityScheme defines how to authenticate with the agent
type SecurityScheme struct {
	ID          string `json:"id"`
	Type        string `json:"type"` // "apiKey", "oauth2", "bearer", "basic", "openIdConnect"
	Description string `json:"description,omitempty"`

	// API Key specific
	In   string `json:"in,omitempty"`   // "header", "query", "cookie"
	Name string `json:"name,omitempty"` // Header/query param name

	// OAuth2 specific
	Flows *OAuthFlows `json:"flows,omitempty"`

	// OpenID Connect specific
	OpenIDConnectURL string `json:"openIdConnectUrl,omitempty"`

	// Bearer token specific
	BearerFormat string `json:"bearerFormat,omitempty"` // e.g., "JWT"
}

// OAuthFlows defines OAuth2 flow configurations
type OAuthFlows struct {
	Implicit          *OAuthFlow `json:"implicit,omitempty"`
	Password          *OAuthFlow `json:"password,omitempty"`
	ClientCredentials *OAuthFlow `json:"clientCredentials,omitempty"`
	AuthorizationCode *OAuthFlow `json:"authorizationCode,omitempty"`
}

// OAuthFlow defines a single OAuth2 flow
type OAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes,omitempty"`
}

// AgentInterface defines a protocol binding for the agent
type AgentInterface struct {
	Protocol    string `json:"protocol"`    // "http+json", "grpc", "websocket"
	URL         string `json:"url"`         // Base URL for this interface
	Description string `json:"description,omitempty"`

	// HTTP specific
	Methods []string `json:"methods,omitempty"` // Supported HTTP methods

	// WebSocket specific
	SubProtocols []string `json:"subProtocols,omitempty"`

	// gRPC specific
	ProtoFile string `json:"protoFile,omitempty"`
}

// Security scheme type constants
const (
	SecurityTypeAPIKey        = "apiKey"
	SecurityTypeOAuth2        = "oauth2"
	SecurityTypeBearer        = "bearer"
	SecurityTypeBasic         = "basic"
	SecurityTypeOpenIDConnect = "openIdConnect"
)

// API Key location constants
const (
	APIKeyInHeader = "header"
	APIKeyInQuery  = "query"
	APIKeyInCookie = "cookie"
)

// Protocol constants
const (
	ProtocolHTTPJSON  = "http+json"
	ProtocolGRPC      = "grpc"
	ProtocolWebSocket = "websocket"
)

// A2AProtocolVersion is the current protocol version
const A2AProtocolVersion = "0.3"

// NewAgentCardV03 creates a new agent card with required fields
func NewAgentCardV03(name, description, url string) *AgentCardV03 {
	return &AgentCardV03{
		Name:            name,
		Description:     description,
		URL:             url,
		ProtocolVersion: A2AProtocolVersion,
		Capabilities:    AgentCapabilitiesV03{},
		Skills:          []AgentSkill{},
		SecuritySchemes: []SecurityScheme{},
		Interfaces:      []AgentInterface{},
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

// WithProvider sets the agent provider
func (c *AgentCardV03) WithProvider(name, url, logoURL string) *AgentCardV03 {
	c.Provider = &AgentProvider{
		Name:    name,
		URL:     url,
		LogoURL: logoURL,
	}
	return c
}

// WithCapabilities sets agent capabilities
func (c *AgentCardV03) WithCapabilities(caps AgentCapabilitiesV03) *AgentCardV03 {
	c.Capabilities = caps
	c.UpdatedAt = time.Now()
	return c
}

// AddSkill adds a skill to the agent
func (c *AgentCardV03) AddSkill(skill AgentSkill) *AgentCardV03 {
	c.Skills = append(c.Skills, skill)
	c.UpdatedAt = time.Now()
	return c
}

// AddSecurityScheme adds a security scheme
func (c *AgentCardV03) AddSecurityScheme(scheme SecurityScheme) *AgentCardV03 {
	c.SecuritySchemes = append(c.SecuritySchemes, scheme)
	c.UpdatedAt = time.Now()
	return c
}

// AddInterface adds a protocol interface
func (c *AgentCardV03) AddInterface(iface AgentInterface) *AgentCardV03 {
	c.Interfaces = append(c.Interfaces, iface)
	c.UpdatedAt = time.Now()
	return c
}

// WithAPIKeySecurity adds API key security scheme
func (c *AgentCardV03) WithAPIKeySecurity(id, headerName, description string) *AgentCardV03 {
	return c.AddSecurityScheme(SecurityScheme{
		ID:          id,
		Type:        SecurityTypeAPIKey,
		Description: description,
		In:          APIKeyInHeader,
		Name:        headerName,
	})
}

// WithHTTPInterface adds HTTP+JSON interface
func (c *AgentCardV03) WithHTTPInterface(url string) *AgentCardV03 {
	return c.AddInterface(AgentInterface{
		Protocol:    ProtocolHTTPJSON,
		URL:         url,
		Description: "HTTP+JSON API",
		Methods:     []string{"GET", "POST"},
	})
}

// ToJSON serializes the agent card to JSON
func (c *AgentCardV03) ToJSON() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

// FromAgentCardJSON deserializes an agent card from JSON
func FromAgentCardJSON(data []byte) (*AgentCardV03, error) {
	var card AgentCardV03
	if err := json.Unmarshal(data, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// Validate validates the agent card has required fields
func (c *AgentCardV03) Validate() error {
	if c.Name == "" {
		return ErrMissingRequiredFields
	}
	if c.Description == "" {
		return ErrMissingRequiredFields
	}
	if c.URL == "" {
		return ErrMissingRequiredFields
	}
	if c.ProtocolVersion == "" {
		return ErrInvalidMessageVersion
	}
	return nil
}
