package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/mcp"

	"github.com/spf13/viper"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MCPHandler handles MCP tool routing.
type MCPHandler struct {
	client      *mcp.Client
	bearerToken string
	log         *logger.Logger
}

// NewMCPHandlerFromConfig creates a new MCP handler from config.
func NewMCPHandlerFromConfig(cfg *config.Config, log *logger.Logger) *MCPHandler {
	serverURL := cfg.MCP.ServerURL
	insecureSkipVerify := cfg.MCP.InsecureSkipVerify

	// Hardcoded fallback for development
	if serverURL == "" {
		serverURL = viper.GetString("MCP_SERVER_URL")
		if serverURL == "" {
			serverURL = "https://localhost:9090"
			insecureSkipVerify = true
		} else {
			// Respect MCP_INSECURE_SKIP_VERIFY even when URL comes from env
			if viper.GetBool("MCP_INSECURE_SKIP_VERIFY") {
				insecureSkipVerify = true
			}
		}
		log.Info("Using default MCP server URL", "server_url", serverURL)
	}
	if serverURL == "" {
		log.Info("MCP server URL not configured, MCP handler disabled")
		return nil
	}

	mcpCfg := mcp.Config{
		ServerURL:          serverURL,
		Timeout:            cfg.MCP.Timeout,
		InsecureSkipVerify: insecureSkipVerify,
		TLSCertFile:        cfg.MCP.TLSCertFile,
		TLSKeyFile:         cfg.MCP.TLSKeyFile,
		TLSCACertFile:      cfg.MCP.TLSCACertFile,
	}

	log.Info("Creating MCP client with config", "url", mcpCfg.ServerURL, "insecure", mcpCfg.InsecureSkipVerify)
	client, err := mcp.NewClient(mcpCfg)
	if err != nil {
		log.Error("failed to create MCP client", "error", err)
		return nil
	}

	log.Info("MCP client initialized", "server_url", serverURL)
	return &MCPHandler{
		client:      client,
		bearerToken: "", // Bearer token will be taken from the incoming request context
		log:         log.Named("mcp_handler"),
	}
}

// NewMCPHandler creates a new MCP handler (for testing or manual injection).
func NewMCPHandler(client *mcp.Client, bearerToken string, log *logger.Logger) *MCPHandler {
	return &MCPHandler{
		client:      client,
		bearerToken: bearerToken,
		log:         log.Named("mcp_handler"),
	}
}

// CallToolRequest represents a request to call an MCP tool.
type CallToolRequest struct {
	Tool string `json:"tool" binding:"required"`
	Args any    `json:"args"`
}

// ToolCallResponse represents the response from an MCP tool call.
type ToolCallResponse struct {
	Result      any    `json:"result,omitempty"`
	Status      int    `json:"status"`
	RequestID   string `json:"request_id"`
	BackendPath string `json:"backend_route,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ListToolsResponse represents the response from listing MCP tools.
type ListToolsResponse struct {
	Tools []ToolInfo `json:"tools"`
}

// ToolInfo represents information about an MCP tool.
type ToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
}

// CallTool handles POST /mcp/tools/call - calls an MCP tool.
// @Summary Call an MCP tool
// @Description Routes a tool call request to the MCP server
// @Tags mcp
// @Accept json
// @Produce json
// @Param request body CallToolRequest true "Tool call request"
// @Success 200 {object} ToolCallResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /mcp/tools/call [post]
func (h *MCPHandler) CallTool(c *gin.Context) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP handler not configured"})
		return
	}

	var req CallToolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	reqID := uuid.NewString()
	ctx := contextWithRequestID(c.Request.Context(), reqID)

	var args json.RawMessage
	if req.Args != nil {
		argsBytes, err := json.Marshal(req.Args)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid args format"})
			return
		}
		args = argsBytes
	} else {
		args = json.RawMessage("{}")
	}

	// Get bearer token from request context (set by auth middleware)
	bearerToken := getBearerToken(c)

	result, err := h.client.CallTool(ctx, req.Tool, args, bearerToken)
	if err != nil {
		h.log.Error("MCP tool call failed", "tool", req.Tool, "request_id", reqID, "error", err)
		c.JSON(http.StatusInternalServerError, ToolCallResponse{
			Status:    http.StatusInternalServerError,
			RequestID: reqID,
			Error:     err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, ToolCallResponse{
		Result:      result.Result,
		Status:      result.Status,
		RequestID:   result.RequestID,
		BackendPath: result.BackendPath,
	})
}

// ListTools handles GET /mcp/tools/list - lists available MCP tools.
// @Summary List MCP tools
// @Description Returns all available tools from the MCP server
// @Tags mcp
// @Produce json
// @Success 200 {object} ListToolsResponse
// @Failure 500 {object} map[string]string
// @Router /mcp/tools/list [get]
func (h *MCPHandler) ListTools(c *gin.Context) {
	h.log.Info("ListTools called", "h", fmt.Sprintf("%v", h), "client", fmt.Sprintf("%v", h != nil && h.client != nil))
	if h == nil || h.client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP handler not configured"})
		return
	}

	ctx := contextWithRequestID(c.Request.Context(), uuid.NewString())
	bearerToken := getBearerToken(c)

	result, err := h.client.ListTools(ctx, bearerToken)
	if err != nil {
		h.log.Error("MCP list tools failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// HealthCheck handles GET /mcp/health - checks MCP server health.
func (h *MCPHandler) HealthCheck(c *gin.Context) {
	if h == nil || h.client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unavailable",
			"error":  "MCP handler not configured",
		})
		return
	}

	ctx := contextWithRequestID(c.Request.Context(), uuid.NewString())
	bearerToken := getBearerToken(c)

	// Try to list tools as a health check
	_, err := h.client.ListTools(ctx, bearerToken)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unavailable",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

// contextWithRequestID adds a request ID to context.
func contextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, "X-Request-ID", requestID)
}

// getBearerToken extracts the bearer token from the Gin context.
func getBearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}
	// Extract token from "Bearer <token>" format
	const prefix = "Bearer "
	if len(authHeader) > len(prefix) && authHeader[:len(prefix)] == prefix {
		return authHeader[len(prefix):]
	}
	return ""
}
