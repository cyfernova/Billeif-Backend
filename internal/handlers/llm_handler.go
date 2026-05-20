package handlers

import (
	"net/http"
	"strconv"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// LLMHandler handles LLM interactions
type LLMHandler struct {
	llm     *services.LLMService
	history *services.LLMChatHistoryService
	log     *logger.Logger
}

// NewLLMHandler creates a new LLM handler
func NewLLMHandler(llm *services.LLMService, history *services.LLMChatHistoryService, log *logger.Logger) *LLMHandler {
	return &LLMHandler{
		llm:     llm,
		history: history,
		log:     log,
	}
}

// APIRequest represents the request body for the chat API
type APIRequest struct {
	Messages       []services.ChatMessage `json:"messages" binding:"required"`
	ConversationID string                 `json:"conversation_id,omitempty"`
}

// AgentAssistRequest represents the request body for agent assist
type AgentAssistRequest struct {
	Intent  string `json:"intent" binding:"required"`
	Context string `json:"context"`
}

// Chat handles general chat requests
// @Summary Chat with LLM
// @Description Sends a chat request to the configured LLM (Claude/Gemini)
// @Tags LLM
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body APIRequest true "Chat messages"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /llm/chat [post]
func (h *LLMHandler) Chat(c *gin.Context) {
	var req APIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}

	response, err := h.llm.ChatWithWebSearch(c.Request.Context(), req.Messages)
	if err != nil {
		h.log.Error("failed to process chat request", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "raw_response": "check server logs for MINMAX RAW RESPONSE"})
		return
	}
	if _, _, err := h.history.SaveExchange(c.Request.Context(), services.SaveLLMChatExchangeInput{
		BusinessID:     businessID,
		UserID:         userID,
		ConversationID: req.ConversationID,
		Messages:       req.Messages,
		Result:         response,
	}); err != nil {
		h.log.Error("failed to persist chat history", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *LLMHandler) ListChatConversations(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	conversations, err := h.history.ListConversations(c.Request.Context(), businessID, userID, limit)
	if err != nil {
		h.log.Error("failed to list chat conversations", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": conversations})
}

func (h *LLMHandler) ListChatMessages(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	messages, err := h.history.ListMessages(c.Request.Context(), businessID, userID, c.Param("id"))
	if err != nil {
		h.log.Error("failed to list chat messages", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": messages})
}

// AgentAssist handles agent-specific assistance
// @Summary Agent assist
// @Description Provides LLM-powered assistance for agent workflows
// @Tags LLM
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body AgentAssistRequest true "Agent assist request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /llm/agent-assist [post]
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
