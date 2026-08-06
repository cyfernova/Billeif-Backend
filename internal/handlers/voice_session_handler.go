package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/voice/session"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

const (
	voiceUserCapacityRetrySeconds   = "5"
	voiceGlobalCapacityRetrySeconds = "10"
)

type VoiceSessionService interface {
	Create(context.Context, session.Scope, session.CreateInput) (*session.Session, error)
	Get(context.Context, session.Scope, string) (*session.Session, error)
	Resume(context.Context, session.Scope, string) (*session.Session, error)
	Close(context.Context, session.Scope, string) error
	CreateResponse(*session.Session) session.CreateResponse
}

type VoiceSessionHandler struct {
	svc VoiceSessionService
	log *logger.Logger
}

func NewVoiceSessionHandler(svc VoiceSessionService, log *logger.Logger) *VoiceSessionHandler {
	if svc == nil {
		return nil
	}
	if log == nil {
		log = logger.Global()
	}
	return &VoiceSessionHandler{svc: svc, log: log.Named("voice_session_handler")}
}

// Create admits a mobile voice session and returns its AgentCore attachment metadata.
// @Summary Create voice session
// @Description Creates an idempotent, capacity-controlled AgentCore voice session. Media and signaling payloads do not traverse this endpoint.
// @Tags Voice
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body session.CreateInput true "Voice session request"
// @Success 201 {object} session.CreateResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 429 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /voice/sessions [post]
func (h *VoiceSessionHandler) Create(c *gin.Context) {
	var input session.CreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body must be valid JSON"})
		return
	}
	scope, ok := validatedVoiceScope(c)
	if !ok {
		return
	}
	if branchID := strings.TrimSpace(input.BranchID); branchID != "" && middleware.GetValidatedBranchID(c) != branchID {
		c.JSON(http.StatusForbidden, gin.H{"error": "validated branch scope required"})
		return
	}
	created, err := h.svc.Create(c.Request.Context(), scope, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, h.svc.CreateResponse(created))
}

// Get returns non-transcript metadata for a voice session owned by the caller.
// @Summary Get voice session
// @Tags Voice
// @Produce json
// @Security BearerAuth
// @Param session_id path string true "Logical voice session ID"
// @Success 200 {object} session.SessionResponse
// @Failure 404 {object} map[string]string
// @Router /voice/sessions/{session_id} [get]
func (h *VoiceSessionHandler) Get(c *gin.Context) {
	scope, ok := validatedVoiceScope(c)
	if !ok {
		return
	}
	value, err := h.svc.Get(c.Request.Context(), scope, c.Param("session_id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, session.SessionMetadata(value))
}

// Resume conditionally extends a session lease and rotates only a stopped runtime ID.
// @Summary Resume voice session
// @Tags Voice
// @Produce json
// @Security BearerAuth
// @Param session_id path string true "Logical voice session ID"
// @Success 200 {object} session.CreateResponse
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /voice/sessions/{session_id}/resume [post]
func (h *VoiceSessionHandler) Resume(c *gin.Context) {
	scope, ok := validatedVoiceScope(c)
	if !ok {
		return
	}
	value, err := h.svc.Resume(c.Request.Context(), scope, c.Param("session_id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, h.svc.CreateResponse(value))
}

// Delete idempotently closes a voice session, releases admission, and stops its runtime.
// @Summary End voice session
// @Tags Voice
// @Security BearerAuth
// @Param session_id path string true "Logical voice session ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /voice/sessions/{session_id} [delete]
func (h *VoiceSessionHandler) Delete(c *gin.Context) {
	scope, ok := validatedVoiceScope(c)
	if !ok {
		return
	}
	if err := h.svc.Close(c.Request.Context(), scope, c.Param("session_id")); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func validatedVoiceScope(c *gin.Context) (session.Scope, bool) {
	allBranches, allowedBranchIDs, branchScopeOK := middleware.GetValidatedBranchScope(c)
	scope := session.Scope{
		UserID: middleware.GetUserID(c), BusinessID: middleware.GetValidatedBusinessID(c),
		AllBranches: allBranches, AllowedBranchIDs: append([]string{}, allowedBranchIDs...),
	}
	if scope.UserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return session.Scope{}, false
	}
	if scope.BusinessID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "validated business scope required"})
		return session.Scope{}, false
	}
	if !branchScopeOK {
		c.JSON(http.StatusForbidden, gin.H{"error": "validated branch scope required"})
		return session.Scope{}, false
	}
	return scope, true
}

func (h *VoiceSessionHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, session.ErrInvalidRequest):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, session.ErrIdempotencyConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "idempotency key conflict"})
	case errors.Is(err, session.ErrUserCapacity):
		c.Header("Retry-After", voiceUserCapacityRetrySeconds)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "an active voice session already exists"})
	case errors.Is(err, session.ErrGlobalCapacity):
		c.Header("Retry-After", voiceGlobalCapacityRetrySeconds)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "voice session capacity is temporarily full"})
	case errors.Is(err, session.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "voice session not found"})
	case errors.Is(err, session.ErrBranchForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "validated branch scope required"})
	case errors.Is(err, session.ErrNotResumable):
		c.JSON(http.StatusConflict, gin.H{"error": "voice session cannot be resumed"})
	case errors.Is(err, session.ErrUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "voice session service is unavailable"})
	default:
		h.log.Error("voice session request failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "voice session request failed"})
	}
}
