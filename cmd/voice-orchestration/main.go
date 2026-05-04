// Package main is the entry point for the voice orchestration service.
// It provides a middleware API that handles: .wav upload -> STT transcription -> LLM processing.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/spf13/viper"
)

// Config holds the service endpoint configuration.
var (
	DEEPGRAM_STT_URL string
	DEEPGRAM_API_KEY string
	MINMAX_LLM_URL   string
	MINMAX_API_KEY   string
)

func init() {
	viper.SetConfigName(".env")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Failed to read .env file: %v", err)
	}

	DEEPGRAM_STT_URL = viper.GetString("DEEPGRAM_API_URL")
	DEEPGRAM_API_KEY = viper.GetString("DEEPGRAM_API_KEY")
	MINMAX_LLM_URL = viper.GetString("LLM_API_URL")
	MINMAX_API_KEY = viper.GetString("LLM_API_KEY")

	if DEEPGRAM_STT_URL == "" {
		DEEPGRAM_STT_URL = "https://api.deepgram.com/v1/listen"
	}
	if DEEPGRAM_API_KEY == "" {
		log.Fatal("DEEPGRAM_API_KEY is required")
	}
}

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

// transcribeSTT sends the .wav file to the Deepgram STT service and extracts the transcript.
func transcribeSTT(wavContent []byte, filename string) (string, error) {
	url := DEEPGRAM_STT_URL + "?model=nova-2&smart_format=true&punctuate=true"

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(wavContent))
	if err != nil {
		return "", fmt.Errorf("failed to create STT request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+DEEPGRAM_API_KEY)
	req.Header.Set("Content-Type", "audio/wav")

	log.Printf("[STT] POST %s", url)
	log.Printf("[STT] Authorization: Token %s...", DEEPGRAM_API_KEY[:10])

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

	var data map[string]any
	if err := json.Unmarshal(respBody, &data); err != nil {
		return "", fmt.Errorf("failed to parse STT JSON response: %w", err)
	}

	// Navigate Deepgram response structure: results -> channels[0] -> alternatives[0] -> transcript
	results, ok := data["results"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("STT response contained no results")
	}
	channels, ok := results["channels"].([]any)
	if !ok || len(channels) == 0 {
		return "", fmt.Errorf("STT response contained no channels")
	}
	channel0, ok := channels[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("STT response contained invalid channel structure")
	}
	alternatives, ok := channel0["alternatives"].([]any)
	if !ok || len(alternatives) == 0 {
		return "", fmt.Errorf("STT response contained no alternatives")
	}
	alt0, ok := alternatives[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("STT response contained invalid alternative structure")
	}
	transcript, ok := alt0["transcript"].(string)
	if !ok || transcript == "" {
		return "", fmt.Errorf("STT response contained no transcript")
	}

	log.Printf("[STT] Transcript: %s", transcript)
	return transcript, nil
}

// queryMINMAX sends the transcript to the MIN MAX LLM and extracts the reply.
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

	// Validate .wav extension.
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

	// Step 1: Transcribe via Deepgram STT.
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
	fmt.Printf("Deepgram STT: %s\n", DEEPGRAM_STT_URL)
	log.Fatal(http.ListenAndServe(":8080", mux))
}