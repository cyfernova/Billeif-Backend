package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/handlers"
	"invoice-backend/internal/middleware"
	"invoice-backend/internal/ratelimit"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

func TestProtectedDeleteRoutesRequireIDParam(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := setupTestRouter(t)

	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	for _, route := range []string{
		"DELETE /api/v1/invoices/:id",
		"DELETE /api/v1/payments/:id",
		"DELETE /api/v1/price-lists/:id",
	} {
		if !routes[route] {
			t.Fatalf("expected registered route %q", route)
		}
	}

	for _, route := range []string{
		"DELETE /api/v1/invoices",
		"DELETE /api/v1/payments",
		"DELETE /api/v1/price-lists",
	} {
		if routes[route] {
			t.Fatalf("did not expect collection delete route %q", route)
		}
	}
}

func TestLegacyInvoiceSendRouteIsNotRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := setupTestRouter(t)

	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
		if route.Method == "POST" && route.Path == "/api/v1/invoices/:id/send" {
			t.Fatal("legacy invoice send route must not be registered before the issue and delivery workflow exists")
		}
	}
	if routes["GET /api/v1/invoices/next-number"] {
		t.Fatal("legacy next-number route must not be registered")
	}
	if !routes["POST /api/v1/invoices/:id/issue"] {
		t.Fatal("canonical invoice issue route must be registered")
	}
	if !routes["POST /api/v1/invoices/:id/previews"] {
		t.Fatal("canonical invoice preview route must be registered")
	}
	if !routes["GET /api/v1/invoices/:id/renders/:render_job_id"] {
		t.Fatal("tenant-scoped invoice render status route must be registered")
	}
	if !routes["POST /api/v1/invoices/:id/deliveries"] {
		t.Fatal("canonical invoice delivery route must be registered")
	}
	if !routes["GET /api/v1/invoices/:id/deliveries/:delivery_id"] {
		t.Fatal("tenant-scoped invoice delivery status route must be registered")
	}
}

func TestPaymentReversalRouteIsRegistered(t *testing.T) {
	router := setupTestRouter(t)
	for _, route := range router.Routes() {
		if route.Method == http.MethodPost && route.Path == "/api/v1/payments/:id/reverse" {
			return
		}
	}
	t.Fatal("payment reversal route must be registered")
}

func TestCustomerCapabilityRouteIsRegisteredAsProtectedRead(t *testing.T) {
	router := setupTestRouter(t)
	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/capabilities" {
			return
		}
	}
	t.Fatal("customer capability route must be registered")
}

func TestCustomerCapabilityRouteRejectsUnauthenticatedRequests(t *testing.T) {
	router := setupTestRouter(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/capabilities?platform=web", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestRouterDoesNotLetGinTrustCallerForwardingHeaders(t *testing.T) {
	router := setupTestRouter(t)
	router.GET("/__test/client-ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	request := httptest.NewRequest(http.MethodGet, "/__test/client-ip", nil)
	request.RemoteAddr = "198.51.100.24:443"
	request.Header.Set("X-Forwarded-For", "203.0.113.11")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "198.51.100.24" {
		t.Fatalf("Gin client identity = status %d body %q, want server-observed peer", response.Code, response.Body.String())
	}
}

func setupTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	identities, err := middleware.NewClientIdentityResolver("development", "")
	if err != nil {
		t.Fatalf("create client identity resolver: %v", err)
	}
	return setupRouter(
		&config.Config{AllowedOrigins: []string{"http://localhost:3000"}},
		&services.Container{AWS: &awsclients.Config{}},
		&handlers.Handler{},
		logger.New(),
		ratelimit.DisabledLimiter{},
		identities,
		config.ProfileHTTP,
	)
}

func TestQueueWorkerProfilesDoNotBuildHTTPRouter(t *testing.T) {
	for _, profile := range []config.Profile{config.ProfileBulkImport, config.ProfileInvoice, config.ProfileGST, config.ProfileBargaining} {
		t.Run(string(profile), func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("worker router initialization panicked: %v", recovered)
				}
			}()
			router := setupRouter(&config.Config{}, nil, nil, logger.New(), ratelimit.DisabledLimiter{}, nil, profile)
			if router != nil {
				t.Fatal("queue worker must not construct an HTTP router")
			}
		})
	}
}
