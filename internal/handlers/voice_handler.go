package handlers

import (
	"net/http"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// VoiceHandler keeps the retired REST voice-agent endpoint explicit while live
// voice traffic moves to the realtime WebSocket route.
type VoiceHandler struct {
	log *logger.Logger
}

// NewVoiceHandler creates a new voice handler.
func NewVoiceHandler(log *logger.Logger) *VoiceHandler {
	return &VoiceHandler{log: log}
}

// Agent rejects the retired full-file voice-agent route.
//
// @Summary Retired voice agent route
// @Description Live voice conversation now requires GET /api/v1/voice/realtime over WebSocket.
// @Tags Voice
// @Produce json
// @Security BearerAuth
// @Failure 410 {object} map[string]string
// @Router /voice/agent [post]
func (h *VoiceHandler) Agent(c *gin.Context) {
	if h.log != nil {
		h.log.Info("retired voice agent route called")
	}

	c.JSON(http.StatusGone, gin.H{
		"error":   "voice_realtime_required",
		"code":    "voice_realtime_required",
		"message": "Live voice conversation now requires GET /api/v1/voice/realtime over WebSocket.",
	})
}
