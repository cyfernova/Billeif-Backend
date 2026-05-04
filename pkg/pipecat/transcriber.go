package pipecat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// SpeakText sends text to Deepgram Aura and returns playable MP3 audio bytes.
func (c *DeepgramClient) SpeakText(ctx context.Context, text string) ([]byte, string, error) {
	requestBody, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal speech request: %w", err)
	}

	url := "https://api.deepgram.com/v1/speak?model=aura-2-thalia-en&encoding=mp3"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		return nil, "", fmt.Errorf("failed to create speech request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to make speech request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read speech response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("Deepgram speech API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "audio/mpeg"
	}

	return respBody, contentType, nil
}

func normalizeAudioContentType(contentType string) string {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch mediaType {
	case "":
		return "audio/wav"
	case "audio/m4a", "audio/x-m4a":
		return "audio/mp4"
	default:
		return contentType
	}
}

// TranscribeAudio sends audio data to Deepgram for transcription.
func (c *DeepgramClient) TranscribeAudio(ctx context.Context, audioData []byte, filename string, contentType string) (string, error) {
	url := c.baseURL + "?model=" + c.model + "&smart_format=true&punctuate=true"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(audioData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", normalizeAudioContentType(contentType))

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
