package config

import "time"

// MCPConfig holds configuration for connecting to the MCP server.
type MCPConfig struct {
	// ServerURL is the base URL of the MCP server (for example, "https://mcp.example.test")
	ServerURL string `mapstructure:"MCP_SERVER_URL"`
	// Timeout is the HTTP client timeout for MCP server calls
	Timeout time.Duration `mapstructure:"MCP_TIMEOUT"`
	// InsecureSkipVerify skips TLS certificate verification (for local dev only)
	InsecureSkipVerify bool `mapstructure:"MCP_INSECURE_SKIP_VERIFY"`
	// TLSCertFile is the path to the client TLS certificate
	TLSCertFile string `mapstructure:"MCP_TLS_CERT_FILE"`
	// TLSKeyFile is the path to the client TLS key
	TLSKeyFile string `mapstructure:"MCP_TLS_KEY_FILE"`
	// TLSCACertFile is the path to the CA certificate for TLS verification
	TLSCACertFile string `mapstructure:"MCP_TLS_CA_FILE"`
}

// DefaultMCPConfig returns default MCP configuration values.
func DefaultMCPConfig() MCPConfig {
	return MCPConfig{
		Timeout: 30 * time.Second,
	}
}
