package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type renderProfilePermissionChecker struct{}

func (renderProfilePermissionChecker) UserHasPermission(context.Context, string, string, string) bool {
	return true
}

func TestRenderProfileGetResponseNeverSerializesPassword(t *testing.T) {
	router, _, businessID, profileID, password := newRenderProfileHandlerTest(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/render-profiles/"+profileID, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), password) {
		t.Fatal("render profile response exposed the stored password")
	}
	var body map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, forbidden := range []string{"password", "legacy_password", "password_ciphertext"} {
		if _, ok := body[forbidden]; ok {
			t.Fatalf("render profile response contains forbidden field %q", forbidden)
		}
	}
	if configured, ok := body["password_configured"].(bool); !ok || !configured {
		t.Fatalf("password_configured = %#v, want true", body["password_configured"])
	}
	if got := body["business_id"]; got != businessID {
		t.Fatalf("business_id = %#v, want %q", got, businessID)
	}
}

func TestRenderProfileHandlersTreatPasswordAsWriteOnly(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		method string
		path   func(string) string
		body   string
		status int
	}{
		{
			name:   "list",
			method: http.MethodGet,
			path:   func(string) string { return "/api/v1/render-profiles" },
			status: http.StatusOK,
		},
		{
			name:   "get default",
			method: http.MethodGet,
			path:   func(string) string { return "/api/v1/render-profiles/default" },
			status: http.StatusOK,
		},
		{
			name:   "get by id",
			method: http.MethodGet,
			path:   func(id string) string { return "/api/v1/render-profiles/" + id },
			status: http.StatusOK,
		},
		{
			name:   "create",
			method: http.MethodPost,
			path:   func(string) string { return "/api/v1/render-profiles" },
			body:   `{"name":"New protected profile","password_protected":true,"password":"synthetic-request-password"}`,
			status: http.StatusCreated,
		},
		{
			name:   "update",
			method: http.MethodPut,
			path:   func(id string) string { return "/api/v1/render-profiles/" + id },
			body:   `{"password_protected":true,"password":"synthetic-request-password"}`,
			status: http.StatusOK,
		},
		{
			name:   "set default",
			method: http.MethodPost,
			path:   func(id string) string { return "/api/v1/render-profiles/" + id + "/default" },
			status: http.StatusOK,
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			router, _, _, profileID, storedPassword := newRenderProfileHandlerTest(t)
			request := httptest.NewRequest(
				fixture.method,
				fixture.path(profileID),
				bytes.NewBufferString(fixture.body),
			)
			if fixture.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != fixture.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, fixture.status, response.Body.String())
			}
			serialized := response.Body.String()
			if strings.Contains(serialized, storedPassword) ||
				strings.Contains(serialized, "synthetic-request-password") ||
				strings.Contains(serialized, "rpw:v1:") {
				t.Fatal("render profile handler response exposed password material")
			}
			var decoded interface{}
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			for _, forbidden := range []string{"password", "legacy_password", "password_ciphertext"} {
				if jsonContainsKey(decoded, forbidden) {
					t.Fatalf("render profile handler response contains forbidden field %q", forbidden)
				}
			}
			if !jsonContainsConfiguredPassword(decoded) {
				t.Fatal("render profile handler response omitted password_configured=true")
			}
		})
	}
}

func jsonContainsKey(value interface{}, key string) bool {
	switch current := value.(type) {
	case map[string]interface{}:
		if _, ok := current[key]; ok {
			return true
		}
		for _, child := range current {
			if jsonContainsKey(child, key) {
				return true
			}
		}
	case []interface{}:
		for _, child := range current {
			if jsonContainsKey(child, key) {
				return true
			}
		}
	}
	return false
}

func jsonContainsConfiguredPassword(value interface{}) bool {
	switch current := value.(type) {
	case map[string]interface{}:
		if configured, ok := current["password_configured"].(bool); ok && configured {
			return true
		}
		for _, child := range current {
			if jsonContainsConfiguredPassword(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range current {
			if jsonContainsConfiguredPassword(child) {
				return true
			}
		}
	}
	return false
}

func newRenderProfileHandlerTest(t *testing.T) (*gin.Engine, *gorm.DB, string, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := db.Exec(`CREATE TABLE render_profiles (
		id TEXT PRIMARY KEY,
		business_id TEXT NOT NULL,
		name TEXT NOT NULL,
		header_html TEXT,
		footer_html TEXT,
		watermark_text TEXT,
		banner_text TEXT,
		font_family TEXT,
		page_size TEXT,
		layout_config TEXT,
		password_protected BOOLEAN NOT NULL DEFAULT FALSE,
		password TEXT,
		password_ciphertext TEXT,
		copy_allowed BOOLEAN NOT NULL DEFAULT TRUE,
		print_allowed BOOLEAN NOT NULL DEFAULT TRUE,
		custom_labels TEXT,
		visibility_config TEXT,
		is_default BOOLEAN NOT NULL DEFAULT FALSE,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create render profiles: %v", err)
	}
	businessID := uuid.NewString()
	profileID := uuid.NewString()
	password := "render-profile-test-password"
	profile := &models.RenderProfile{
		ID:                profileID,
		BusinessID:        businessID,
		Name:              "Protected profile",
		PasswordProtected: true,
		LegacyPassword:    &password,
		IsDefault:         true,
	}
	if err := db.Create(profile).Error; err != nil {
		t.Fatalf("seed render profile: %v", err)
	}

	cfg := &config.Config{}
	cfg.Credentials.EncryptionKey = base64.StdEncoding.EncodeToString(
		[]byte("0123456789abcdef0123456789abcdef"),
	)
	service := services.NewDocumentService(
		db,
		cfg,
		nil,
		postgres.NewDocumentRepository(db),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&awsclients.Config{},
		renderProfilePermissionChecker{},
		logger.NewWithEnv("test"),
	)
	handler := NewRenderProfileHandler(service, logger.NewWithEnv("test"))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("business_id", businessID)
		c.Set("user_id", "render-profile-test-user")
		c.Next()
	})
	router.GET("/api/v1/render-profiles", handler.List)
	router.GET("/api/v1/render-profiles/default", handler.GetDefault)
	router.GET("/api/v1/render-profiles/:id", handler.Get)
	router.POST("/api/v1/render-profiles", handler.Create)
	router.POST("/api/v1/render-profiles/:id/default", handler.SetDefault)
	router.PUT("/api/v1/render-profiles/:id", handler.Update)
	return router, db, businessID, profileID, password
}
