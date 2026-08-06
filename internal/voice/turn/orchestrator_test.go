package turn

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"invoice-backend/internal/providers/sarvam"
)

func TestOrchestratorStreamsFinalOnlyTurnWithBoundedContext(t *testing.T) {
	t.Parallel()

	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, request sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "नमस्ते "}); err != nil {
				t.Fatalf("first HandleChatEvent() error = %v", err)
			}
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "दुनिया"}); err != nil {
				t.Fatalf("second HandleChatEvent() error = %v", err)
			}
			return sarvam.ChatResult{
				Text:         "नमस्ते दुनिया",
				FinishReason: "stop",
				Usage:        sarvam.ChatUsage{PromptTokens: 30, CompletionTokens: 5, TotalTokens: 35},
			}, nil
		},
	}}
	sink := &recordingTextHandler{}
	tools := &fakeToolExecutor{definitions: []sarvam.ChatToolDefinition{{
		Name: "list_invoices", Description: "List invoices", Parameters: json.RawMessage(`{"type":"object"}`),
	}}}
	turns := make([]CompletedTurn, 8)
	for index := range turns {
		turns[index] = CompletedTurn{
			User:      "user-" + string(rune('a'+index)),
			Assistant: "assistant-" + string(rune('a'+index)),
		}
	}

	orchestrator, err := NewOrchestrator(OrchestratorConfig{
		Context: context.Background(),
		Chat:    chat,
		Tools:   tools,
		Window: ConversationWindow{
			SystemPolicy:    "Billeif voice policy",
			BusinessContext: "business=b-1; branch=br-1",
			Summary:         "The user asked about receivables.",
			Turns:           turns,
		},
		TextHandler: sink,
	})
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })

	result, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{
		Text:             "आज की बिक्री बताओ",
		DetectedLanguage: "hi-IN",
		ResponseLanguage: "hi-IN",
	})
	if err != nil {
		t.Fatalf("HandleFinal() error = %v (chat calls=%d requests=%#v)", err, chat.CallCount(), chat.Requests())
	}
	if result.Text != "नमस्ते दुनिया" {
		t.Fatalf("result text = %q", result.Text)
	}
	if result.Usage != (sarvam.ChatUsage{PromptTokens: 30, CompletionTokens: 5, TotalTokens: 35}) {
		t.Fatalf("usage = %#v", result.Usage)
	}
	if got := sink.joined(); got != result.Text {
		t.Fatalf("streamed text = %q, want %q", got, result.Text)
	}
	if got := sink.snapshot(); !reflect.DeepEqual(got, []string{"नमस्ते ", "दुनिया"}) {
		t.Fatalf("incremental deltas = %#v", got)
	}

	requests := chat.Requests()
	if len(requests) != 1 {
		t.Fatalf("chat requests = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.MaxTokens != DefaultVoiceMaxTokens {
		t.Fatalf("max tokens = %d, want %d", request.MaxTokens, DefaultVoiceMaxTokens)
	}
	if !reflect.DeepEqual(request.Tools, tools.definitions) {
		t.Fatalf("tool definitions = %#v", request.Tools)
	}
	joined := chatMessageText(request.Messages)
	for _, forbidden := range []string{"user-a", "assistant-a", "user-b", "assistant-b"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("bounded context leaked old turn %q in %q", forbidden, joined)
		}
	}
	for _, required := range []string{
		"Billeif voice policy", "business=b-1; branch=br-1",
		"The user asked about receivables.", "user-c", "assistant-h",
		"आज की बिक्री बताओ", "hi-IN", "one to three spoken sentences",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("request context missing %q in %q", required, joined)
		}
	}
}

func TestOrchestratorFencesCompletionUntilGenerationPlaybackLifecycleFinishes(t *testing.T) {
	t.Parallel()

	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "spoken answer"}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{
				Text: "spoken answer", FinishReason: "stop",
				Usage: sarvam.ChatUsage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6},
			}, nil
		},
	}}
	handler := newLifecycleTextHandler()
	orchestrator := mustTestOrchestrator(t, chat, nil, handler)
	final := FinalTranscript{
		Text: "authoritative transcript", ProviderLanguage: "fr-FR",
		DetectedLanguage: "", ResponseLanguage: "ta-IN",
		SpeechEndedAt: time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC),
		STTFinalAt:    time.Date(2026, 8, 7, 8, 0, 1, 0, time.UTC),
	}

	type outcome struct {
		result TurnResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, err := orchestrator.HandleFinal(context.Background(), final)
		finished <- outcome{result: result, err: err}
	}()

	select {
	case got := <-handler.completeStarted:
		if got.GenerationID != 1 || got.Text != "spoken answer" || got.Transcript != final {
			t.Fatalf("completion result = %#v, want generation, text, and authoritative transcript", got)
		}
	case <-time.After(time.Second):
		t.Fatal("generation completion lifecycle was not reached")
	}
	select {
	case got := <-finished:
		t.Fatalf("turn completed before playback lifecycle: %#v", got)
	default:
	}
	close(handler.releaseComplete)

	select {
	case got := <-finished:
		if got.err != nil {
			t.Fatalf("HandleFinal() error = %v", got.err)
		}
		if got.result.Transcript != final {
			t.Fatalf("TurnResult transcript = %#v, want %#v", got.result.Transcript, final)
		}
	case <-time.After(time.Second):
		t.Fatal("turn did not complete after playback lifecycle")
	}
	if got := handler.eventsSnapshot(); !reflect.DeepEqual(got, []string{"begin", "delta", "complete"}) {
		t.Fatalf("generation lifecycle events = %#v", got)
	}
}

func TestOrchestratorExecutesToolsThroughBoundRegistryAndContinues(t *testing.T) {
	t.Parallel()

	toolCall := sarvam.ChatToolCall{
		ID: "call-1", Name: "get_invoice", Arguments: json.RawMessage(`{"id":"06dc54d4-db33-4f97-9721-48a0c62c3218"}`),
	}
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{ToolCall: &toolCall}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{ToolCalls: []sarvam.ChatToolCall{toolCall}, FinishReason: "tool_calls", Usage: sarvam.ChatUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}}, nil
		},
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "Invoice INV-42 is paid."}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{Text: "Invoice INV-42 is paid.", FinishReason: "stop", Usage: sarvam.ChatUsage{PromptTokens: 15, CompletionTokens: 6, TotalTokens: 21}}, nil
		},
	}}
	toolExecutor := &fakeToolExecutor{
		definitions: []sarvam.ChatToolDefinition{{Name: "get_invoice", Description: "Get one invoice", Parameters: json.RawMessage(`{"type":"object"}`)}},
		responses:   map[string]json.RawMessage{"get_invoice": json.RawMessage(`{"id":"06dc54d4-db33-4f97-9721-48a0c62c3218","status":"paid"}`)},
	}
	orchestrator := mustTestOrchestrator(t, chat, toolExecutor, &recordingTextHandler{})

	result, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{
		Text: "Is invoice 42 paid?", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
	})
	if err != nil {
		t.Fatalf("HandleFinal() error = %v", err)
	}
	if result.Text != "Invoice INV-42 is paid." {
		t.Fatalf("result text = %q", result.Text)
	}
	if result.Usage != (sarvam.ChatUsage{PromptTokens: 25, CompletionTokens: 8, TotalTokens: 33}) {
		t.Fatalf("aggregate usage = %#v", result.Usage)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "get_invoice" || !result.Tools[0].Success {
		t.Fatalf("tool outcomes = %#v", result.Tools)
	}
	if got := toolExecutor.Calls(); len(got) != 1 || got[0].name != "get_invoice" || string(got[0].arguments) != string(toolCall.Arguments) {
		t.Fatalf("tool calls = %#v", got)
	}

	requests := chat.Requests()
	if len(requests) != 2 {
		t.Fatalf("chat requests = %d, want 2", len(requests))
	}
	continued := requests[1].Messages
	if len(continued) < 2 {
		t.Fatalf("continued messages = %#v", continued)
	}
	assistant := continued[len(continued)-2]
	toolMessage := continued[len(continued)-1]
	if assistant.Role != sarvam.ChatRoleAssistant || !reflect.DeepEqual(assistant.ToolCalls, []sarvam.ChatToolCall{toolCall}) {
		t.Fatalf("assistant tool-call message = %#v", assistant)
	}
	if toolMessage.Role != sarvam.ChatRoleTool || toolMessage.ToolCallID != toolCall.ID || toolMessage.Content != string(toolExecutor.responses["get_invoice"]) {
		t.Fatalf("tool result message = %#v", toolMessage)
	}
}

func TestOrchestratorConvertsToolFailureToBoundedGenericResult(t *testing.T) {
	t.Parallel()

	toolCall := sarvam.ChatToolCall{ID: "call-secret", Name: "get_customer", Arguments: json.RawMessage(`{"id":"d9f7196e-9fe0-49f1-9a19-039c15adbd04"}`)}
	chat := &scriptedChatStreamer{steps: []chatStep{
		toolChatStep(toolCall),
		func(ctx context.Context, request sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			last := request.Messages[len(request.Messages)-1]
			if strings.Contains(last.Content, "bearer-super-secret") || last.Content != genericToolFailureJSON {
				t.Fatalf("tool failure sent to model = %q", last.Content)
			}
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "I could not retrieve that customer."}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{Text: "I could not retrieve that customer.", FinishReason: "stop"}, nil
		},
	}}
	tools := &fakeToolExecutor{err: errors.New("upstream included bearer-super-secret")}
	orchestrator := mustTestOrchestrator(t, chat, tools, &recordingTextHandler{})

	result, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "Find customer", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if err != nil {
		t.Fatalf("HandleFinal() error = %v", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Success || result.Tools[0].ErrorCode != ToolErrorExecution {
		t.Fatalf("tool outcomes = %#v", result.Tools)
	}
}

func TestOrchestratorPausesToolRoundForAuthorizationRefresh(t *testing.T) {
	t.Parallel()

	call := sarvam.ChatToolCall{ID: "call-refresh", Name: "get_invoice", Arguments: json.RawMessage(`{"id":"06dc54d4-db33-4f97-9721-48a0c62c3218"}`)}
	chat := &scriptedChatStreamer{steps: []chatStep{toolChatStep(call)}}
	tools := &fakeToolExecutor{err: refreshRequiredTestError{}}
	sink := &recordingTextHandler{}
	orchestrator := mustTestOrchestrator(t, chat, tools, sink)

	result, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "get invoice", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, ErrToolAuthorizationRefresh) {
		t.Fatalf("HandleFinal() error = %v, want ErrToolAuthorizationRefresh", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].ErrorCode != ToolErrorAuthorizationRefresh || result.Tools[0].Success {
		t.Fatalf("tool outcomes = %#v", result.Tools)
	}
	if chat.CallCount() != 1 {
		t.Fatalf("chat calls = %d, want no follow-up while authorization is stale", chat.CallCount())
	}
	if sink.joined() != "" {
		t.Fatalf("authorization refresh path emitted text %q", sink.joined())
	}
}

func TestOrchestratorRejectsExtraToolRoundBeforeExecution(t *testing.T) {
	t.Parallel()

	callOne := sarvam.ChatToolCall{ID: "call-1", Name: "list_invoices", Arguments: json.RawMessage(`{}`)}
	callTwo := sarvam.ChatToolCall{ID: "call-2", Name: "list_invoices", Arguments: json.RawMessage(`{}`)}
	chat := &scriptedChatStreamer{steps: []chatStep{toolChatStep(callOne), toolChatStep(callTwo)}}
	tools := &fakeToolExecutor{responses: map[string]json.RawMessage{"list_invoices": json.RawMessage(`{"items":[]}`)}}
	orchestrator, err := NewOrchestrator(defaultTestOrchestratorConfig(chat, tools, &recordingTextHandler{}))
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	orchestrator.maxToolRounds = 1
	t.Cleanup(func() { _ = orchestrator.Close() })

	_, err = orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "List invoices", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, ErrToolRoundLimit) {
		t.Fatalf("HandleFinal() error = %v, want ErrToolRoundLimit", err)
	}
	if got := len(tools.Calls()); got != 1 {
		t.Fatalf("executed tools = %d, want only first round", got)
	}
}

func TestOrchestratorRequiresAuthoritativeFinalBeforeChat(t *testing.T) {
	t.Parallel()

	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "Final answer"}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{Text: "Final answer", FinishReason: "stop"}, nil
		},
	}}
	orchestrator := mustTestOrchestrator(t, chat, nil, &recordingTextHandler{})

	// The orchestrator deliberately exposes no partial-transcript entrypoint.
	// A turn can only enter the reasoning state through this typed final event.
	if got := chat.CallCount(); got != 0 {
		t.Fatalf("chat calls before final = %d", got)
	}
	_, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "authoritative final", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if err != nil {
		t.Fatalf("HandleFinal() error = %v", err)
	}
	if got := chat.CallCount(); got != 1 {
		t.Fatalf("chat calls after final = %d, want 1", got)
	}
}

func TestOrchestratorTotalTimeoutCancelsChat(t *testing.T) {
	t.Parallel()

	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, _ sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			<-ctx.Done()
			return sarvam.ChatResult{}, ctx.Err()
		},
	}}
	config := defaultTestOrchestratorConfig(chat, nil, &recordingTextHandler{})
	config.FirstOutputTimeout = 100 * time.Millisecond
	config.TotalTimeout = 20 * time.Millisecond
	orchestrator, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })

	_, err = orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "hello", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, ErrGenerationTimeout) {
		t.Fatalf("HandleFinal() error = %v, want ErrGenerationTimeout", err)
	}
}

func TestOrchestratorGenerationTimeoutDoesNotCancelPacedPlaybackAfterFinalResult(t *testing.T) {
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "paced spoken answer"}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{
				Text: "paced spoken answer", FinishReason: "stop",
				Usage: sarvam.ChatUsage{PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7},
			}, nil
		},
	}}
	handler := &pacedLifecycleTextHandler{frames: 4, interval: 15 * time.Millisecond}
	config := defaultTestOrchestratorConfig(chat, nil, handler)
	config.FirstOutputTimeout = 100 * time.Millisecond
	config.TotalTimeout = 20 * time.Millisecond
	orchestrator, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })

	result, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{
		Text: "hello", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
	})
	if err != nil {
		t.Fatalf("HandleFinal() error = %v", err)
	}
	if result.Text != "paced spoken answer" {
		t.Fatalf("result text = %q", result.Text)
	}
	if got := handler.completed.Load(); got != int32(handler.frames) {
		t.Fatalf("paced playback frames = %d, want %d", got, handler.frames)
	}
}

func TestOrchestratorSpeechLifecycleTimeoutBoundsStalledProviderAfterFinalResult(t *testing.T) {
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "completed answer"}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{Text: "completed answer", FinishReason: "stop"}, nil
		},
	}}
	handler := newLifecycleTextHandler()
	config := defaultTestOrchestratorConfig(chat, nil, handler)
	config.FirstOutputTimeout = 100 * time.Millisecond
	config.TotalTimeout = 20 * time.Millisecond
	config.SpeechTimeout = 35 * time.Millisecond
	orchestrator, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })

	started := time.Now()
	_, err = orchestrator.HandleFinal(context.Background(), FinalTranscript{
		Text: "hello", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
	})
	if !errors.Is(err, ErrSpeechLifecycleTimeout) {
		t.Fatalf("HandleFinal() error = %v, want ErrSpeechLifecycleTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("stalled speech lifecycle returned after %s, want bounded return", elapsed)
	}
}

func TestNewOrchestratorRejectsUnboundedSpeechLifecycleTimeout(t *testing.T) {
	chat := &scriptedChatStreamer{}
	config := defaultTestOrchestratorConfig(chat, nil, &recordingTextHandler{})
	config.SpeechTimeout = maxSpeechLifecycleTimeout + time.Millisecond
	if _, err := NewOrchestrator(config); !errors.Is(err, ErrInvalidTurnTimeout) {
		t.Fatalf("NewOrchestrator() error = %v, want ErrInvalidTurnTimeout", err)
	}
	if defaultSpeechLifecycleTimeout < 2*time.Minute {
		t.Fatalf("default speech lifecycle timeout = %s, want room for 220-token real-time playout", defaultSpeechLifecycleTimeout)
	}
}

func TestOrchestratorBargeInStillCancelsPlaybackAfterGenerationDeadlineDisarmed(t *testing.T) {
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "spoken answer"}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{Text: "spoken answer", FinishReason: "stop"}, nil
		},
	}}
	handler := newLifecycleTextHandler()
	controller := NewBargeIn()
	config := defaultTestOrchestratorConfig(chat, nil, handler)
	config.BargeIn = controller
	config.FirstOutputTimeout = 100 * time.Millisecond
	config.TotalTimeout = 20 * time.Millisecond
	orchestrator, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })

	finished := make(chan error, 1)
	go func() {
		_, handleErr := orchestrator.HandleFinal(context.Background(), FinalTranscript{
			Text: "hello", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
		})
		finished <- handleErr
	}()
	select {
	case <-handler.completeStarted:
	case <-time.After(time.Second):
		t.Fatal("playback lifecycle was not reached")
	}
	select {
	case err := <-finished:
		t.Fatalf("playback lifecycle ended after generation deadline was disarmed: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	if _, err := controller.Interrupt(controller.Current()); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, ErrTurnInterrupted) {
			t.Fatalf("HandleFinal() error = %v, want ErrTurnInterrupted", err)
		}
	case <-time.After(time.Second):
		t.Fatal("barge-in did not cancel playback lifecycle")
	}
}

func TestOrchestratorFirstUsefulOutputTimeout(t *testing.T) {
	t.Parallel()

	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, _ sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			<-ctx.Done()
			return sarvam.ChatResult{}, ctx.Err()
		},
	}}
	config := defaultTestOrchestratorConfig(chat, nil, &recordingTextHandler{})
	config.FirstOutputTimeout = 15 * time.Millisecond
	config.TotalTimeout = 100 * time.Millisecond
	orchestrator, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })

	_, err = orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "hello", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, ErrFirstOutputTimeout) {
		t.Fatalf("HandleFinal() error = %v, want ErrFirstOutputTimeout", err)
	}
}

func TestOrchestratorProgressDisarmsFirstOutputWithoutPrematureText(t *testing.T) {
	t.Parallel()

	progressed := make(chan struct{})
	release := make(chan struct{})
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			progress, ok := handler.(sarvam.ChatProgressHandler)
			if !ok {
				t.Fatal("orchestrator collector does not implement ChatProgressHandler")
			}
			if err := progress.HandleChatProgress(ctx); err != nil {
				return sarvam.ChatResult{}, err
			}
			close(progressed)
			<-release
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "validated answer"}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{Text: "validated answer", FinishReason: "stop"}, nil
		},
	}}
	sink := &recordingTextHandler{}
	config := defaultTestOrchestratorConfig(chat, nil, sink)
	config.FirstOutputTimeout = 10 * time.Millisecond
	config.TotalTimeout = time.Second
	orchestrator, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })
	done := make(chan error, 1)
	go func() {
		_, handleErr := orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "hello", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
		done <- handleErr
	}()
	<-progressed
	select {
	case <-time.After(30 * time.Millisecond):
	case err := <-done:
		t.Fatalf("turn ended before terminal stream: %v", err)
	}
	if got := sink.joined(); got != "" {
		t.Fatalf("progress exposed premature text %q", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("HandleFinal() error = %v", err)
	}
	if got := sink.joined(); got != "validated answer" {
		t.Fatalf("terminal text = %q", got)
	}
}

func TestOrchestratorStreamsAcceptedDeltasButDoesNotCommitFailedStream(t *testing.T) {
	t.Parallel()

	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "must-not-escape"}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{}, errors.New("stream broke after content")
		},
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "safe answer"}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{Text: "safe answer", FinishReason: "stop"}, nil
		},
	}}
	sink := &recordingTextHandler{}
	orchestrator := mustTestOrchestrator(t, chat, nil, sink)

	_, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "failed turn", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, ErrChatStream) {
		t.Fatalf("failed HandleFinal() error = %v, want ErrChatStream", err)
	}
	if got := sink.joined(); got != "must-not-escape" {
		t.Fatalf("accepted text before stream failure = %q", got)
	}

	result, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "next turn", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if err != nil {
		t.Fatalf("second HandleFinal() error = %v", err)
	}
	if result.GenerationID != 2 {
		t.Fatalf("second generation = %d, want 2", result.GenerationID)
	}
	requests := chat.Requests()
	if len(requests) != 2 || strings.Contains(chatMessageText(requests[1].Messages), "failed turn") {
		t.Fatalf("failed turn leaked into completed memory: %#v", requests)
	}
}

func TestOrchestratorDiscardsTextFromToolCallRound(t *testing.T) {
	t.Parallel()

	call := sarvam.ChatToolCall{ID: "call-1", Name: "list_invoices", Arguments: json.RawMessage(`{}`)}
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "premature"}); err != nil {
				return sarvam.ChatResult{}, err
			}
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{ToolCall: &call}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{Text: "premature", ToolCalls: []sarvam.ChatToolCall{call}, FinishReason: "tool_calls"}, nil
		},
	}}
	sink := &recordingTextHandler{}
	tools := &fakeToolExecutor{responses: map[string]json.RawMessage{"list_invoices": json.RawMessage(`{"items":[]}`)}}
	orchestrator := mustTestOrchestrator(t, chat, tools, sink)

	_, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "list", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, ErrChatProtocol) {
		t.Fatalf("HandleFinal() error = %v, want ErrChatProtocol", err)
	}
	if got := sink.joined(); got != "premature" {
		t.Fatalf("accepted text before mixed tool-call rejection = %q", got)
	}
	if got := len(tools.Calls()); got != 0 {
		t.Fatalf("tools executed after mixed text/tool stream = %d", got)
	}
}

func TestOrchestratorForwardsAcceptedTextBeforeChatTerminal(t *testing.T) {
	t.Parallel()

	deltaDelivered := make(chan struct{})
	releaseTerminal := make(chan struct{})
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "Your invoice is ready."}); err != nil {
				return sarvam.ChatResult{}, err
			}
			close(deltaDelivered)
			<-releaseTerminal
			return sarvam.ChatResult{Text: "Your invoice is ready.", FinishReason: "stop"}, nil
		},
	}}
	sink := &recordingTextHandler{}
	orchestrator := mustTestOrchestrator(t, chat, nil, sink)
	type outcome struct {
		result TurnResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{
			Text: "status", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
		})
		finished <- outcome{result: result, err: err}
	}()
	<-deltaDelivered
	if got := sink.joined(); got != "Your invoice is ready." {
		t.Fatalf("incremental text before terminal = %q", got)
	}
	select {
	case result := <-finished:
		t.Fatalf("HandleFinal returned before chat terminal: %#v", result)
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseTerminal)
	result := <-finished
	if result.err != nil || result.result.Text != "Your invoice is ready." {
		t.Fatalf("HandleFinal() = (%#v, %v)", result.result, result.err)
	}
	if got := sink.snapshot(); !reflect.DeepEqual(got, []string{"Your invoice is ready."}) {
		t.Fatalf("handler deltas = %#v, want no terminal replay", got)
	}
}

func TestOrchestratorBargeInCancelsActiveChatAndFencesCompletion(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	chatCause := make(chan error, 1)
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, _ sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			close(started)
			<-ctx.Done()
			chatCause <- context.Cause(ctx)
			return sarvam.ChatResult{}, ctx.Err()
		},
	}}
	controller := NewBargeIn()
	config := defaultTestOrchestratorConfig(chat, nil, &recordingTextHandler{})
	config.BargeIn = controller
	orchestrator, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })
	type outcome struct {
		result TurnResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, handleErr := orchestrator.HandleFinal(context.Background(), FinalTranscript{
			Text: "status", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
		})
		finished <- outcome{result: result, err: handleErr}
	}()
	<-started
	generation := controller.Current()
	if generation != 1 {
		t.Fatalf("active generation = %d, want 1", generation)
	}
	if next, err := controller.Interrupt(generation); err != nil || next != 2 {
		t.Fatalf("Interrupt() = (%d, %v), want generation 2", next, err)
	}
	if cause := <-chatCause; !errors.Is(cause, ErrTurnInterrupted) {
		t.Fatalf("chat context cause = %v", cause)
	}
	result := <-finished
	if !errors.Is(result.err, ErrTurnInterrupted) {
		t.Fatalf("HandleFinal() error = %v, want ErrTurnInterrupted", result.err)
	}
	if result.result.GenerationID != generation || result.result.Text != "" {
		t.Fatalf("canceled result = %#v", result.result)
	}
}

func TestOrchestratorClassifiesGenerationCancelledDuringTextCallbackAsInterrupt(t *testing.T) {
	t.Parallel()

	handler := &cancellationAwareTextHandler{entered: make(chan struct{})}
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, events sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			err := events.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "late delta"})
			return sarvam.ChatResult{}, err
		},
	}}
	controller := NewBargeIn()
	config := defaultTestOrchestratorConfig(chat, nil, handler)
	config.BargeIn = controller
	orchestrator, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })
	finished := make(chan error, 1)
	go func() {
		_, handleErr := orchestrator.HandleFinal(context.Background(), FinalTranscript{
			Text: "status", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
		})
		finished <- handleErr
	}()
	<-handler.entered
	if _, err := controller.Interrupt(controller.Current()); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if err := <-finished; !errors.Is(err, ErrTurnInterrupted) {
		t.Fatalf("HandleFinal() error = %v, want ErrTurnInterrupted", err)
	}
}

func TestOrchestratorCancellationAndCloseFenceNewTurns(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, _ sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			close(started)
			<-ctx.Done()
			return sarvam.ChatResult{}, ctx.Err()
		},
	}}
	orchestrator := mustTestOrchestrator(t, chat, nil, &recordingTextHandler{})
	result := make(chan error, 1)
	go func() {
		_, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "hello", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
		result <- err
	}()
	<-started
	if err := orchestrator.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-result; !errors.Is(err, ErrOrchestratorClosed) {
		t.Fatalf("active HandleFinal() error = %v, want ErrOrchestratorClosed", err)
	}
	_, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "again", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, ErrOrchestratorClosed) {
		t.Fatalf("post-close HandleFinal() error = %v", err)
	}
}

func TestOrchestratorDoesNotCallChatForCanceledFinal(t *testing.T) {
	t.Parallel()

	chat := &scriptedChatStreamer{}
	orchestrator := mustTestOrchestrator(t, chat, nil, &recordingTextHandler{})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := orchestrator.HandleFinal(canceled, FinalTranscript{Text: "hello", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("HandleFinal() error = %v, want context.Canceled", err)
	}
	if chat.CallCount() != 0 {
		t.Fatalf("chat calls for canceled final = %d", chat.CallCount())
	}
}

func TestOrchestratorTextHandlerCanSynchronouslyCloseWithoutCommit(t *testing.T) {
	t.Parallel()

	chat := &scriptedChatStreamer{steps: []chatStep{
		func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "answer"}); err != nil {
				return sarvam.ChatResult{}, err
			}
			return sarvam.ChatResult{Text: "answer", FinishReason: "stop"}, nil
		},
	}}
	var orchestrator *Orchestrator
	handler := TextDeltaHandlerFunc(func(context.Context, uint64, string) error {
		return orchestrator.Close()
	})
	var err error
	orchestrator, err = NewOrchestrator(defaultTestOrchestratorConfig(chat, nil, handler))
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	_, err = orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "hello", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, ErrOrchestratorClosed) {
		t.Fatalf("HandleFinal() error = %v, want ErrOrchestratorClosed", err)
	}
	_, err = orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "again", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, ErrOrchestratorClosed) {
		t.Fatalf("second HandleFinal() error = %v, want ErrOrchestratorClosed", err)
	}
	if chat.CallCount() != 1 {
		t.Fatalf("chat calls = %d, want no committed follow-up", chat.CallCount())
	}
}

func TestOrchestratorValidatesConfigurationAndFinal(t *testing.T) {
	t.Parallel()

	valid := defaultTestOrchestratorConfig(&scriptedChatStreamer{}, nil, &recordingTextHandler{})
	tests := []struct {
		name   string
		mutate func(*OrchestratorConfig)
		want   error
	}{
		{name: "nil context", mutate: func(config *OrchestratorConfig) { config.Context = nil }, want: ErrOrchestratorContextRequired},
		{name: "nil chat", mutate: func(config *OrchestratorConfig) { config.Chat = nil }, want: ErrChatStreamerRequired},
		{name: "nil handler", mutate: func(config *OrchestratorConfig) { config.TextHandler = nil }, want: ErrTextHandlerRequired},
		{name: "empty policy", mutate: func(config *OrchestratorConfig) { config.Window.SystemPolicy = "" }, want: ErrInvalidConversationContext},
		{name: "empty business", mutate: func(config *OrchestratorConfig) { config.Window.BusinessContext = "" }, want: ErrInvalidConversationContext},
		{name: "too few tokens", mutate: func(config *OrchestratorConfig) { config.MaxTokens = MinVoiceMaxTokens - 1 }, want: ErrInvalidVoiceMaxTokens},
		{name: "too many tokens", mutate: func(config *OrchestratorConfig) { config.MaxTokens = MaxVoiceMaxTokens + 1 }, want: ErrInvalidVoiceMaxTokens},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			_, err := NewOrchestrator(config)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewOrchestrator() error = %v, want %v", err, test.want)
			}
		})
	}

	orchestrator := mustTestOrchestrator(t, &scriptedChatStreamer{}, nil, &recordingTextHandler{})
	finals := []FinalTranscript{
		{},
		{Text: strings.Repeat("x", MaxFinalTranscriptBytes+1), DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"},
		{Text: "hello", DetectedLanguage: "en-IN", ResponseLanguage: "fr-FR"},
		{Text: "hello", ProviderLanguage: "fr-FR\nunsafe", ResponseLanguage: "en-IN"},
	}
	for _, final := range finals {
		if _, err := orchestrator.HandleFinal(context.Background(), final); !errors.Is(err, ErrInvalidFinalTranscript) {
			t.Fatalf("HandleFinal(%#v) error = %v, want ErrInvalidFinalTranscript", final, err)
		}
	}
}

func TestOrchestratorRejectsConcurrentTurn(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	chat := &scriptedChatStreamer{steps: []chatStep{
		func(context.Context, sarvam.ChatRequest, sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
			close(started)
			<-release
			return sarvam.ChatResult{}, errors.New("released")
		},
	}}
	orchestrator := mustTestOrchestrator(t, chat, nil, &recordingTextHandler{})
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "one", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	}()
	<-started
	_, err := orchestrator.HandleFinal(context.Background(), FinalTranscript{Text: "two", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"})
	if !errors.Is(err, ErrTurnInProgress) {
		t.Fatalf("concurrent HandleFinal() error = %v, want ErrTurnInProgress", err)
	}
	close(release)
	<-firstDone
}

type chatStep func(context.Context, sarvam.ChatRequest, sarvam.ChatEventHandler) (sarvam.ChatResult, error)

type scriptedChatStreamer struct {
	mu       sync.Mutex
	steps    []chatStep
	requests []sarvam.ChatRequest
	calls    atomic.Int64
}

func (streamer *scriptedChatStreamer) StreamChat(ctx context.Context, request sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
	streamer.calls.Add(1)
	streamer.mu.Lock()
	streamer.requests = append(streamer.requests, cloneTestChatRequest(request))
	index := len(streamer.requests) - 1
	var step chatStep
	if index < len(streamer.steps) {
		step = streamer.steps[index]
	}
	streamer.mu.Unlock()
	if step == nil {
		return sarvam.ChatResult{}, errors.New("unexpected chat request")
	}
	return step(ctx, request, handler)
}

func (streamer *scriptedChatStreamer) Requests() []sarvam.ChatRequest {
	streamer.mu.Lock()
	defer streamer.mu.Unlock()
	requests := make([]sarvam.ChatRequest, len(streamer.requests))
	for index := range streamer.requests {
		requests[index] = cloneTestChatRequest(streamer.requests[index])
	}
	return requests
}

func (streamer *scriptedChatStreamer) CallCount() int64 { return streamer.calls.Load() }

type recordingTextHandler struct {
	mu     sync.Mutex
	deltas []string
	err    error
}

type cancellationAwareTextHandler struct{ entered chan struct{} }

func (handler *cancellationAwareTextHandler) HandleTextDelta(ctx context.Context, _ uint64, _ string) error {
	close(handler.entered)
	<-ctx.Done()
	return errors.New("provider write unblocked by cancellation")
}

type lifecycleTextHandler struct {
	mu              sync.Mutex
	events          []string
	completeStarted chan TurnResult
	releaseComplete chan struct{}
}

type pacedLifecycleTextHandler struct {
	frames    int
	interval  time.Duration
	completed atomic.Int32
}

func (*pacedLifecycleTextHandler) BeginTextGeneration(context.Context, uint64, FinalTranscript) error {
	return nil
}

func (*pacedLifecycleTextHandler) HandleTextDelta(context.Context, uint64, string) error { return nil }

func (handler *pacedLifecycleTextHandler) CompleteTextGeneration(ctx context.Context, _ uint64, _ TurnResult) error {
	for frame := 0; frame < handler.frames; frame++ {
		if frame != 0 {
			timer := time.NewTimer(handler.interval)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
		handler.completed.Add(1)
	}
	return nil
}

func newLifecycleTextHandler() *lifecycleTextHandler {
	return &lifecycleTextHandler{
		completeStarted: make(chan TurnResult, 1),
		releaseComplete: make(chan struct{}),
	}
}

func (handler *lifecycleTextHandler) BeginTextGeneration(_ context.Context, _ uint64, _ FinalTranscript) error {
	handler.recordEvent("begin")
	return nil
}

func (handler *lifecycleTextHandler) HandleTextDelta(_ context.Context, _ uint64, _ string) error {
	handler.recordEvent("delta")
	return nil
}

func (handler *lifecycleTextHandler) CompleteTextGeneration(ctx context.Context, _ uint64, result TurnResult) error {
	handler.recordEvent("complete")
	handler.completeStarted <- result
	select {
	case <-handler.releaseComplete:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (handler *lifecycleTextHandler) recordEvent(event string) {
	handler.mu.Lock()
	handler.events = append(handler.events, event)
	handler.mu.Unlock()
}

func (handler *lifecycleTextHandler) eventsSnapshot() []string {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	return append([]string(nil), handler.events...)
}

func (handler *recordingTextHandler) HandleTextDelta(_ context.Context, _ uint64, delta string) error {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.deltas = append(handler.deltas, delta)
	return handler.err
}

func (handler *recordingTextHandler) joined() string {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	return strings.Join(handler.deltas, "")
}

func (handler *recordingTextHandler) snapshot() []string {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	return append([]string(nil), handler.deltas...)
}

type recordedToolCall struct {
	name      string
	arguments json.RawMessage
}

type refreshRequiredTestError struct{}

func (refreshRequiredTestError) Error() string                      { return "refresh required" }
func (refreshRequiredTestError) AuthorizationRefreshRequired() bool { return true }

type fakeToolExecutor struct {
	mu          sync.Mutex
	definitions []sarvam.ChatToolDefinition
	responses   map[string]json.RawMessage
	err         error
	calls       []recordedToolCall
}

func (executor *fakeToolExecutor) Definitions() []sarvam.ChatToolDefinition {
	return append([]sarvam.ChatToolDefinition(nil), executor.definitions...)
}

func (executor *fakeToolExecutor) Execute(_ context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	executor.calls = append(executor.calls, recordedToolCall{name: name, arguments: append(json.RawMessage(nil), arguments...)})
	if executor.err != nil {
		return nil, executor.err
	}
	return append(json.RawMessage(nil), executor.responses[name]...), nil
}

func (executor *fakeToolExecutor) Calls() []recordedToolCall {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return append([]recordedToolCall(nil), executor.calls...)
}

func mustTestOrchestrator(t *testing.T, chat sarvam.ChatStreamer, tools ToolExecutor, handler TextDeltaHandler) *Orchestrator {
	t.Helper()
	orchestrator, err := NewOrchestrator(defaultTestOrchestratorConfig(chat, tools, handler))
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })
	return orchestrator
}

func defaultTestOrchestratorConfig(chat sarvam.ChatStreamer, tools ToolExecutor, handler TextDeltaHandler) OrchestratorConfig {
	return OrchestratorConfig{
		Context: context.Background(), Chat: chat, Tools: tools, TextHandler: handler,
		Window: ConversationWindow{SystemPolicy: "voice policy", BusinessContext: "business=b-1", Summary: "summary"},
	}
}

func toolChatStep(call sarvam.ChatToolCall) chatStep {
	return func(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
		if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{ToolCall: &call}); err != nil {
			return sarvam.ChatResult{}, err
		}
		return sarvam.ChatResult{ToolCalls: []sarvam.ChatToolCall{call}, FinishReason: "tool_calls"}, nil
	}
}

func chatMessageText(messages []sarvam.ChatMessage) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(message.Content)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func cloneTestChatRequest(request sarvam.ChatRequest) sarvam.ChatRequest {
	cloned := request
	cloned.Messages = append([]sarvam.ChatMessage(nil), request.Messages...)
	for index := range cloned.Messages {
		cloned.Messages[index].ToolCalls = append([]sarvam.ChatToolCall(nil), request.Messages[index].ToolCalls...)
	}
	cloned.Tools = append([]sarvam.ChatToolDefinition(nil), request.Tools...)
	return cloned
}
