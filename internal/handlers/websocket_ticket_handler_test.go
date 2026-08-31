package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type stubWebSocketTicketIssuer struct {
	subject    string
	businessID string
	result     *services.WebSocketTicketIssue
	err        error
}

func (s *stubWebSocketTicketIssuer) Issue(_ context.Context, subject, businessID string) (*services.WebSocketTicketIssue, error) {
	s.subject = subject
	s.businessID = businessID
	return s.result, s.err
}

func TestWebSocketTicketHandlerIssuesForAuthenticatedTenantScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	expiresAt := time.Date(2026, time.August, 31, 12, 1, 0, 0, time.UTC)
	issuer := &stubWebSocketTicketIssuer{result: &services.WebSocketTicketIssue{
		Ticket:    "opaque-ticket",
		ExpiresAt: expiresAt,
	}}
	handler := &WebSocketTicketHandler{service: issuer, log: logger.New()}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/websocket/tickets", nil)
	context.Set("user_id", "subject-1")
	context.Set("business_id", "10000000-0000-0000-0000-000000000001")

	handler.Issue(context)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if issuer.subject != "subject-1" || issuer.businessID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("issued scope = subject %q business %q", issuer.subject, issuer.businessID)
	}
	var body services.WebSocketTicketIssue
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Ticket != "opaque-ticket" || !body.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("response = %#v", body)
	}
}

func TestWebSocketTicketHandlerRejectsMissingScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	issuer := &stubWebSocketTicketIssuer{}
	handler := &WebSocketTicketHandler{service: issuer, log: logger.New()}
	for _, fixture := range []struct {
		name       string
		userID     string
		businessID string
		wantStatus int
	}{
		{name: "subject", businessID: "business-1", wantStatus: http.StatusUnauthorized},
		{name: "business", userID: "subject-1", wantStatus: http.StatusForbidden},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/websocket/tickets", nil)
			context.Set("user_id", fixture.userID)
			context.Set("business_id", fixture.businessID)

			handler.Issue(context)

			if recorder.Code != fixture.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, fixture.wantStatus)
			}
		})
	}
}
