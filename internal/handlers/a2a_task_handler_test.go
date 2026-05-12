package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newA2ATestRouter(t *testing.T) (*gin.Engine, *services.A2ATaskService, *services.A2APushService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := createSQLiteA2ATestSchema(db); err != nil {
		t.Fatalf("create sqlite a2a schema: %v", err)
	}

	log := logger.New()
	pushService := services.NewA2APushService(db, nil, log)
	taskService := services.NewA2ATaskService(db, log, pushService)
	handler := NewA2ATaskHandler(taskService, pushService, log)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "user-1")
		c.Set("business_id", "biz-1")
		c.Next()
	})
	router.Any("/api/v1/a2a/*a2aPath", handler.Handle)
	return router, taskService, pushService
}

func createSQLiteA2ATestSchema(db *gorm.DB) error {
	statements := []string{
		`CREATE TABLE a2a_tasks (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			state TEXT NOT NULL,
			artifacts TEXT NOT NULL DEFAULT '[]',
			messages TEXT NOT NULL DEFAULT '[]',
			metadata TEXT NOT NULL DEFAULT '{}',
			user_id TEXT,
			business_id TEXT,
			created_at DATETIME,
			updated_at DATETIME
		);`,
		`CREATE TABLE a2a_task_events (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			event_id TEXT NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE a2a_task_push_configs (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			business_id TEXT,
			url TEXT NOT NULL,
			token TEXT,
			authentication TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		);`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func newValidSendMessageBody(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: "msg-1",
			Role:      a2a.RoleUser,
			Parts: []a2a.Part{
				{Text: "Process this request."},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal send message request: %v", err)
	}
	return body
}

func TestWellKnownAgentCardLatestFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Server:  config.ServerConfig{BaseURL: "https://api.example.com"},
		Cognito: config.CognitoConfig{Region: "us-east-1", UserPoolID: "pool-123"},
	}
	handler := NewWellKnownHandler(cfg, logger.New())
	router := gin.New()
	router.GET("/.well-known/agent-card.json", handler.GetAgentCard)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	var card a2a.AgentCard
	if err := json.Unmarshal(res.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}
	if len(card.ProtocolVersions) != 1 || card.ProtocolVersions[0] != a2a.SupportedVersion {
		t.Fatalf("unexpected protocol versions: %#v", card.ProtocolVersions)
	}
	if got := card.SupportedInterfaces[0].URL; got != "https://api.example.com/api/v1/a2a" {
		t.Fatalf("unexpected A2A base URL: %s", got)
	}
	if _, exists := card.SecuritySchemes["api_key"]; exists {
		t.Fatal("legacy api_key scheme should not be present")
	}
}

func TestA2AHandlerRejectsMissingVersionHeader(t *testing.T) {
	router, _, _ := newA2ATestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/a2a/message:send", bytes.NewReader(newValidSendMessageBody(t)))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	if !strings.Contains(res.Header().Get("Content-Type"), a2a.ContentTypeProblemJSON) {
		t.Fatalf("expected problem+json content type, got %s", res.Header().Get("Content-Type"))
	}
}

func TestA2AHandlerUsesExactColonRoutes(t *testing.T) {
	router, _, _ := newA2ATestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/a2a/messagefoo", bytes.NewReader(newValidSendMessageBody(t)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(a2a.HeaderVersion, a2a.SupportedVersion)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-spec route, got %d", res.Code)
	}
}

func TestA2AHandlerMessageLifecycleAndPushConfigCRUD(t *testing.T) {
	router, _, _ := newA2ATestRouter(t)

	sendReq := httptest.NewRequest(http.MethodPost, "/api/v1/a2a/message:send", bytes.NewReader(newValidSendMessageBody(t)))
	sendReq.Header.Set("Content-Type", "application/json")
	sendReq.Header.Set(a2a.HeaderVersion, a2a.SupportedVersion)
	sendRes := httptest.NewRecorder()
	router.ServeHTTP(sendRes, sendReq)

	if sendRes.Code != http.StatusOK {
		t.Fatalf("expected 200 from message:send, got %d: %s", sendRes.Code, sendRes.Body.String())
	}

	var sendResp a2a.SendMessageResponse
	if err := json.Unmarshal(sendRes.Body.Bytes(), &sendResp); err != nil {
		t.Fatalf("decode send message response: %v", err)
	}
	if sendResp.Task == nil || sendResp.Task.ID == "" {
		t.Fatalf("expected task in response, got %#v", sendResp)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/a2a/tasks/"+sendResp.Task.ID, nil)
	getReq.Header.Set(a2a.HeaderVersion, a2a.SupportedVersion)
	getRes := httptest.NewRecorder()
	router.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected 200 from get task, got %d", getRes.Code)
	}

	pushBody, err := json.Marshal(a2a.TaskPushNotificationConfig{
		PushNotificationConfig: a2a.PushNotificationConfig{
			ID:    "cfg-1",
			URL:   "https://93.184.216.34/webhook",
			Token: "notify-token",
		},
	})
	if err != nil {
		t.Fatalf("marshal push config: %v", err)
	}

	createPushReq := httptest.NewRequest(http.MethodPost, "/api/v1/a2a/tasks/"+sendResp.Task.ID+"/pushNotificationConfigs", bytes.NewReader(pushBody))
	createPushReq.Header.Set("Content-Type", "application/json")
	createPushReq.Header.Set(a2a.HeaderVersion, a2a.SupportedVersion)
	createPushRes := httptest.NewRecorder()
	router.ServeHTTP(createPushRes, createPushReq)
	if createPushRes.Code != http.StatusOK {
		t.Fatalf("expected 200 from create push config, got %d: %s", createPushRes.Code, createPushRes.Body.String())
	}

	listPushReq := httptest.NewRequest(http.MethodGet, "/api/v1/a2a/tasks/"+sendResp.Task.ID+"/pushNotificationConfigs", nil)
	listPushReq.Header.Set(a2a.HeaderVersion, a2a.SupportedVersion)
	listPushRes := httptest.NewRecorder()
	router.ServeHTTP(listPushRes, listPushReq)
	if listPushRes.Code != http.StatusOK {
		t.Fatalf("expected 200 from list push configs, got %d", listPushRes.Code)
	}

	getPushReq := httptest.NewRequest(http.MethodGet, "/api/v1/a2a/tasks/"+sendResp.Task.ID+"/pushNotificationConfigs/cfg-1", nil)
	getPushReq.Header.Set(a2a.HeaderVersion, a2a.SupportedVersion)
	getPushRes := httptest.NewRecorder()
	router.ServeHTTP(getPushRes, getPushReq)
	if getPushRes.Code != http.StatusOK {
		t.Fatalf("expected 200 from get push config, got %d", getPushRes.Code)
	}

	deletePushReq := httptest.NewRequest(http.MethodDelete, "/api/v1/a2a/tasks/"+sendResp.Task.ID+"/pushNotificationConfigs/cfg-1", nil)
	deletePushReq.Header.Set(a2a.HeaderVersion, a2a.SupportedVersion)
	deletePushRes := httptest.NewRecorder()
	router.ServeHTTP(deletePushRes, deletePushReq)
	if deletePushRes.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from delete push config, got %d", deletePushRes.Code)
	}
}

func TestA2AHandlerMessageStream(t *testing.T) {
	router, _, _ := newA2ATestRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/a2a/message:stream", bytes.NewReader(newValidSendMessageBody(t)))
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(a2a.HeaderVersion, a2a.SupportedVersion)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send stream request: %v", err)
	}
	defer res.Body.Close()
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read stream response body: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from message:stream, got %d: %s", res.StatusCode, string(bodyBytes))
	}
	body := string(bodyBytes)
	if !strings.Contains(body, "event: task") {
		t.Fatalf("expected task SSE event, got %q", body)
	}
	if !strings.Contains(body, "event: status-update") {
		t.Fatalf("expected status-update SSE event, got %q", body)
	}
}

func TestA2AHandlerSubscribeStreamSupportsSlashPath(t *testing.T) {
	router, taskService, _ := newA2ATestRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	var sendReq a2a.SendMessageRequest
	if err := json.Unmarshal(newValidSendMessageBody(t), &sendReq); err != nil {
		t.Fatalf("decode send message request: %v", err)
	}

	sendResp, err := taskService.SendMessage(context.Background(), &sendReq, "user-1", "biz-1")
	if err != nil {
		t.Fatalf("send message: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/a2a/tasks/"+sendResp.Task.ID+"/subscribe", nil)
	if err != nil {
		t.Fatalf("build subscribe request: %v", err)
	}
	req.Header.Set(a2a.HeaderVersion, a2a.SupportedVersion)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send subscribe request: %v", err)
	}
	defer res.Body.Close()

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read subscribe response body: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from subscribe stream, got %d: %s", res.StatusCode, string(bodyBytes))
	}
	body := string(bodyBytes)
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected subscribe SSE error event, got %q", body)
	}
	if !strings.Contains(body, "Subscription Failed") {
		t.Fatalf("expected subscribe handler error payload, got %q", body)
	}
}
