package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type handlerActorPermissionChecker struct{}

func (handlerActorPermissionChecker) UserHasPermission(_ context.Context, userID, businessID, _ string) bool {
	return userID == "accountant-1" && businessID == "business-1"
}

type handlerActorCustomerRepository struct{}

func (*handlerActorCustomerRepository) Create(_ context.Context, customer *models.Customer) error {
	customer.ID = "customer-1"
	return nil
}
func (*handlerActorCustomerRepository) GetByID(context.Context, string, string) (*models.Customer, error) {
	return &models.Customer{ID: "customer-1", BusinessID: "business-1"}, nil
}
func (*handlerActorCustomerRepository) GetByBusinessID(context.Context, string, int, int) ([]*models.Customer, int64, error) {
	return nil, 0, nil
}
func (*handlerActorCustomerRepository) Update(context.Context, *models.Customer) error { return nil }
func (*handlerActorCustomerRepository) Delete(context.Context, string) error           { return nil }

type handlerActorVendorRepository struct{}

func (*handlerActorVendorRepository) Create(_ context.Context, vendor *models.Vendor) error {
	vendor.ID = "vendor-1"
	return nil
}
func (*handlerActorVendorRepository) GetByID(context.Context, string, string) (*models.Vendor, error) {
	return &models.Vendor{ID: "vendor-1", BusinessID: "business-1"}, nil
}
func (*handlerActorVendorRepository) GetByBusinessID(context.Context, string, int, int) ([]*models.Vendor, int64, error) {
	return nil, 0, nil
}
func (*handlerActorVendorRepository) Update(context.Context, *models.Vendor) error { return nil }
func (*handlerActorVendorRepository) Delete(context.Context, string) error         { return nil }

type handlerActorRenderRepository struct {
	interfaces.DocumentRepository
}

type handlerActorCapabilityGuard struct{}

func (handlerActorCapabilityGuard) Require(context.Context, services.CapabilityRequest) error {
	return nil
}

func (*handlerActorRenderRepository) CreateRenderProfile(_ context.Context, profile *models.RenderProfile) error {
	profile.ID = "profile-1"
	return nil
}
func (*handlerActorRenderRepository) GetRenderProfile(context.Context, string, string) (*models.RenderProfile, error) {
	return &models.RenderProfile{ID: "profile-1", BusinessID: "business-1"}, nil
}
func (*handlerActorRenderRepository) UpdateRenderProfile(context.Context, *models.RenderProfile) error {
	return nil
}
func (*handlerActorRenderRepository) DeleteRenderProfile(context.Context, string, string) error {
	return nil
}

func mutationActorRouter(includeUser bool) *gin.Engine {
	log := logger.New()
	checker := handlerActorPermissionChecker{}
	customerService := services.NewCustomerService(&handlerActorCustomerRepository{}, checker, log).
		WithCapabilityGuard(handlerActorCapabilityGuard{})
	customer := NewCustomerHandler(customerService, log)
	vendor := NewVendorHandler(services.NewVendorService(&handlerActorVendorRepository{}, checker, log), log)
	documents := services.NewDocumentService(
		nil, nil, nil, &handlerActorRenderRepository{}, nil, nil, nil, nil, nil, nil, nil,
		&awsclients.Config{}, checker, log,
	)
	renderProfile := NewRenderProfileHandler(documents, log)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("business_id", "business-1")
		c.Set("role", "accountant")
		if includeUser {
			c.Set("user_id", "accountant-1")
		}
		c.Next()
	})
	router.POST("/customers", customer.Create)
	router.PUT("/customers/:id", customer.Update)
	router.DELETE("/customers/:id", customer.Delete)
	router.POST("/customers/import", customer.Import)
	router.POST("/vendors", vendor.Create)
	router.PUT("/vendors/:id", vendor.Update)
	router.DELETE("/vendors/:id", vendor.Delete)
	router.POST("/render-profiles", renderProfile.Create)
	router.PUT("/render-profiles/:id", renderProfile.Update)
	router.POST("/render-profiles/:id/default", renderProfile.SetDefault)
	router.DELETE("/render-profiles/:id", renderProfile.Delete)
	return router
}

func TestMutationHandlersPropagateServerDerivedActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{method: http.MethodPost, path: "/customers", body: `{"name":"Customer","email":"customer@example.com"}`, wantStatus: http.StatusCreated},
		{method: http.MethodPut, path: "/customers/customer-1", body: `{"name":"Updated"}`, wantStatus: http.StatusOK},
		{method: http.MethodDelete, path: "/customers/customer-1", wantStatus: http.StatusNoContent},
		{method: http.MethodPost, path: "/customers/import", body: `[{"name":"Customer","email":"customer@example.com"}]`, wantStatus: http.StatusOK},
		{method: http.MethodPost, path: "/vendors", body: `{"name":"Vendor","email":"vendor@example.com"}`, wantStatus: http.StatusCreated},
		{method: http.MethodPut, path: "/vendors/vendor-1", body: `{"name":"Updated"}`, wantStatus: http.StatusOK},
		{method: http.MethodDelete, path: "/vendors/vendor-1", wantStatus: http.StatusNoContent},
		{method: http.MethodPost, path: "/render-profiles", body: `{"name":"Profile"}`, wantStatus: http.StatusCreated},
		{method: http.MethodPut, path: "/render-profiles/profile-1", body: `{"name":"Updated"}`, wantStatus: http.StatusOK},
		{method: http.MethodPost, path: "/render-profiles/profile-1/default", wantStatus: http.StatusOK},
		{method: http.MethodDelete, path: "/render-profiles/profile-1", wantStatus: http.StatusNoContent},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			mutationActorRouter(true).ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestMutationHandlerMissingServerActorReturnsForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/customers", bytes.NewBufferString(`{"name":"Customer","email":"customer@example.com"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mutationActorRouter(false).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}
