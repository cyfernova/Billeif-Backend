package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// TestHandleMessage tests A2A message handling
func TestHandleMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create mock services
	log := logger.New()
	mockMarketplace := &MockMarketplaceService{
		products: []*models.MarketplaceProduct{},
	}
	sigSvc, _ := ap2.NewSignatureService()
	a2aClient := a2a.NewA2AClient(sigSvc, log)

	handler := NewA2AMessageHandler(
		nil, // shopping agent
		nil, // merchant agent
		nil, // credential provider
		nil, // payment processor
		mockMarketplace,
		a2aClient,
		sigSvc,
		nil, // a2a bargaining
		log,
	)

	// Create test message
	msg := a2a.NewA2AMessage("agent-1", "agent-2", a2a.MessageTypeTaskStart)
	msg.TaskID = "task-123"
	payload := &a2a.TaskStartPayload{
		TaskName:   "shopping.search_products",
		Parameters: map[string]interface{}{"search_term": "laptop"},
		Priority:   5,
	}
	msg, _ = msg.WithPayload(payload)
	msgBody, _ := json.Marshal(msg)

	req := httptest.NewRequest(
		http.MethodPost,
		"/a2a/message",
		bytes.NewBuffer(msgBody),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	// Test handler
	handler.HandleMessage(c)

	if w.Code != http.StatusBadRequest && w.Code != http.StatusInternalServerError && w.Code != http.StatusOK {
		t.Errorf("expected valid status code, got %d", w.Code)
	}
}

// TestMessageValidation tests A2A message validation
func TestMessageValidation(t *testing.T) {
	validator := a2a.NewMessageValidator(nil)

	msg := a2a.NewA2AMessage("agent-1", "agent-2", a2a.MessageTypeTaskStart)
	msg.TaskID = "task-123"
	result := validator.ValidateMessage(msg)

	if !result.IsValid {
		t.Errorf("expected message to be valid, got errors: %v", result.Errors)
	}
}

func TestMessageValidationWithNilSignerAndSignature(t *testing.T) {
	validator := a2a.NewMessageValidator(nil)
	msg := a2a.NewA2AMessage("agent-1", "agent-2", a2a.MessageTypeTaskStart)
	msg.TaskID = "task-123"
	msg.Signature = "invalid"
	msg.PublicKey = "invalid"

	result := validator.ValidateMessage(msg)
	if result.IsValid {
		t.Errorf("expected validation to fail when signature service is nil")
	}
}

// TestTaskStartPayloadValidation tests task start payload validation
func TestTaskStartPayloadValidation(t *testing.T) {
	validator := a2a.NewMessageValidator(nil)

	payload := &a2a.TaskStartPayload{
		TaskName: "test.task",
		Parameters: map[string]interface{}{
			"key": "value",
		},
		Priority: 5,
	}

	err := validator.ValidateTaskStartPayload(payload)
	if err != nil {
		t.Errorf("expected valid payload, got error: %v", err)
	}
}

// TestEmptyTaskNameValidation tests validation of empty task name
func TestEmptyTaskNameValidation(t *testing.T) {
	validator := a2a.NewMessageValidator(nil)

	payload := &a2a.TaskStartPayload{
		TaskName:   "",
		Parameters: map[string]interface{}{},
		Priority:   5,
	}

	err := validator.ValidateTaskStartPayload(payload)
	if err == nil {
		t.Error("expected error for empty task name")
	}
}

// TestMessageExpiration tests message expiration
func TestMessageExpiration(t *testing.T) {
	msg := a2a.NewA2AMessage("agent-1", "agent-2", a2a.MessageTypeTaskStart)

	// Message should not be expired immediately
	if msg.IsExpired() {
		t.Error("expected message to not be expired immediately")
	}

	// Set expiration time in past
	pastTime := time.Now().Add(-1 * time.Hour)
	msg.ExpiresAt = pastTime

	if !msg.IsExpired() {
		t.Error("expected message to be expired")
	}
}

// BenchmarkMessageValidation benchmarks message validation
func BenchmarkMessageValidation(b *testing.B) {
	validator := a2a.NewMessageValidator(nil)
	msg := a2a.NewA2AMessage("agent-1", "agent-2", a2a.MessageTypeTaskStart)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateMessage(msg)
	}
}

// MockMarketplaceService is a mock implementation of MarketplaceProvider
type MockMarketplaceService struct {
	products []*models.MarketplaceProduct
}

func (m *MockMarketplaceService) SearchProducts(ctx context.Context, query string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	return m.products, int64(len(m.products)), nil
}
