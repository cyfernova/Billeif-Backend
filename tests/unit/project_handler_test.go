package unit
// go test -v ./tests/unit/... -run "TestProject" 
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock ProjectService
// =============================================================================

type MockProjectService struct {
	mock.Mock
}

func (m *MockProjectService) List(ctx context.Context, businessID string) ([]*models.Project, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Project), args.Error(1)
}

func (m *MockProjectService) Create(ctx context.Context, businessID string, input services.CreateProjectInput) (*models.Project, error) {
	args := m.Called(ctx, businessID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Project), args.Error(1)
}

func (m *MockProjectService) Update(ctx context.Context, businessID, id string, input services.UpdateProjectInput) (*models.Project, error) {
	args := m.Called(ctx, businessID, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Project), args.Error(1)
}

func (m *MockProjectService) Delete(ctx context.Context, businessID, id string) error {
	args := m.Called(ctx, businessID, id)
	return args.Error(0)
}

// =============================================================================
// Testable wrapper
// =============================================================================

type ProjectHandlerTestable struct {
	svc *MockProjectService
	log *logger.Logger
}

func NewProjectHandlerTestable(svc *MockProjectService, log *logger.Logger) *ProjectHandlerTestable {
	return &ProjectHandlerTestable{svc: svc, log: log}
}

func (h *ProjectHandlerTestable) List(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	projects, err := h.svc.List(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": projects})
}

func (h *ProjectHandlerTestable) Create(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	project, err := h.svc.Create(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, project)
}

func (h *ProjectHandlerTestable) Update(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpdateProjectInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	project, err := h.svc.Update(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, project)
}

func (h *ProjectHandlerTestable) Delete(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), businessID, c.Param("id")); err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// =============================================================================
// List Tests
// =============================================================================

func TestProjectList_Success(t *testing.T) {
	mockSvc := new(MockProjectService)
	log := logger.New()
	handler := NewProjectHandlerTestable(mockSvc, log)

	projects := []*models.Project{
		{ID: "proj-1", BusinessID: "biz-123", Name: "Project 1", Code: "P1"},
		{ID: "proj-2", BusinessID: "biz-123", Name: "Project 2", Code: "P2"},
	}

	mockSvc.On("List", mock.Anything, "biz-123").Return(projects, nil)

	router := gin.New()
	router.GET("/projects", func(c *gin.Context) {
		createProjectTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestProjectList_InternalError(t *testing.T) {
	mockSvc := new(MockProjectService)
	log := logger.New()
	handler := NewProjectHandlerTestable(mockSvc, log)

	mockSvc.On("List", mock.Anything, "biz-123").Return(nil, errors.New("database error"))

	router := gin.New()
	router.GET("/projects", func(c *gin.Context) {
		createProjectTestContext(c, "user-123", "biz-123")
		handler.List(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Create Tests
// =============================================================================

func TestProjectCreate_Success(t *testing.T) {
	mockSvc := new(MockProjectService)
	log := logger.New()
	handler := NewProjectHandlerTestable(mockSvc, log)

	project := &models.Project{
		ID:         "proj-123",
		BusinessID: "biz-123",
		Name:       "New Project",
		Code:       "NP001",
		IsActive:   true,
	}

	input := services.CreateProjectInput{
		Name: "New Project",
		Code: "NP001",
	}

	mockSvc.On("Create", mock.Anything, "biz-123", input).Return(project, nil)

	router := gin.New()
	router.POST("/projects", func(c *gin.Context) {
		createProjectTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	reqBody := map[string]interface{}{
		"name": "New Project",
		"code": "NP001",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestProjectCreate_InvalidInput(t *testing.T) {
	mockSvc := new(MockProjectService)
	log := logger.New()
	handler := NewProjectHandlerTestable(mockSvc, log)

	router := gin.New()
	router.POST("/projects", func(c *gin.Context) {
		createProjectTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestProjectCreate_ServiceError(t *testing.T) {
	mockSvc := new(MockProjectService)
	log := logger.New()
	handler := NewProjectHandlerTestable(mockSvc, log)

	input := services.CreateProjectInput{
		Name: "New Project",
		Code: "NP001",
	}

	mockSvc.On("Create", mock.Anything, "biz-123", input).Return(nil, errors.New("service error"))

	router := gin.New()
	router.POST("/projects", func(c *gin.Context) {
		createProjectTestContext(c, "user-123", "biz-123")
		handler.Create(c)
	})

	reqBody := map[string]interface{}{
		"name": "New Project",
		"code": "NP001",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
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

func TestProjectUpdate_Success(t *testing.T) {
	mockSvc := new(MockProjectService)
	log := logger.New()
	handler := NewProjectHandlerTestable(mockSvc, log)

	project := &models.Project{
		ID:         "proj-123",
		BusinessID: "biz-123",
		Name:       "Updated Project",
		Code:       "UP001",
	}

	name := "Updated Project"
	input := services.UpdateProjectInput{
		Name: &name,
	}

	mockSvc.On("Update", mock.Anything, "biz-123", "proj-123", input).Return(project, nil)

	router := gin.New()
	router.PUT("/projects/:id", func(c *gin.Context) {
		createProjectTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Project",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/projects/proj-123", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestProjectUpdate_NotFound(t *testing.T) {
	mockSvc := new(MockProjectService)
	log := logger.New()
	handler := NewProjectHandlerTestable(mockSvc, log)

	name := "Updated Project"
	input := services.UpdateProjectInput{
		Name: &name,
	}

	mockSvc.On("Update", mock.Anything, "biz-123", "nonexistent", input).Return(nil, ErrNotFound)

	router := gin.New()
	router.PUT("/projects/:id", func(c *gin.Context) {
		createProjectTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Project",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/projects/nonexistent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestProjectUpdate_InvalidInput(t *testing.T) {
	mockSvc := new(MockProjectService)
	log := logger.New()
	handler := NewProjectHandlerTestable(mockSvc, log)

	router := gin.New()
	router.PUT("/projects/:id", func(c *gin.Context) {
		createProjectTestContext(c, "user-123", "biz-123")
		handler.Update(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/projects/proj-123", bytes.NewBuffer([]byte("invalid json")))
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

func TestProjectDelete_Success(t *testing.T) {
	mockSvc := new(MockProjectService)
	log := logger.New()
	handler := NewProjectHandlerTestable(mockSvc, log)

	mockSvc.On("Delete", mock.Anything, "biz-123", "proj-123").Return(nil)

	router := gin.New()
	router.DELETE("/projects/:id", func(c *gin.Context) {
		createProjectTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/projects/proj-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestProjectDelete_NotFound(t *testing.T) {
	mockSvc := new(MockProjectService)
	log := logger.New()
	handler := NewProjectHandlerTestable(mockSvc, log)

	mockSvc.On("Delete", mock.Anything, "biz-123", "nonexistent").Return(ErrNotFound)

	router := gin.New()
	router.DELETE("/projects/:id", func(c *gin.Context) {
		createProjectTestContext(c, "user-123", "biz-123")
		handler.Delete(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/projects/nonexistent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Helper
// =============================================================================

func createProjectTestContext(c *gin.Context, userID, businessID string) {
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	c.Set("role", "member")
}
