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
	"invoice-backend/pkg/nlp"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock IntentProcessingService
// =============================================================================

type MockIntentProcessingService struct {
	mock.Mock
}

func (m *MockIntentProcessingService) ProcessIntent(ctx context.Context, req *services.ProcessIntentRequest) *services.ProcessIntentResponse {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*services.ProcessIntentResponse)
}

func (m *MockIntentProcessingService) ParseIntentOnly(ctx context.Context, naturalLanguage string) (*nlp.IntentParseResult, error) {
	args := m.Called(ctx, naturalLanguage)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*nlp.IntentParseResult), args.Error(1)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type IntentHandlerTestable struct {
	intentProcessing *MockIntentProcessingService
	log              *logger.Logger
}

func NewIntentHandlerTestable(intentProcessing *MockIntentProcessingService, log *logger.Logger) *IntentHandlerTestable {
	return &IntentHandlerTestable{
		intentProcessing: intentProcessing,
		log:              log,
	}
}

func (h *IntentHandlerTestable) ProcessIntent(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		Intent     string `json:"intent" binding:"required"`
		MaxResults int    `json:"max_results,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.MaxResults == 0 {
		req.MaxResults = 20
	}

	processReq := &services.ProcessIntentRequest{
		NaturalLanguage: req.Intent,
		UserID:          userID,
		MaxResults:      req.MaxResults,
	}

	result := h.intentProcessing.ProcessIntent(c.Request.Context(), processReq)

	if !result.Success {
		h.log.Warn("intent processing failed", "user_id", userID, "error", result.Error)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": result.Error,
		})
		return
	}

	h.log.Info("intent processed successfully",
		"user_id", userID,
		"confidence", result.ParseResult.Confidence,
		"matched_products", result.MatchResults.TotalMatched)

	c.JSON(http.StatusOK, gin.H{
		"parse_result": result.ParseResult,
		"match_results": gin.H{
			"total_matched":  result.MatchResults.TotalMatched,
			"total_searched": result.MatchResults.TotalSearched,
			"products":       result.MatchResults.Products,
		},
	})
}

func (h *IntentHandlerTestable) ValidateIntent(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		Intent string `json:"intent" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	parseReq := &services.ProcessIntentRequest{
		NaturalLanguage: req.Intent,
		UserID:          userID,
		MaxResults:      1,
	}

	result := h.intentProcessing.ProcessIntent(c.Request.Context(), parseReq)

	c.JSON(http.StatusOK, gin.H{
		"valid":            result.Success,
		"confidence":       result.ParseResult.Confidence,
		"matched_products": result.MatchResults.TotalMatched,
		"error":            result.Error,
	})
}

func (h *IntentHandlerTestable) ParseIntent(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		Intent string `json:"intent" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	parseResult, err := h.intentProcessing.ParseIntentOnly(c.Request.Context(), req.Intent)
	if err != nil {
		h.log.Error("intent parsing failed", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to parse intent",
		})
		return
	}

	h.log.Info("intent parsed",
		"user_id", userID,
		"confidence", parseResult.Confidence,
		"categories", parseResult.Intent.Categories,
		"keywords", parseResult.Intent.Keywords)

	c.JSON(http.StatusOK, parseResult)
}

// =============================================================================
// ProcessIntent Tests
// =============================================================================

func TestProcessIntent_Success(t *testing.T) {
	mockSvc := new(MockIntentProcessingService)
	log := logger.New()
	handler := NewIntentHandlerTestable(mockSvc, log)

	parseResult := &nlp.IntentParseResult{
		Intent: &nlp.ShoppingIntent{
			Categories: []string{"electronics", "computers"},
			Keywords:   []string{"laptop", "gaming"},
			Quantity:   1,
		},
		Confidence:  85.5,
		RawResponse: "mock raw response",
		Parsed:      true,
	}

	matchResults := &services.MatchResults{
		TotalMatched:  10,
		TotalSearched: 25,
		Products:      []*services.MatchedProduct{},
		Intent:        parseResult.Intent,
	}

	response := &services.ProcessIntentResponse{
		ParseResult:  parseResult,
		MatchResults: matchResults,
		Success:      true,
	}

	mockSvc.On("ProcessIntent", mock.Anything, mock.Anything).Return(response)

	router := gin.New()
	router.POST("/intent/process", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ProcessIntent(c)
	})

	reqBody := map[string]interface{}{
		"intent": "I need a gaming laptop",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/intent/process", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestProcessIntent_Failure(t *testing.T) {
	mockSvc := new(MockIntentProcessingService)
	log := logger.New()
	handler := NewIntentHandlerTestable(mockSvc, log)

	response := &services.ProcessIntentResponse{
		Success: false,
		Error:   "intent parsing failed",
	}

	mockSvc.On("ProcessIntent", mock.Anything, mock.Anything).Return(response)

	router := gin.New()
	router.POST("/intent/process", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ProcessIntent(c)
	})

	reqBody := map[string]interface{}{
		"intent": "some query that will fail",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/intent/process", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestProcessIntent_InvalidInput(t *testing.T) {
	mockSvc := new(MockIntentProcessingService)
	log := logger.New()
	handler := NewIntentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/intent/process", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ProcessIntent(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/intent/process", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestProcessIntent_WithMaxResults(t *testing.T) {
	mockSvc := new(MockIntentProcessingService)
	log := logger.New()
	handler := NewIntentHandlerTestable(mockSvc, log)

	parseResult := &nlp.IntentParseResult{
		Intent: &nlp.ShoppingIntent{
			Categories: []string{"electronics"},
			Keywords:   []string{"phone"},
		},
		Confidence: 90.0,
		Parsed:     true,
	}

	matchResults := &services.MatchResults{
		TotalMatched:  5,
		TotalSearched: 10,
		Products:      []*services.MatchedProduct{},
		Intent:        parseResult.Intent,
	}

	response := &services.ProcessIntentResponse{
		ParseResult:  parseResult,
		MatchResults: matchResults,
		Success:      true,
	}

	mockSvc.On("ProcessIntent", mock.Anything, mock.Anything).Return(response)

	router := gin.New()
	router.POST("/intent/process", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ProcessIntent(c)
	})

	reqBody := map[string]interface{}{
		"intent":      "buy a phone",
		"max_results": 5,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/intent/process", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ValidateIntent Tests
// =============================================================================

func TestValidateIntent_Success(t *testing.T) {
	mockSvc := new(MockIntentProcessingService)
	log := logger.New()
	handler := NewIntentHandlerTestable(mockSvc, log)

	parseResult := &nlp.IntentParseResult{
		Intent: &nlp.ShoppingIntent{
			Categories: []string{"electronics"},
			Keywords:   []string{"laptop"},
		},
		Confidence: 80.0,
		Parsed:     true,
	}

	matchResults := &services.MatchResults{
		TotalMatched:  15,
		TotalSearched: 50,
		Products:      []*services.MatchedProduct{},
		Intent:        parseResult.Intent,
	}

	response := &services.ProcessIntentResponse{
		ParseResult:  parseResult,
		MatchResults: matchResults,
		Success:      true,
	}

	mockSvc.On("ProcessIntent", mock.Anything, mock.Anything).Return(response)

	router := gin.New()
	router.POST("/intent/validate", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ValidateIntent(c)
	})

	reqBody := map[string]interface{}{
		"intent": "gaming laptop",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/intent/validate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestValidateIntent_InvalidInput(t *testing.T) {
	mockSvc := new(MockIntentProcessingService)
	log := logger.New()
	handler := NewIntentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/intent/validate", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ValidateIntent(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/intent/validate", bytes.NewBuffer([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// ParseIntent Tests
// =============================================================================

func TestParseIntent_Success(t *testing.T) {
	mockSvc := new(MockIntentProcessingService)
	log := logger.New()
	handler := NewIntentHandlerTestable(mockSvc, log)

	parseResult := &nlp.IntentParseResult{
		Intent: &nlp.ShoppingIntent{
			Categories:      []string{"electronics", "computers"},
			Keywords:        []string{"laptop", "gaming", "high-performance"},
			PriceRange:      &nlp.PriceRange{Min: 500, Max: 2000},
			Brands:          []string{"Dell", "HP"},
			Urgency:         "medium",
			Quantity:        1,
			RawIntent:       "I need a gaming laptop under $2000",
			MaxDeliveryDays: intPointer(7),
		},
		Confidence:  92.5,
		RawResponse: "mock parsed response",
		Parsed:      true,
	}

	mockSvc.On("ParseIntentOnly", mock.Anything, "gaming laptop under 2000 dollars").Return(parseResult, nil)

	router := gin.New()
	router.POST("/intent/parse", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ParseIntent(c)
	})

	reqBody := map[string]interface{}{
		"intent": "gaming laptop under 2000 dollars",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/intent/parse", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestParseIntent_ServiceError(t *testing.T) {
	mockSvc := new(MockIntentProcessingService)
	log := logger.New()
	handler := NewIntentHandlerTestable(mockSvc, log)

	mockSvc.On("ParseIntentOnly", mock.Anything, "complex query").Return(nil, errors.New("parsing error"))

	router := gin.New()
	router.POST("/intent/parse", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ParseIntent(c)
	})

	reqBody := map[string]interface{}{
		"intent": "complex query",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/intent/parse", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestParseIntent_InvalidInput(t *testing.T) {
	mockSvc := new(MockIntentProcessingService)
	log := logger.New()
	handler := NewIntentHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/intent/parse", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ParseIntent(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/intent/parse", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// Helper
// =============================================================================

func intPointer(i int) *int {
	return &i
}
