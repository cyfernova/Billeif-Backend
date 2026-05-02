package handlers

import (
	"encoding/base64"
	"io"
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// VoiceHandler handles voice transcription requests
type VoiceHandler struct {
	voice *services.VoiceService
	log   *logger.Logger
}

// NewVoiceHandler creates a new voice handler
func NewVoiceHandler(voice *services.VoiceService, log *logger.Logger) *VoiceHandler {
	return &VoiceHandler{
		voice: voice,
		log:   log,
	}
}

// TranscribeRequest represents the request body for transcription
type TranscribeRequest struct {
	Filename string `json:"filename"`
}

// Transcribe handles voice/audio transcription
// @Summary Transcribe audio
// @Description Converts audio data to text using MiniMax API
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
	result, err := h.voice.Transcribe(c.Request.Context(), audioData, filename)
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

	result, err := h.voice.Transcribe(c.Request.Context(), audioData, filename)
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
