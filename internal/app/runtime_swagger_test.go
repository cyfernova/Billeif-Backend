package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/config"

	"github.com/gin-gonic/gin"
)

func TestRegisterSwaggerRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	cfg := &config.Config{
		Server: config.ServerConfig{
			BaseURL: "https://pf5f3g949h.execute-api.us-east-1.amazonaws.com/dev",
		},
		Cognito: config.CognitoConfig{
			ClientID: "test-client-id",
			Domain:   "invoice-backend-app.auth.us-east-1.amazoncognito.com",
		},
	}

	registerSwaggerRoutes(router, cfg)

	req := httptest.NewRequest(http.MethodGet, "/swagger.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected swagger spec route to return 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected swagger UI route to return 200, got %d", rec.Code)
	}
}
