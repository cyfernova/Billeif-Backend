package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// VoiceHandler handles voice transcription requests
type VoiceHandler struct {
	voice voiceTranscribeService
	llm   llmChatService
	log   *logger.Logger
}

type voiceTranscribeService interface {
	Transcribe(ctx context.Context, audioData []byte, filename string, contentType string) (*services.TranscriptionResult, error)
	Speak(ctx context.Context, text string) (*services.SpeechResult, error)
}

type llmChatService interface {
	Chat(ctx context.Context, messages []services.ChatMessage) (string, error)
	ChatWithOptions(ctx context.Context, messages []services.ChatMessage, options services.LLMChatOptions) (string, error)
}

const voiceAgentMaxTokens = 240

const voiceAgentSystemPrompt = "You are Billeif's voice agent for small-business finance. Answer directly in plain text, keep replies under 80 words, and do not include reasoning."

// NewVoiceHandler creates a new voice handler
func NewVoiceHandler(voice voiceTranscribeService, llm llmChatService, log *logger.Logger) *VoiceHandler {
	return &VoiceHandler{
		voice: voice,
		llm:   llm,
		log:   log,
	}
}

// TranscribeRequest represents the request body for transcription
type TranscribeRequest struct {
	Filename string `json:"filename"`
}

// Transcribe handles voice/audio transcription
// @Summary Transcribe audio
// @Description Converts audio data to text using Deepgram
// @Tags Voice
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "Audio file (WAV)"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /voice/transcribe [post]
func (h *VoiceHandler) Transcribe(c *gin.Context) {
	// Get the audio file from form data
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		h.log.Error("failed to get audio file", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "audio file is required"})
		return
	}
	defer file.Close()

	// Read file content
	audioData, err := io.ReadAll(file)
	if err != nil {
		h.log.Error("failed to read audio file", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read audio file"})
		return
	}

	// Get filename
	filename := header.Filename
	if filename == "" {
		filename = "audio.wav"
	}

	// Transcribe
	result, err := h.voice.Transcribe(c.Request.Context(), audioData, filename, uploadedContentType(headerContentType(header)))
	if err != nil {
		h.log.Error("transcription failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"text":     result.Text,
		"filename": filename,
		"size":     len(audioData),
	})
}

type VoiceAgentResponse struct {
	Transcript               string `json:"transcript"`
	Response                 string `json:"response"`
	ResponseAudioBase64      string `json:"response_audio_base64,omitempty"`
	ResponseAudioContentType string `json:"response_audio_content_type,omitempty"`
	ResponseAudioFilename    string `json:"response_audio_filename,omitempty"`
	Filename                 string `json:"filename"`
	Size                     int    `json:"size"`
}

// Agent handles one full voice-agent turn: upload audio, transcribe, then ask the LLM.
// @Summary Send a voice agent turn
// @Description Transcribes uploaded audio, appends the transcript to visible chat history, and returns the MiniMax text response.
// @Tags Voice
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "Audio file"
// @Param messages formData string false "JSON array of prior visible user/assistant messages"
// @Success 200 {object} VoiceAgentResponse
// @Failure 400 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /voice/agent [post]
func (h *VoiceHandler) Agent(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		h.log.Error("failed to get voice agent audio file", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "audio file is required"})
		return
	}
	defer file.Close()

	audioData, err := io.ReadAll(file)
	if err != nil {
		h.log.Error("failed to read voice agent audio file", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read audio file"})
		return
	}
	if len(audioData) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "audio file is empty"})
		return
	}

	messages, err := parseVoiceAgentMessages(c.PostForm("messages"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filename := header.Filename
	if filename == "" {
		filename = "audio.wav"
	}

	result, err := h.voice.Transcribe(c.Request.Context(), audioData, filename, uploadedContentType(headerContentType(header)))
	if err != nil {
		h.log.Error("voice agent transcription failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	transcript := strings.TrimSpace(result.Text)
	if transcript == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "transcript is empty"})
		return
	}

	messages = append(messages, services.ChatMessage{Role: "user", Content: transcript})
	response, err := h.llm.ChatWithOptions(c.Request.Context(), messages, services.LLMChatOptions{
		MaxTokens: voiceAgentMaxTokens,
		System:    voiceAgentSystemPrompt,
	})
	if err != nil {
		h.log.Error("voice agent LLM request failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	speech, err := h.voice.Speak(c.Request.Context(), response)
	if err != nil {
		h.log.Error("voice agent speech synthesis failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, VoiceAgentResponse{
		Transcript:               transcript,
		Response:                 response,
		ResponseAudioBase64:      base64.StdEncoding.EncodeToString(speech.Audio),
		ResponseAudioContentType: speech.ContentType,
		ResponseAudioFilename:    "billeif-response.mp3",
		Filename:                 filename,
		Size:                     len(audioData),
	})
}

// TranscribeBytes handles transcription from raw bytes (for internal use)
// @Summary Transcribe audio from bytes
// @Description Internal endpoint for processing audio bytes
// @Tags Voice
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body TranscribeBytesRequest true "Audio bytes request"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /voice/transcribe-bytes [post]
func (h *VoiceHandler) TranscribeBytes(c *gin.Context) {
	var req TranscribeBytesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Convert base64 to bytes if needed
	var audioData []byte
	if req.AudioBase64 != "" {
		// Use base64 data
		decoded, err := decodeBase64(req.AudioBase64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid base64 data"})
			return
		}
		audioData = decoded
	} else if len(req.AudioBytes) > 0 {
		audioData = req.AudioBytes
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "audio data is required"})
		return
	}

	filename := req.Filename
	if filename == "" {
		filename = "audio.wav"
	}

	result, err := h.voice.Transcribe(c.Request.Context(), audioData, filename, "")
	if err != nil {
		h.log.Error("transcription failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"text": result.Text})
}

// TranscribeBytesRequest represents the request for byte-based transcription
type TranscribeBytesRequest struct {
	Filename    string `json:"filename"`
	AudioBytes  []byte `json:"audio_bytes"`
	AudioBase64 string `json:"audio_base64"`
}

// decodeBase64 decodes a base64 string to bytes
func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

type voiceAgentMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func parseVoiceAgentMessages(raw string) ([]services.ChatMessage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var visibleMessages []voiceAgentMessage
	if err := json.Unmarshal([]byte(raw), &visibleMessages); err != nil {
		return nil, fmt.Errorf("messages must be a JSON array of visible chat messages")
	}

	messages := make([]services.ChatMessage, 0, len(visibleMessages))
	for i, message := range visibleMessages {
		role := strings.TrimSpace(message.Role)
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("messages[%d].role must be user or assistant", i)
		}

		content := strings.TrimSpace(message.Content)
		if content == "" {
			return nil, fmt.Errorf("messages[%d].content is required", i)
		}

		messages = append(messages, services.ChatMessage{Role: role, Content: content})
	}

	return messages, nil
}

func headerContentType(header *multipart.FileHeader) string {
	if header == nil {
		return ""
	}
	return header.Header.Get("Content-Type")
}

func uploadedContentType(contentType string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return "audio/wav"
	}
	return contentType
}
