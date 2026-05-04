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

func TestLLMServiceChatSendsMiniMaxAuthorizationHeader(t *testing.T) {
	t.Parallel()

	const apiKey = "test-minimax-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer "+apiKey; got != want {
			t.Fatalf("Authorization header = %q, want %q", got, want)
		}
		if got := r.Header.Get("x-api-key"); got != apiKey {
			t.Fatalf("x-api-key header = %q, want %q", got, apiKey)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Fatal("anthropic-version header was empty")
		}

		var req AnthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "MiniMax-M2.7" {
			t.Fatalf("model = %q, want MiniMax-M2.7", req.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"model":"MiniMax-M2.7"}`))
	}))
	defer server.Close()

	svc := NewLLMService(config.LLMConfig{
		APIKey:  apiKey,
		APIURL:  server.URL,
		Model:   "MiniMax-M2.7",
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
