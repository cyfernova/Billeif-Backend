package mcp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Config holds configuration for the MCP client.
type Config struct {
	ServerURL          string
	Timeout            time.Duration
	InsecureSkipVerify bool
	TLSCertFile        string
	TLSKeyFile         string
	TLSCACertFile      string
}

// Client makes calls to the MCP server.
type Client struct {
	baseURL     *url.URL
	httpClient  *http.Client
	tlsCertFile string
	tlsKeyFile  string
	tlsCAFile   string
}

// NewClient creates a new MCP client from config.
func NewClient(cfg Config) (*Client, error) {
	parsed, err := url.Parse(cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("parse MCP server URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("MCP server URL must use http or https")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
	}

	if cfg.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	} else if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}

		var caCertPool *x509.CertPool
		if cfg.TLSCACertFile != "" {
			caCertPool = x509.NewCertPool()
			caCertBytes, err := os.ReadFile(cfg.TLSCACertFile)
			if err != nil {
				return nil, fmt.Errorf("read CA cert: %w", err)
			}
			if !caCertPool.AppendCertsFromPEM(caCertBytes) {
				return nil, fmt.Errorf("failed to parse CA cert")
			}
		}

		transport.TLSClientConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caCertPool,
		}
	}

	return &Client{
		baseURL: parsed,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
		tlsCertFile: cfg.TLSCertFile,
		tlsKeyFile:  cfg.TLSKeyFile,
		tlsCAFile:   cfg.TLSCACertFile,
	}, nil
}

// ToolCallRequest represents a tool call request.
type ToolCallRequest struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args,omitempty"`
}

// ToolCallResponse represents the response from a tool call.
type ToolCallResponse struct {
	Result      json.RawMessage `json:"result"`
	Status      int             `json:"status"`
	RequestID   string          `json:"request_id"`
	BackendPath string          `json:"backend_route"`
}

// ListToolsResponse represents the response from listing tools.
type ListToolsResponse struct {
	Tools []ListedTool `json:"tools"`
}

// ListedTool represents a tool in the list.
type ListedTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
}

// ErrorResponse represents an error from the MCP server.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CallTool calls a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, tool string, args json.RawMessage, bearerToken string) (*ToolCallResponse, error) {
	reqBody := ToolCallRequest{
		Tool: tool,
		Args: args,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	reqURL := c.baseURL.ResolveReference(&url.URL{Path: "/mcp/tools/call"})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if bearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call MCP server: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MiB limit
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		var errResp ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Message != "" {
			return nil, fmt.Errorf("MCP error %s: %s", errResp.Code, errResp.Message)
		}
		return nil, fmt.Errorf("MCP server returned status %d", resp.StatusCode)
	}

	var result ToolCallResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// ListTools returns all available tools from the MCP server.
func (c *Client) ListTools(ctx context.Context, bearerToken string) (*ListToolsResponse, error) {
	reqURL := c.baseURL.ResolveReference(&url.URL{Path: "/mcp/tools/list"})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")
	if bearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call MCP server: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		var errResp ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Message != "" {
			return nil, fmt.Errorf("MCP error %s: %s", errResp.Code, errResp.Message)
		}
		return nil, fmt.Errorf("MCP server returned status %d", resp.StatusCode)
	}

	var result ListToolsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}
