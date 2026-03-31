package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock LLMService
// =============================================================================

type MockLLMService struct {
	mock.Mock
}

func (m *MockLLMService) Chat(ctx context.Context, messages []services.ChatMessage) (string, error) {
	args := m.Called(ctx, messages)
	return args.String(0), args.Error(1)
}

func (m *MockLLMService) ProcessAgentIntent(ctx context.Context, intent string, contextInfo string) (string, error) {
	args := m.Called(ctx, intent, contextInfo)
	return args.String(0), args.Error(1)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type LLMHandlerTestable struct {
	llm *MockLLMService
	log *logger.Logger
}

func NewLLMHandlerTestable(llm *MockLLMService, log *logger.Logger) *LLMHandlerTestable {
	return &LLMHandlerTestable{
		llm: llm,
		log: log,
	}
}

func (h *LLMHandlerTestable) Chat(c *gin.Context) {
	var req struct {
		Messages []services.ChatMessage `json:"messages" binding:"required"`
	}
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

func (h *LLMHandlerTestable) AgentAssist(c *gin.Context) {
	var req struct {
		Intent  string `json:"intent" binding:"required"`
		Context string `json:"context"`
	}
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

// =============================================================================
// Chat Tests
// =============================================================================

func TestChat_Success(t *testing.T) {
	mockSvc := new(MockLLMService)
	log := logger.New()
	handler := NewLLMHandlerTestable(mockSvc, log)

	messages := []services.ChatMessage{
		{Role: "user", Content: "Hello, how are you?"},
	}
	expectedResponse := "I'm doing great, thanks for asking!"

	mockSvc.On("Chat", mock.Anything, messages).Return(expectedResponse, nil)

	router := gin.New()
	router.POST("/llm/chat", func(c *gin.Context) {
		handler.Chat(c)
	})

	reqBody := map[string]interface{}{
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello, how are you?"},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/llm/chat", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(res.Body.Bytes(), &resp)
	if resp["response"] != expectedResponse {
		t.Fatalf("expected response '%s', got '%s'", expectedResponse, resp["response"])
	}

	mockSvc.AssertExpectations(t)
}

func TestChat_ServiceError(t *testing.T) {
	mockSvc := new(MockLLMService)
	log := logger.New()
	handler := NewLLMHandlerTestable(mockSvc, log)

	messages := []services.ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	mockSvc.On("Chat", mock.Anything, messages).Return("", errors.New("LLM service unavailable"))

	router := gin.New()
	router.POST("/llm/chat", func(c *gin.Context) {
		handler.Chat(c)
	})

	reqBody := map[string]interface{}{
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/llm/chat", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestChat_InvalidInput_MissingMessages(t *testing.T) {
	mockSvc := new(MockLLMService)
	log := logger.New()
	handler := NewLLMHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/llm/chat", func(c *gin.Context) {
		handler.Chat(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/llm/chat", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestChat_InvalidInput_EmptyJSON(t *testing.T) {
	mockSvc := new(MockLLMService)
	log := logger.New()
	handler := NewLLMHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/llm/chat", func(c *gin.Context) {
		handler.Chat(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/llm/chat", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestChat_WithMultipleMessages(t *testing.T) {
	mockSvc := new(MockLLMService)
	log := logger.New()
	handler := NewLLMHandlerTestable(mockSvc, log)

	messages := []services.ChatMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "2+2 equals 4."},
	}
	expectedResponse := "Is there anything else you'd like to know?"

	mockSvc.On("Chat", mock.Anything, messages).Return(expectedResponse, nil)

	router := gin.New()
	router.POST("/llm/chat", func(c *gin.Context) {
		handler.Chat(c)
	})

	reqBody := map[string]interface{}{
		"messages": []map[string]interface{}{
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "What is 2+2?"},
			{"role": "assistant", "content": "2+2 equals 4."},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/llm/chat", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// AgentAssist Tests
// =============================================================================

func TestAgentAssist_Success(t *testing.T) {
	mockSvc := new(MockLLMService)
	log := logger.New()
	handler := NewLLMHandlerTestable(mockSvc, log)

	intent := "Create an AI agent for invoice processing"
	context := "Invoice processing involves extracting data from PDFs, validating GST numbers, and creating corresponding records in the database."
	expectedResponse := "Based on your requirements, I suggest creating an AI agent with the following capabilities..."

	mockSvc.On("ProcessAgentIntent", mock.Anything, intent, context).Return(expectedResponse, nil)

	router := gin.New()
	router.POST("/llm/agent-assist", func(c *gin.Context) {
		handler.AgentAssist(c)
	})

	reqBody := map[string]interface{}{
		"intent":  intent,
		"context": context,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/llm/agent-assist", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(res.Body.Bytes(), &resp)
	if resp["response"] != expectedResponse {
		t.Fatalf("expected response '%s', got '%s'", expectedResponse, resp["response"])
	}

	mockSvc.AssertExpectations(t)
}

func TestAgentAssist_Success_WithoutContext(t *testing.T) {
	mockSvc := new(MockLLMService)
	log := logger.New()
	handler := NewLLMHandlerTestable(mockSvc, log)

	intent := "Design a chatbot for customer support"
	expectedResponse := "Here's a design for a customer support chatbot..."

	mockSvc.On("ProcessAgentIntent", mock.Anything, intent, "").Return(expectedResponse, nil)

	router := gin.New()
	router.POST("/llm/agent-assist", func(c *gin.Context) {
		handler.AgentAssist(c)
	})

	reqBody := map[string]interface{}{
		"intent": intent,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/llm/agent-assist", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestAgentAssist_ServiceError(t *testing.T) {
	mockSvc := new(MockLLMService)
	log := logger.New()
	handler := NewLLMHandlerTestable(mockSvc, log)

	intent := "Create an AI agent"
	context := "Some context"

	mockSvc.On("ProcessAgentIntent", mock.Anything, intent, context).Return("", errors.New("LLM processing failed"))

	router := gin.New()
	router.POST("/llm/agent-assist", func(c *gin.Context) {
		handler.AgentAssist(c)
	})

	reqBody := map[string]interface{}{
		"intent":  intent,
		"context": context,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/llm/agent-assist", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestAgentAssist_InvalidInput_MissingIntent(t *testing.T) {
	mockSvc := new(MockLLMService)
	log := logger.New()
	handler := NewLLMHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/llm/agent-assist", func(c *gin.Context) {
		handler.AgentAssist(c)
	})

	reqBody := map[string]interface{}{
		"context": "Some context",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/llm/agent-assist", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestAgentAssist_InvalidInput_EmptyJSON(t *testing.T) {
	mockSvc := new(MockLLMService)
	log := logger.New()
	handler := NewLLMHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/llm/agent-assist", func(c *gin.Context) {
		handler.AgentAssist(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/llm/agent-assist", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}
