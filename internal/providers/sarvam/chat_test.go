package sarvam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const chatTestAPIKey = "chat-test-api-key-never-log"

var _ ChatStreamer = (*ChatClient)(nil)
var _ ChatProgressHandler = (*progressRecordingChatHandler)(nil)

func TestChatStreamExactWireTextUsageAndReasoningIsolation(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.URL.RawQuery != "" {
			t.Errorf("request target = %s %s?%s, want POST /v1/chat/completions", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if got := r.Header.Values(APIKeyHeader); len(got) != 1 || got[0] != chatTestAPIKey {
			t.Errorf("%s = %#v, want one canonical credential", APIKeyHeader, got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q, want text/event-stream", got)
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxRequestBytes+1))
		if err != nil {
			t.Errorf("read chat request: %v", err)
			return
		}
		assertExactChatWireRequest(t, body)
		requestSeen <- struct{}{}

		writeChatSSE(t, w,
			`{"id":"chunk-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"private chain of thought","content":"Hello "},"finish_reason":null}]}`,
			`{"id":"chunk-2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"world"},"finish_reason":"stop"}]}`,
			`{"id":"chunk-3","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":41,"completion_tokens":7,"total_tokens":48}}`,
			"[DONE]",
		)
	}))
	defer server.Close()

	streamer := newChatTestClient(t, server.URL, server.Client())
	handler := &recordingChatHandler{}
	result, err := streamer.StreamChat(context.Background(), ChatRequest{
		Messages: []ChatMessage{
			{Role: ChatRoleSystem, Content: "Answer briefly."},
			{Role: ChatRoleUser, Content: "Where is invoice 1?"},
			{Role: ChatRoleAssistant, ToolCalls: []ChatToolCall{{ID: "call_previous", Name: "lookup_invoice", Arguments: json.RawMessage(`{"invoice_id":"inv_1"}`)}}},
			{Role: ChatRoleTool, ToolCallID: "call_previous", Content: `{"status":"paid"}`},
		},
		Tools: []ChatToolDefinition{{
			Name: "lookup_invoice", Description: "Look up one invoice.",
			Parameters: json.RawMessage(`{"type":"object","properties":{"invoice_id":{"type":"string"}},"required":["invoice_id"]}`),
		}},
	}, handler)
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	select {
	case <-requestSeen:
	default:
		t.Fatal("chat request was not observed")
	}
	if result.Text != "Hello world" {
		t.Fatalf("result text = %q, want %q", result.Text, "Hello world")
	}
	if result.FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", result.FinishReason)
	}
	if len(result.ToolCalls) != 0 {
		t.Fatalf("result tool calls = %#v, want none", result.ToolCalls)
	}
	if result.Usage != (ChatUsage{PromptTokens: 41, CompletionTokens: 7, TotalTokens: 48}) {
		t.Fatalf("result usage = %#v", result.Usage)
	}
	events := handler.snapshot()
	wantEvents := []ChatEvent{{TextDelta: "Hello "}, {TextDelta: "world"}}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("handler events = %#v, want %#v; reasoning_content must never be emitted", events, wantEvents)
	}
}

func TestChatStreamAcceptsBoundedOfficialOptionalMetadata(t *testing.T) {
	for _, fixture := range []string{
		"chat-sse-official-metadata-null.txt",
		"chat-sse-official-metadata-populated.txt",
	} {
		t.Run(fixture, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", fixture))
			if err != nil {
				t.Fatalf("read official contract fixture: %v", err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				writeRawChatSSE(w, string(body))
			}))
			defer server.Close()

			handler := &recordingChatHandler{}
			result, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), handler)
			if err != nil {
				t.Fatalf("StreamChat() error = %v", err)
			}
			if result.Text != "Hello world" || result.FinishReason != "stop" ||
				result.Usage != (ChatUsage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6}) {
				t.Fatalf("StreamChat() result = %#v", result)
			}
			if got := handler.snapshot(); !reflect.DeepEqual(got, []ChatEvent{{TextDelta: "Hello "}, {TextDelta: "world"}}) {
				t.Fatalf("handler events = %#v", got)
			}
		})
	}
}

func TestChatStreamRejectsUnsafeOptionalMetadataAndDuplicateKeys(t *testing.T) {
	validUsage := `{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`
	deepMetadata := `{"leaf":true}`
	for range 20 {
		deepMetadata = `{"nested":` + deepMetadata + `}`
	}
	tests := []struct {
		name  string
		event string
	}{
		{
			name:  "unknown top-level field",
			event: `{"security_context":"must-not-be-ignored","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "unknown delta field",
			event: `{"choices":[{"index":0,"delta":{"system_prompt":"must-not-be-ignored","content":"hello"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "unknown choice field",
			event: `{"choices":[{"index":0,"internal_trace":{"secret":true},"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "duplicate core key",
			event: `{"choices":[{"index":0,"delta":{"content":"first"},"finish_reason":"stop"}],"choices":[{"index":0,"delta":{"content":"second"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "duplicate nested core key",
			event: `{"choices":[{"index":0,"delta":{"content":"first","content":"second"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "duplicate metadata key",
			event: `{"choices":[{"index":0,"logprobs":{"content":[],"content":[]},"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "wrong logprobs shape",
			event: `{"choices":[{"index":0,"logprobs":[],"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "wrong refusal shape",
			event: `{"choices":[{"index":0,"delta":{"refusal":{},"content":"hello"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "wrong legacy function call shape",
			event: `{"choices":[{"index":0,"delta":{"function_call":"unsafe","content":"hello"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "wrong usage details shape",
			event: `{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "oversized refusal",
			event: `{"choices":[{"index":0,"delta":{"refusal":` + string(mustMarshalChatTestJSON(t, strings.Repeat("x", 20<<10))) + `,"content":"hello"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "oversized logprobs",
			event: `{"choices":[{"index":0,"logprobs":{"content":"` + strings.Repeat("x", 20<<10) + `"},"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
		},
		{
			name:  "deep usage details",
			event: `{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				events := []string{test.event, validUsage, "[DONE]"}
				if test.name == "deep usage details" {
					events = []string{
						test.event,
						`{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"completion_tokens_details":` + deepMetadata + `}}`,
						"[DONE]",
					}
				} else if test.name == "wrong usage details shape" {
					events = []string{
						test.event,
						`{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"prompt_tokens_details":[]}}`,
						"[DONE]",
					}
				}
				writeChatSSE(t, w, events...)
			}))
			defer server.Close()

			_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
			if !errors.Is(err, ErrChatMalformedStream) {
				t.Fatalf("StreamChat() error = %v, want ErrChatMalformedStream", err)
			}
		})
	}
}

func TestChatStreamAssemblesFragmentedToolCallsAndReturnsDeepCopies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeChatSSE(t, w,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_lookup","type":"function","function":{"name":"look","arguments":"{\"invoice_"}}]},"finish_reason":null}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_lookup","type":"function","function":{"name":"up_invoice","arguments":"id\":\"inv"}}]},"finish_reason":null}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"_1\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":9,"total_tokens":21}}`,
			"[DONE]",
		)
	}))
	defer server.Close()

	handler := newProgressRecordingChatHandler()
	result, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), handler)
	if err != nil {
		t.Fatalf("StreamChat() error = %v", err)
	}
	if result.Text != "" || len(result.ToolCalls) != 1 {
		t.Fatalf("result = %#v, want one tool call and no text", result)
	}
	if result.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q, want tool_calls", result.FinishReason)
	}
	if got := handler.progressCalls.Load(); got != 1 {
		t.Fatalf("tool progress callbacks = %d, want exactly 1", got)
	}
	call := result.ToolCalls[0]
	if call.ID != "call_lookup" || call.Name != "lookup_invoice" {
		t.Fatalf("tool identity = %#v", call)
	}
	if string(call.Arguments) != `{"invoice_id":"inv_1"}` {
		t.Fatalf("tool arguments = %s", call.Arguments)
	}
	events := handler.snapshot()
	if len(events) != 1 || events[0].ToolCall == nil {
		t.Fatalf("handler events = %#v, want one completed tool call", events)
	}
	if !reflect.DeepEqual(*events[0].ToolCall, call) {
		t.Fatalf("handler tool = %#v, result tool = %#v", *events[0].ToolCall, call)
	}
	events[0].ToolCall.Arguments[0] = '['
	if string(result.ToolCalls[0].Arguments) != `{"invoice_id":"inv_1"}` {
		t.Fatal("handler event and ChatResult share mutable tool argument storage")
	}
	result.ToolCalls[0].Arguments[0] = '['
	if fresh := handler.snapshot(); len(fresh) != 1 || fresh[0].ToolCall == nil || string(fresh[0].ToolCall.Arguments) != `{"invoice_id":"inv_1"}` {
		t.Fatal("ChatResult and handler-owned tool call share mutable argument storage")
	}
}

func TestChatStreamRoundTripsRepeatedToolNamesWithUniqueIDs(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if calls.Add(1) == 1 {
			writeChatSSE(t, w,
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_invoice_1","type":"function","function":{"name":"get_invoice","arguments":"{\"invoice_id\":\"inv_1\"}"}},{"index":1,"id":"call_invoice_2","type":"function","function":{"name":"get_invoice","arguments":"{\"invoice_id\":\"inv_2\"}"}}]},"finish_reason":"tool_calls"}]}`,
				`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
				"[DONE]",
			)
			return
		}

		var wire struct {
			Messages []struct {
				Role      ChatRole `json:"role"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Errorf("decode repeated-tool request: %v", err)
			return
		}
		assistant := wire.Messages[2]
		if assistant.Role != ChatRoleAssistant || len(assistant.ToolCalls) != 2 ||
			assistant.ToolCalls[0].ID != "call_invoice_1" || assistant.ToolCalls[1].ID != "call_invoice_2" ||
			assistant.ToolCalls[0].Function.Name != "get_invoice" || assistant.ToolCalls[1].Function.Name != "get_invoice" ||
			assistant.ToolCalls[0].Function.Arguments == assistant.ToolCalls[1].Function.Arguments {
			t.Errorf("repeated tool calls did not round-trip: %#v", assistant)
		}
		writeChatSSE(t, w,
			`{"choices":[{"index":0,"delta":{"content":"both invoices loaded"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11}}`,
			"[DONE]",
		)
	}))
	defer server.Close()

	client := newChatTestClient(t, server.URL, server.Client())
	first, err := client.StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
	if err != nil {
		t.Fatalf("first StreamChat() error = %v", err)
	}
	if len(first.ToolCalls) != 2 || first.ToolCalls[0].Name != "get_invoice" || first.ToolCalls[1].Name != "get_invoice" || first.ToolCalls[0].ID == first.ToolCalls[1].ID {
		t.Fatalf("repeated-name result = %#v", first.ToolCalls)
	}
	request := validChatRequest()
	request.Messages = append(request.Messages,
		ChatMessage{Role: ChatRoleAssistant, ToolCalls: first.ToolCalls},
		ChatMessage{Role: ChatRoleTool, ToolCallID: first.ToolCalls[0].ID, Content: `{"invoice_id":"inv_1","ok":true}`},
		ChatMessage{Role: ChatRoleTool, ToolCallID: first.ToolCalls[1].ID, Content: `{"invoice_id":"inv_2","ok":true}`},
	)
	second, err := client.StreamChat(context.Background(), request, &recordingChatHandler{})
	if err != nil || second.Text != "both invoices loaded" {
		t.Fatalf("second StreamChat() = (%#v, %v)", second, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
}

func TestChatStreamReportsProgressOnceAndStreamsTextBeforeTerminal(t *testing.T) {
	reasoningWritten := make(chan struct{})
	contentWritten := make(chan struct{})
	releaseContent := make(chan struct{})
	releaseTerminal := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"private\"},\"finish_reason\":null}]}\n\n")
		flushChatResponse(w)
		close(reasoningWritten)
		<-releaseContent

		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		flushChatResponse(w)
		close(contentWritten)
		<-releaseTerminal

		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flushChatResponse(w)
	}))
	defer server.Close()
	defer closeIfOpen(releaseContent)
	defer closeIfOpen(releaseTerminal)

	handler := newProgressRecordingChatHandler()
	type outcome struct {
		result ChatResult
		err    error
	}
	client := newChatTestClient(t, server.URL, server.Client())
	finished := make(chan outcome, 1)
	go func() {
		result, err := client.StreamChat(context.Background(), validChatRequest(), handler)
		finished <- outcome{result: result, err: err}
	}()

	select {
	case <-reasoningWritten:
	case <-time.After(time.Second):
		t.Fatal("reasoning-only event was not written")
	}
	select {
	case <-handler.progress:
		t.Fatal("role/reasoning-only delta reported visible-output progress")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseContent)
	select {
	case <-contentWritten:
	case <-time.After(time.Second):
		t.Fatal("content delta was not written")
	}
	select {
	case <-handler.progress:
	case <-time.After(time.Second):
		t.Fatal("first visible content delta did not report progress")
	}
	select {
	case <-handler.eventRecorded:
	case <-time.After(time.Second):
		t.Fatal("first visible content delta was not recorded")
	}
	if events := handler.snapshot(); !reflect.DeepEqual(events, []ChatEvent{{TextDelta: "hello"}}) {
		t.Fatalf("incremental content before terminal = %#v, want first accepted delta", events)
	}
	close(releaseTerminal)
	select {
	case got := <-finished:
		if got.err != nil || got.result.Text != "hello world" {
			t.Fatalf("StreamChat() = (%#v, %v)", got.result, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("StreamChat did not finish after terminal events")
	}
	if got := handler.progressCalls.Load(); got != 1 {
		t.Fatalf("progress callbacks = %d, want exactly 1", got)
	}
	if events := handler.snapshot(); !reflect.DeepEqual(events, []ChatEvent{{TextDelta: "hello"}, {TextDelta: " world"}}) {
		t.Fatalf("handler events = %#v, want each delta exactly once", events)
	}
}

func TestChatStreamCancellationAndDeadline(t *testing.T) {
	for _, test := range []struct {
		name    string
		context func() (context.Context, context.CancelFunc)
		wantErr error
	}{
		{
			name: "cancellation",
			context: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			wantErr: context.Canceled,
		},
		{
			name: "deadline",
			context: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 40*time.Millisecond)
			},
			wantErr: context.DeadlineExceeded,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()

			ctx, cancel := test.context()
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(ctx, validChatRequest(), &recordingChatHandler{})
				result <- err
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("chat request did not start")
			}
			if test.wantErr == context.Canceled {
				cancel()
			}
			select {
			case err := <-result:
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("StreamChat() error = %v, want %v", err, test.wantErr)
				}
			case <-time.After(time.Second):
				t.Fatal("StreamChat did not honor caller context")
			}
		})
	}
}

func TestChatStreamRetries429And503ExactlyOnceBeforeEvents(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			var attemptMu sync.Mutex
			var firstAttempt time.Time
			var secondAttempt time.Time
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					attemptMu.Lock()
					firstAttempt = time.Now()
					attemptMu.Unlock()
					w.Header().Set("x-request-id", "retry-safe-id")
					w.WriteHeader(status)
					_, _ = io.WriteString(w, "provider-secret-body")
					return
				}
				attemptMu.Lock()
				secondAttempt = time.Now()
				attemptMu.Unlock()
				writeChatSSE(t, w,
					`{"choices":[{"index":0,"delta":{"content":"recovered"},"finish_reason":"stop"}]}`,
					`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
					"[DONE]",
				)
			}))
			defer server.Close()

			result, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
			if err != nil || result.Text != "recovered" {
				t.Fatalf("StreamChat() = (%#v, %v), want recovered result", result, err)
			}
			if got := calls.Load(); got != 2 {
				t.Fatalf("provider calls = %d, want exactly 2", got)
			}
			attemptMu.Lock()
			elapsed := secondAttempt.Sub(firstAttempt)
			attemptMu.Unlock()
			if elapsed < 40*time.Millisecond {
				t.Fatalf("retry delay = %v, want bounded backoff before retry", elapsed)
			}
		})
	}

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
	if !errors.Is(err, ErrChatUnavailable) {
		t.Fatalf("double 503 error = %v, want ErrChatUnavailable", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("double 503 calls = %d, want retry budget of one", got)
	}
}

func TestChatRetryBackoffHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitChatRetry(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitChatRetry() error = %v, want context.Canceled", err)
	}
}

func TestChatStreamNeverRetriesAfterAcceptedEvent(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeChatSSE(t, w,
			`{"choices":[{"index":0,"delta":{"content":"accepted"},"finish_reason":null}]}`,
			`{"error":{"status":503,"message":"provider-sensitive-detail"}}`,
		)
	}))
	defer server.Close()

	handler := &recordingChatHandler{}
	_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), handler)
	if !errors.Is(err, ErrChatUnavailable) {
		t.Fatalf("post-event provider error = %v, want ErrChatUnavailable", err)
	}
	if strings.Contains(err.Error(), "provider-sensitive-detail") {
		t.Fatalf("error leaked provider detail: %q", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls after accepted event = %d, want 1", got)
	}
	if events := handler.snapshot(); !reflect.DeepEqual(events, []ChatEvent{{TextDelta: "accepted"}}) {
		t.Fatalf("accepted incremental events = %#v", events)
	}
}

func TestChatStreamDoesNotRetryInsufficientQuota429(t *testing.T) {
	for _, code := range []string{"insufficient_quota_error", "insufficient_quota"} {
		t.Run(code, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = fmt.Fprintf(w, `{"error":{"code":%q,"message":"provider-sensitive-billing-detail"}}`, code)
			}))
			defer server.Close()

			_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
			if !errors.Is(err, ErrChatQuotaExceeded) {
				t.Fatalf("insufficient quota error = %v, want ErrChatQuotaExceeded", err)
			}
			if strings.Contains(err.Error(), "provider-sensitive") {
				t.Fatalf("quota error leaked provider body: %q", err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("insufficient quota calls = %d, want no retry", got)
			}
		})
	}
}

func TestChatStreamMapsStringOnlySSEProviderCodes(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		wantErr   error
		wantCalls int32
	}{
		{name: "current quota code", code: "insufficient_quota_error", wantErr: ErrChatQuotaExceeded, wantCalls: 1},
		{name: "legacy quota code", code: "insufficient_quota", wantErr: ErrChatQuotaExceeded, wantCalls: 1},
		{name: "current rate limit code", code: "rate_limit_exceeded_error", wantCalls: 2},
		{name: "legacy rate limit code", code: "rate_limit_exceeded", wantCalls: 2},
		{name: "internal server code", code: "internal_server_error", wantCalls: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					writeChatSSE(t, w, fmt.Sprintf(`{"error":{"code":%q}}`, test.code))
					return
				}
				writeChatSSE(t, w,
					`{"choices":[{"index":0,"delta":{"content":"recovered"},"finish_reason":"stop"}]}`,
					`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
					"[DONE]",
				)
			}))
			defer server.Close()

			result, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("StreamChat() error = %v, want %v", err, test.wantErr)
				}
			} else if err != nil || result.Text != "recovered" {
				t.Fatalf("StreamChat() = (%#v, %v), want recovered result", result, err)
			}
			if got := calls.Load(); got != test.wantCalls {
				t.Fatalf("provider calls = %d, want %d", got, test.wantCalls)
			}
		})
	}
}

func TestChatStreamRequiresCoherentFinishUsageAndDone(t *testing.T) {
	validText := `{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`
	validUsage := `{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`
	tests := []struct {
		name       string
		events     []string
		wantEvents []ChatEvent
	}{
		{name: "missing finish", events: []string{`{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`, validUsage, "[DONE]"}, wantEvents: []ChatEvent{{TextDelta: "hello"}}},
		{name: "length finish", events: []string{`{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"length"}]}`, validUsage, "[DONE]"}},
		{name: "content filter finish", events: []string{`{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"content_filter"}]}`, validUsage, "[DONE]"}},
		{name: "duplicate finish", events: []string{validText, `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, validUsage, "[DONE]"}, wantEvents: []ChatEvent{{TextDelta: "hello"}}},
		{name: "choice after finish", events: []string{validText, `{"choices":[{"index":0,"delta":{"content":"late"},"finish_reason":null}]}`, validUsage, "[DONE]"}, wantEvents: []ChatEvent{{TextDelta: "hello"}}},
		{name: "stop without text", events: []string{`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, validUsage, "[DONE]"}},
		{name: "stop with tools", events: []string{`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_one","type":"function","function":{"name":"lookup_invoice","arguments":"{}"}}]},"finish_reason":"stop"}]}`, validUsage, "[DONE]"}},
		{name: "tool calls with text", events: []string{`{"choices":[{"index":0,"delta":{"content":"mixed","tool_calls":[{"index":0,"id":"call_one","type":"function","function":{"name":"lookup_invoice","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`, validUsage, "[DONE]"}},
		{name: "tool finish without tools", events: []string{`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`, validUsage, "[DONE]"}},
		{name: "usage before finish", events: []string{validUsage, validText, "[DONE]"}},
		{name: "usage without explicit empty choices", events: []string{validText, `{"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`, "[DONE]"}, wantEvents: []ChatEvent{{TextDelta: "hello"}}},
		{name: "usage with null choices", events: []string{validText, `{"choices":null,"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`, "[DONE]"}, wantEvents: []ChatEvent{{TextDelta: "hello"}}},
		{name: "duplicate usage", events: []string{validText, validUsage, validUsage, "[DONE]"}, wantEvents: []ChatEvent{{TextDelta: "hello"}}},
		{name: "extra choice", events: []string{`{"choices":[{"index":0,"delta":{"content":"a"},"finish_reason":"stop"},{"index":1,"delta":{"content":"b"},"finish_reason":null}]}`, validUsage, "[DONE]"}},
		{name: "wrong choice index", events: []string{`{"choices":[{"index":1,"delta":{"content":"a"},"finish_reason":"stop"}]}`, validUsage, "[DONE]"}},
		{name: "incoherent usage", events: []string{validText, `{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":99}}`, "[DONE]"}, wantEvents: []ChatEvent{{TextDelta: "hello"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeChatSSE(t, w, test.events...)
			}))
			defer server.Close()
			handler := &recordingChatHandler{}
			_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), handler)
			if !errors.Is(err, ErrChatMalformedStream) {
				t.Fatalf("StreamChat() error = %v, want ErrChatMalformedStream", err)
			}
			if events := handler.snapshot(); len(events) != len(test.wantEvents) || (len(events) != 0 && !reflect.DeepEqual(events, test.wantEvents)) {
				t.Fatalf("incremental handler events = %#v, want %#v", events, test.wantEvents)
			}
		})
	}
}

func TestChatStreamDoneTerminatesWithoutWaitingForHTTPEOF(t *testing.T) {
	doneWritten := make(chan struct{})
	peerClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeChatSSE(t, w,
			`{"choices":[{"index":0,"delta":{"content":"complete"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`,
			"[DONE]",
		)
		close(doneWritten)
		<-r.Context().Done()
		close(peerClosed)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := newChatTestClient(t, server.URL, server.Client())
	type outcome struct {
		result ChatResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, err := client.StreamChat(ctx, validChatRequest(), &recordingChatHandler{})
		finished <- outcome{result: result, err: err}
	}()

	select {
	case <-doneWritten:
	case <-time.After(time.Second):
		t.Fatal("server did not write terminal marker")
	}
	select {
	case got := <-finished:
		if got.err != nil || got.result.Text != "complete" {
			t.Fatalf("StreamChat() = (%#v, %v)", got.result, got.err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("StreamChat waited for HTTP EOF after terminal [DONE]")
	}
	select {
	case <-peerClosed:
	case <-time.After(time.Second):
		t.Fatal("stream response body was not closed after [DONE]")
	}
}

func TestChatStreamAcceptsCRLFCommentsAndMultilineData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		writeRawChatSSE(w, ": bounded heartbeat\r\n"+
			"data: {\"choices\":[{\"index\":0,\r\n"+
			"data: \"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\r\n\r\n"+
			": terminal metadata follows\r\n"+
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\r\n\r\n"+
			"data: [DONE]\r\n\r\n")
	}))
	defer server.Close()

	handler := &recordingChatHandler{}
	result, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), handler)
	if err != nil || result.Text != "hello" || result.FinishReason != "stop" {
		t.Fatalf("CRLF multiline StreamChat() = (%#v, %v)", result, err)
	}
	if events := handler.snapshot(); len(events) != 1 || events[0].TextDelta != "hello" {
		t.Fatalf("CRLF multiline events = %#v", events)
	}
}

func TestChatStreamRequiresSSEContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	defer server.Close()
	_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
	if !errors.Is(err, ErrChatMalformedStream) {
		t.Fatalf("non-SSE content type error = %v, want ErrChatMalformedStream", err)
	}
}

func TestChatStreamRejectsMalformedAndOversizedSSE(t *testing.T) {
	tests := []struct {
		name      string
		writeBody func(http.ResponseWriter)
		wantErr   error
	}{
		{
			name: "malformed JSON",
			writeBody: func(w http.ResponseWriter) {
				writeRawChatSSE(w, "data: {\n\n")
			},
			wantErr: ErrChatMalformedStream,
		},
		{
			name: "oversized line",
			writeBody: func(w http.ResponseWriter) {
				writeRawChatSSE(w, "data: "+strings.Repeat("x", chatMaxSSELineBytes+1)+"\n\n")
			},
			wantErr: ErrChatStreamTooLarge,
		},
		{
			name: "oversized event",
			writeBody: func(w http.ResponseWriter) {
				line := "data: " + strings.Repeat("x", chatMaxSSELineBytes/2) + "\n"
				writeRawChatSSE(w, line+line+line+"\n")
			},
			wantErr: ErrChatStreamTooLarge,
		},
		{
			name: "missing usage",
			writeBody: func(w http.ResponseWriter) {
				writeRawChatSSE(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n"+
					"data: [DONE]\n\n")
			},
			wantErr: ErrChatMalformedStream,
		},
		{
			name: "missing done",
			writeBody: func(w http.ResponseWriter) {
				writeRawChatSSE(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n"+
					"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
			},
			wantErr: ErrChatMalformedStream,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				test.writeBody(w)
			}))
			defer server.Close()
			_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("StreamChat() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestChatRequestTokenBoundsAndSanitizedProviderErrors(t *testing.T) {
	var calls atomic.Int32
	var observed []int
	var observedMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		observedMu.Lock()
		observed = append(observed, payload.MaxTokens)
		observedMu.Unlock()
		writeChatSSE(t, w,
			`{"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			"[DONE]",
		)
	}))
	defer server.Close()
	streamer := newChatTestClient(t, server.URL, server.Client())

	for _, maxTokens := range []int{0, 120, 220} {
		request := validChatRequest()
		request.MaxTokens = maxTokens
		if _, err := streamer.StreamChat(context.Background(), request, &recordingChatHandler{}); err != nil {
			t.Fatalf("StreamChat(max_tokens=%d) error = %v", maxTokens, err)
		}
	}
	for _, maxTokens := range []int{-1, 119, 221} {
		request := validChatRequest()
		request.MaxTokens = maxTokens
		if _, err := streamer.StreamChat(context.Background(), request, &recordingChatHandler{}); !errors.Is(err, ErrChatInvalidRequest) {
			t.Fatalf("StreamChat(max_tokens=%d) error = %v, want ErrChatInvalidRequest", maxTokens, err)
		}
	}
	observedMu.Lock()
	gotTokens := append([]int(nil), observed...)
	observedMu.Unlock()
	if !reflect.DeepEqual(gotTokens, []int{180, 120, 220}) {
		t.Fatalf("wire max_tokens = %v, want [180 120 220]", gotTokens)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("provider calls = %d, want only three valid requests", got)
	}

	const providerBody = "provider-sensitive-response-body"
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-request-id", chatTestAPIKey)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, providerBody)
	}))
	defer errorServer.Close()
	_, err := newChatTestClient(t, errorServer.URL, errorServer.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
	if err == nil || strings.Contains(err.Error(), providerBody) || strings.Contains(err.Error(), chatTestAPIKey) {
		t.Fatalf("unsanitized provider error = %v", err)
	}
}

func TestChatStreamBoundsAggregateTextToolArgumentsAndEventCount(t *testing.T) {
	t.Run("text aggregate", func(t *testing.T) {
		chunk := strings.Repeat("t", chatMaxTextBytes/2)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeChatSSE(t, w,
				fmt.Sprintf(`{"choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`, chunk),
				fmt.Sprintf(`{"choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`, chunk),
				fmt.Sprintf(`{"choices":[{"index":0,"delta":{"content":%q},"finish_reason":"stop"}]}`, "x"),
			)
		}))
		defer server.Close()
		_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
		if !errors.Is(err, ErrChatStreamTooLarge) {
			t.Fatalf("text aggregate error = %v, want ErrChatStreamTooLarge", err)
		}
	})

	t.Run("tool argument aggregate", func(t *testing.T) {
		fragment := strings.Repeat("a", chatMaxToolArgumentsBytes/2)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeChatSSE(t, w,
				fmt.Sprintf(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_big","type":"function","function":{"name":"lookup_invoice","arguments":%q}}]},"finish_reason":null}]}`, fragment),
				fmt.Sprintf(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":%q}}]},"finish_reason":null}]}`, fragment),
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"x"}}]},"finish_reason":"tool_calls"}]}`,
			)
		}))
		defer server.Close()
		_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
		if !errors.Is(err, ErrChatStreamTooLarge) {
			t.Fatalf("tool argument aggregate error = %v, want ErrChatStreamTooLarge", err)
		}
	})

	t.Run("event count", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			for range chatMaxSSEEvents + 1 {
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":null}]}\n\n")
			}
		}))
		defer server.Close()
		_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
		if !errors.Is(err, ErrChatStreamTooLarge) {
			t.Fatalf("event count error = %v, want ErrChatStreamTooLarge", err)
		}
	})

	t.Run("request aggregate", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
		defer server.Close()
		request := validChatRequest()
		request.Messages[1].Content = strings.Repeat("x", chatMaxMessageBytes+1)
		_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), request, &recordingChatHandler{})
		if !errors.Is(err, ErrChatInvalidRequest) {
			t.Fatalf("oversized request error = %v, want ErrChatInvalidRequest", err)
		}
		if calls.Load() != 0 {
			t.Fatal("oversized request reached provider")
		}
	})
}

func TestChatStreamRejectsBlankDuplicateAndInconsistentToolFragments(t *testing.T) {
	tests := []struct {
		name   string
		events []string
	}{
		{
			name: "blank id",
			events: []string{
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"","type":"function","function":{"name":"lookup_invoice","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
			},
		},
		{
			name: "inconsistent id",
			events: []string{
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_one","type":"function","function":{"name":"lookup_","arguments":"{"}}]},"finish_reason":null}]}`,
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_two","function":{"name":"invoice","arguments":"}"}}]},"finish_reason":"tool_calls"}]}`,
			},
		},
		{
			name: "duplicate ids",
			events: []string{
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_same","type":"function","function":{"name":"lookup_invoice","arguments":"{}"}},{"index":1,"id":"call_same","type":"function","function":{"name":"list_invoices","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
			},
		},
		{
			name: "non-object arguments",
			events: []string{
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_array","type":"function","function":{"name":"lookup_invoice","arguments":"[]"}}]},"finish_reason":"tool_calls"}]}`,
			},
		},
		{
			name: "index gap",
			events: []string{
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_gap","type":"function","function":{"name":"lookup_invoice","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				events := append([]string(nil), test.events...)
				events = append(events,
					`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
					"[DONE]",
				)
				writeChatSSE(t, w, events...)
			}))
			defer server.Close()
			_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
			if !errors.Is(err, ErrChatMalformedStream) {
				t.Fatalf("tool fragment error = %v, want ErrChatMalformedStream", err)
			}
		})
	}
}

func TestChatStreamRejectsRedirectsAndContainsHandlerFailures(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", destination.URL+"/credential-target")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	_, err := newChatTestClient(t, origin.URL, origin.Client()).StreamChat(context.Background(), validChatRequest(), &recordingChatHandler{})
	if !errors.Is(err, ErrChatTransport) || destinationCalls.Load() != 0 {
		t.Fatalf("redirect result = (%v, destination calls %d), want contained transport failure", err, destinationCalls.Load())
	}

	for _, test := range []struct {
		name    string
		handler ChatEventHandler
	}{
		{name: "error", handler: ChatEventHandlerFunc(func(context.Context, ChatEvent) error { return errors.New("handler-sensitive-detail") })},
		{name: "panic", handler: ChatEventHandlerFunc(func(context.Context, ChatEvent) error { panic("handler-panic-sensitive-detail") })},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeChatSSE(t, w,
					`{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
					`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
					"[DONE]",
				)
			}))
			defer server.Close()
			_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), test.handler)
			if !errors.Is(err, ErrChatHandlerFailed) || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("handler failure = %v, want sanitized ErrChatHandlerFailed", err)
			}
		})
	}
}

func TestChatStreamContainsProgressHandlerFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		panicValue bool
	}{
		{name: "error"},
		{name: "panic", panicValue: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeChatSSE(t, w,
					`{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
					`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
					"[DONE]",
				)
			}))
			defer server.Close()
			handler := &failingProgressChatHandler{panicValue: test.panicValue}
			_, err := newChatTestClient(t, server.URL, server.Client()).StreamChat(context.Background(), validChatRequest(), handler)
			if !errors.Is(err, ErrChatHandlerFailed) || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("progress handler failure = %v, want sanitized ErrChatHandlerFailed", err)
			}
			if got := handler.eventCalls.Load(); got != 0 {
				t.Fatalf("terminal event callbacks = %d, want none after progress failure", got)
			}
		})
	}
}

func TestChatClientRequiresExplicitDependenciesAndContext(t *testing.T) {
	if client, err := NewChatClient(nil); client != nil || !errors.Is(err, ErrChatClientRequired) {
		t.Fatalf("NewChatClient(nil) = (%v, %v)", client, err)
	}
	streamer := newChatTestClient(t, "http://127.0.0.1:1", &http.Client{})
	var nilContext context.Context
	if _, err := streamer.StreamChat(nilContext, validChatRequest(), &recordingChatHandler{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil context error = %v, want ErrContextRequired", err)
	}
	if _, err := streamer.StreamChat(context.Background(), validChatRequest(), nil); !errors.Is(err, ErrChatHandlerRequired) {
		t.Fatalf("nil handler error = %v, want ErrChatHandlerRequired", err)
	}
	var typedNil *recordingChatHandler
	if _, err := streamer.StreamChat(context.Background(), validChatRequest(), typedNil); !errors.Is(err, ErrChatHandlerRequired) {
		t.Fatalf("typed nil handler error = %v, want ErrChatHandlerRequired", err)
	}
}

func assertExactChatWireRequest(t *testing.T, body []byte) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode chat wire JSON: %v", err)
	}
	for _, key := range []string{"model", "messages", "tools", "stream", "n", "reasoning_effort", "temperature", "max_tokens"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("chat wire request missing %q: %s", key, body)
		}
	}
	if len(payload) != 8 {
		t.Fatalf("chat wire keys = %#v, want exactly 8", payload)
	}
	if payload["model"] != "sarvam-105b" || payload["stream"] != true || payload["n"] != float64(1) || payload["temperature"] != 0.2 || payload["max_tokens"] != float64(180) {
		t.Fatalf("fixed chat policy changed: %#v", payload)
	}
	if payload["reasoning_effort"] != nil {
		t.Fatalf("reasoning_effort = %#v, want explicit null", payload["reasoning_effort"])
	}
	if _, ok := payload["stream_options"]; ok {
		t.Fatalf("stream_options is not part of Sarvam's documented request schema: %#v", payload["stream_options"])
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 4 {
		t.Fatalf("wire messages = %#v", payload["messages"])
	}
	assistant := messages[2].(map[string]any)
	toolCalls := assistant["tool_calls"].([]any)
	function := toolCalls[0].(map[string]any)["function"].(map[string]any)
	if function["name"] != "lookup_invoice" || function["arguments"] != `{"invoice_id":"inv_1"}` {
		t.Fatalf("assistant tool call did not round-trip: %#v", function)
	}
	toolResult := messages[3].(map[string]any)
	if toolResult["role"] != "tool" || toolResult["tool_call_id"] != "call_previous" || toolResult["content"] != `{"status":"paid"}` {
		t.Fatalf("tool result did not round-trip: %#v", toolResult)
	}
}

func validChatRequest() ChatRequest {
	return ChatRequest{
		Messages: []ChatMessage{
			{Role: ChatRoleSystem, Content: "Answer briefly."},
			{Role: ChatRoleUser, Content: "Hello"},
		},
		Tools: []ChatToolDefinition{{
			Name: "lookup_invoice", Description: "Look up an invoice.",
			Parameters: json.RawMessage(`{"type":"object","properties":{"invoice_id":{"type":"string"}}}`),
		}},
	}
}

func newChatTestClient(t *testing.T, baseURL string, httpClient *http.Client) *ChatClient {
	t.Helper()
	providerClient, err := NewClient(Config{APIKey: chatTestAPIKey, BaseURL: baseURL}, httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client, err := NewChatClient(providerClient)
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}
	return client
}

func writeChatSSE(t *testing.T, w http.ResponseWriter, events ...string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range events {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", event); err != nil {
			t.Errorf("write SSE event: %v", err)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

func mustMarshalChatTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal chat test JSON: %v", err)
	}
	return payload
}

func writeRawChatSSE(w http.ResponseWriter, body string) {
	_, _ = io.WriteString(w, body)
}

func flushChatResponse(w http.ResponseWriter) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func closeIfOpen(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

type recordingChatHandler struct {
	mu     sync.Mutex
	events []ChatEvent
}

type progressRecordingChatHandler struct {
	recordingChatHandler
	progress      chan struct{}
	eventRecorded chan struct{}
	progressCalls atomic.Int32
}

func newProgressRecordingChatHandler() *progressRecordingChatHandler {
	return &progressRecordingChatHandler{
		progress:      make(chan struct{}, 1),
		eventRecorded: make(chan struct{}, 1),
	}
}

func (handler *progressRecordingChatHandler) HandleChatEvent(ctx context.Context, event ChatEvent) error {
	if err := handler.recordingChatHandler.HandleChatEvent(ctx, event); err != nil {
		return err
	}
	select {
	case handler.eventRecorded <- struct{}{}:
	default:
	}
	return nil
}

func (handler *progressRecordingChatHandler) HandleChatProgress(context.Context) error {
	handler.progressCalls.Add(1)
	select {
	case handler.progress <- struct{}{}:
	default:
	}
	return nil
}

type failingProgressChatHandler struct {
	panicValue bool
	eventCalls atomic.Int32
}

func (handler *failingProgressChatHandler) HandleChatEvent(context.Context, ChatEvent) error {
	handler.eventCalls.Add(1)
	return nil
}

func (handler *failingProgressChatHandler) HandleChatProgress(context.Context) error {
	if handler.panicValue {
		panic("progress-handler-panic-sensitive-detail")
	}
	return errors.New("progress-handler-error-sensitive-detail")
}

func (handler *recordingChatHandler) HandleChatEvent(_ context.Context, event ChatEvent) error {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	cloned := ChatEvent{TextDelta: event.TextDelta}
	if event.ToolCall != nil {
		call := *event.ToolCall
		call.Arguments = bytes.Clone(event.ToolCall.Arguments)
		cloned.ToolCall = &call
	}
	handler.events = append(handler.events, cloned)
	return nil
}

func (handler *recordingChatHandler) snapshot() []ChatEvent {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	result := make([]ChatEvent, len(handler.events))
	for index, event := range handler.events {
		result[index] = ChatEvent{TextDelta: event.TextDelta}
		if event.ToolCall != nil {
			call := *event.ToolCall
			call.Arguments = bytes.Clone(event.ToolCall.Arguments)
			result[index].ToolCall = &call
		}
	}
	return result
}
