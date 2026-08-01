package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type RealtimeVoiceHandler struct {
	svc            *services.RealtimeVoiceService
	log            *logger.Logger
	allowedOrigins []string
	upgrader       websocket.Upgrader
}

func NewRealtimeVoiceHandler(svc *services.RealtimeVoiceService, allowedOrigins []string, log *logger.Logger) *RealtimeVoiceHandler {
	handler := &RealtimeVoiceHandler{
		svc:            svc,
		log:            log.Named("realtime_voice_handler"),
		allowedOrigins: allowedOrigins,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 10 * time.Second,
		},
	}
	handler.upgrader.CheckOrigin = handler.checkOrigin
	return handler
}

func (h *RealtimeVoiceHandler) Handle(c *gin.Context) {
	if h == nil || h.svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "realtime voice service is not configured",
			"code":  "voice_realtime_unavailable",
		})
		return
	}

	if err := h.svc.ConfigError(); err != nil {
		h.log.Warn("realtime voice configuration unavailable", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "realtime voice service is not configured",
			"code":  "voice_realtime_unavailable",
		})
		return
	}

	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	requestedBusinessID := firstNonEmpty(c.Query("business_id"), c.GetHeader("business_id"), c.GetHeader("X-Business-ID"))
	businessID, ok := requireEffectiveBusinessScope(c, requestedBusinessID)
	if !ok {
		return
	}
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "business_id is required",
			"code":  "business_id_required",
		})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.log.Warn("failed to upgrade realtime voice websocket", "error", err, "user_id", userID, "business_id", businessID)
		return
	}

	h.svc.Serve(c.Request.Context(), conn, services.RealtimeVoiceSessionRequest{
		UserID:         userID,
		BusinessID:     businessID,
		ConversationID: strings.TrimSpace(c.Query("conversation_id")),
		AccessToken:    bearerTokenFromAuthorizationHeader(c.GetHeader("Authorization")),
		Voice:          strings.TrimSpace(c.Query("voice")),
		Language:       strings.TrimSpace(c.Query("language")),
	})
}

func bearerTokenFromAuthorizationHeader(header string) string {
	trimmed := strings.TrimSpace(header)
	parts := strings.SplitN(trimmed, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return strings.TrimSpace(parts[1])
	}
	return trimmed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (h *RealtimeVoiceHandler) checkOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	for _, allowed := range h.allowedOrigins {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}

	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := strings.ToLower(parsedOrigin.Host)
	for _, allowed := range h.allowedOrigins {
		parsedAllowed, err := url.Parse(strings.TrimSpace(allowed))
		if err == nil && strings.EqualFold(parsedAllowed.Host, originHost) {
			return true
		}
	}
	return false
}
