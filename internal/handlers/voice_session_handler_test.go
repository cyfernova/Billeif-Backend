package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/voice/session"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type fakeVoiceSessionService struct {
	created     *session.Session
	got         *session.Session
	resumed     *session.Session
	createErr   error
	getErr      error
	resumeErr   error
	closeErr    error
	createScope session.Scope
	createInput session.CreateInput
	getScope    session.Scope
	getID       string
	resumeScope session.Scope
	resumeID    string
	closeScope  session.Scope
	closeID     string
}

func (f *fakeVoiceSessionService) Create(_ context.Context, scope session.Scope, input session.CreateInput) (*session.Session, error) {
	f.createScope, f.createInput = scope, input
	return f.created, f.createErr
}

func (f *fakeVoiceSessionService) Get(_ context.Context, scope session.Scope, id string) (*session.Session, error) {
	f.getScope, f.getID = scope, id
	return f.got, f.getErr
}

func (f *fakeVoiceSessionService) Resume(_ context.Context, scope session.Scope, id string) (*session.Session, error) {
	f.resumeScope, f.resumeID = scope, id
	return f.resumed, f.resumeErr
}

func (f *fakeVoiceSessionService) Close(_ context.Context, scope session.Scope, id string) error {
	f.closeScope, f.closeID = scope, id
	return f.closeErr
}

func (f *fakeVoiceSessionService) CreateResponse(value *session.Session) session.CreateResponse {
	return session.CreateResponse{
		SessionID: value.ID, RuntimeSessionID: value.RuntimeSessionID,
		AgentRuntimeARN: "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/test", AgentRuntimeQualifier: "PROD",
		KVSChannelIndex: value.KVSChannelIndex, ProtocolVersion: value.ProtocolVersion,
		ExpiresAt: value.ExpiresAt, RotateAt: value.RotateAt, SpokenLanguages: append([]string(nil), session.SpokenLanguages...),
	}
}

func TestVoiceSessionHandlerCreateUsesAuthenticatedScopeAndReturnsAllowlistedContract(t *testing.T) {
	value := handlerSession()
	svc := &fakeVoiceSessionService{created: value}
	router := voiceSessionTestRouter(svc)
	body := `{"branch_id":"cbd6e793-62e6-4c32-a106-065709caf460","idempotency_key":"35e046c7-23f4-4c8d-b79d-581229de44ad","preferred_language":"en-IN","fallback_language":"hi-IN","consent":{"transcript_storage":true,"audio_recording":false,"policy_version":"2026-08-01"},"client":{"platform":"ios","app_version":"1.0.0","protocol_version":1}}`

	response := performVoiceRequest(router, http.MethodPost, "/api/v1/voice/sessions", body)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if svc.createScope != (session.Scope{UserID: "user-1", BusinessID: "business-1"}) {
		t.Fatalf("handler did not use middleware identity: %#v", svc.createScope)
	}
	if svc.createInput.BranchID != "cbd6e793-62e6-4c32-a106-065709caf460" {
		t.Fatalf("validated branch not forwarded: %#v", svc.createInput)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["session_id"] != value.ID || len(payload["spoken_languages"].([]any)) != 11 {
		t.Fatalf("wrong create response: %#v", payload)
	}
	for _, forbidden := range []string{"user_id", "business_id", "transcript", "sarvam", "credentials", "pk", "sk"} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("response leaked %q: %#v", forbidden, payload)
		}
	}
}

func TestVoiceSessionHandlerSupportsGetResumeAndDelete(t *testing.T) {
	value := handlerSession()
	svc := &fakeVoiceSessionService{got: value, resumed: value}
	router := voiceSessionTestRouter(svc)

	get := performVoiceRequest(router, http.MethodGet, "/api/v1/voice/sessions/"+value.ID, "")
	if get.Code != http.StatusOK || strings.Contains(get.Body.String(), "transcript") || strings.Contains(get.Body.String(), "user-1") {
		t.Fatalf("unsafe get response status=%d body=%s", get.Code, get.Body.String())
	}
	resume := performVoiceRequest(router, http.MethodPost, "/api/v1/voice/sessions/"+value.ID+"/resume", "")
	if resume.Code != http.StatusOK || svc.resumeID != value.ID {
		t.Fatalf("resume status=%d body=%s", resume.Code, resume.Body.String())
	}
	deleted := performVoiceRequest(router, http.MethodDelete, "/api/v1/voice/sessions/"+value.ID, "")
	if deleted.Code != http.StatusNoContent || svc.closeID != value.ID || deleted.Body.Len() != 0 {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestVoiceSessionHandlerMapsCapacityAndOwnershipWithoutLeaks(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantRetry  string
	}{
		{name: "user capacity", err: session.ErrUserCapacity, wantStatus: http.StatusTooManyRequests, wantRetry: "5"},
		{name: "global capacity", err: session.ErrGlobalCapacity, wantStatus: http.StatusServiceUnavailable, wantRetry: "10"},
		{name: "idempotency conflict", err: session.ErrIdempotencyConflict, wantStatus: http.StatusConflict},
		{name: "invalid", err: session.ErrInvalidRequest, wantStatus: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeVoiceSessionService{createErr: tc.err}
			response := performVoiceRequest(voiceSessionTestRouter(svc), http.MethodPost, "/api/v1/voice/sessions", validVoiceHandlerBody())
			if response.Code != tc.wantStatus || response.Header().Get("Retry-After") != tc.wantRetry {
				t.Fatalf("status=%d retry=%q body=%s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
			}
		})
	}

	svc := &fakeVoiceSessionService{getErr: session.ErrNotFound}
	notFound := performVoiceRequest(voiceSessionTestRouter(svc), http.MethodGet, "/api/v1/voice/sessions/voice_private", "")
	if notFound.Code != http.StatusNotFound || strings.Contains(notFound.Body.String(), "owner") || strings.Contains(notFound.Body.String(), "business") {
		t.Fatalf("ownership mismatch leaked details: %d %s", notFound.Code, notFound.Body.String())
	}
}

func TestVoiceSessionHandlerRequiresValidatedBranch(t *testing.T) {
	svc := &fakeVoiceSessionService{created: handlerSession()}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "user-1")
		c.Set("validated_business_id", "business-1")
		c.Next()
	})
	handler := NewVoiceSessionHandler(svc, logger.New())
	router.POST("/api/v1/voice/sessions", handler.Create)
	response := performVoiceRequest(router, http.MethodPost, "/api/v1/voice/sessions", validVoiceHandlerBody())
	if response.Code != http.StatusForbidden || svc.createScope.UserID != "" {
		t.Fatalf("unvalidated branch reached service: status=%d scope=%#v", response.Code, svc.createScope)
	}
}

func voiceSessionTestRouter(svc VoiceSessionService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "user-1")
		c.Set("validated_business_id", "business-1")
		c.Set("validated_branch_id", "cbd6e793-62e6-4c32-a106-065709caf460")
		c.Next()
	})
	handler := NewVoiceSessionHandler(svc, logger.New())
	routes := router.Group("/api/v1/voice/sessions")
	routes.POST("", handler.Create)
	routes.GET("/:session_id", handler.Get)
	routes.POST("/:session_id/resume", handler.Resume)
	routes.DELETE("/:session_id", handler.Delete)
	return router
}

func performVoiceRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func handlerSession() *session.Session {
	now := time.Date(2026, 8, 6, 7, 0, 0, 0, time.UTC)
	return &session.Session{
		ID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST", RuntimeSessionID: "voice-session-01K1ABCDE2FGHIJK3LMNOPQRST",
		UserID: "user-1", BusinessID: "business-1", BranchID: "cbd6e793-62e6-4c32-a106-065709caf460",
		Status: session.StatusActive, ProtocolVersion: 1, KVSChannelIndex: 7,
		PreferredLanguage: "en-IN", FallbackLanguage: "hi-IN", CurrentLanguage: "en-IN",
		CreatedAt: now, UpdatedAt: now, LeaseExpiresAt: now.Add(2 * time.Minute), ExpiresAt: now.Add(55 * time.Minute), RotateAt: now.Add(52 * time.Minute), Resumable: true,
	}
}

func validVoiceHandlerBody() string {
	return `{"branch_id":"cbd6e793-62e6-4c32-a106-065709caf460","idempotency_key":"35e046c7-23f4-4c8d-b79d-581229de44ad","preferred_language":"en-IN","fallback_language":"hi-IN","consent":{"transcript_storage":true,"audio_recording":false,"policy_version":"2026-08-01"},"client":{"platform":"ios","app_version":"1.0.0","protocol_version":1}}`
}
