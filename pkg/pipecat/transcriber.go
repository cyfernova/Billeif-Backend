package pipecat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// MiniMaxClient handles transcription via MiniMax API
type MiniMaxClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// MiniMaxTranscriptionRequest represents the request to MiniMax transcription API
type MiniMaxTranscriptionRequest struct {
	Model     string `json:"model"`
	AudioURL  string `json:"audio_url,omitempty"`
	Language  string `json:"language,omitempty"`
	Timestamp bool   `json:"timestamp,omitempty"`
}

// MiniMaxTranscriptionResponse represents the response from MiniMax transcription API
type MiniMaxTranscriptionResponse struct {
	Text string `json:"text"`
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// NewMiniMaxClient creates a new MiniMax transcription client
func NewMiniMaxClient(apiKey string) *MiniMaxClient {
	return &MiniMaxClient{
		apiKey:  apiKey,
		baseURL: "https://api.minimax.io/v1",
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// TranscribeAudio sends audio data to MiniMax for transcription
func (c *MiniMaxClient) TranscribeAudio(ctx context.Context, audioData []byte, filename string) (string, error) {
	// Create multipart form data request
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add audio file
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return "", fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add model parameter
	if err := writer.WriteField("model", "speech-01"); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}

	// Add language parameter (default to English)
	if err := writer.WriteField("language", "en"); err != nil {
		return "", fmt.Errorf("failed to write language field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close writer: %w", err)
	}

	// Create request
	url := c.baseURL + "/text/voice_transcription"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	// Make request
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("MiniMax API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var transcriptionResp MiniMaxTranscriptionResponse
	if err := json.Unmarshal(respBody, &transcriptionResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if transcriptionResp.Code != 0 {
		return "", fmt.Errorf("MiniMax API error: %s", transcriptionResp.Msg)
	}

	return transcriptionResp.Text, nil
}

// TranscribeWithLLMSummary sends transcribed text to LLM for processing/summary
func (c *MiniMaxClient) TranscribeWithLLMSummary(ctx context.Context, audioData []byte, filename string, systemPrompt string) (string, error) {
	transcription, err := c.TranscribeAudio(ctx, audioData, filename)
	if err != nil {
		return "", fmt.Errorf("transcription failed: %w", err)
	}

	return transcription, nil
}
