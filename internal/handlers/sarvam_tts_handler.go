package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type SarvamTTSHandler struct {
	svc *services.SarvamTTSService
	log *logger.Logger
}

func NewSarvamTTSHandler(svc *services.SarvamTTSService, log *logger.Logger) *SarvamTTSHandler {
	return &SarvamTTSHandler{svc: svc, log: log.Named("sarvam_tts_handler")}
}

// Synthesize converts text in any supported Sarvam language into audio.
// @Summary Convert text to speech with Sarvam AI
// @Description Converts up to 3500 characters to speech using Sarvam Bulbul v3 and returns binary audio.
// @Tags Voice
// @Accept json
// @Produce audio/mpeg,audio/wav,audio/aac,audio/ogg,audio/flac,application/octet-stream
// @Security BearerAuth
// @Param input body services.SarvamTTSRequest true "Text-to-speech options"
// @Success 200 {file} binary "Synthesized audio"
// @Failure 400 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /voice/text-to-speech [post]
func (h *SarvamTTSHandler) Synthesize(c *gin.Context) {
	var input services.SarvamTTSRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body must be valid JSON"})
		return
	}
	result, err := h.svc.Synthesize(c.Request.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidSarvamTTS):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, services.ErrSarvamTTSUnavailable):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		default:
			h.log.Error("Sarvam text-to-speech failed", "error", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "text-to-speech provider failed"})
		}
		return
	}
	if strings.TrimSpace(result.RequestID) != "" {
		c.Header("X-Sarvam-Request-ID", result.RequestID)
	}
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=voice.%s", responseFileExtension(result.ContentType)))
	c.Data(http.StatusOK, result.ContentType, result.Audio)
}

// ListLanguages lists every language supported by the Sarvam TTS integration.
// @Summary List Sarvam text-to-speech languages
// @Tags Voice
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Router /voice/text-to-speech/languages [get]
func (h *SarvamTTSHandler) ListLanguages(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"model": services.SarvamTTSModel, "languages": services.SarvamLanguages})
}

func responseFileExtension(contentType string) string {
	switch strings.Split(contentType, ";")[0] {
	case "audio/mpeg":
		return "mp3"
	case "audio/wav":
		return "wav"
	case "audio/aac":
		return "aac"
	case "audio/ogg":
		return "opus"
	case "audio/flac":
		return "flac"
	default:
		return "audio"
	}
}
