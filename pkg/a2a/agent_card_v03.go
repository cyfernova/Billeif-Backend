package a2a

type AgentCard struct {
	Name                string                    `json:"name"`
	Description         string                    `json:"description"`
	ProtocolVersions    []string                  `json:"protocolVersions"`
	SupportedInterfaces []AgentInterface          `json:"supportedInterfaces"`
	Provider            *AgentProvider            `json:"provider,omitempty"`
	Version             string                    `json:"version"`
	DocumentationURL    string                    `json:"documentationUrl,omitempty"`
	Capabilities        AgentCapabilities         `json:"capabilities"`
	SecuritySchemes     map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	Security            []map[string][]string     `json:"security,omitempty"`
	DefaultInputModes   []string                  `json:"defaultInputModes"`
	DefaultOutputModes  []string                  `json:"defaultOutputModes"`
	Skills              []AgentSkill              `json:"skills"`
	Signatures          []AgentCardSignature      `json:"signatures,omitempty"`
	IconURL             string                    `json:"iconUrl,omitempty"`
}

type AgentInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	Tenant          string `json:"tenant,omitempty"`
}

type AgentProvider struct {
	URL          string `json:"url"`
	Organization string `json:"organization"`
}

type AgentCapabilities struct {
	Streaming         bool             `json:"streaming,omitempty"`
	PushNotifications bool             `json:"pushNotifications,omitempty"`
	Extensions        []AgentExtension `json:"extensions,omitempty"`
	ExtendedAgentCard bool             `json:"extendedAgentCard,omitempty"`
}

type AgentExtension struct {
	URI         string                 `json:"uri"`
	Description string                 `json:"description,omitempty"`
	Required    bool                   `json:"required,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
}

type AgentSkill struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Tags        []string              `json:"tags"`
	Examples    []string              `json:"examples,omitempty"`
	InputModes  []string              `json:"inputModes,omitempty"`
	OutputModes []string              `json:"outputModes,omitempty"`
	Security    []map[string][]string `json:"security,omitempty"`
}

type AgentCardSignature struct {
	Protected string                 `json:"protected"`
	Signature string                 `json:"signature"`
	Header    map[string]interface{} `json:"header,omitempty"`
}

type SecurityScheme struct {
	APIKeySecurityScheme        *APIKeySecurityScheme        `json:"apiKeySecurityScheme,omitempty"`
	HTTPAuthSecurityScheme      *HTTPAuthSecurityScheme      `json:"httpAuthSecurityScheme,omitempty"`
	OAuth2SecurityScheme        *OAuth2SecurityScheme        `json:"oauth2SecurityScheme,omitempty"`
	OpenIDConnectSecurityScheme *OpenIDConnectSecurityScheme `json:"openIdConnectSecurityScheme,omitempty"`
	MTLSSecurityScheme          *MutualTLSSecurityScheme     `json:"mtlsSecurityScheme,omitempty"`
}

type APIKeySecurityScheme struct {
	Description string `json:"description,omitempty"`
	Location    string `json:"location"`
	Name        string `json:"name"`
}

type HTTPAuthSecurityScheme struct {
	Description  string `json:"description,omitempty"`
	Scheme       string `json:"scheme"`
	BearerFormat string `json:"bearerFormat,omitempty"`
}

type OAuth2SecurityScheme struct {
	Description       string      `json:"description,omitempty"`
	Flows             *OAuthFlows `json:"flows,omitempty"`
	OAuth2MetadataURL string      `json:"oauth2MetadataUrl,omitempty"`
}

type OpenIDConnectSecurityScheme struct {
	Description      string `json:"description,omitempty"`
	OpenIDConnectURL string `json:"openIdConnectUrl"`
}

type MutualTLSSecurityScheme struct {
	Description string `json:"description,omitempty"`
}

type OAuthFlows struct {
	AuthorizationCode *AuthorizationCodeOAuthFlow `json:"authorizationCode,omitempty"`
	ClientCredentials *ClientCredentialsOAuthFlow `json:"clientCredentials,omitempty"`
	DeviceCode        *DeviceCodeOAuthFlow        `json:"deviceCode,omitempty"`
}

type AuthorizationCodeOAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl"`
	TokenURL         string            `json:"tokenUrl"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes"`
	PKCERequired     bool              `json:"pkceRequired,omitempty"`
}

type ClientCredentialsOAuthFlow struct {
	TokenURL   string            `json:"tokenUrl"`
	RefreshURL string            `json:"refreshUrl,omitempty"`
	Scopes     map[string]string `json:"scopes"`
}

type DeviceCodeOAuthFlow struct {
	DeviceAuthorizationURL string            `json:"deviceAuthorizationUrl"`
	TokenURL               string            `json:"tokenUrl"`
	RefreshURL             string            `json:"refreshUrl,omitempty"`
	Scopes                 map[string]string `json:"scopes"`
}

func NewAgentCard(name, description, version string) *AgentCard {
	return &AgentCard{
		Name:                name,
		Description:         description,
		Version:             version,
		ProtocolVersions:    []string{SupportedVersion},
		SupportedInterfaces: []AgentInterface{},
		Capabilities:        AgentCapabilities{},
		SecuritySchemes:     map[string]SecurityScheme{},
		Security:            []map[string][]string{},
		DefaultInputModes:   []string{},
		DefaultOutputModes:  []string{},
		Skills:              []AgentSkill{},
	}
}
