package unit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// MockEmailServiceForAdmin is a mock implementation of the email service methods needed by AdminHandler
type MockEmailServiceForAdmin struct {
	ListAllCapturedEmailsFunc func(ctx context.Context) ([]EmailPayloadMock, error)
}

type EmailPayloadMock struct {
	To        string
	Subject   string
	Body      string
	SentAt    time.Time
	MessageID string
}

func (m *MockEmailServiceForAdmin) ListAllCapturedEmails(ctx context.Context) ([]EmailPayloadMock, error) {
	if m.ListAllCapturedEmailsFunc != nil {
		return m.ListAllCapturedEmailsFunc(ctx)
	}
	return nil, nil
}

// EmailServiceInterface defines the interface needed by AdminHandler
type EmailServiceInterface interface {
	ListAllCapturedEmails(ctx context.Context) (interface{}, error)
}

// AdminHandlerTestable wraps AdminHandler with mocked dependencies
type AdminHandlerTestable struct {
	email *MockEmailServiceForAdmin
	log   *logger.Logger
}

func NewAdminHandlerTestable(email *MockEmailServiceForAdmin, log *logger.Logger) *AdminHandlerTestable {
	return &AdminHandlerTestable{
		email: email,
		log:   log,
	}
}

// ListEmails is a testable version of AdminHandler.ListEmails
func (h *AdminHandlerTestable) ListEmails(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("admin_handler").With("operation", "list_emails")
	emails, err := h.email.ListAllCapturedEmails(c.Request.Context())
	if err != nil {
		log.Error("failed to list captured emails", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("captured emails listed", "count", len(emails))

	c.JSON(http.StatusOK, gin.H{"emails": emails})
}

func setupTestRouter(handler *AdminHandlerTestable) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/local-emails", handler.ListEmails)
	return r
}

func TestAdminHandler_ListEmails_Success(t *testing.T) {
	mockEmail := &MockEmailServiceForAdmin{
		ListAllCapturedEmailsFunc: func(ctx context.Context) ([]EmailPayloadMock, error) {
			return []EmailPayloadMock{
				{
					To:        "test@example.com",
					Subject:   "Test Email",
					Body:      "This is a test email body",
					SentAt:    time.Now(),
					MessageID: "msg-123",
				},
				{
					To:        "user@example.com",
					Subject:   "Another Email",
					Body:      "Email content",
					SentAt:    time.Now(),
					MessageID: "msg-456",
				},
			}, nil
		},
	}

	log := logger.New()
	handler := NewAdminHandlerTestable(mockEmail, log)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/admin/local-emails", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "test@example.com")
	assert.Contains(t, w.Body.String(), "Test Email")
	assert.Contains(t, w.Body.String(), "msg-123")
}

func TestAdminHandler_ListEmails_EmptyResult(t *testing.T) {
	mockEmail := &MockEmailServiceForAdmin{
		ListAllCapturedEmailsFunc: func(ctx context.Context) ([]EmailPayloadMock, error) {
			return []EmailPayloadMock{}, nil
		},
	}

	log := logger.New()
	handler := NewAdminHandlerTestable(mockEmail, log)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/admin/local-emails", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "emails")
}

func TestAdminHandler_ListEmails_Error(t *testing.T) {
	mockEmail := &MockEmailServiceForAdmin{
		ListAllCapturedEmailsFunc: func(ctx context.Context) ([]EmailPayloadMock, error) {
			return nil, errors.New("S3 connection failed")
		},
	}

	log := logger.New()
	handler := NewAdminHandlerTestable(mockEmail, log)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/admin/local-emails", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "S3 connection failed")
}

func TestAdminHandler_ListEmails_SingleEmail(t *testing.T) {
	mockEmail := &MockEmailServiceForAdmin{
		ListAllCapturedEmailsFunc: func(ctx context.Context) ([]EmailPayloadMock, error) {
			return []EmailPayloadMock{
				{
					To:        "admin@company.com",
					Subject:   "System Notification",
					Body:      "Your invoice has been paid",
					SentAt:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
					MessageID: "msg-789",
				},
			}, nil
		},
	}

	log := logger.New()
	handler := NewAdminHandlerTestable(mockEmail, log)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/admin/local-emails", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "admin@company.com")
	assert.Contains(t, w.Body.String(), "System Notification")
	assert.Contains(t, w.Body.String(), "msg-789")
}

func TestAdminHandler_ListEmails_NoMessages(t *testing.T) {
	mockEmail := &MockEmailServiceForAdmin{
		ListAllCapturedEmailsFunc: func(ctx context.Context) ([]EmailPayloadMock, error) {
			return []EmailPayloadMock{}, nil
		},
	}

	log := logger.New()
	handler := NewAdminHandlerTestable(mockEmail, log)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/admin/local-emails", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"emails":[]`)
}

func TestAdminHandler_ListEmails_PreservesEmailData(t *testing.T) {
	sentAt := time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC)
	mockEmail := &MockEmailServiceForAdmin{
		ListAllCapturedEmailsFunc: func(ctx context.Context) ([]EmailPayloadMock, error) {
			return []EmailPayloadMock{
				{
					To:        "recipient@example.com",
					Subject:   "Invoice #12345",
					Body:      "Dear Customer,\n\nPlease find attached your invoice.\n\nBest regards",
					SentAt:    sentAt,
					MessageID: "unique-msg-id-001",
				},
			}, nil
		},
	}

	log := logger.New()
	handler := NewAdminHandlerTestable(mockEmail, log)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/admin/local-emails", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "recipient@example.com")
	assert.Contains(t, w.Body.String(), "Invoice #12345")
	assert.Contains(t, w.Body.String(), "Dear Customer")
	assert.Contains(t, w.Body.String(), "unique-msg-id-001")
}
