package app

import (
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/handlers"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

func TestProtectedDeleteRoutesRequireIDParam(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := setupRouter(
		&config.Config{AllowedOrigins: []string{"http://localhost:3000"}},
		&services.Container{AWS: &awsclients.Config{}},
		&handlers.Handler{},
		logger.New(),
	)

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

	router := setupRouter(
		&config.Config{AllowedOrigins: []string{"http://localhost:3000"}},
		&services.Container{AWS: &awsclients.Config{}},
		&handlers.Handler{},
		logger.New(),
	)

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
}
