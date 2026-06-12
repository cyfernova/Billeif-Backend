package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/mcp"
)

const voiceMCPFunctionName = "run_billeif_mcp_tool"

type voiceMCPToolScope int

const (
	voiceMCPNoScope voiceMCPToolScope = iota
	voiceMCPQueryScope
	voiceMCPBodyScope
)

type voiceMCPToolRule struct {
	Description string
	Scope       voiceMCPToolScope
}

var voiceMCPToolAllowlist = map[string]voiceMCPToolRule{
	"get_customers":             {Description: "List customers for the current business.", Scope: voiceMCPQueryScope},
	"get_customers_by_id":       {Description: "Get one customer for the current business.", Scope: voiceMCPNoScope},
	"get_invoices":              {Description: "List invoices for the current business.", Scope: voiceMCPQueryScope},
	"get_invoices_by_id":        {Description: "Get one invoice for the current business.", Scope: voiceMCPNoScope},
	"get_invoices_next_number":  {Description: "Get the next invoice number for the current business.", Scope: voiceMCPQueryScope},
	"get_payments":              {Description: "List payments for the current business.", Scope: voiceMCPQueryScope},
	"get_payments_by_id":        {Description: "Get one payment for the current business.", Scope: voiceMCPNoScope},
	"get_products":              {Description: "List products for the current business.", Scope: voiceMCPQueryScope},
	"get_products_by_id":        {Description: "Get one product for the current business.", Scope: voiceMCPNoScope},
	"get_vendors":               {Description: "List vendors for the current business.", Scope: voiceMCPQueryScope},
	"get_vendors_by_id":         {Description: "Get one vendor for the current business.", Scope: voiceMCPNoScope},
	"post_customers":            {Description: "Create a customer for the current business.", Scope: voiceMCPBodyScope},
	"post_invoices":             {Description: "Create an invoice for the current business.", Scope: voiceMCPBodyScope},
	"post_payments":             {Description: "Record a payment for the current business.", Scope: voiceMCPNoScope},
	"post_products":             {Description: "Create a product for the current business.", Scope: voiceMCPBodyScope},
	"post_products_by_id_stock": {Description: "Adjust product stock for the current business.", Scope: voiceMCPNoScope},
	"post_vendors":              {Description: "Create a vendor for the current business.", Scope: voiceMCPBodyScope},
	"put_customers_by_id":       {Description: "Update a customer for the current business.", Scope: voiceMCPNoScope},
	"put_invoices_by_id":        {Description: "Update an invoice for the current business.", Scope: voiceMCPNoScope},
	"put_payments_by_id":        {Description: "Update a payment for the current business.", Scope: voiceMCPNoScope},
	"put_products_by_id":        {Description: "Update a product for the current business.", Scope: voiceMCPNoScope},
	"put_vendors_by_id":         {Description: "Update a vendor for the current business.", Scope: voiceMCPNoScope},
}

type VoiceMCPToolCaller interface {
	CallTool(ctx context.Context, tool string, args json.RawMessage, bearerToken string) (*mcp.ToolCallResponse, error)
}

type VoiceMCPBridge struct {
	caller VoiceMCPToolCaller
	log    *logger.Logger
}

func NewVoiceMCPBridgeFromConfig(cfg config.MCPConfig, log *logger.Logger) (*VoiceMCPBridge, error) {
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return nil, nil
	}
	client, err := mcp.NewClient(mcp.Config{
		ServerURL:          cfg.ServerURL,
		Timeout:            cfg.Timeout,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		TLSCertFile:        cfg.TLSCertFile,
		TLSKeyFile:         cfg.TLSKeyFile,
		TLSCACertFile:      cfg.TLSCACertFile,
	})
	if err != nil {
		return nil, err
	}
	return NewVoiceMCPBridge(client, log), nil
}

func NewVoiceMCPBridge(caller VoiceMCPToolCaller, log *logger.Logger) *VoiceMCPBridge {
	if caller == nil {
		return nil
	}
	if log == nil {
		log = logger.Global()
	}
	return &VoiceMCPBridge{caller: caller, log: log.Named("voice_mcp_bridge")}
}

func (b *VoiceMCPBridge) Enabled() bool {
	return b != nil && b.caller != nil
}

func BuildVoiceMCPFunctionDefinitions() []map[string]interface{} {
	tools := voiceMCPAllowedToolNames()
	return []map[string]interface{}{
		{
			"name":        voiceMCPFunctionName,
			"description": "Run one allowlisted Billeif finance action through the MCP server for the current authenticated business. Use only when the user clearly asks to list, view, create, update, record, or adjust ordinary finance records. Do not delete data, authenticate users, administer accounts, send invoices, or call unrelated tools.",
			"parameters": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"tool": map[string]interface{}{
						"type":        "string",
						"description": "Allowlisted MCP tool to run.",
						"enum":        tools,
					},
					"args": map[string]interface{}{
						"type":                 "object",
						"description":          "Arguments for the selected MCP tool. Use {\"query\":{...}} for list tools, {\"body\":{...}} for create/update tools, and {\"path\":{...}} for tools that require an ID.",
						"additionalProperties": true,
					},
				},
				"required": []string{"tool", "args"},
			},
		},
	}
}

func (b *VoiceMCPBridge) HandleFunctionCall(ctx context.Context, call DeepgramFunctionCall, businessID, accessToken string) string {
	if !b.Enabled() {
		return voiceMCPErrorContent("mcp_unavailable", "Billeif actions are not available in this voice session.")
	}
	if strings.TrimSpace(accessToken) == "" {
		return voiceMCPErrorContent("missing_auth_token", "The voice session is missing the authorization needed to run Billeif actions.")
	}

	request, err := parseVoiceMCPFunctionArguments(call.Arguments)
	if err != nil {
		return voiceMCPErrorContent("invalid_arguments", err.Error())
	}
	rule, ok := voiceMCPToolAllowlist[request.Tool]
	if !ok {
		return voiceMCPErrorContent("tool_not_allowed", "That Billeif action is not available from voice.")
	}

	preparedArgs, err := prepareVoiceMCPToolArgs(request.Args, rule.Scope, businessID)
	if err != nil {
		return voiceMCPErrorContent("invalid_tool_args", err.Error())
	}

	result, err := b.caller.CallTool(ctx, request.Tool, preparedArgs, strings.TrimSpace(accessToken))
	if err != nil {
		b.log.Warn("voice MCP tool call failed", "tool", request.Tool, "error", err)
		return voiceMCPErrorContent("mcp_call_failed", err.Error())
	}
	return voiceMCPSuccessContent(request.Tool, result)
}

type voiceMCPFunctionRequest struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

func parseVoiceMCPFunctionArguments(raw json.RawMessage) (voiceMCPFunctionRequest, error) {
	payload, err := normalizeFunctionArguments(raw)
	if err != nil {
		return voiceMCPFunctionRequest{}, err
	}

	var req voiceMCPFunctionRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return voiceMCPFunctionRequest{}, fmt.Errorf("function arguments must include tool and args: %w", err)
	}
	req.Tool = strings.TrimSpace(req.Tool)
	if req.Tool == "" {
		return voiceMCPFunctionRequest{}, errors.New("tool is required")
	}
	if len(bytes.TrimSpace(req.Args)) == 0 || bytes.Equal(bytes.TrimSpace(req.Args), []byte("null")) {
		req.Args = json.RawMessage(`{}`)
	}
	return req, nil
}

func normalizeFunctionArguments(raw json.RawMessage) (json.RawMessage, error) {
	payload := bytes.TrimSpace(raw)
	if len(payload) == 0 || bytes.Equal(payload, []byte("null")) {
		return nil, errors.New("function arguments are required")
	}
	if payload[0] != '"' {
		return payload, nil
	}

	var encoded string
	if err := json.Unmarshal(payload, &encoded); err != nil {
		return nil, fmt.Errorf("function arguments string is invalid: %w", err)
	}
	decoded := bytes.TrimSpace([]byte(encoded))
	if len(decoded) == 0 {
		return nil, errors.New("function arguments string is empty")
	}
	return decoded, nil
}

func prepareVoiceMCPToolArgs(raw json.RawMessage, scope voiceMCPToolScope, businessID string) (json.RawMessage, error) {
	payload := bytes.TrimSpace(raw)
	if len(payload) == 0 || bytes.Equal(payload, []byte("null")) {
		payload = []byte("{}")
	}

	var args map[string]interface{}
	if err := json.Unmarshal(payload, &args); err != nil {
		return nil, fmt.Errorf("tool args must be a JSON object: %w", err)
	}
	if args == nil {
		args = map[string]interface{}{}
	}

	switch scope {
	case voiceMCPQueryScope:
		query := ensureNestedObject(args, "query")
		if err := enforceVoiceMCPBusinessID(query, businessID); err != nil {
			return nil, err
		}
	case voiceMCPBodyScope:
		body := ensureNestedObject(args, "body")
		if err := enforceVoiceMCPBusinessID(body, businessID); err != nil {
			return nil, err
		}
	case voiceMCPNoScope:
		if err := rejectMismatchedVoiceMCPBusinessID(args, businessID); err != nil {
			return nil, err
		}
	}

	out, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("encode tool args: %w", err)
	}
	return out, nil
}

func ensureNestedObject(args map[string]interface{}, key string) map[string]interface{} {
	if existing, ok := args[key].(map[string]interface{}); ok {
		return existing
	}
	next := map[string]interface{}{}
	args[key] = next
	return next
}

func enforceVoiceMCPBusinessID(target map[string]interface{}, businessID string) error {
	if strings.TrimSpace(businessID) == "" {
		return errors.New("authenticated business_id is required")
	}
	for _, key := range []string{"business_id", "businessId"} {
		if value, ok := target[key]; ok {
			if strings.TrimSpace(fmt.Sprint(value)) != businessID {
				return errors.New("tool business_id does not match authenticated voice session")
			}
			return nil
		}
	}
	target["business_id"] = businessID
	return nil
}

func rejectMismatchedVoiceMCPBusinessID(args map[string]interface{}, businessID string) error {
	for _, sectionName := range []string{"query", "body"} {
		section, ok := args[sectionName].(map[string]interface{})
		if !ok {
			continue
		}
		for _, key := range []string{"business_id", "businessId"} {
			if value, ok := section[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != businessID {
				return errors.New("tool business_id does not match authenticated voice session")
			}
		}
	}
	return nil
}

func voiceMCPErrorContent(code, message string) string {
	return mustJSONText(map[string]interface{}{
		"ok":      false,
		"error":   code,
		"message": message,
	})
}

func voiceMCPSuccessContent(tool string, result *mcp.ToolCallResponse) string {
	status := 0
	requestID := ""
	backendRoute := ""
	var payload json.RawMessage = json.RawMessage(`{}`)
	if result != nil {
		status = result.Status
		requestID = result.RequestID
		backendRoute = result.BackendPath
		if len(bytes.TrimSpace(result.Result)) > 0 {
			payload = result.Result
		}
	}
	return mustJSONText(map[string]interface{}{
		"ok":            true,
		"tool":          tool,
		"status":        status,
		"request_id":    requestID,
		"backend_route": backendRoute,
		"result":        payload,
	})
}

func mustJSONText(value interface{}) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return `{"ok":false,"error":"json_encode_failed"}`
	}
	return string(payload)
}

func voiceMCPAllowedToolNames() []string {
	tools := make([]string, 0, len(voiceMCPToolAllowlist))
	for tool := range voiceMCPToolAllowlist {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	return tools
}
