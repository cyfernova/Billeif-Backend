package nlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GeminiClient implements the LLMClient interface for Google's Gemini API
type GeminiClient struct {
	apiKey string
	model  string
	client *http.Client
}

// GeminiRequest represents a request to Gemini API
type GeminiRequest struct {
	Contents []GeminiContent `json:"contents"`
}

// GeminiContent represents content in a Gemini request
type GeminiContent struct {
	Role  string       `json:"role"`
	Parts []GeminiPart `json:"parts"`
}

// GeminiPart represents a part in Gemini content
type GeminiPart struct {
	Text string `json:"text"`
}

// GeminiResponse represents a response from Gemini API
type GeminiResponse struct {
	Candidates []struct {
		Content      GeminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// NewGeminiClient creates a new Gemini client
func NewGeminiClient(apiKey, model string) (*GeminiClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API key is required")
	}

	if model == "" {
		model = "gemini-2.0-flash" // Use Flash model for fast inference
	}

	return &GeminiClient{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Call makes a request to Gemini API
func (g *GeminiClient) Call(ctx context.Context, prompt string) (string, error) {
	req := &GeminiRequest{
		Contents: []GeminiContent{
			{
				Role: "user",
				Parts: []GeminiPart{
					{
						Text: prompt,
					},
				},
			},
		},
	}

	// Marshal request to JSON
	reqBody, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request URL with API key
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		g.model, g.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")

	// Make the request
	resp, err := g.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to make API request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Gemini API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var geminiResp GeminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return "", fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	// Check for API errors
	if geminiResp.Error != nil {
		return "", fmt.Errorf("Gemini API error: %s", geminiResp.Error.Message)
	}

	// Extract text response
	if len(geminiResp.Candidates) == 0 {
		return "", fmt.Errorf("empty response from Gemini API")
	}

	if len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no text content in Gemini response")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}
