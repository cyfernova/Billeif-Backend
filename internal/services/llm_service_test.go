package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"
)

func TestLLMServiceChatSendsDeepSeekOpenAIRequest(t *testing.T) {
	t.Parallel()

	const apiKey = "test-deepseek-key"
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
		if req.Model != "deepseek-v4-flash" {
			t.Fatalf("model = %q, want deepseek-v4-flash", req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hello" {
			t.Fatalf("messages = %#v, want one OpenAI user message", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"model":"deepseek-v4-flash"}`))
	}))
	defer server.Close()

	svc := NewLLMService(config.LLMConfig{
		APIKey:  apiKey,
		APIURL:  server.URL,
		Model:   "deepseek-v4-flash",
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
