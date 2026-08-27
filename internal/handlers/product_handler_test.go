package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type recordingProductService struct {
	called       string
	businessIDs  []string
	createInput  services.CreateProductInput
	product      *models.Product
	serviceError error
}

func (s *recordingProductService) record(operation, businessID string) {
	s.called = operation
	s.businessIDs = append(s.businessIDs, businessID)
}

func (s *recordingProductService) Create(_ context.Context, input services.CreateProductInput) (*models.Product, error) {
	s.record("create", input.BusinessID)
	s.createInput = input
	return s.product, s.serviceError
}

func (s *recordingProductService) GetByBusiness(_ context.Context, businessID, _ string) (*models.Product, error) {
	s.record("get", businessID)
	return s.product, s.serviceError
}

func (s *recordingProductService) ListWithFilters(_ context.Context, businessID string, _ services.ProductListFilter, _, _ int) ([]*models.Product, int64, error) {
	s.record("list", businessID)
	return []*models.Product{s.product}, 1, s.serviceError
}

func (s *recordingProductService) UpdateByBusiness(_ context.Context, businessID, _ string, _ services.UpdateProductInput) (*models.Product, error) {
	s.record("update", businessID)
	return s.product, s.serviceError
}

func (s *recordingProductService) CloneByBusiness(_ context.Context, businessID, _ string) (*models.Product, error) {
	s.record("clone", businessID)
	return s.product, s.serviceError
}

func (s *recordingProductService) DeleteByBusiness(_ context.Context, businessID, _ string) error {
	s.record("delete", businessID)
	return s.serviceError
}

func (s *recordingProductService) GetImageUploadURLByBusiness(_ context.Context, businessID, _, _ string) (string, error) {
	s.record("upload_image", businessID)
	return "https://example.test/upload", s.serviceError
}

func (s *recordingProductService) AdjustStockByBusiness(_ context.Context, businessID, _ string, _ services.StockAdjustmentInput) (*models.Product, error) {
	s.record("adjust_stock", businessID)
	return s.product, s.serviceError
}

func newRecordingProductHandler() (*ProductHandler, *recordingProductService) {
	service := &recordingProductService{product: &models.Product{ID: "product-1", BusinessID: "business-b"}}
	return NewProductHandler(service, logger.New()), service
}

type productBusinessRepository struct{}

func (productBusinessRepository) Create(context.Context, *models.BusinessProfile) error { return nil }

func (productBusinessRepository) GetByID(_ context.Context, businessID string) (*models.BusinessProfile, error) {
	if businessID != "business-b" {
		return nil, errors.New("business not found")
	}
	return &models.BusinessProfile{ID: businessID, OwnerID: "user-a"}, nil
}

func (productBusinessRepository) Update(context.Context, *models.BusinessProfile) error { return nil }
func (productBusinessRepository) Delete(context.Context, string) error                  { return nil }
func (productBusinessRepository) List(context.Context, string, int, int) ([]*models.BusinessProfile, int64, error) {
	return nil, 0, nil
}

var _ interfaces.BusinessRepository = productBusinessRepository{}

type productTeamMemberRepository struct{}

func (productTeamMemberRepository) Create(context.Context, *models.TeamMember) error { return nil }
func (productTeamMemberRepository) GetByID(context.Context, string, string) (*models.TeamMember, error) {
	return nil, errors.New("team member not found")
}
func (productTeamMemberRepository) GetByBusinessID(context.Context, string, int, int) ([]*models.TeamMember, int64, error) {
	return nil, 0, nil
}
func (productTeamMemberRepository) Update(context.Context, *models.TeamMember) error { return nil }
func (productTeamMemberRepository) Delete(context.Context, string) error             { return nil }
func (productTeamMemberRepository) GetByUserID(context.Context, string) ([]*models.TeamMember, error) {
	return nil, nil
}

var _ interfaces.TeamMemberRepository = productTeamMemberRepository{}

func newProductBusinessAuthService() *services.BusinessAuthService {
	return services.NewBusinessAuthService(nil, productBusinessRepository{}, productTeamMemberRepository{}, logger.New())
}

func productTokenScope(c *gin.Context) {
	c.Set("user_id", "user-a")
	c.Set("business_id", "business-a")
}

func productTokenScopeMiddleware(c *gin.Context) {
	productTokenScope(c)
	c.Next()
}

func TestProductHandler_UsesValidatedBusinessScopeForEveryOperation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		method      string
		path        string
		body        string
		scopeHeader string
		permission  string
		call        func(*ProductHandler, *gin.Context)
		want        string
	}{
		{name: "get", method: http.MethodGet, path: "/products/product-1", scopeHeader: "business-b", permission: services.PermissionProductsView, call: (*ProductHandler).Get, want: "get"},
		{name: "list", method: http.MethodGet, path: "/products", scopeHeader: "business-b", permission: services.PermissionProductsView, call: (*ProductHandler).List, want: "list"},
		{name: "update", method: http.MethodPut, path: "/products/product-1", body: `{}`, scopeHeader: "business-b", permission: services.PermissionProductsManage, call: (*ProductHandler).Update, want: "update"},
		{name: "clone", method: http.MethodPost, path: "/products/product-1/clone", scopeHeader: "business-b", permission: services.PermissionProductsManage, call: (*ProductHandler).Clone, want: "clone"},
		{name: "delete", method: http.MethodDelete, path: "/products/product-1", scopeHeader: "business-b", permission: services.PermissionProductsManage, call: (*ProductHandler).Delete, want: "delete"},
		{name: "upload image", method: http.MethodPost, path: "/products/product-1/image", scopeHeader: "business-b", permission: services.PermissionProductsManage, call: (*ProductHandler).UploadImage, want: "upload_image"},
		{name: "adjust stock", method: http.MethodPost, path: "/products/product-1/stock", body: `{"quantity":1}`, scopeHeader: "business-b", permission: services.PermissionProductsManage, call: (*ProductHandler).AdjustStock, want: "adjust_stock"},
		{name: "create body scope", method: http.MethodPost, path: "/products", body: `{"business_id":"business-b","name":"Widget","price":1}`, permission: services.PermissionProductsManage, call: (*ProductHandler).Create, want: "create"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, service := newRecordingProductHandler()
			router := gin.New()
			authService := newProductBusinessAuthService()
			routeHandler := func(c *gin.Context) {
				tt.call(handler, c)
			}
			middlewareChain := []gin.HandlerFunc{
				productTokenScopeMiddleware,
				middleware.BusinessAuth(authService),
				middleware.RequirePermission(authService, tt.permission),
				routeHandler,
			}
			router.Any("/products", middlewareChain...)
			router.Any("/products/:id", middlewareChain...)
			router.Any("/products/:id/clone", middlewareChain...)
			router.Any("/products/:id/image", middlewareChain...)
			router.Any("/products/:id/stock", middlewareChain...)

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.scopeHeader != "" {
				req.Header.Set("X-Business-ID", tt.scopeHeader)
			}
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			require.NotEqual(t, http.StatusNotFound, res.Code)
			require.Equal(t, tt.want, service.called)
			require.Equal(t, []string{"business-b"}, service.businessIDs)
		})
	}
}

func TestProductHandler_MissingBusinessScopeDoesNotCallService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, service := newRecordingProductHandler()
	authService := newProductBusinessAuthService()
	router := gin.New()
	router.GET("/products/:id", func(c *gin.Context) {
		// Authenticated user without a token business claim or requested scope.
		c.Set("user_id", "user-a")
		c.Next()
	}, middleware.BusinessAuth(authService), middleware.RequirePermission(authService, services.PermissionProductsView), handler.Get)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/products/product-1", nil)
	router.ServeHTTP(res, req)

	require.Equal(t, http.StatusForbidden, res.Code)
	require.Empty(t, service.businessIDs)
}

func TestProductHandler_CreateRejectsBusinessMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, service := newRecordingProductHandler()
	authService := newProductBusinessAuthService()
	router := gin.New()
	router.POST("/products", productTokenScopeMiddleware, middleware.BusinessAuth(authService), middleware.RequirePermission(authService, services.PermissionProductsManage), handler.Create)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/products", strings.NewReader(`{"business_id":"business-a","name":"Widget","price":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Business-ID", "business-b")
	router.ServeHTTP(res, req)

	require.Equal(t, http.StatusForbidden, res.Code)
	require.Empty(t, service.businessIDs)
}

func TestProductHandler_ServiceErrorStillReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, service := newRecordingProductHandler()
	service.serviceError = errors.New("service failure")
	router := gin.New()
	// This request has a valid alternate business scope, so the service error
	// assertion remains independent from scope rejection.
	authService := newProductBusinessAuthService()
	router.GET("/products/:id", productTokenScopeMiddleware, middleware.BusinessAuth(authService), middleware.RequirePermission(authService, services.PermissionProductsView), handler.Get)
	req := httptest.NewRequest(http.MethodGet, "/products/product-1", nil)
	req.Header.Set("X-Business-ID", "business-b")

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	require.Equal(t, http.StatusNotFound, res.Code)
	require.Equal(t, []string{"business-b"}, service.businessIDs)
}

func TestProductHandler_DeniedBusinessScopeDoesNotCallService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, service := newRecordingProductHandler()
	authService := newProductBusinessAuthService()
	router := gin.New()
	router.GET("/products/:id", productTokenScopeMiddleware, middleware.BusinessAuth(authService), middleware.RequirePermission(authService, services.PermissionProductsView), handler.Get)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/products/product-1", nil)
	req.Header.Set("X-Business-ID", "business-c")
	router.ServeHTTP(res, req)

	require.Equal(t, http.StatusForbidden, res.Code)
	require.Empty(t, service.businessIDs)
}
