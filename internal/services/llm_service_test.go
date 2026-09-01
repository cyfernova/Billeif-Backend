package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"
)

func TestLLMServiceChatSendsOpenAICompatibleRequest(t *testing.T) {
	t.Parallel()

	const apiKey = "test-llm-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer "+apiKey; got != want {
			t.Fatalf("Authorization header = %q, want %q", got, want)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Fatalf("x-api-key header = %q, want empty", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "" {
			t.Fatalf("anthropic-version header = %q, want empty", got)
		}

		var req OpenAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "test-llm-model" {
			t.Fatalf("model = %q, want test-llm-model", req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hello" {
			t.Fatalf("messages = %#v, want one OpenAI user message", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"model":"test-llm-model"}`))
	}))
	defer server.Close()

	svc := NewLLMService(config.LLMConfig{
		APIKey:  apiKey,
		APIURL:  server.URL,
		Model:   "test-llm-model",
		Timeout: 5,
	}, logger.NewWithEnv("test"))

	got, err := svc.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("Chat response = %q, want ok", got)
	}
}

func TestLLMServiceBusinessChatRejectsUnavailableAICapabilityBeforeProvider(t *testing.T) {
	var providerCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	guard := &recordingCapabilityGuard{err: &CapabilityUnavailableError{
		Code: "capability_unavailable", Capability: CapabilityAI,
		State: CapabilityStateUnknown, ReasonCode: ReasonProviderHealthUnknown,
	}}
	service := NewLLMService(config.LLMConfig{APIKey: "test", APIURL: server.URL, Timeout: 1}, logger.New()).WithCapabilityGuard(guard)

	_, err := service.ChatWithWebSearchForBusiness(context.Background(), "biz-1", "user-1", []ChatMessage{{Role: "user", Content: "hello"}})

	var unavailable *CapabilityUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("business chat error = %T %v, want CapabilityUnavailableError", err, err)
	}
	if providerCalls != 0 {
		t.Fatalf("provider calls = %d, want zero", providerCalls)
	}
	if guard.request.Capability != CapabilityAI || guard.request.BusinessID != "biz-1" || guard.request.UserID != "user-1" {
		t.Fatalf("guard request = %#v", guard.request)
	}
}

func TestLLMServiceBusinessAgentAssistRejectsUnavailableAICapabilityBeforeProvider(t *testing.T) {
	var providerCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	guard := &recordingCapabilityGuard{err: &CapabilityUnavailableError{
		Code: "capability_unavailable", Capability: CapabilityAI,
		State: CapabilityStateUnknown, ReasonCode: ReasonProviderHealthUnknown,
	}}
	service := NewLLMService(config.LLMConfig{APIKey: "test", APIURL: server.URL, Timeout: 1}, logger.New()).WithCapabilityGuard(guard)

	_, err := service.ProcessAgentIntentForBusiness(context.Background(), "biz-1", "user-1", "suggest an agent", "")

	var unavailable *CapabilityUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("agent assist error = %T %v, want CapabilityUnavailableError", err, err)
	}
	if providerCalls != 0 {
		t.Fatalf("provider calls = %d, want zero", providerCalls)
	}
}

func TestLLMServiceBusinessChatRecordsTenantScopedProviderOutcome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"model":"test"}`))
	}))
	defer server.Close()
	now := time.Date(2026, 9, 1, 14, 0, 0, 0, time.UTC)
	cache := NewCapabilityHealthCache(CapabilityHealthCacheOptions{Now: func() time.Time { return now }})
	recorder := NewCapabilityHealthRecorder(cache, func() time.Time { return now })
	service := NewLLMService(config.LLMConfig{APIKey: "test", APIURL: server.URL, Model: "test", Timeout: 1}, logger.New()).
		WithCapabilityGuard(&recordingCapabilityGuard{}).
		WithCapabilityHealthRecorder(recorder)

	_, err := service.ChatWithWebSearchForBusiness(context.Background(), "biz-1", "user-1", []ChatMessage{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("business chat: %v", err)
	}

	fact, ok := cache.CustomerFact("biz-1", CapabilityAI)
	if !ok || fact.Status != CapabilityProviderHealthy || fact.ObservedAt == nil || !fact.ObservedAt.Equal(now) {
		t.Fatalf("provider fact = %#v, found=%v", fact, ok)
	}
	if _, otherTenant := cache.CustomerFact("biz-2", CapabilityAI); otherTenant {
		t.Fatal("provider outcome leaked to another tenant")
	}
}

func TestLLMServiceChatWithWebSearchUsesExaContext(t *testing.T) {
	t.Parallel()

	const exaKey = "test-exa-key"
	exaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("x-api-key"), exaKey; got != want {
			t.Fatalf("x-api-key header = %q, want %q", got, want)
		}
		var req exaSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode Exa request: %v", err)
		}
		if req.Query != "What is the latest GST e-invoice update today?" {
			t.Fatalf("Exa query = %q", req.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"GST update","url":"https://example.test/gst","publishedDate":"2026-05-15","author":"Example","highlights":["Latest GST e-invoice update for testing."],"image":"https://example.test/gst.png","favicon":"https://example.test/favicon.ico","extras":{"imageLinks":["https://example.test/gst-extra.png"]}}]}`))
	}))
	defer exaServer.Close()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req OpenAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode LLM request: %v", err)
		}
		if len(req.Messages) < 3 {
			t.Fatalf("messages = %#v, want system prompt, Exa context, user message", req.Messages)
		}
		foundSearchContext := false
		for _, msg := range req.Messages {
			if msg.Role == "system" && strings.Contains(msg.Content, "Exa web search") && strings.Contains(msg.Content, "https://example.test/gst") {
				foundSearchContext = true
				break
			}
		}
		if !foundSearchContext {
			t.Fatalf("LLM request did not include Exa search context: %#v", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Use the cited GST update."}}],"model":"test-llm-model"}`))
	}))
	defer llmServer.Close()

	svc := NewLLMService(config.LLMConfig{
		APIKey:     "test-llm-key",
		APIURL:     llmServer.URL,
		Model:      "test-llm-model",
		Timeout:    5,
		ExaAPIKey:  exaKey,
		ExaBaseURL: exaServer.URL,
		ExaTimeout: 5,
	}, logger.NewWithEnv("test"))

	got, err := svc.ChatWithWebSearch(context.Background(), []ChatMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "What is the latest GST e-invoice update today?"},
	})
	if err != nil {
		t.Fatalf("ChatWithWebSearch returned error: %v", err)
	}
	if got.Response != "Use the cited GST update." {
		t.Fatalf("response = %q", got.Response)
	}
	if got.WebSearch == nil || !got.WebSearch.Used || got.WebSearch.Query == "" || len(got.WebSearch.Results) != 1 {
		t.Fatalf("web search metadata = %#v", got.WebSearch)
	}
	result := got.WebSearch.Results[0]
	if result.ImageURL != "https://example.test/gst.png" || result.FaviconURL != "https://example.test/favicon.ico" || len(result.ImageURLs) != 1 {
		t.Fatalf("web search image metadata = %#v", result)
	}
}

func TestLLMServiceChatWithWebSearchRequiresExaKey(t *testing.T) {
	t.Parallel()

	svc := NewLLMService(config.LLMConfig{
		APIKey:  "test-llm-key",
		APIURL:  "https://llm.example.test/chat/completions",
		Model:   "test-llm-model",
		Timeout: 5,
	}, logger.NewWithEnv("test"))

	_, err := svc.ChatWithWebSearch(context.Background(), []ChatMessage{{Role: "user", Content: "latest GST update today"}})
	if err == nil || !strings.Contains(err.Error(), "EXA_API_KEY") {
		t.Fatalf("error = %v, want missing EXA_API_KEY error", err)
	}
}

func TestLLMServiceChatWithWebSearchUsesExaForExternalQuestion(t *testing.T) {
	t.Parallel()

	const exaKey = "test-exa-key"
	exaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req exaSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode Exa request: %v", err)
		}
		if req.Query != "Who is Mira Murati?" {
			t.Fatalf("Exa query = %q", req.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Mira Murati profile","url":"https://example.test/mira","highlights":["Mira Murati is a technology executive."]}]}`))
	}))
	defer exaServer.Close()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req OpenAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode LLM request: %v", err)
		}
		foundSearchContext := false
		for _, msg := range req.Messages {
			if msg.Role == "system" && strings.Contains(msg.Content, "Exa web search") && strings.Contains(msg.Content, "https://example.test/mira") {
				foundSearchContext = true
				break
			}
		}
		if !foundSearchContext {
			t.Fatalf("LLM request did not include autonomous Exa search context: %#v", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Answered with web context."}}],"model":"test-llm-model"}`))
	}))
	defer llmServer.Close()

	svc := NewLLMService(config.LLMConfig{
		APIKey:     "test-llm-key",
		APIURL:     llmServer.URL,
		Model:      "test-llm-model",
		Timeout:    5,
		ExaAPIKey:  exaKey,
		ExaBaseURL: exaServer.URL,
		ExaTimeout: 5,
	}, logger.NewWithEnv("test"))

	got, err := svc.ChatWithWebSearch(context.Background(), []ChatMessage{{Role: "user", Content: "Who is Mira Murati?"}})
	if err != nil {
		t.Fatalf("ChatWithWebSearch returned error: %v", err)
	}
	if got.WebSearch == nil || !got.WebSearch.Used {
		t.Fatalf("web search metadata = %#v, want used", got.WebSearch)
	}
}

func TestLLMServiceChatWithWebSearchDoesNotRequireExaForAppWorkflow(t *testing.T) {
	t.Parallel()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req OpenAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode LLM request: %v", err)
		}
		for _, msg := range req.Messages {
			if msg.Role == "system" && strings.Contains(msg.Content, "Exa web search") {
				t.Fatalf("unexpected Exa context for local workflow query: %#v", req.Messages)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Create an invoice from the New Invoice screen."}}],"model":"test-llm-model"}`))
	}))
	defer llmServer.Close()

	svc := NewLLMService(config.LLMConfig{
		APIKey:  "test-llm-key",
		APIURL:  llmServer.URL,
		Model:   "test-llm-model",
		Timeout: 5,
	}, logger.NewWithEnv("test"))

	got, err := svc.ChatWithWebSearch(context.Background(), []ChatMessage{{Role: "user", Content: "How do I create an invoice?"}})
	if err != nil {
		t.Fatalf("ChatWithWebSearch returned error: %v", err)
	}
	if got.WebSearch == nil || got.WebSearch.Used {
		t.Fatalf("web search metadata = %#v, want not used", got.WebSearch)
	}
}
