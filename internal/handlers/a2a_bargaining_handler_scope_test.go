package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type a2aHandlerServiceFake struct {
	scope   services.A2ANegotiationScope
	autoReq *services.AutonomousNegotiationRequest
	err     error
}

func (f *a2aHandlerServiceFake) StartNegotiation(
	_ context.Context,
	scope services.A2ANegotiationScope,
	buyerAgentID,
	sellerAgentID string,
	initialAmount float64,
) (*services.A2ASession, error) {
	f.scope = scope
	return &services.A2ASession{
		NegotiationID: "session-1", DBNegotiationID: "negotiation-1",
		BuyerAgentID: buyerAgentID, SellerAgentID: sellerAgentID,
		UserID: scope.UserID, BusinessID: scope.BusinessID,
		BuyerAgent:    &models.Agent{ID: buyerAgentID, Name: "Buyer", Type: "shopping"},
		SellerAgent:   &models.Agent{ID: sellerAgentID, Name: "Seller", Type: "merchant"},
		InitialAmount: initialAmount, CurrentAmount: initialAmount,
	}, f.err
}

func (f *a2aHandlerServiceFake) StartAutonomousNegotiation(
	_ context.Context,
	scope services.A2ANegotiationScope,
	req *services.AutonomousNegotiationRequest,
) (*services.A2ASession, error) {
	f.scope = scope
	f.autoReq = req
	return &services.A2ASession{NegotiationID: "session-1", DBNegotiationID: "negotiation-1"}, f.err
}

func (f *a2aHandlerServiceFake) GetSessionProgress(
	_ context.Context,
	scope services.A2ANegotiationScope,
	_ string,
) (*services.A2ASessionProgress, error) {
	f.scope = scope
	return &services.A2ASessionProgress{NegotiationID: "session-1", Status: "running"}, f.err
}

func (f *a2aHandlerServiceFake) GetSessionProgressByNegotiationID(
	_ context.Context,
	scope services.A2ANegotiationScope,
	_ string,
) (*services.A2ASessionProgress, error) {
	f.scope = scope
	return &services.A2ASessionProgress{NegotiationUUID: "negotiation-1", Status: "running"}, f.err
}

func (f *a2aHandlerServiceFake) StopNegotiation(
	_ context.Context,
	scope services.A2ANegotiationScope,
	_ string,
) error {
	f.scope = scope
	return f.err
}

func (f *a2aHandlerServiceFake) EnqueueNegotiationRound(context.Context, string, string, int) error {
	return nil
}

func newScopedA2AHandlerRouter(service a2aBargainingService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := NewA2ABargainingHandler(service, nil, nil, logger.New())
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "authenticated-user")
		c.Set("business_id", "token-business")
		c.Set("validated_business_id", "effective-business")
		c.Next()
	})
	router.POST("/start", handler.StartNegotiation)
	router.POST("/autonomous", handler.StartAutonomousNegotiation)
	router.GET("/progress/:sessionId", handler.GetSessionProgress)
	router.GET("/result/:negotiationId", handler.GetNegotiationProgress)
	router.POST("/stop/:sessionId", handler.StopNegotiation)
	return router
}

func TestA2ABargainingHandlerIgnoresBodyUserAndUsesAuthenticatedEffectiveScope(t *testing.T) {
	service := &a2aHandlerServiceFake{}
	router := newScopedA2AHandlerRouter(service)
	body := []byte(`{
		"buyer_agent_id":"55555555-5555-5555-5555-555555555555",
		"seller_agent_id":"66666666-6666-6666-6666-666666666666",
		"initial_amount":100,
		"user_id":"99999999-9999-9999-9999-999999999999"
	}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/autonomous", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.scope.UserID != "authenticated-user" || service.scope.BusinessID != "effective-business" {
		t.Fatalf("scope = %#v", service.scope)
	}
	if service.autoReq == nil || service.autoReq.UserID != "" {
		t.Fatalf("attacker body user reached service request: %#v", service.autoReq)
	}
}

func TestA2ABargainingHandlerDoesNotValidateIgnoredBodyUser(t *testing.T) {
	service := &a2aHandlerServiceFake{}
	router := newScopedA2AHandlerRouter(service)
	body := []byte(`{
		"buyer_agent_id":"55555555-5555-5555-5555-555555555555",
		"seller_agent_id":"66666666-6666-6666-6666-666666666666",
		"initial_amount":100,
		"user_id":"attacker-controlled"
	}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/autonomous", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("ignored body user affected response: %d %s", recorder.Code, recorder.Body.String())
	}
	if service.scope.UserID != "authenticated-user" || service.autoReq == nil || service.autoReq.UserID != "" {
		t.Fatalf("ignored body user reached scope or service request: %#v %#v", service.scope, service.autoReq)
	}
}

func TestA2ABargainingHandlerScopesProgressResultAndStop(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/progress/session-1"},
		{method: http.MethodGet, path: "/result/negotiation-1"},
		{method: http.MethodPost, path: "/stop/session-1"},
	} {
		t.Run(test.path, func(t *testing.T) {
			service := &a2aHandlerServiceFake{}
			router := newScopedA2AHandlerRouter(service)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if service.scope.UserID != "authenticated-user" || service.scope.BusinessID != "effective-business" {
				t.Fatalf("scope = %#v", service.scope)
			}
		})
	}
}

func TestA2ABargainingHandlerReturnsUniformNotFoundForForeignLifecycleScope(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/progress/foreign"},
		{method: http.MethodGet, path: "/result/foreign"},
		{method: http.MethodPost, path: "/stop/foreign"},
	} {
		t.Run(test.path, func(t *testing.T) {
			service := &a2aHandlerServiceFake{err: services.ErrA2ANegotiationNotFound}
			router := newScopedA2AHandlerRouter(service)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
			if recorder.Code != http.StatusNotFound || recorder.Body.String() != "{\"error\":\"negotiation not found\"}" {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestA2ABargainingHandlerReturnsUniformNotFoundForForeignStart(t *testing.T) {
	for _, path := range []string{"/start", "/autonomous"} {
		t.Run(path, func(t *testing.T) {
			service := &a2aHandlerServiceFake{err: services.ErrA2ANegotiationNotFound}
			router := newScopedA2AHandlerRouter(service)
			body := []byte(`{
				"buyer_agent_id":"55555555-5555-5555-5555-555555555555",
				"seller_agent_id":"66666666-6666-6666-6666-666666666666",
				"initial_amount":100
			}`)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNotFound || recorder.Body.String() != "{\"error\":\"negotiation not found\"}" {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestA2ABargainingHandlerReturnsInternalServerErrorForRepositoryFailures(t *testing.T) {
	repositoryFailure := errors.New("database unavailable")
	startBody := []byte(`{
		"buyer_agent_id":"55555555-5555-5555-5555-555555555555",
		"seller_agent_id":"66666666-6666-6666-6666-666666666666",
		"initial_amount":100
	}`)
	tests := []struct {
		method string
		path   string
		body   []byte
	}{
		{method: http.MethodPost, path: "/start", body: startBody},
		{method: http.MethodPost, path: "/autonomous", body: startBody},
		{method: http.MethodGet, path: "/progress/session-1"},
		{method: http.MethodGet, path: "/result/negotiation-1"},
		{method: http.MethodPost, path: "/stop/session-1"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			service := &a2aHandlerServiceFake{err: repositoryFailure}
			router := newScopedA2AHandlerRouter(service)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, bytes.NewReader(test.body))
			if test.body != nil {
				request.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("response = %d %s, want 500", recorder.Code, recorder.Body.String())
			}
		})
	}
}
