package services

import (
	"context"
	"encoding/json"
	"testing"

	"invoice-backend/pkg/mcp"

	"github.com/stretchr/testify/require"
)

type fakeVoiceMCPCaller struct {
	tool        string
	args        json.RawMessage
	bearerToken string
	calls       int
	result      *mcp.ToolCallResponse
	err         error
}

func (f *fakeVoiceMCPCaller) CallTool(_ context.Context, tool string, args json.RawMessage, bearerToken string) (*mcp.ToolCallResponse, error) {
	f.calls++
	f.tool = tool
	f.args = append(json.RawMessage(nil), args...)
	f.bearerToken = bearerToken
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &mcp.ToolCallResponse{
		Result:      json.RawMessage(`{"items":[{"name":"Acme"}]}`),
		Status:      200,
		RequestID:   "req-1",
		BackendPath: "/api/v1/customers",
	}, nil
}

func TestBuildVoiceMCPFunctionDefinitionsExposeOnlyAllowlistedFinanceTools(t *testing.T) {
	definitions := BuildVoiceMCPFunctionDefinitions()
	require.Len(t, definitions, 1)
	require.Equal(t, voiceMCPFunctionName, definitions[0]["name"])

	parameters := definitions[0]["parameters"].(map[string]interface{})
	properties := parameters["properties"].(map[string]interface{})
	toolProperty := properties["tool"].(map[string]interface{})
	tools := toolProperty["enum"].([]string)

	require.Contains(t, tools, "get_customers")
	require.Contains(t, tools, "post_invoices_by_id_send")
	require.NotContains(t, tools, "delete_customers_by_id")
	require.NotContains(t, tools, "delete_business_profiles_by_id")
	require.NotContains(t, tools, "post_auth_login")
}

func TestVoiceMCPBridgeInjectsBusinessIDIntoQueryScopedTool(t *testing.T) {
	caller := &fakeVoiceMCPCaller{}
	bridge := NewVoiceMCPBridge(caller, nil)
	content := bridge.HandleFunctionCall(context.Background(), DeepgramFunctionCall{
		Name:      voiceMCPFunctionName,
		Arguments: json.RawMessage(`{"tool":"get_customers","args":{"query":{"limit":5}}}`),
	}, "biz-123", "access-token")

	require.Equal(t, 1, caller.calls)
	require.Equal(t, "get_customers", caller.tool)
	require.Equal(t, "access-token", caller.bearerToken)

	var args struct {
		Query map[string]interface{} `json:"query"`
	}
	require.NoError(t, json.Unmarshal(caller.args, &args))
	require.Equal(t, float64(5), args.Query["limit"])
	require.Equal(t, "biz-123", args.Query["business_id"])

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(content), &result))
	require.Equal(t, true, result["ok"])
	require.Equal(t, "get_customers", result["tool"])
}

func TestVoiceMCPBridgeInjectsBusinessIDIntoBodyScopedTool(t *testing.T) {
	caller := &fakeVoiceMCPCaller{}
	bridge := NewVoiceMCPBridge(caller, nil)
	content := bridge.HandleFunctionCall(context.Background(), DeepgramFunctionCall{
		Name:      voiceMCPFunctionName,
		Arguments: json.RawMessage(`{"tool":"post_products","args":{"body":{"name":"Widget"}}}`),
	}, "biz-123", "access-token")

	require.Equal(t, 1, caller.calls)

	var args struct {
		Body map[string]interface{} `json:"body"`
	}
	require.NoError(t, json.Unmarshal(caller.args, &args))
	require.Equal(t, "Widget", args.Body["name"])
	require.Equal(t, "biz-123", args.Body["business_id"])
	require.Contains(t, content, `"ok":true`)
}

func TestVoiceMCPBridgeParsesDeepgramStringArguments(t *testing.T) {
	caller := &fakeVoiceMCPCaller{}
	bridge := NewVoiceMCPBridge(caller, nil)
	encodedArguments, err := json.Marshal(`{"tool":"get_invoices","args":{"query":{"status":"paid"}}}`)
	require.NoError(t, err)

	content := bridge.HandleFunctionCall(context.Background(), DeepgramFunctionCall{
		Name:      voiceMCPFunctionName,
		Arguments: encodedArguments,
	}, "biz-123", "access-token")

	require.Equal(t, 1, caller.calls)
	require.Equal(t, "get_invoices", caller.tool)
	require.Contains(t, string(caller.args), `"business_id":"biz-123"`)
	require.Contains(t, content, `"ok":true`)
}

func TestVoiceMCPBridgeRejectsMismatchedBusinessID(t *testing.T) {
	caller := &fakeVoiceMCPCaller{}
	bridge := NewVoiceMCPBridge(caller, nil)
	content := bridge.HandleFunctionCall(context.Background(), DeepgramFunctionCall{
		Name:      voiceMCPFunctionName,
		Arguments: json.RawMessage(`{"tool":"get_customers","args":{"query":{"business_id":"other-biz"}}}`),
	}, "biz-123", "access-token")

	require.Zero(t, caller.calls)
	require.Contains(t, content, `"ok":false`)
	require.Contains(t, content, `"invalid_tool_args"`)
}

func TestVoiceMCPBridgeRequiresAccessToken(t *testing.T) {
	caller := &fakeVoiceMCPCaller{}
	bridge := NewVoiceMCPBridge(caller, nil)
	content := bridge.HandleFunctionCall(context.Background(), DeepgramFunctionCall{
		Name:      voiceMCPFunctionName,
		Arguments: json.RawMessage(`{"tool":"get_customers","args":{}}`),
	}, "biz-123", "")

	require.Zero(t, caller.calls)
	require.Contains(t, content, `"missing_auth_token"`)
}
