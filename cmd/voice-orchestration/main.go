// Package main is the entry point for the voice orchestration service.
// It provides a middleware API that handles: .wav upload -> STT transcription -> MIN MAX LLM processing.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
)

// Config holds the service endpoint configuration.
// Swap these values for different environments (local, staging, production).
var (
	MINMAX_STT_URL  = "https://api.minimax.io/v1/text/voice_transcription"
	MINMAX_LLM_URL  = "https://api.minimax.io/anthropic/v1/messages"
	MINMAX_API_KEY  = "sk-cp-zqif4snq4REvqsQ4Wmrw2jKeBI4PQs6jT2ENfYjM2N5nwkmS3GBvZoUvWnYG8aDclplWweqsEX_ybzZFh-aeremTMNhs4DptzZFuEzVsrCDpvhEqh_LsPKc"
)

// Response represents the JSON body returned on successful transcription + LLM processing.
type Response struct {
	Filename    string `json:"filename"`
	Transcript  string `json:"transcript"`
	LLMResponse string `json:"llm_response"`
}

// ErrorResponse represents the JSON body returned on failure.
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// Health handler: returns {"status": "ok"} for liveness checks.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// transcribeSTT sends the .wav file to the MiniMax STT service and extracts the transcript.
// It tries "text", "transcript", and "result" keys in that order.
func transcribeSTT(wavContent []byte, filename string) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add audio file under "file" field.
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create multipart form file: %w", err)
	}
	if _, err := part.Write(wavContent); err != nil {
		return "", fmt.Errorf("failed to write wav content: %w", err)
	}

	// Add model parameter (MiniMax expects "speech-01" for transcription).
	if err := writer.WriteField("model", "speech-01"); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}

	// Add language parameter (default to English).
	if err := writer.WriteField("language", "en"); err != nil {
		return "", fmt.Errorf("failed to write language field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, MINMAX_STT_URL, bytes.NewReader(body.Bytes()))
	if err != nil {
		return "", fmt.Errorf("failed to create STT request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", MINMAX_API_KEY))

	log.Printf("[STT] POST %s", MINMAX_STT_URL)
	log.Printf("[STT] Content-Type: %s", writer.FormDataContentType())
	log.Printf("[STT] Authorization: Bearer %s... [REDACTED]", MINMAX_API_KEY[:10])

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("STT service call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("[STT] Status: %d | Body: %s", resp.StatusCode, string(respBody))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("STT returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// MiniMax transcription response has fields: text, code, msg.
	var data map[string]any
	if err := json.Unmarshal(respBody, &data); err != nil {
		return "", fmt.Errorf("failed to parse STT JSON response: %w", err)
	}

	// Check for API error code.
	if code, ok := data["code"].(float64); ok && code != 0 {
		return "", fmt.Errorf("STT API error: %v", data["msg"])
	}

	// Try keys in order of preference.
	for _, key := range []string{"text", "transcript", "result"} {
		if val, ok := data[key]; ok {
			if str, ok := val.(string); ok && str != "" {
				log.Printf("[STT] Transcript extracted via key %q: %s", key, str)
				return str, nil
			}
		}
	}
	return "", fmt.Errorf("STT response contained no usable transcript field (tried text, transcript, result)")
}

// queryMINMAX sends the transcript to the MIN MAX LLM and extracts the reply.
// It tries "reply", "response", and "choices[0].message.content" keys in that order.
// MIN MAX (Anthropic-compatible) expects model + messages payload.
func queryMINMAX(transcript string) (string, error) {
	payload := map[string]any{
		"model": "MiniMax-M2.7",
		"messages": []map[string]string{
			{"role": "user", "content": transcript},
		},
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal MIN MAX payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, MINMAX_LLM_URL, bytes.NewReader(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("failed to create MIN MAX request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", MINMAX_API_KEY)
	req.Header.Set("anthropic-version", "2023-06-01")

	log.Printf("[LLM] POST %s", MINMAX_LLM_URL)
	log.Printf("[LLM] Headers: Content-Type=application/json, x-api-key=%s..., anthropic-version=2023-06-01", MINMAX_API_KEY[:10])
	log.Printf("[LLM] Payload: %s", string(jsonPayload))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("MIN MAX service call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("[LLM] Status: %d | Body: %s", resp.StatusCode, string(respBody))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("MIN MAX returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("failed to parse MIN MAX JSON response: %w", err)
	}

	// Try simple keys first.
	for _, key := range []string{"reply", "response"} {
		if val, ok := data[key]; ok {
			if str, ok := val.(string); ok && str != "" {
				return str, nil
			}
		}
	}

	// Try Anthropic-format "content" field from completion response.
	if content, ok := data["content"].(string); ok && content != "" {
		return content, nil
	}

	// Try nested "choices[0].message.content" via slice traversal.
	if choices, ok := data["choices"].([]any); ok && len(choices) > 0 {
		if choice0, ok := choices[0].(map[string]any); ok {
			if msg, ok := choice0["message"].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok && content != "" {
					return content, nil
				}
			}
		}
	}

	return "", fmt.Errorf("MIN MAX response contained no usable reply field (tried reply, response, content, choices[0].message.content)")
}

// voiceTranscribeHandler handles POST /api/v1/voice/transcribe.
// It expects a multipart form upload with a .wav file under field name "file".
func voiceTranscribeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errResp := ErrorResponse{Error: "method not allowed", Details: "only POST is accepted"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(errResp)
		return
	}

	// Parse multipart form with a 10 MB limit for the .wav file.
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		errResp := ErrorResponse{Error: "failed to parse multipart form", Details: err.Error()}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errResp)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		errResp := ErrorResponse{Error: "missing or invalid 'file' form field", Details: err.Error()}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errResp)
		return
	}
	defer file.Close()

	// Validate .wav extension by sniffing the Content-Type header too.
	filename := header.Filename
	if !strings.HasSuffix(strings.ToLower(filename), ".wav") {
		errResp := ErrorResponse{Error: "invalid file type", Details: "only .wav files are accepted"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errResp)
		return
	}

	wavContent, err := io.ReadAll(file)
	if err != nil {
		errResp := ErrorResponse{Error: "failed to read uploaded file", Details: err.Error()}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errResp)
		return
	}

	// Step 1: Transcribe via STT.
	log.Printf("[HANDLER] Received file: %s (%d bytes), starting STT transcription...", filename, len(wavContent))
	transcript, err := transcribeSTT(wavContent, filename)
	if err != nil {
		errResp := ErrorResponse{Error: "STT transcription failed", Details: err.Error()}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(errResp)
		return
	}

	// Step 2: Query MIN MAX LLM.
	log.Printf("[HANDLER] STT done, transcript=%q. Querying MIN MAX LLM...", transcript)
	llmResponse, err := queryMINMAX(transcript)
	if err != nil {
		errResp := ErrorResponse{Error: "MIN MAX LLM query failed", Details: err.Error()}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(errResp)
		return
	}

	// Step 3: Return combined result.
	resp := Response{
		Filename:    filename,
		Transcript:  transcript,
		LLMResponse: llmResponse,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/v1/voice/transcribe", voiceTranscribeHandler)

	fmt.Println("Voice Orchestration Service listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
