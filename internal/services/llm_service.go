package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"
)

// LLMService handles interactions with LLM APIs
type LLMService struct {
	config config.LLMConfig
	log    *logger.Logger
	client *http.Client
}

// NewLLMService creates a new LLM service
func NewLLMService(cfg config.LLMConfig, log *logger.Logger) *LLMService {
	return &LLMService{
		config: cfg,
		log:    log,
		client: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
	}
}

// ChatMessage represents a message in the chat conversation
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents the request body for the chat API
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ChatResponse represents the response from the chat API
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int         `json:"index"`
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
}

// Chat sends a chat request to the LLM
func (s *LLMService) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	log := logger.FromContext(ctx).With("service", "llm", "operation", "chat", "message_count", len(messages))
	start := time.Now()

	reqBody := ChatRequest{
		Model:    s.config.Model,
		Messages: messages,
		Stream:   false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		log.Error("failed to marshal LLM request", "error", err)
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.APIURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Error("failed to create LLM request", "error", err)
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		log.Error("failed to call LLM API", "error", err, "duration_ms", time.Since(start).Milliseconds())
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Error("LLM API returned non-200",
			"status_code", resp.StatusCode,
			"duration_ms", time.Since(start).Milliseconds(),
			"response_size", len(bodyBytes),
		)
		return "", fmt.Errorf("API returned error: %s - %s", resp.Status, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		log.Error("failed to decode LLM response", "error", err, "duration_ms", time.Since(start).Milliseconds())
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		log.Warn("LLM response contained no choices", "duration_ms", time.Since(start).Milliseconds())
		return "", fmt.Errorf("no choices in response")
	}

	log.Info("LLM chat completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"model", chatResp.Model,
		"choice_count", len(chatResp.Choices),
	)
	return chatResp.Choices[0].Message.Content, nil
}

// ProcessAgentIntent processes a user intent with agent context
func (s *LLMService) ProcessAgentIntent(ctx context.Context, intent string, contextInfo string) (string, error) {
	log := logger.FromContext(ctx).With("service", "llm", "operation", "process_agent_intent")
	log.Debug("processing agent intent", "intent_length", len(intent), "has_context", contextInfo != "")

	systemPrompt := `# AI Assistant Idea Generator - Base Template

## Core Purpose
You are a specialized AI designed to help users ideate and conceptualize AI assistants and agents. Your primary goal is to generate creative, practical, and innovative ideas for AI solutions based on user requirements.

## Terminology
- **AI Assistant**: A system prompt applied to a large language model, potentially with modified parameters (especially temperature) and additional data pipelines through RAG or API access.
- **Agent**: An assistant with "tool" access, allowing it to take actions upon other APIs or systems through intermediary software.

## Response Format
For each assistant idea, provide:

1. **Name**: A creative but descriptive name
2. **Description**: A concise explanation of what the assistant does and its benefits
3. **Capabilities**: Required technical capabilities beyond a basic LLM (vision, RAG, APIs, etc.)

## Ideation Guidelines
- Focus on practical, valuable assistants that solve real problems
- Balance creativity with feasibility
- Include a mix of business and personal use cases unless specified otherwise
- Avoid repeating suggestions from the same session`

	if contextInfo != "" {
		systemPrompt += fmt.Sprintf("\n\n## Additional Context\n%s", contextInfo)
	}

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: intent},
	}

	response, err := s.Chat(ctx, messages)
	if err != nil {
		log.Error("agent intent processing failed", "error", err)
		return "", err
	}

	log.Info("agent intent processed", "response_length", len(response))
	return response, nil
}
