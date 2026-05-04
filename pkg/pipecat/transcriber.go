package pipecat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DeepgramClient handles transcription via Deepgram API
type DeepgramClient struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// DeepgramTranscriptionResponse represents the response from Deepgram API
type DeepgramTranscriptionResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string  `json:"transcript"`
				Confidence float64 `json:"confidence"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

// NewDeepgramClient creates a new Deepgram transcription client
func NewDeepgramClient(apiKey string) *DeepgramClient {
	return &DeepgramClient{
		apiKey:  apiKey,
		baseURL: "https://api.deepgram.com/v1/listen",
		model:   "nova-2",
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// TranscribeAudio sends audio data to Deepgram for transcription
func (c *DeepgramClient) TranscribeAudio(ctx context.Context, audioData []byte, filename string) (string, error) {
	url := c.baseURL + "?model=" + c.model + "&smart_format=true&punctuate=true"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(audioData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", "audio/wav")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Deepgram API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var transcriptionResp DeepgramTranscriptionResponse
	if err := json.Unmarshal(respBody, &transcriptionResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(transcriptionResp.Results.Channels) == 0 ||
		len(transcriptionResp.Results.Channels[0].Alternatives) == 0 {
		return "", fmt.Errorf("Deepgram response contained no transcript")
	}

	return transcriptionResp.Results.Channels[0].Alternatives[0].Transcript, nil
}
