package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type stubNotificationService struct {
	businessID string
	userID     string
	id         string
	listed     []*models.Notification
	marked     *models.Notification
	updated    int64
	err        error
}

func (s *stubNotificationService) List(_ context.Context, businessID, userID string, _ int) ([]*models.Notification, error) {
	s.businessID, s.userID = businessID, userID
	return s.listed, s.err
}

func (s *stubNotificationService) MarkRead(_ context.Context, id, businessID, userID string) (*models.Notification, error) {
	s.id, s.businessID, s.userID = id, businessID, userID
	return s.marked, s.err
}

func (s *stubNotificationService) MarkAllRead(_ context.Context, businessID, userID string) (int64, error) {
	s.businessID, s.userID = businessID, userID
	return s.updated, s.err
}

func TestNotificationHandlerListsCurrentTenantAndUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	service := &stubNotificationService{listed: []*models.Notification{{ID: "notification-1", BusinessID: "business-a", UserID: "user-a", Type: "system", Title: "Ready", Body: "Done", CreatedAt: now}}}
	handler := &NotificationHandler{service: service, log: logger.New()}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	ctx.Set("business_id", "business-a")
	ctx.Set("user_id", "user-a")

	handler.List(ctx)

	if recorder.Code != http.StatusOK || service.businessID != "business-a" || service.userID != "user-a" {
		t.Fatalf("status = %d scope = %q/%q body = %s", recorder.Code, service.businessID, service.userID, recorder.Body.String())
	}
	var body []*models.Notification
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || len(body) != 1 {
		t.Fatalf("response = %#v, %v", body, err)
	}
}

func TestNotificationHandlerReadEndpointsUsePathAndCurrentScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &stubNotificationService{marked: &models.Notification{ID: "notification-1"}, updated: 3}
	handler := &NotificationHandler{service: service, log: logger.New()}

	readRecorder := httptest.NewRecorder()
	readContext, _ := gin.CreateTestContext(readRecorder)
	readContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/notifications/notification-1/read", nil)
	readContext.Params = gin.Params{{Key: "id", Value: "notification-1"}}
	readContext.Set("business_id", "business-a")
	readContext.Set("user_id", "user-a")
	handler.MarkRead(readContext)
	if readRecorder.Code != http.StatusOK || service.id != "notification-1" || service.businessID != "business-a" || service.userID != "user-a" {
		t.Fatalf("read status = %d scope = %q/%q id = %q", readRecorder.Code, service.businessID, service.userID, service.id)
	}

	allRecorder := httptest.NewRecorder()
	allContext, _ := gin.CreateTestContext(allRecorder)
	allContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/notifications/read-all", nil)
	allContext.Set("business_id", "business-a")
	allContext.Set("user_id", "user-a")
	handler.MarkAllRead(allContext)
	if allRecorder.Code != http.StatusOK || allRecorder.Body.String() != "{\"updated\":3}" {
		t.Fatalf("read-all status = %d body = %s", allRecorder.Code, allRecorder.Body.String())
	}
}

func TestNotificationHandlerRejectsMissingScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &stubNotificationService{}
	handler := &NotificationHandler{service: service, log: logger.New()}
	for _, fixture := range []struct {
		name       string
		businessID string
		userID     string
		want       int
	}{
		{name: "business", userID: "user-a", want: http.StatusForbidden},
		{name: "user", businessID: "business-a", want: http.StatusUnauthorized},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
			ctx.Set("business_id", fixture.businessID)
			ctx.Set("user_id", fixture.userID)
			handler.List(ctx)
			if recorder.Code != fixture.want {
				t.Fatalf("status = %d, want %d", recorder.Code, fixture.want)
			}
		})
	}
}
