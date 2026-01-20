package handlers

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// LLMHandler handles LLM interactions
type LLMHandler struct {
	llm *services.LLMService
	log *logger.Logger
}

// NewLLMHandler creates a new LLM handler
func NewLLMHandler(llm *services.LLMService, log *logger.Logger) *LLMHandler {
	return &LLMHandler{
		llm: llm,
		log: log,
	}
}

// APIRequest represents the request body for the chat API
type APIRequest struct {
	Messages []services.ChatMessage `json:"messages" binding:"required"`
}

// AgentAssistRequest represents the request body for agent assist
type AgentAssistRequest struct {
	Intent  string `json:"intent" binding:"required"`
	Context string `json:"context"`
}

// Chat handles general chat requests
// POST /llm/chat
func (h *LLMHandler) Chat(c *gin.Context) {
	var req APIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.llm.Chat(c.Request.Context(), req.Messages)
	if err != nil {
		h.log.Error("failed to process chat request", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process chat request"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"response": response})
}

// AgentAssist handles agent-specific assistance
// POST /llm/agent-assist
func (h *LLMHandler) AgentAssist(c *gin.Context) {
	var req AgentAssistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.llm.ProcessAgentIntent(c.Request.Context(), req.Intent, req.Context)
	if err != nil {
		h.log.Error("failed to process agent intent", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process agent intent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"response": response})
}
