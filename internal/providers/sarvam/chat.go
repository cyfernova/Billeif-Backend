package sarvam

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	chatPath = "/v1/chat/completions"

	chatMinOutputTokens     = 120
	chatMaxOutputTokens     = 220
	chatDefaultOutputTokens = 180
	chatRetryBackoff        = 50 * time.Millisecond

	chatMaxMessages             = 32
	chatMaxMessageBytes         = 32 << 10
	chatMaxMessageAggregate     = 256 << 10
	chatMaxTools                = 32
	chatMaxToolDescriptionBytes = 4 << 10
	chatMaxToolSchemaBytes      = 32 << 10
	chatMaxToolSchemaAggregate  = 256 << 10
	chatMaxToolCalls            = 16
	chatMaxToolIDBytes          = 128
	chatMaxToolNameBytes        = 64
	chatMaxToolArgumentsBytes   = 64 << 10
	chatMaxToolArgumentsTotal   = 128 << 10

	chatMaxSSELineBytes      = 64 << 10
	chatMaxSSEEventBytes     = 64 << 10
	chatMaxSSEEvents         = 512
	chatMaxTextBytes         = 64 << 10
	chatMaxReasoningBytes    = 64 << 10
	chatMaxMetadataBytes     = 16 << 10
	chatMaxRefusalBytes      = 8 << 10
	chatMaxJSONDepth         = 16
	chatMaxJSONEntries       = 256
	chatMaxProviderErrorCode = 32
	chatMaxErrorBodyBytes    = 8 << 10
)

var (
	ErrChatClientRequired  = errors.New("Sarvam chat client is required")
	ErrChatHandlerRequired = errors.New("Sarvam chat event handler is required")
	ErrChatInvalidRequest  = errors.New("invalid Sarvam chat request")
	ErrChatTransport       = errors.New("Sarvam chat transport failed")
	ErrChatRateLimited     = errors.New("Sarvam chat rate limited")
	ErrChatQuotaExceeded   = errors.New("Sarvam chat quota exceeded")
	ErrChatUnavailable     = errors.New("Sarvam chat unavailable")
	ErrChatMalformedStream = errors.New("malformed Sarvam chat stream")
	ErrChatStreamTooLarge  = errors.New("Sarvam chat stream exceeds size limit")
	ErrChatHandlerFailed   = errors.New("Sarvam chat event handler failed")
)

type ChatRole string

const (
	ChatRoleSystem    ChatRole = "system"
	ChatRoleUser      ChatRole = "user"
	ChatRoleAssistant ChatRole = "assistant"
	ChatRoleTool      ChatRole = "tool"
)

// ChatStreamer is the provider-neutral turn-orchestrator boundary. Validated
// text deltas are delivered as their SSE events arrive; completed tool calls
// remain staged until finish reason, usage, [DONE], and aggregate consistency
// have all been validated.
type ChatStreamer interface {
	StreamChat(context.Context, ChatRequest, ChatEventHandler) (ChatResult, error)
}

type ChatEventHandler interface {
	HandleChatEvent(context.Context, ChatEvent) error
}

// ChatProgressHandler is an optional capability on ChatEventHandler. It is
// notified once when the first visible text or tool-call delta is validated.
type ChatProgressHandler interface {
	HandleChatProgress(context.Context) error
}

type ChatEventHandlerFunc func(context.Context, ChatEvent) error

func (handler ChatEventHandlerFunc) HandleChatEvent(ctx context.Context, event ChatEvent) error {
	if handler == nil {
		return ErrChatHandlerRequired
	}
	return handler(ctx, event)
}

type ChatRequest struct {
	Messages  []ChatMessage
	Tools     []ChatToolDefinition
	MaxTokens int
}

type ChatMessage struct {
	Role       ChatRole
	Content    string
	ToolCallID string
	ToolCalls  []ChatToolCall
}

type ChatToolDefinition struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type ChatToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type ChatEvent struct {
	TextDelta string
	ToolCall  *ChatToolCall
}

type ChatUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type ChatResult struct {
	Text         string
	ToolCalls    []ChatToolCall
	Usage        ChatUsage
	FinishReason string
}

type ChatClient struct {
	client   *Client
	endpoint string
}

type chatWireRequest struct {
	Model           string            `json:"model"`
	Messages        []chatWireMessage `json:"messages"`
	Tools           []chatWireTool    `json:"tools,omitempty"`
	Stream          bool              `json:"stream"`
	N               int               `json:"n"`
	ReasoningEffort *string           `json:"reasoning_effort"`
	Temperature     float64           `json:"temperature"`
	MaxTokens       int               `json:"max_tokens"`
}

type chatWireMessage struct {
	Role       ChatRole           `json:"role"`
	Content    string             `json:"content"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	ToolCalls  []chatWireToolCall `json:"tool_calls,omitempty"`
}

type chatWireTool struct {
	Type     string           `json:"type"`
	Function chatWireFunction `json:"function"`
}

type chatWireFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatWireToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function chatWireCalledFunction `json:"function"`
}

type chatWireCalledFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatSSEChunk struct {
	ID                string                `json:"id,omitempty"`
	Object            string                `json:"object,omitempty"`
	Created           int64                 `json:"created,omitempty"`
	Model             string                `json:"model,omitempty"`
	SystemFingerprint *string               `json:"system_fingerprint,omitempty"`
	ServiceTier       *string               `json:"service_tier,omitempty"`
	Choices           *[]chatSSEChoice      `json:"choices"`
	Usage             *chatSSEUsage         `json:"usage,omitempty"`
	Error             *chatSSEProviderError `json:"error,omitempty"`
}

type chatSSEChoice struct {
	Index        int             `json:"index"`
	Delta        chatSSEDelta    `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs,omitempty"`
}

type chatSSEDelta struct {
	Role             *string            `json:"role,omitempty"`
	Content          *string            `json:"content,omitempty"`
	ReasoningContent *string            `json:"reasoning_content,omitempty"`
	ToolCalls        []chatSSEToolDelta `json:"tool_calls,omitempty"`
	Refusal          json.RawMessage    `json:"refusal,omitempty"`
	FunctionCall     json.RawMessage    `json:"function_call,omitempty"`
}

type chatSSEToolDelta struct {
	Index    int                  `json:"index"`
	ID       *string              `json:"id,omitempty"`
	Type     *string              `json:"type,omitempty"`
	Function chatSSEFunctionDelta `json:"function"`
}

type chatSSEFunctionDelta struct {
	Name      *string `json:"name,omitempty"`
	Arguments *string `json:"arguments,omitempty"`
}

type chatSSEUsage struct {
	PromptTokens            int             `json:"prompt_tokens"`
	CompletionTokens        int             `json:"completion_tokens"`
	TotalTokens             int             `json:"total_tokens"`
	CompletionTokensDetails json.RawMessage `json:"completion_tokens_details,omitempty"`
	PromptTokensDetails     json.RawMessage `json:"prompt_tokens_details,omitempty"`
}

type chatSSEProviderError struct {
	Status  int             `json:"status,omitempty"`
	Code    json.RawMessage `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Type    string          `json:"type,omitempty"`
	Param   json.RawMessage `json:"param,omitempty"`
}

type chatToolBuilder struct {
	id           string
	kind         string
	name         strings.Builder
	arguments    strings.Builder
	hasID        bool
	hasType      bool
	hasName      bool
	hasArguments bool
}

type chatStreamState struct {
	handler ChatEventHandler
	ctx     context.Context

	accepted      bool
	progressSeen  bool
	eventCount    int
	text          strings.Builder
	reasoningSize int
	toolArgSize   int
	tools         map[int]*chatToolBuilder
	usage         ChatUsage
	usageSeen     bool
	finishReason  string
	finishSeen    bool
	doneSeen      bool
}

func NewChatClient(client *Client) (*ChatClient, error) {
	if client == nil || client.apiKey == "" || client.httpClient == nil {
		return nil, ErrChatClientRequired
	}
	endpoint, err := client.endpoint(chatPath)
	if err != nil {
		return nil, ErrChatClientRequired
	}
	return &ChatClient{client: client, endpoint: endpoint}, nil
}

func (client *ChatClient) StreamChat(ctx context.Context, request ChatRequest, handler ChatEventHandler) (ChatResult, error) {
	if ctx == nil {
		return ChatResult{}, ErrContextRequired
	}
	if client == nil || client.client == nil || client.client.httpClient == nil || client.client.apiKey == "" || client.endpoint == "" {
		return ChatResult{}, ErrChatClientRequired
	}
	if isNilChatHandler(handler) {
		return ChatResult{}, ErrChatHandlerRequired
	}
	if err := ctx.Err(); err != nil {
		return ChatResult{}, chatContextError(err)
	}
	payload, err := marshalChatRequest(request)
	if err != nil {
		return ChatResult{}, err
	}

	for attempt := 0; attempt < 2; attempt++ {
		result, accepted, streamErr := client.streamChatOnce(ctx, payload, handler)
		if streamErr == nil {
			return result, nil
		}
		if attempt == 0 && !accepted && chatRetryable(streamErr) {
			if err := waitChatRetry(ctx); err != nil {
				return ChatResult{}, err
			}
			continue
		}
		return ChatResult{}, streamErr
	}
	return ChatResult{}, ErrChatTransport
}

func (client *ChatClient) streamChatOnce(ctx context.Context, payload []byte, handler ChatEventHandler) (ChatResult, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ChatResult{}, false, ErrChatInvalidRequest
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set(APIKeyHeader, client.client.apiKey)

	response, err := client.client.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return ChatResult{}, false, chatTransportError(ctx, err)
	}
	if response == nil || response.Body == nil {
		return ChatResult{}, false, ErrChatTransport
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ChatResult{}, false, chatHTTPStatusError(response, client.client.apiKey)
	}
	defer response.Body.Close()
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "text/event-stream") {
		return ChatResult{}, false, ErrChatMalformedStream
	}
	state := &chatStreamState{ctx: ctx, handler: handler, tools: make(map[int]*chatToolBuilder)}
	result, err := state.read(response.Body)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ChatResult{}, state.accepted, chatContextError(ctxErr)
		}
		return ChatResult{}, state.accepted, err
	}
	return result, state.accepted, nil
}

func marshalChatRequest(request ChatRequest) ([]byte, error) {
	maxTokens := request.MaxTokens
	if maxTokens == 0 {
		maxTokens = chatDefaultOutputTokens
	}
	if maxTokens < chatMinOutputTokens || maxTokens > chatMaxOutputTokens || len(request.Messages) == 0 || len(request.Messages) > chatMaxMessages || len(request.Tools) > chatMaxTools {
		return nil, ErrChatInvalidRequest
	}
	messages, err := validateChatMessages(request.Messages)
	if err != nil {
		return nil, err
	}
	tools, err := validateChatTools(request.Tools)
	if err != nil {
		return nil, err
	}
	wire := chatWireRequest{
		Model:       DefaultLLMModel,
		Messages:    messages,
		Tools:       tools,
		Stream:      true,
		N:           1,
		Temperature: 0.2,
		MaxTokens:   maxTokens,
	}
	payload, err := json.Marshal(wire)
	if err != nil || int64(len(payload)) > MaxRequestBytes {
		return nil, ErrChatInvalidRequest
	}
	return payload, nil
}

func validateChatMessages(messages []ChatMessage) ([]chatWireMessage, error) {
	wire := make([]chatWireMessage, len(messages))
	aggregate := 0
	for index, message := range messages {
		if !utf8.ValidString(message.Content) || len(message.Content) > chatMaxMessageBytes || containsNUL(message.Content) {
			return nil, ErrChatInvalidRequest
		}
		aggregate += len(message.Content)
		if aggregate > chatMaxMessageAggregate {
			return nil, ErrChatInvalidRequest
		}
		wire[index] = chatWireMessage{Role: message.Role, Content: message.Content}
		switch message.Role {
		case ChatRoleSystem, ChatRoleUser:
			if strings.TrimSpace(message.Content) == "" || message.ToolCallID != "" || len(message.ToolCalls) != 0 {
				return nil, ErrChatInvalidRequest
			}
		case ChatRoleAssistant:
			if message.ToolCallID != "" || (strings.TrimSpace(message.Content) == "" && len(message.ToolCalls) == 0) || len(message.ToolCalls) > chatMaxToolCalls {
				return nil, ErrChatInvalidRequest
			}
			calls, err := validateRequestToolCalls(message.ToolCalls)
			if err != nil {
				return nil, err
			}
			wire[index].ToolCalls = calls
		case ChatRoleTool:
			if strings.TrimSpace(message.Content) == "" || !safeChatIdentifier(message.ToolCallID, chatMaxToolIDBytes) || len(message.ToolCalls) != 0 {
				return nil, ErrChatInvalidRequest
			}
			wire[index].ToolCallID = message.ToolCallID
		default:
			return nil, ErrChatInvalidRequest
		}
	}
	return wire, nil
}

func validateRequestToolCalls(calls []ChatToolCall) ([]chatWireToolCall, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	wire := make([]chatWireToolCall, len(calls))
	ids := make(map[string]struct{}, len(calls))
	aggregate := 0
	for index, call := range calls {
		if !safeChatIdentifier(call.ID, chatMaxToolIDBytes) || !safeChatToolName(call.Name) || !validJSONObject(call.Arguments, chatMaxToolArgumentsBytes) {
			return nil, ErrChatInvalidRequest
		}
		if _, duplicate := ids[call.ID]; duplicate {
			return nil, ErrChatInvalidRequest
		}
		ids[call.ID] = struct{}{}
		aggregate += len(call.Arguments)
		if aggregate > chatMaxToolArgumentsTotal {
			return nil, ErrChatInvalidRequest
		}
		wire[index] = chatWireToolCall{
			ID: call.ID, Type: "function",
			Function: chatWireCalledFunction{Name: call.Name, Arguments: string(call.Arguments)},
		}
	}
	return wire, nil
}

func validateChatTools(tools []ChatToolDefinition) ([]chatWireTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	wire := make([]chatWireTool, len(tools))
	names := make(map[string]struct{}, len(tools))
	aggregate := 0
	for index, tool := range tools {
		if !safeChatToolName(tool.Name) || !utf8.ValidString(tool.Description) || len(tool.Description) > chatMaxToolDescriptionBytes || containsNUL(tool.Description) || !validJSONObject(tool.Parameters, chatMaxToolSchemaBytes) {
			return nil, ErrChatInvalidRequest
		}
		if _, duplicate := names[tool.Name]; duplicate {
			return nil, ErrChatInvalidRequest
		}
		names[tool.Name] = struct{}{}
		aggregate += len(tool.Parameters)
		if aggregate > chatMaxToolSchemaAggregate {
			return nil, ErrChatInvalidRequest
		}
		wire[index] = chatWireTool{Type: "function", Function: chatWireFunction{
			Name: tool.Name, Description: tool.Description, Parameters: bytes.Clone(tool.Parameters),
		}}
	}
	return wire, nil
}

func (state *chatStreamState) read(body io.Reader) (ChatResult, error) {
	reader := bufio.NewReaderSize(body, chatMaxSSELineBytes+2)
	var event strings.Builder
	for {
		line, prefix, err := reader.ReadLine()
		if prefix || len(line) > chatMaxSSELineBytes {
			return ChatResult{}, ErrChatStreamTooLarge
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return ChatResult{}, ErrChatMalformedStream
		}
		if len(line) == 0 {
			if event.Len() > 0 {
				done, consumeErr := state.consumeEvent(event.String())
				event.Reset()
				if consumeErr != nil {
					return ChatResult{}, consumeErr
				}
				if done {
					return state.finish()
				}
			}
		} else if line[0] != ':' {
			if !bytes.HasPrefix(line, []byte("data:")) {
				return ChatResult{}, ErrChatMalformedStream
			}
			data := line[len("data:"):]
			if len(data) > 0 && data[0] == ' ' {
				data = data[1:]
			}
			if event.Len() > 0 {
				event.WriteByte('\n')
			}
			if event.Len()+len(data) > chatMaxSSEEventBytes {
				return ChatResult{}, ErrChatStreamTooLarge
			}
			event.Write(data)
		}
		if errors.Is(err, io.EOF) {
			if event.Len() > 0 {
				done, consumeErr := state.consumeEvent(event.String())
				if consumeErr != nil {
					return ChatResult{}, consumeErr
				}
				if done {
					return state.finish()
				}
			}
			if !state.doneSeen {
				return ChatResult{}, ErrChatMalformedStream
			}
			return state.finish()
		}
	}
}

func (state *chatStreamState) consumeEvent(data string) (bool, error) {
	if err := state.ctx.Err(); err != nil {
		return false, chatContextError(err)
	}
	state.eventCount++
	if state.eventCount > chatMaxSSEEvents {
		return false, ErrChatStreamTooLarge
	}
	if data == "[DONE]" {
		if state.doneSeen || !state.usageSeen {
			return false, ErrChatMalformedStream
		}
		state.doneSeen = true
		return true, nil
	}
	if state.doneSeen {
		return false, ErrChatMalformedStream
	}
	if data == "" || len(data) > chatMaxSSEEventBytes || !utf8.ValidString(data) {
		return false, ErrChatMalformedStream
	}
	if !validChatJSONDocument([]byte(data)) {
		return false, ErrChatMalformedStream
	}
	var chunk chatSSEChunk
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&chunk); err != nil || requireChatJSONEOF(decoder) != nil {
		return false, ErrChatMalformedStream
	}
	if chunk.Error != nil {
		return false, chatSSEError(chunk.Error)
	}
	if chunk.Choices == nil {
		return false, ErrChatMalformedStream
	}
	choices := *chunk.Choices
	if len(choices) == 0 {
		if chunk.Usage == nil || state.usageSeen || !state.finishSeen {
			return false, ErrChatMalformedStream
		}
		if !validChatOptionalObject(chunk.Usage.CompletionTokensDetails) ||
			!validChatOptionalObject(chunk.Usage.PromptTokensDetails) {
			return false, ErrChatMalformedStream
		}
		usage := ChatUsage{
			PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens, TotalTokens: chunk.Usage.TotalTokens,
		}
		if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.TotalTokens < 0 || usage.TotalTokens != usage.PromptTokens+usage.CompletionTokens {
			return false, ErrChatMalformedStream
		}
		state.usage = usage
		state.usageSeen = true
		state.accepted = true
		return false, nil
	}
	if state.finishSeen || state.usageSeen || chunk.Usage != nil || len(choices) != 1 || choices[0].Index != 0 {
		return false, ErrChatMalformedStream
	}
	choice := choices[0]
	if !validChatOptionalObject(choice.Logprobs) ||
		!validChatOptionalString(choice.Delta.Refusal, chatMaxRefusalBytes) ||
		!validChatOptionalObject(choice.Delta.FunctionCall) {
		return false, ErrChatMalformedStream
	}
	if choice.Delta.Role != nil && *choice.Delta.Role != "assistant" {
		return false, ErrChatMalformedStream
	}
	hasText := choice.Delta.Content != nil && *choice.Delta.Content != ""
	hasTools := len(choice.Delta.ToolCalls) != 0
	if (hasText && (hasTools || len(state.tools) != 0)) || (hasTools && state.text.Len() != 0) {
		return false, ErrChatMalformedStream
	}
	if choice.FinishReason != nil {
		if state.finishSeen || !validChatFinishReason(*choice.FinishReason) {
			return false, ErrChatMalformedStream
		}
		if (*choice.FinishReason == "stop" && (hasTools || len(state.tools) != 0)) ||
			(*choice.FinishReason == "tool_calls" && (hasText || state.text.Len() != 0)) {
			return false, ErrChatMalformedStream
		}
		state.finishReason = *choice.FinishReason
		state.finishSeen = true
	}
	state.accepted = true
	if choice.Delta.ReasoningContent != nil {
		state.reasoningSize += len(*choice.Delta.ReasoningContent)
		if state.reasoningSize > chatMaxReasoningBytes {
			return false, ErrChatStreamTooLarge
		}
	}
	if hasText {
		if state.text.Len()+len(*choice.Delta.Content) > chatMaxTextBytes {
			return false, ErrChatStreamTooLarge
		}
		state.text.WriteString(*choice.Delta.Content)
	}
	if err := state.consumeToolDeltas(choice.Delta.ToolCalls); err != nil {
		return false, err
	}
	if !state.progressSeen && (hasText || hasChatToolProgress(choice.Delta.ToolCalls)) {
		state.progressSeen = true
		if progressHandler, ok := state.handler.(ChatProgressHandler); ok {
			if err := safeHandleChatProgress(progressHandler, state.ctx); err != nil {
				return false, err
			}
		}
	}
	if hasText {
		if err := safeHandleChatEvent(state.handler, state.ctx, ChatEvent{TextDelta: *choice.Delta.Content}); err != nil {
			return false, err
		}
	}
	return false, nil
}

func hasChatToolProgress(deltas []chatSSEToolDelta) bool {
	for _, delta := range deltas {
		if delta.ID != nil || delta.Type != nil || (delta.Function.Name != nil && *delta.Function.Name != "") || (delta.Function.Arguments != nil && *delta.Function.Arguments != "") {
			return true
		}
	}
	return false
}

func (state *chatStreamState) consumeToolDeltas(deltas []chatSSEToolDelta) error {
	seen := make(map[int]struct{}, len(deltas))
	for _, delta := range deltas {
		if delta.Index < 0 || delta.Index >= chatMaxToolCalls {
			return ErrChatMalformedStream
		}
		if _, duplicate := seen[delta.Index]; duplicate {
			return ErrChatMalformedStream
		}
		seen[delta.Index] = struct{}{}
		builder := state.tools[delta.Index]
		if builder == nil {
			builder = &chatToolBuilder{}
			state.tools[delta.Index] = builder
		}
		if delta.ID != nil {
			if !safeChatIdentifier(*delta.ID, chatMaxToolIDBytes) || (builder.hasID && builder.id != *delta.ID) {
				return ErrChatMalformedStream
			}
			builder.id = *delta.ID
			builder.hasID = true
		}
		if delta.Type != nil {
			if *delta.Type != "function" || (builder.hasType && builder.kind != *delta.Type) {
				return ErrChatMalformedStream
			}
			builder.kind = *delta.Type
			builder.hasType = true
		}
		if delta.Function.Name != nil {
			if !utf8.ValidString(*delta.Function.Name) || builder.name.Len()+len(*delta.Function.Name) > chatMaxToolNameBytes {
				return ErrChatMalformedStream
			}
			builder.name.WriteString(*delta.Function.Name)
			builder.hasName = true
		}
		if delta.Function.Arguments != nil {
			if !utf8.ValidString(*delta.Function.Arguments) || builder.arguments.Len()+len(*delta.Function.Arguments) > chatMaxToolArgumentsBytes {
				return ErrChatStreamTooLarge
			}
			state.toolArgSize += len(*delta.Function.Arguments)
			if state.toolArgSize > chatMaxToolArgumentsTotal {
				return ErrChatStreamTooLarge
			}
			builder.arguments.WriteString(*delta.Function.Arguments)
			builder.hasArguments = true
		}
		if delta.ID == nil && delta.Type == nil && delta.Function.Name == nil && delta.Function.Arguments == nil {
			return ErrChatMalformedStream
		}
	}
	return nil
}

func (state *chatStreamState) finish() (ChatResult, error) {
	if !state.finishSeen || !state.usageSeen || !state.doneSeen {
		return ChatResult{}, ErrChatMalformedStream
	}
	result := ChatResult{Text: state.text.String(), Usage: state.usage, FinishReason: state.finishReason}
	if len(state.tools) > 0 {
		result.ToolCalls = make([]ChatToolCall, len(state.tools))
	}
	ids := make(map[string]struct{}, len(state.tools))
	for index := 0; index < len(state.tools); index++ {
		builder := state.tools[index]
		if builder == nil || !builder.hasID || !builder.hasType || !builder.hasName || !builder.hasArguments || !safeChatIdentifier(builder.id, chatMaxToolIDBytes) || !safeChatToolName(builder.name.String()) {
			return ChatResult{}, ErrChatMalformedStream
		}
		arguments := []byte(builder.arguments.String())
		if !validJSONObject(arguments, chatMaxToolArgumentsBytes) {
			return ChatResult{}, ErrChatMalformedStream
		}
		if _, duplicate := ids[builder.id]; duplicate {
			return ChatResult{}, ErrChatMalformedStream
		}
		ids[builder.id] = struct{}{}
		call := ChatToolCall{ID: builder.id, Name: builder.name.String(), Arguments: bytes.Clone(arguments)}
		result.ToolCalls[index] = call
	}
	switch state.finishReason {
	case "stop":
		if strings.TrimSpace(result.Text) == "" || len(result.ToolCalls) != 0 {
			return ChatResult{}, ErrChatMalformedStream
		}
	case "tool_calls":
		if result.Text != "" || len(result.ToolCalls) == 0 {
			return ChatResult{}, ErrChatMalformedStream
		}
	default:
		return ChatResult{}, ErrChatMalformedStream
	}
	for _, call := range result.ToolCalls {
		eventCall := ChatToolCall{ID: call.ID, Name: call.Name, Arguments: bytes.Clone(call.Arguments)}
		if err := safeHandleChatEvent(state.handler, state.ctx, ChatEvent{ToolCall: &eventCall}); err != nil {
			return ChatResult{}, err
		}
	}
	return result, nil
}

func safeHandleChatEvent(handler ChatEventHandler, ctx context.Context, event ChatEvent) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrChatHandlerFailed
		}
	}()
	if err := ctx.Err(); err != nil {
		return chatContextError(err)
	}
	if err := handler.HandleChatEvent(ctx, event); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return chatContextError(ctxErr)
		}
		return ErrChatHandlerFailed
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return chatContextError(ctxErr)
	}
	return nil
}

func safeHandleChatProgress(handler ChatProgressHandler, ctx context.Context) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrChatHandlerFailed
		}
	}()
	if err := ctx.Err(); err != nil {
		return chatContextError(err)
	}
	if err := handler.HandleChatProgress(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return chatContextError(ctxErr)
		}
		return ErrChatHandlerFailed
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return chatContextError(ctxErr)
	}
	return nil
}

func chatHTTPStatusError(response *http.Response, apiKey string) error {
	if response == nil || response.Body == nil {
		return ErrChatTransport
	}
	status := response.StatusCode
	requestID := response.Header.Get("x-request-id")
	if status != http.StatusTooManyRequests {
		drainAndClose(response.Body)
		return chatStatusError(status, requestID, apiKey, false)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, chatMaxErrorBodyBytes+1))
	_ = response.Body.Close()
	quotaExceeded := err == nil && len(body) <= chatMaxErrorBodyBytes && chatInsufficientQuota(body)
	clear(body)
	return chatStatusError(status, requestID, apiKey, quotaExceeded)
}

func chatInsufficientQuota(body []byte) bool {
	if len(body) == 0 || !json.Valid(body) {
		return false
	}
	var envelope struct {
		Code  string `json:"code"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	return chatQuotaCode(envelope.Code) || (envelope.Error != nil && chatQuotaCode(envelope.Error.Code))
}

func chatStatusError(status int, requestID, apiKey string, quotaExceeded bool) error {
	providerErr := &ProviderError{StatusCode: status, RequestID: safeRequestID(requestID, apiKey)}
	if quotaExceeded {
		return fmt.Errorf("%w: %w", ErrChatQuotaExceeded, providerErr)
	}
	switch status {
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: %w", ErrChatRateLimited, providerErr)
	case http.StatusInternalServerError, http.StatusServiceUnavailable:
		return fmt.Errorf("%w: %w", ErrChatUnavailable, providerErr)
	default:
		return fmt.Errorf("%w: %w", ErrChatTransport, providerErr)
	}
}

func chatSSEError(providerErr *chatSSEProviderError) error {
	if providerErr == nil {
		return ErrChatMalformedStream
	}
	status := providerErr.Status
	var stringCode string
	if len(providerErr.Code) > 0 && len(providerErr.Code) <= chatMaxProviderErrorCode {
		_ = json.Unmarshal(providerErr.Code, &stringCode)
		switch {
		case chatQuotaCode(stringCode):
			return ErrChatQuotaExceeded
		case chatRateLimitCode(stringCode):
			return ErrChatRateLimited
		case stringCode == "internal_server_error":
			return ErrChatUnavailable
		}
	}
	if status == 0 && len(providerErr.Code) > 0 && len(providerErr.Code) <= chatMaxProviderErrorCode {
		var numeric int
		if json.Unmarshal(providerErr.Code, &numeric) == nil {
			status = numeric
		}
	}
	switch status {
	case http.StatusTooManyRequests:
		return ErrChatRateLimited
	case http.StatusInternalServerError, http.StatusServiceUnavailable:
		return ErrChatUnavailable
	default:
		return ErrChatTransport
	}
}

func chatQuotaCode(code string) bool {
	return code == "insufficient_quota_error" || code == "insufficient_quota"
}

func chatRateLimitCode(code string) bool {
	return code == "rate_limit_exceeded_error" || code == "rate_limit_exceeded"
}

func chatTransportError(ctx context.Context, transportErr error) error {
	if err := ctx.Err(); err != nil {
		return chatContextError(err)
	}
	if errors.Is(transportErr, context.Canceled) {
		return chatContextError(context.Canceled)
	}
	if errors.Is(transportErr, context.DeadlineExceeded) {
		return chatContextError(context.DeadlineExceeded)
	}
	return ErrChatTransport
}

func chatContextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %w", ErrChatTransport, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrChatTransport, context.DeadlineExceeded)
	}
	return ErrChatTransport
}

func chatRetryable(err error) bool {
	return errors.Is(err, ErrChatRateLimited) || errors.Is(err, ErrChatUnavailable)
}

func waitChatRetry(ctx context.Context) error {
	timer := time.NewTimer(chatRetryBackoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return chatContextError(ctx.Err())
	case <-timer.C:
		return nil
	}
}

func validChatFinishReason(reason string) bool {
	switch reason {
	case "stop", "tool_calls":
		return true
	default:
		return false
	}
}

func validJSONObject(value []byte, limit int) bool {
	if len(value) == 0 || len(value) > limit || !json.Valid(value) {
		return false
	}
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}

// validChatJSONDocument validates the complete provider event before typed
// decoding. encoding/json otherwise accepts duplicate object keys, which can
// make security decisions depend on whether a parser keeps the first or last
// value. The provider event is already byte-bounded; depth and container size
// are bounded here as an additional parser resource limit.
func validChatJSONDocument(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') || !consumeChatJSONObject(decoder, 1) {
		return false
	}
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

func consumeChatJSONObject(decoder *json.Decoder, depth int) bool {
	if depth > chatMaxJSONDepth {
		return false
	}
	seen := make(map[string]struct{})
	entries := 0
	for decoder.More() {
		entries++
		if entries > chatMaxJSONEntries {
			return false
		}
		keyToken, err := decoder.Token()
		if err != nil {
			return false
		}
		key, ok := keyToken.(string)
		if !ok {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
		if !consumeChatJSONValue(decoder, depth) {
			return false
		}
	}
	closing, err := decoder.Token()
	return err == nil && closing == json.Delim('}')
}

func consumeChatJSONArray(decoder *json.Decoder, depth int) bool {
	if depth > chatMaxJSONDepth {
		return false
	}
	entries := 0
	for decoder.More() {
		entries++
		if entries > chatMaxJSONEntries || !consumeChatJSONValue(decoder, depth) {
			return false
		}
	}
	closing, err := decoder.Token()
	return err == nil && closing == json.Delim(']')
}

func consumeChatJSONValue(decoder *json.Decoder, depth int) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		switch token.(type) {
		case nil, bool, string, json.Number:
			return true
		default:
			return false
		}
	}
	switch delimiter {
	case '{':
		return consumeChatJSONObject(decoder, depth+1)
	case '[':
		return consumeChatJSONArray(decoder, depth+1)
	default:
		return false
	}
}

func validChatOptionalObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	if len(trimmed) > chatMaxMetadataBytes {
		return false
	}
	return validChatJSONDocument(trimmed)
}

func validChatOptionalString(raw json.RawMessage, maximumBytes int) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return false
	}
	return len(value) <= maximumBytes && utf8.ValidString(value) && !containsNUL(value)
}

func safeChatIdentifier(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' || character == ':' {
			continue
		}
		return false
	}
	return true
}

func safeChatToolName(value string) bool {
	if value == "" || len(value) > chatMaxToolNameBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func containsNUL(value string) bool {
	return strings.IndexByte(value, 0) >= 0
}

func requireChatJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrChatMalformedStream
	}
	return nil
}

func isNilChatHandler(handler ChatEventHandler) bool {
	if handler == nil {
		return true
	}
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (client *ChatClient) String() string { return "SarvamChatClient" }

var _ ChatStreamer = (*ChatClient)(nil)
