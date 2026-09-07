package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"invoice-backend/internal/config"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
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

func TestSwaggerDocumentsExactSubscriptionMutationAndHistoryStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerSwaggerRoutes(router, &config.Config{})
	request := httptest.NewRequest(http.MethodGet, "/swagger.json", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("swagger status = %d", response.Code)
	}
	var document struct {
		Paths map[string]map[string]struct {
			Responses  map[string]json.RawMessage `json:"responses"`
			Parameters []struct {
				Name    string `json:"name"`
				Minimum any    `json:"minimum"`
				Maximum any    `json:"maximum"`
				Default any    `json:"default"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode swagger: %v", err)
	}
	for _, path := range []string{"/subscriptions/checkout", "/subscriptions/plan-change", "/subscriptions/cancellation"} {
		responses := document.Paths[path]["post"].Responses
		for _, status := range []string{"200", "400", "409", "422", "500", "503"} {
			if _, ok := responses[status]; !ok {
				t.Fatalf("%s response %s is undocumented", path, status)
			}
		}
	}
	for _, path := range []string{"/subscriptions/billing-history", "/subscriptions/audit"} {
		operation := document.Paths[path]["get"]
		for _, status := range []string{"200", "400", "500", "503"} {
			if _, ok := operation.Responses[status]; !ok {
				t.Fatalf("%s response %s is undocumented", path, status)
			}
		}
		if len(operation.Parameters) != 1 || operation.Parameters[0].Name != "limit" ||
			fmt.Sprint(operation.Parameters[0].Minimum) != "1" || fmt.Sprint(operation.Parameters[0].Maximum) != "100" || fmt.Sprint(operation.Parameters[0].Default) != "50" {
			t.Fatalf("%s limit contract = %#v", path, operation.Parameters)
		}
	}
}

func TestStaticSubscriptionContractsDocumentDistinctInternalFailures(t *testing.T) {
	type errorExample struct {
		Error struct {
			Code    string `yaml:"code"`
			Message string `yaml:"message"`
		} `yaml:"error"`
	}
	type contractDocument struct {
		Paths map[string]map[string]struct {
			Responses map[string]struct {
				Ref string `yaml:"$ref"`
			} `yaml:"responses"`
		} `yaml:"paths"`
		Components struct {
			Responses map[string]struct {
				Description string `yaml:"description"`
				Content     map[string]struct {
					Example errorExample `yaml:"example"`
				} `yaml:"content"`
			} `yaml:"responses"`
		} `yaml:"components"`
	}

	for _, fixture := range []struct {
		name       string
		path       string
		pathPrefix string
	}{
		{name: "docs", path: "../../docs/openapi.yaml"},
		{name: "root", path: "../../openapi/openapi.yaml", pathPrefix: "/api/v1"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw, err := os.ReadFile(fixture.path)
			if err != nil {
				t.Fatalf("read contract: %v", err)
			}
			var document contractDocument
			if err := yaml.Unmarshal(raw, &document); err != nil {
				t.Fatalf("parse contract: %v", err)
			}
			for _, path := range []string{"/subscriptions/checkout", "/subscriptions/plan-change", "/subscriptions/cancellation"} {
				got := document.Paths[fixture.pathPrefix+path]["post"].Responses["500"].Ref
				if got != "#/components/responses/SubscriptionMutationInternal" {
					t.Fatalf("%s mutation 500 ref = %q", path, got)
				}
			}
			mutation := document.Components.Responses["SubscriptionMutationInternal"]
			if mutation.Description != "Subscription request could not be completed" {
				t.Fatalf("mutation 500 description = %q", mutation.Description)
			}
			mutationExample := mutation.Content["application/json"].Example.Error
			if mutationExample.Code != "subscription_mutation_internal_error" || mutationExample.Message != "subscription request could not be completed" {
				t.Fatalf("mutation 500 example = %#v", mutationExample)
			}
			history := document.Components.Responses["SubscriptionInternal"].Content["application/json"].Example.Error
			if history.Code != "subscription_internal_error" || history.Message != "subscription history could not be loaded" {
				t.Fatalf("history 500 example = %#v", history)
			}
		})
	}

	handoff, err := os.ReadFile("../../docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md")
	if err != nil {
		t.Fatalf("read frontend handoff: %v", err)
	}
	for _, exact := range []string{
		`{"error":{"code":"subscription_mutation_internal_error","message":"subscription request could not be completed"}}`,
		`{"error":{"code":"subscription_internal_error","message":"subscription history could not be loaded"}}`,
	} {
		if !strings.Contains(string(handoff), exact) {
			t.Fatalf("frontend handoff is missing exact response %s", exact)
		}
	}
}

func TestOperationContractsArePublishedAndExplicitlyFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerSwaggerRoutes(router, &config.Config{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/swagger.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("swagger status = %d", rec.Code)
	}

	for _, fixture := range []struct {
		path       string
		pathPrefix string
		raw        []byte
	}{
		{path: "../../docs/openapi.yaml"},
		{path: "../../openapi/openapi.yaml", pathPrefix: "/api/v1"},
		{path: "/swagger.json", raw: rec.Body.Bytes()},
	} {
		raw := fixture.raw
		if raw == nil {
			var err error
			raw, err = os.ReadFile(fixture.path)
			if err != nil {
				t.Fatalf("read %s: %v", fixture.path, err)
			}
		}
		var document struct {
			Paths map[string]any `yaml:"paths"`
		}
		if err := yaml.Unmarshal(raw, &document); err != nil {
			t.Fatalf("decode %s: %v", fixture.path, err)
		}
		contract := string(raw)
		for _, path := range []string{
			"/operations", "/operations/{operation_id}",
			"/operations/{operation_id}/timeline", "/operations/{operation_id}/recovery",
			"/operator/operations/{operation_id}",
			"/operator/operations/{operation_id}/timeline",
			"/operator/operations/{operation_id}/recovery",
		} {
			if _, ok := document.Paths[fixture.pathPrefix+path]; !ok {
				t.Fatalf("%s missing operation contract %s", fixture.path, fixture.pathPrefix+path)
			}
		}
		for _, exact := range []string{"reconciliation_required", "unknown", "step_up_required", "source_status", "recovery_actions"} {
			if !strings.Contains(contract, exact) {
				t.Fatalf("%s missing %q", fixture.path, exact)
			}
		}
	}

	handoff, err := os.ReadFile("../../docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md")
	if err != nil {
		t.Fatalf("read frontend handoff: %v", err)
	}
	for _, exact := range []string{
		"## OPS-007: Aggregate business operational visibility and safe recovery",
		"## OPS-008: Separately authorized operator detail and recovery boundary",
		"403 operator_not_configured", "403 operator_access_denied", "409 unsafe_replay",
		"422 unsupported_recovery", "HTTP status is `428`", "503 recovery_audit_unavailable",
	} {
		if !strings.Contains(string(handoff), exact) {
			t.Fatalf("frontend handoff missing exact operation contract %q", exact)
		}
	}
}

func containsCapabilityMutationError(raw json.RawMessage) bool {
	return strings.Contains(string(raw), "handlers.CapabilityMutationError")
}
