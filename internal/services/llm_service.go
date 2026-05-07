package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

type LLMChatOptions struct {
	MaxTokens int
	System    string
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

// ContentBlock represents a text content block accepted by callers that build rich messages.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ChatMessage represents a message in the chat conversation.
type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// ToOpenAIFormat converts a ChatMessage to an OpenAI-compatible chat message.
func (m ChatMessage) ToOpenAIFormat() OpenAIChatMessage {
	return OpenAIChatMessage{
		Role:    m.Role,
		Content: stringifyMessageContent(m.Content),
	}
}

type OpenAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIChatRequest struct {
	Model     string              `json:"model"`
	Messages  []OpenAIChatMessage `json:"messages"`
	MaxTokens int                 `json:"max_tokens,omitempty"`
	Stream    bool                `json:"stream"`
}

type OpenAIChatResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Error   *OpenAIError   `json:"error,omitempty"`
}

type OpenAIChoice struct {
	Index        int               `json:"index"`
	Message      OpenAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type OpenAIError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

func stringifyMessageContent(content interface{}) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case ContentBlock:
		return v.Text
	case []ContentBlock:
		return joinContentBlocks(v)
	case []interface{}:
		return joinInterfaceContentBlocks(v)
	case map[string]interface{}:
		return textFromMap(v)
	default:
		return fmt.Sprint(v)
	}
}

func joinContentBlocks(blocks []ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func joinInterfaceContentBlocks(items []interface{}) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case string:
			if v != "" {
				parts = append(parts, v)
			}
		case ContentBlock:
			if v.Text != "" {
				parts = append(parts, v.Text)
			}
		case map[string]interface{}:
			if text := textFromMap(v); text != "" {
				parts = append(parts, text)
			}
		default:
			text := fmt.Sprint(v)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func textFromMap(m map[string]interface{}) string {
	if text, ok := m["text"].(string); ok {
		return text
	}
	if content, ok := m["content"].(string); ok {
		return content
	}
	return ""
}

// Chat sends a chat request to the LLM
func (s *LLMService) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	return s.ChatWithOptions(ctx, messages, LLMChatOptions{})
}

// ChatWithOptions sends a chat request to the LLM with call-site-specific generation limits.
func (s *LLMService) ChatWithOptions(ctx context.Context, messages []ChatMessage, options LLMChatOptions) (string, error) {
	log := logger.FromContext(ctx).With("service", "llm", "operation", "chat", "message_count", len(messages))
	start := time.Now()
	maxTokens := options.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	openAIMessages := make([]OpenAIChatMessage, 0, len(messages)+1)
	if options.System != "" {
		openAIMessages = append(openAIMessages, OpenAIChatMessage{
			Role:    "system",
			Content: options.System,
		})
	}
	for _, msg := range messages {
		openAIMessages = append(openAIMessages, msg.ToOpenAIFormat())
	}

	reqBody := OpenAIChatRequest{
		Model:     s.config.Model,
		Messages:  openAIMessages,
		MaxTokens: maxTokens,
		Stream:    false,
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
	apiKey := strings.TrimSpace(s.config.APIKey)
	req.Header.Set("Authorization", "Bearer "+apiKey)

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
	log.Debug("LLM response received", "response_size", len(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		log.Error("LLM API returned non-200",
			"status_code", resp.StatusCode,
			"duration_ms", time.Since(start).Milliseconds(),
			"response_size", len(bodyBytes),
			"response_body", string(bodyBytes),
		)
		return "", fmt.Errorf("API returned error: %s - %s", resp.Status, string(bodyBytes))
	}

	var chatResp OpenAIChatResponse
	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
		log.Error("failed to decode LLM response", "error", err, "duration_ms", time.Since(start).Milliseconds(), "body", string(bodyBytes))
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if chatResp.Error != nil && chatResp.Error.Message != "" {
		log.Error("LLM API returned error payload",
			"error_type", chatResp.Error.Type,
			"error_code", chatResp.Error.Code,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return "", fmt.Errorf("LLM API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		log.Warn("LLM response contained no choices", "duration_ms", time.Since(start).Milliseconds(), "response", chatResp, "raw_body", string(bodyBytes))
		return "", fmt.Errorf("no choices in response. Raw response: %s", string(bodyBytes))
	}

	log.Info("LLM chat completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"model", chatResp.Model,
	)

	content := chatResp.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("no text content found in response")
	}

	return content, nil
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
