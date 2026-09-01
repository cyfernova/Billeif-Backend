package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSwaggerDocumentsCapabilityErrorsOnlyForGuardedReportExport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerSwaggerRoutes(router, &config.Config{})
	req := httptest.NewRequest(http.MethodGet, "/swagger.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("swagger status = %d", rec.Code)
	}
	var document struct {
		Paths map[string]map[string]struct {
			Responses map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode swagger: %v", err)
	}
	exportResponses := document.Paths["/reports/{key}/export"]["post"].Responses
	for _, status := range []string{"403", "422", "429", "503"} {
		if _, ok := exportResponses[status]; !ok {
			t.Fatalf("export response %s is undocumented", status)
		}
		if !containsCapabilityMutationError(exportResponses[status]) {
			t.Fatalf("export response %s does not use CapabilityMutationError", status)
		}
	}
	queryResponses := document.Paths["/reports/{key}/query"]["post"].Responses
	for _, status := range []string{"422", "429", "503"} {
		if response, ok := queryResponses[status]; ok && containsCapabilityMutationError(response) {
			t.Fatalf("unguarded query documents capability response %s", status)
		}
	}
}

func containsCapabilityMutationError(raw json.RawMessage) bool {
	return strings.Contains(string(raw), "handlers.CapabilityMutationError")
}
