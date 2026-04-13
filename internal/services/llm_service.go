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

// ContentBlock represents a message content block
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ChatMessage represents a message in the chat conversation (content can be string or array)
type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// ToAnthropicFormat converts a ChatMessage to Anthropic format with content as array
func (m ChatMessage) ToAnthropicFormat() ChatMessage {
	var contentBlocks []ContentBlock
	switch v := m.Content.(type) {
	case string:
		contentBlocks = []ContentBlock{{Type: "text", Text: v}}
	case []ContentBlock:
		contentBlocks = v
	case []interface{}:
		for _, item := range v {
			if block, ok := item.(ContentBlock); ok {
				contentBlocks = append(contentBlocks, block)
			} else if mmap, ok := item.(map[string]interface{}); ok {
				contentBlocks = append(contentBlocks, ContentBlock{
					Type: mmap["type"].(string),
					Text: mmap["text"].(string),
				})
			}
		}
	}
	return ChatMessage{Role: m.Role, Content: contentBlocks}
}

// AnthropicRequest represents the request body for Anthropic API via MinMax proxy
type AnthropicRequest struct {
	Model            string        `json:"model"`
	Messages         []ChatMessage `json:"messages"`
	MaxTokens        int           `json:"max_tokens"`
	Stream           bool          `json:"stream"`
	System           string        `json:"system,omitempty"`
	AnthropicVersion string        `json:"anthropic_version"`
}

// AnthropicResponse represents the response from Anthropic API via MinMax proxy
type AnthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking,omitempty"`
	} `json:"content"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
}

// Chat sends a chat request to the LLM
func (s *LLMService) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	log := logger.FromContext(ctx).With("service", "llm", "operation", "chat", "message_count", len(messages))
	start := time.Now()

	// Convert messages to Anthropic format (content as array)
	anthropicMessages := make([]ChatMessage, len(messages))
	for i, msg := range messages {
		anthropicMessages[i] = msg.ToAnthropicFormat()
	}

	reqBody := AnthropicRequest{
		Model:            s.config.Model,
		Messages:         anthropicMessages,
		MaxTokens:        1024,
		Stream:           false,
		AnthropicVersion: "vertex-2023-06-01",
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
	req.Header.Set("x-api-key", s.config.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		log.Error("failed to call LLM API", "error", err, "duration_ms", time.Since(start).Milliseconds())
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("failed to read response body", "error", err)
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	log.Info("LLM RAW RESPONSE", "body", string(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		log.Error("LLM API returned non-200",
			"status_code", resp.StatusCode,
			"duration_ms", time.Since(start).Milliseconds(),
			"response_size", len(bodyBytes),
			"response_body", string(bodyBytes),
		)
		return "", fmt.Errorf("API returned error: %s - %s", resp.Status, string(bodyBytes))
	}

	var chatResp AnthropicResponse
	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
		log.Error("failed to decode LLM response", "error", err, "duration_ms", time.Since(start).Milliseconds(), "body", string(bodyBytes))
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(chatResp.Content) == 0 {
		log.Warn("LLM response contained no content", "duration_ms", time.Since(start).Milliseconds(), "response", chatResp, "raw_body", string(bodyBytes))
		return "", fmt.Errorf("no content in response. Raw response: %s", string(bodyBytes))
	}

	log.Info("LLM chat completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"model", chatResp.Model,
	)

	// Find the text content block (skip thinking blocks)
	for _, block := range chatResp.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("no text content found in response")
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
