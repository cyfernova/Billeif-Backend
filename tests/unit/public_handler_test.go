package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock ReportService for Public endpoints
// =============================================================================

type MockReportServicePublic struct {
	mock.Mock
}

func (m *MockReportServicePublic) GetPublicMetadata(ctx context.Context, token string) (*services.PublicReportShareMetadata, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.PublicReportShareMetadata), args.Error(1)
}

func (m *MockReportServicePublic) AccessPublicShare(ctx context.Context, token string, input services.ReportShareAccessInput, ipAddress, userAgent string) (*services.PublicReportShareAccessResponse, error) {
	args := m.Called(ctx, token, input, ipAddress, userAgent)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.PublicReportShareAccessResponse), args.Error(1)
}

// =============================================================================
// Mock AgentDiscoveryService for Public endpoints
// =============================================================================

type MockAgentDiscoveryServicePublic struct {
	mock.Mock
}

func (m *MockAgentDiscoveryServicePublic) GetPublicAgents(ctx context.Context, page, limit int) ([]*models.AgentRegistry, int64, error) {
	args := m.Called(ctx, page, limit)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*models.AgentRegistry), args.Get(1).(int64), args.Error(2)
}

// =============================================================================
// Testable wrappers
// =============================================================================

type PublicReportHandlerTestable struct {
	svc *MockReportServicePublic
	log *logger.Logger
}

func NewPublicReportHandlerTestable(svc *MockReportServicePublic, log *logger.Logger) *PublicReportHandlerTestable {
	return &PublicReportHandlerTestable{svc: svc, log: log}
}

func (h *PublicReportHandlerTestable) PublicMetadata(c *gin.Context) {
	result, err := h.svc.GetPublicMetadata(c.Request.Context(), c.Param("token"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		switch err.Error() {
		case "report share expired", "report share revoked":
			statusCode = http.StatusGone
		default:
			if isNotFoundErr(err) {
				statusCode = http.StatusNotFound
			}
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *PublicReportHandlerTestable) PublicAccess(c *gin.Context) {
	var input services.ReportShareAccessInput
	_ = c.ShouldBindJSON(&input)
	if input.Page == 0 {
		input.Page = 1
	}
	if input.Limit == 0 {
		input.Limit = 20
	}

	result, err := h.svc.AccessPublicShare(c.Request.Context(), c.Param("token"), input, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		statusCode := http.StatusInternalServerError
		switch err.Error() {
		case "passcode required", "invalid passcode":
			statusCode = http.StatusUnauthorized
		case "report share expired", "report share revoked":
			statusCode = http.StatusGone
		default:
			if isNotFoundErr(err) {
				statusCode = http.StatusNotFound
			}
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

type PublicAgentDiscoveryHandlerTestable struct {
	discovery *MockAgentDiscoveryServicePublic
	log       *logger.Logger
}

func NewPublicAgentDiscoveryHandlerTestable(discovery *MockAgentDiscoveryServicePublic, log *logger.Logger) *PublicAgentDiscoveryHandlerTestable {
	return &PublicAgentDiscoveryHandlerTestable{discovery: discovery, log: log}
}

func (h *PublicAgentDiscoveryHandlerTestable) GetPublicAgents(c *gin.Context) {
	page := 1
	limit := 10

	agents, total, err := h.discovery.GetPublicAgents(c.Request.Context(), page, limit)
	if err != nil {
		h.log.Error("failed to get public agents", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get public agents"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"agents": agents,
		"total":  total,
		"page":   page,
		"limit":  limit,
	})
}

// =============================================================================
// Report PublicMetadata Tests
// =============================================================================

func TestPublicReportMetadata_Success(t *testing.T) {
	mockSvc := new(MockReportServicePublic)
	log := logger.New()
	handler := NewPublicReportHandlerTestable(mockSvc, log)

	now := time.Now()
	metadata := &services.PublicReportShareMetadata{
		Title:            "Monthly Sales Report",
		ReportKey:        "sales_monthly",
		ReportName:       "Monthly Sales",
		ShareMode:        "view",
		RequiresPasscode: false,
		ExpiresAt:        &now,
		Status:           "active",
		CreatedAt:        now,
		Columns:          []string{"date", "amount", "customer"},
	}

	mockSvc.On("GetPublicMetadata", mock.Anything, "token-123").Return(metadata, nil)

	router := gin.New()
	router.GET("/public/report-shares/:token/metadata", func(c *gin.Context) {
		handler.PublicMetadata(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/public/report-shares/token-123/metadata", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicReportMetadata_NotFound(t *testing.T) {
	mockSvc := new(MockReportServicePublic)
	log := logger.New()
	handler := NewPublicReportHandlerTestable(mockSvc, log)

	mockSvc.On("GetPublicMetadata", mock.Anything, "invalid-token").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/public/report-shares/:token/metadata", func(c *gin.Context) {
		handler.PublicMetadata(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/public/report-shares/invalid-token/metadata", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicReportMetadata_Expired(t *testing.T) {
	mockSvc := new(MockReportServicePublic)
	log := logger.New()
	handler := NewPublicReportHandlerTestable(mockSvc, log)

	mockSvc.On("GetPublicMetadata", mock.Anything, "expired-token").Return(nil, errors.New("report share expired"))

	router := gin.New()
	router.GET("/public/report-shares/:token/metadata", func(c *gin.Context) {
		handler.PublicMetadata(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/public/report-shares/expired-token/metadata", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Report PublicAccess Tests
// =============================================================================

func TestPublicReportAccess_Success(t *testing.T) {
	mockSvc := new(MockReportServicePublic)
	log := logger.New()
	handler := NewPublicReportHandlerTestable(mockSvc, log)

	metadata := &services.PublicReportShareMetadata{
		Title:  "Monthly Sales Report",
		Status: "active",
	}
	response := &services.PublicReportShareAccessResponse{
		Metadata: *metadata,
	}

	input := services.ReportShareAccessInput{
		Page:  1,
		Limit: 20,
	}

	mockSvc.On("AccessPublicShare", mock.Anything, "token-123", input, "192.0.2.1", "TestAgent").Return(response, nil)

	router := gin.New()
	router.POST("/public/report-shares/:token/access", func(c *gin.Context) {
		c.Request.RemoteAddr = "192.0.2.1:1234"
		c.Request.Header.Set("User-Agent", "TestAgent")
		handler.PublicAccess(c)
	})

	reqBody := map[string]interface{}{
		"page":  1,
		"limit": 20,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/report-shares/token-123/access", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicReportAccess_Unauthorized(t *testing.T) {
	mockSvc := new(MockReportServicePublic)
	log := logger.New()
	handler := NewPublicReportHandlerTestable(mockSvc, log)

	input := services.ReportShareAccessInput{
		Page:  1,
		Limit: 20,
	}

	mockSvc.On("AccessPublicShare", mock.Anything, "protected-token", input, "192.0.2.1", "TestAgent").Return(nil, errors.New("passcode required"))

	router := gin.New()
	router.POST("/public/report-shares/:token/access", func(c *gin.Context) {
		c.Request.RemoteAddr = "192.0.2.1:1234"
		c.Request.Header.Set("User-Agent", "TestAgent")
		handler.PublicAccess(c)
	})

	reqBody := map[string]interface{}{
		"page": 1,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/report-shares/protected-token/access", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicReportAccess_Expired(t *testing.T) {
	mockSvc := new(MockReportServicePublic)
	log := logger.New()
	handler := NewPublicReportHandlerTestable(mockSvc, log)

	input := services.ReportShareAccessInput{
		Page:  1,
		Limit: 20,
	}

	mockSvc.On("AccessPublicShare", mock.Anything, "expired-token", input, "192.0.2.1", "TestAgent").Return(nil, errors.New("report share expired"))

	router := gin.New()
	router.POST("/public/report-shares/:token/access", func(c *gin.Context) {
		c.Request.RemoteAddr = "192.0.2.1:1234"
		c.Request.Header.Set("User-Agent", "TestAgent")
		handler.PublicAccess(c)
	})

	reqBody := map[string]interface{}{
		"page": 1,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/report-shares/expired-token/access", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestPublicReportAccess_NotFound(t *testing.T) {
	mockSvc := new(MockReportServicePublic)
	log := logger.New()
	handler := NewPublicReportHandlerTestable(mockSvc, log)

	input := services.ReportShareAccessInput{
		Page:  1,
		Limit: 20,
	}

	mockSvc.On("AccessPublicShare", mock.Anything, "nonexistent-token", input, "192.0.2.1", "TestAgent").Return(nil, ErrNotFound)

	router := gin.New()
	router.POST("/public/report-shares/:token/access", func(c *gin.Context) {
		c.Request.RemoteAddr = "192.0.2.1:1234"
		c.Request.Header.Set("User-Agent", "TestAgent")
		handler.PublicAccess(c)
	})

	reqBody := map[string]interface{}{
		"page": 1,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/public/report-shares/nonexistent-token/access", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// AgentDiscovery GetPublicAgents Tests
// =============================================================================

func TestGetPublicAgents_Success(t *testing.T) {
	mockSvc := new(MockAgentDiscoveryServicePublic)
	log := logger.New()
	handler := NewPublicAgentDiscoveryHandlerTestable(mockSvc, log)

	agents := []*models.AgentRegistry{
		{AgentName: "Invoice Agent", AgentType: "invoice"},
		{AgentName: "Payment Agent", AgentType: "payment"},
	}

	mockSvc.On("GetPublicAgents", mock.Anything, 1, 10).Return(agents, int64(2), nil)

	router := gin.New()
	router.GET("/discovery/agents/public", func(c *gin.Context) {
		handler.GetPublicAgents(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/discovery/agents/public", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetPublicAgents_Empty(t *testing.T) {
	mockSvc := new(MockAgentDiscoveryServicePublic)
	log := logger.New()
	handler := NewPublicAgentDiscoveryHandlerTestable(mockSvc, log)

	mockSvc.On("GetPublicAgents", mock.Anything, 1, 10).Return([]*models.AgentRegistry{}, int64(0), nil)

	router := gin.New()
	router.GET("/discovery/agents/public", func(c *gin.Context) {
		handler.GetPublicAgents(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/discovery/agents/public", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestGetPublicAgents_InternalError(t *testing.T) {
	mockSvc := new(MockAgentDiscoveryServicePublic)
	log := logger.New()
	handler := NewPublicAgentDiscoveryHandlerTestable(mockSvc, log)

	mockSvc.On("GetPublicAgents", mock.Anything, 1, 10).Return(nil, int64(0), errors.New("service error"))

	router := gin.New()
	router.GET("/discovery/agents/public", func(c *gin.Context) {
		handler.GetPublicAgents(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/discovery/agents/public", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}
