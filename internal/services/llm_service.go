package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
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
	reqBody := ChatRequest{
		Model:    s.config.Model,
		Messages: messages,
		Stream:   false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.APIURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("API returned error: %s - %s", resp.Status, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// ProcessAgentIntent processes a user intent with agent context
func (s *LLMService) ProcessAgentIntent(ctx context.Context, intent string, contextInfo string) (string, error) {
	systemPrompt := "You are an intelligent assistant for an agent marketplace. " +
		"Your goal is to help users find the right agents or understand how to use them. " +
		"Provide helpful, concise responses based on the available context."

	if contextInfo != "" {
		systemPrompt += fmt.Sprintf("\n\nContext Information:\n%s", contextInfo)
	}

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: intent},
	}

	return s.Chat(ctx, messages)
}
