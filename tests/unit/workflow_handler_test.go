package unit

// go test -v ./tests/unit/... -run "TestWorkflow"
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock WorkflowService
// =============================================================================

type MockWorkflowService struct {
	mock.Mock
}

func (m *MockWorkflowService) CreateWorkflow(ctx context.Context, workflow *services.Workflow) error {
	args := m.Called(ctx, workflow)
	return args.Error(0)
}

func (m *MockWorkflowService) GetWorkflow(ctx context.Context, workflowID string) (*services.Workflow, error) {
	args := m.Called(ctx, workflowID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.Workflow), args.Error(1)
}

func (m *MockWorkflowService) GetWorkflowsByUser(ctx context.Context, userID string, page, limit int) ([]services.Workflow, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]services.Workflow), args.Get(1).(int64), args.Error(2)
}

func (m *MockWorkflowService) UpdateWorkflow(ctx context.Context, workflow *services.Workflow) error {
	args := m.Called(ctx, workflow)
	return args.Error(0)
}

func (m *MockWorkflowService) DeleteWorkflow(ctx context.Context, workflowID string) error {
	args := m.Called(ctx, workflowID)
	return args.Error(0)
}

func (m *MockWorkflowService) PauseWorkflow(ctx context.Context, workflowID string) error {
	args := m.Called(ctx, workflowID)
	return args.Error(0)
}

func (m *MockWorkflowService) ResumeWorkflow(ctx context.Context, workflowID string) error {
	args := m.Called(ctx, workflowID)
	return args.Error(0)
}

func (m *MockWorkflowService) RunWorkflow(ctx context.Context, workflowID string) (*services.WorkflowRun, error) {
	args := m.Called(ctx, workflowID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.WorkflowRun), args.Error(1)
}

func (m *MockWorkflowService) GetWorkflowRuns(ctx context.Context, workflowID string, page, limit int) ([]services.WorkflowRun, int64, error) {
	args := m.Called(ctx, workflowID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]services.WorkflowRun), args.Get(1).(int64), args.Error(2)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type WorkflowHandlerTestable struct {
	svc *MockWorkflowService
	log *logger.Logger
}

func NewWorkflowHandlerTestable(svc *MockWorkflowService, log *logger.Logger) *WorkflowHandlerTestable {
	return &WorkflowHandlerTestable{svc: svc, log: log}
}

func (h *WorkflowHandlerTestable) CreateWorkflow(c *gin.Context) {
	var req struct {
		Name                 string                         `json:"name" binding:"required"`
		Description          string                         `json:"description,omitempty"`
		AgentID              *string                        `json:"agentId,omitempty"`
		Trigger              *services.WorkflowTrigger      `json:"trigger" binding:"required"`
		Action               *services.WorkflowAction       `json:"action" binding:"required"`
		NotificationSettings *services.NotificationSettings `json:"notificationSettings,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User authentication required"})
		return
	}

	workflow := &services.Workflow{
		Name:        req.Name,
		Description: req.Description,
		UserID:      userID.(string),
		AgentID:     req.AgentID,
		Trigger:     req.Trigger,
		Action:      req.Action,
		Status:      services.WorkflowStatusActive,
		IsEnabled:   true,
	}

	if err := h.svc.CreateWorkflow(c.Request.Context(), workflow); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create workflow: " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"workflow": workflow, "message": "Workflow created successfully"})
}

func (h *WorkflowHandlerTestable) GetWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow ID is required"})
		return
	}

	workflow, err := h.svc.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"workflow": workflow})
}

func (h *WorkflowHandlerTestable) ListWorkflows(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User authentication required"})
		return
	}

	page := 1
	limit := 20
	if p := c.DefaultQuery("page", "1"); p != "" {
		if pVal, err := strconv.Atoi(p); err == nil && pVal > 0 {
			page = pVal
		}
	}
	if l := c.DefaultQuery("limit", "20"); l != "" {
		if lVal, err := strconv.Atoi(l); err == nil && lVal > 0 && lVal <= 100 {
			limit = lVal
		}
	}

	workflows, total, err := h.svc.GetWorkflowsByUser(c.Request.Context(), userID.(string), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve workflows"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"workflows": workflows, "total": total, "page": page, "limit": limit})
}

func (h *WorkflowHandlerTestable) UpdateWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow ID is required"})
		return
	}

	var req struct {
		Name                 *string                        `json:"name,omitempty"`
		Description          *string                        `json:"description,omitempty"`
		Trigger              *services.WorkflowTrigger      `json:"trigger,omitempty"`
		Action               *services.WorkflowAction       `json:"action,omitempty"`
		NotificationSettings *services.NotificationSettings `json:"notificationSettings,omitempty"`
		IsEnabled            *bool                          `json:"isEnabled,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	workflow, err := h.svc.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if req.Name != nil {
		workflow.Name = *req.Name
	}
	if req.Description != nil {
		workflow.Description = *req.Description
	}
	if req.Trigger != nil {
		workflow.Trigger = req.Trigger
	}
	if req.Action != nil {
		workflow.Action = req.Action
	}
	if req.IsEnabled != nil {
		workflow.IsEnabled = *req.IsEnabled
	}

	if err := h.svc.UpdateWorkflow(c.Request.Context(), workflow); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update workflow: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"workflow": workflow, "message": "Workflow updated successfully"})
}

func (h *WorkflowHandlerTestable) DeleteWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow ID is required"})
		return
	}

	workflow, err := h.svc.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := h.svc.DeleteWorkflow(c.Request.Context(), workflowID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete workflow"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Workflow deleted successfully"})
}

func (h *WorkflowHandlerTestable) PauseWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow ID is required"})
		return
	}

	workflow, err := h.svc.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := h.svc.PauseWorkflow(c.Request.Context(), workflowID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to pause workflow: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Workflow paused successfully"})
}

func (h *WorkflowHandlerTestable) ResumeWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow ID is required"})
		return
	}

	workflow, err := h.svc.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := h.svc.ResumeWorkflow(c.Request.Context(), workflowID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resume workflow: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Workflow resumed successfully"})
}

func (h *WorkflowHandlerTestable) RunWorkflow(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow ID is required"})
		return
	}

	workflow, err := h.svc.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	run, err := h.svc.RunWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to run workflow: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"run": run, "message": "Workflow executed"})
}

func (h *WorkflowHandlerTestable) GetWorkflowRuns(c *gin.Context) {
	workflowID := c.Param("id")
	if workflowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow ID is required"})
		return
	}

	workflow, err := h.svc.GetWorkflow(c.Request.Context(), workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	userID, _ := c.Get("user_id")
	if workflow.UserID != userID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	page := 1
	limit := 20
	if p := c.DefaultQuery("page", "1"); p != "" {
		if pVal, err := strconv.Atoi(p); err == nil && pVal > 0 {
			page = pVal
		}
	}
	if l := c.DefaultQuery("limit", "20"); l != "" {
		if lVal, err := strconv.Atoi(l); err == nil && lVal > 0 && lVal <= 100 {
			limit = lVal
		}
	}

	runs, total, err := h.svc.GetWorkflowRuns(c.Request.Context(), workflowID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve workflow runs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs, "total": total, "page": page, "limit": limit})
}

// =============================================================================
// Create Tests
// =============================================================================

func TestWorkflowCreate_Success(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("CreateWorkflow", mock.Anything, mock.Anything).Return(nil)

	router := gin.New()
	router.POST("/workflows", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.CreateWorkflow(c)
	})

	reqBody := map[string]interface{}{
		"name":    "Test Workflow",
		"trigger": map[string]interface{}{"type": "time"},
		"action":  map[string]interface{}{"type": "notify"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowCreate_InvalidInput(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/workflows", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.CreateWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestWorkflowCreate_MissingUser(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/workflows", func(c *gin.Context) {
		handler.CreateWorkflow(c)
	})

	reqBody := map[string]interface{}{
		"name":    "Test Workflow",
		"trigger": map[string]interface{}{"type": "time"},
		"action":  map[string]interface{}{"type": "notify"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

func TestWorkflowCreate_ServiceError(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("CreateWorkflow", mock.Anything, mock.Anything).Return(errors.New("service error"))

	router := gin.New()
	router.POST("/workflows", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.CreateWorkflow(c)
	})

	reqBody := map[string]interface{}{
		"name":    "Test Workflow",
		"trigger": map[string]interface{}{"type": "time"},
		"action":  map[string]interface{}{"type": "notify"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Get Tests
// =============================================================================

func TestWorkflowGet_Success(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:        "workflow-123",
		Name:      "Test Workflow",
		UserID:    "user-123",
		Status:    services.WorkflowStatusActive,
		IsEnabled: true,
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)

	router := gin.New()
	router.GET("/workflows/:id", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.GetWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/workflows/workflow-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowGet_NotFound(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("GetWorkflow", mock.Anything, "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/workflows/:id", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.GetWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/workflows/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowGet_Forbidden(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:     "workflow-123",
		Name:   "Test Workflow",
		UserID: "other-user",
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)

	router := gin.New()
	router.GET("/workflows/:id", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.GetWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/workflows/workflow-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// List Tests
// =============================================================================

func TestWorkflowList_Success(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflows := []services.Workflow{
		{ID: "workflow-1", Name: "Workflow 1", UserID: "user-123", Status: services.WorkflowStatusActive},
		{ID: "workflow-2", Name: "Workflow 2", UserID: "user-123", Status: services.WorkflowStatusPaused},
	}

	mockSvc.On("GetWorkflowsByUser", mock.Anything, "user-123", 1, 20).Return(workflows, int64(2), nil)

	router := gin.New()
	router.GET("/workflows", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ListWorkflows(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/workflows", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowList_Empty(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("GetWorkflowsByUser", mock.Anything, "user-123", 1, 20).Return([]services.Workflow{}, int64(0), nil)

	router := gin.New()
	router.GET("/workflows", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ListWorkflows(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/workflows", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowList_ServiceError(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("GetWorkflowsByUser", mock.Anything, "user-123", 1, 20).Return(nil, int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/workflows", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ListWorkflows(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/workflows", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Update Tests
// =============================================================================

func TestWorkflowUpdate_Success(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	existingWorkflow := &services.Workflow{
		ID:        "workflow-123",
		Name:      "Old Name",
		UserID:    "user-123",
		Status:    services.WorkflowStatusActive,
		IsEnabled: true,
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(existingWorkflow, nil)
	mockSvc.On("UpdateWorkflow", mock.Anything, mock.Anything).Return(nil)

	router := gin.New()
	router.PUT("/workflows/:id", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.UpdateWorkflow(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Name",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/workflows/workflow-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowUpdate_NotFound(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("GetWorkflow", mock.Anything, "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.PUT("/workflows/:id", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.UpdateWorkflow(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Name",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/workflows/nonexistent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowUpdate_Forbidden(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:     "workflow-123",
		Name:   "Test Workflow",
		UserID: "other-user",
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)

	router := gin.New()
	router.PUT("/workflows/:id", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.UpdateWorkflow(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Name",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/workflows/workflow-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowUpdate_InvalidInput(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	router := gin.New()
	router.PUT("/workflows/:id", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.UpdateWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/workflows/workflow-123", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// Delete Tests
// =============================================================================

func TestWorkflowDelete_Success(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:     "workflow-123",
		Name:   "Test Workflow",
		UserID: "user-123",
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)
	mockSvc.On("DeleteWorkflow", mock.Anything, "workflow-123").Return(nil)

	router := gin.New()
	router.DELETE("/workflows/:id", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.DeleteWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/workflows/workflow-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowDelete_NotFound(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("GetWorkflow", mock.Anything, "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.DELETE("/workflows/:id", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.DeleteWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/workflows/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowDelete_Forbidden(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:     "workflow-123",
		Name:   "Test Workflow",
		UserID: "other-user",
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)

	router := gin.New()
	router.DELETE("/workflows/:id", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.DeleteWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/workflows/workflow-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Pause Tests
// =============================================================================

func TestWorkflowPause_Success(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:        "workflow-123",
		Name:      "Test Workflow",
		UserID:    "user-123",
		Status:    services.WorkflowStatusActive,
		IsEnabled: true,
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)
	mockSvc.On("PauseWorkflow", mock.Anything, "workflow-123").Return(nil)

	router := gin.New()
	router.POST("/workflows/:id/pause", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.PauseWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows/workflow-123/pause", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowPause_NotFound(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("GetWorkflow", mock.Anything, "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.POST("/workflows/:id/pause", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.PauseWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows/nonexistent/pause", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowPause_Forbidden(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:     "workflow-123",
		Name:   "Test Workflow",
		UserID: "other-user",
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)

	router := gin.New()
	router.POST("/workflows/:id/pause", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.PauseWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows/workflow-123/pause", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Resume Tests
// =============================================================================

func TestWorkflowResume_Success(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:        "workflow-123",
		Name:      "Test Workflow",
		UserID:    "user-123",
		Status:    services.WorkflowStatusPaused,
		IsEnabled: true,
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)
	mockSvc.On("ResumeWorkflow", mock.Anything, "workflow-123").Return(nil)

	router := gin.New()
	router.POST("/workflows/:id/resume", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ResumeWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows/workflow-123/resume", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowResume_NotFound(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("GetWorkflow", mock.Anything, "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.POST("/workflows/:id/resume", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ResumeWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows/nonexistent/resume", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowResume_Forbidden(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:     "workflow-123",
		Name:   "Test Workflow",
		UserID: "other-user",
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)

	router := gin.New()
	router.POST("/workflows/:id/resume", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.ResumeWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows/workflow-123/resume", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Run Tests
// =============================================================================

func TestWorkflowRun_Success(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:        "workflow-123",
		Name:      "Test Workflow",
		UserID:    "user-123",
		Status:    services.WorkflowStatusActive,
		IsEnabled: true,
	}

	now := time.Now()
	run := &services.WorkflowRun{
		ID:          "run-123",
		WorkflowID:  "workflow-123",
		TriggerType: "manual",
		Status:      "completed",
		StartedAt:   now,
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)
	mockSvc.On("RunWorkflow", mock.Anything, "workflow-123").Return(run, nil)

	router := gin.New()
	router.POST("/workflows/:id/run", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.RunWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows/workflow-123/run", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowRun_NotFound(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("GetWorkflow", mock.Anything, "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.POST("/workflows/:id/run", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.RunWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows/nonexistent/run", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowRun_Forbidden(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:     "workflow-123",
		Name:   "Test Workflow",
		UserID: "other-user",
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)

	router := gin.New()
	router.POST("/workflows/:id/run", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.RunWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows/workflow-123/run", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowRun_ServiceError(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:        "workflow-123",
		Name:      "Test Workflow",
		UserID:    "user-123",
		Status:    services.WorkflowStatusActive,
		IsEnabled: true,
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)
	mockSvc.On("RunWorkflow", mock.Anything, "workflow-123").Return(nil, errors.New("execution failed"))

	router := gin.New()
	router.POST("/workflows/:id/run", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.RunWorkflow(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/workflows/workflow-123/run", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetWorkflowRuns Tests
// =============================================================================

func TestWorkflowRuns_Success(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:        "workflow-123",
		Name:      "Test Workflow",
		UserID:    "user-123",
		Status:    services.WorkflowStatusActive,
		IsEnabled: true,
	}

	now := time.Now()
	runs := []services.WorkflowRun{
		{ID: "run-1", WorkflowID: "workflow-123", TriggerType: "manual", Status: "completed", StartedAt: now},
		{ID: "run-2", WorkflowID: "workflow-123", TriggerType: "scheduled", Status: "failed", StartedAt: now},
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)
	mockSvc.On("GetWorkflowRuns", mock.Anything, "workflow-123", 1, 20).Return(runs, int64(2), nil)

	router := gin.New()
	router.GET("/workflows/:id/runs", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.GetWorkflowRuns(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/workflows/workflow-123/runs", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowRuns_NotFound(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	mockSvc.On("GetWorkflow", mock.Anything, "nonexistent").Return(nil, ErrNotFound)

	router := gin.New()
	router.GET("/workflows/:id/runs", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.GetWorkflowRuns(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/workflows/nonexistent/runs", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowRuns_Forbidden(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:     "workflow-123",
		Name:   "Test Workflow",
		UserID: "other-user",
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)

	router := gin.New()
	router.GET("/workflows/:id/runs", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.GetWorkflowRuns(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/workflows/workflow-123/runs", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestWorkflowRuns_ServiceError(t *testing.T) {
	mockSvc := new(MockWorkflowService)
	log := logger.New()
	handler := NewWorkflowHandlerTestable(mockSvc, log)

	workflow := &services.Workflow{
		ID:        "workflow-123",
		Name:      "Test Workflow",
		UserID:    "user-123",
		Status:    services.WorkflowStatusActive,
		IsEnabled: true,
	}

	mockSvc.On("GetWorkflow", mock.Anything, "workflow-123").Return(workflow, nil)
	mockSvc.On("GetWorkflowRuns", mock.Anything, "workflow-123", 1, 20).Return(nil, int64(0), errors.New("database error"))

	router := gin.New()
	router.GET("/workflows/:id/runs", func(c *gin.Context) {
		c.Set("user_id", "user-123")
		handler.GetWorkflowRuns(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/workflows/workflow-123/runs", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}
