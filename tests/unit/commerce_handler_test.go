package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Mock Commerce Service
// =============================================================================

type MockCommerceService struct {
	mock.Mock
}

func (m *MockCommerceService) ListStorefronts(ctx context.Context, businessID string) ([]*models.Storefront, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Storefront), args.Error(1)
}

func (m *MockCommerceService) CreateStorefront(ctx context.Context, input services.UpsertStorefrontInput) (*models.Storefront, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Storefront), args.Error(1)
}

func (m *MockCommerceService) GetStorefront(ctx context.Context, businessID, id string) (*models.Storefront, error) {
	args := m.Called(ctx, businessID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Storefront), args.Error(1)
}

func (m *MockCommerceService) UpdateStorefrontSettings(ctx context.Context, businessID, id string, input services.UpsertStorefrontInput) (*models.Storefront, error) {
	args := m.Called(ctx, businessID, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Storefront), args.Error(1)
}

func (m *MockCommerceService) ListStorefrontProducts(ctx context.Context, businessID, storefrontID string) ([]*models.StorefrontProduct, error) {
	args := m.Called(ctx, businessID, storefrontID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.StorefrontProduct), args.Error(1)
}

func (m *MockCommerceService) ReplaceStorefrontProducts(ctx context.Context, businessID, storefrontID string, input []services.UpsertStorefrontProductInput) ([]*models.StorefrontProduct, error) {
	args := m.Called(ctx, businessID, storefrontID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.StorefrontProduct), args.Error(1)
}

func (m *MockCommerceService) ListStorefrontCoupons(ctx context.Context, storefrontID string) ([]*models.StorefrontCoupon, error) {
	args := m.Called(ctx, storefrontID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.StorefrontCoupon), args.Error(1)
}

func (m *MockCommerceService) CreateStorefrontCoupon(ctx context.Context, storefrontID string, input services.UpsertStorefrontCouponInput) (*models.StorefrontCoupon, error) {
	args := m.Called(ctx, storefrontID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StorefrontCoupon), args.Error(1)
}

func (m *MockCommerceService) UpdateStorefrontCoupon(ctx context.Context, storefrontID, couponID string, input services.UpsertStorefrontCouponInput) (*models.StorefrontCoupon, error) {
	args := m.Called(ctx, storefrontID, couponID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StorefrontCoupon), args.Error(1)
}

func (m *MockCommerceService) ListStorefrontOrders(ctx context.Context, businessID, storefrontID, status string, page, limit int) ([]*models.StoreOrder, int64, error) {
	args := m.Called(ctx, businessID, storefrontID, status, page, limit)
	if args.Get(0) == nil {
		return nil, 0, args.Error(2)
	}
	return args.Get(0).([]*models.StoreOrder), args.Get(1).(int64), args.Error(2)
}

func (m *MockCommerceService) ApproveStoreOrder(ctx context.Context, businessID, storefrontID, orderID string) (*models.StoreOrder, error) {
	args := m.Called(ctx, businessID, storefrontID, orderID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StoreOrder), args.Error(1)
}

func (m *MockCommerceService) CancelStoreOrder(ctx context.Context, businessID, storefrontID, orderID, reason string) (*models.StoreOrder, error) {
	args := m.Called(ctx, businessID, storefrontID, orderID, reason)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StoreOrder), args.Error(1)
}

func (m *MockCommerceService) GetCatalog(ctx context.Context, slug string) (*services.StorefrontCatalogResponse, error) {
	args := m.Called(ctx, slug)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.StorefrontCatalogResponse), args.Error(1)
}

func (m *MockCommerceService) ValidateCoupon(ctx context.Context, slug string, input services.ValidateCouponInput) (*services.CouponValidationResult, error) {
	args := m.Called(ctx, slug, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.CouponValidationResult), args.Error(1)
}

func (m *MockCommerceService) Checkout(ctx context.Context, slug, idempotencyKey string, input services.StorefrontCheckoutInput) (*services.CheckoutResult, error) {
	args := m.Called(ctx, slug, idempotencyKey, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.CheckoutResult), args.Error(1)
}

func (m *MockCommerceService) GetPublicOrder(ctx context.Context, slug, token string) (*models.StoreOrder, error) {
	args := m.Called(ctx, slug, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.StoreOrder), args.Error(1)
}

func (m *MockCommerceService) ListFeatureEntitlements(ctx context.Context, businessID string) ([]*models.FeatureEntitlement, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.FeatureEntitlement), args.Error(1)
}

func (m *MockCommerceService) SyncFeatureEntitlements(ctx context.Context, businessID string) ([]*models.FeatureEntitlement, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.FeatureEntitlement), args.Error(1)
}

func (m *MockCommerceService) ListRoles(ctx context.Context, businessID string) ([]*models.Role, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Role), args.Error(1)
}

func (m *MockCommerceService) CreateRole(ctx context.Context, input services.UpsertRoleInput) (*models.Role, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Role), args.Error(1)
}

func (m *MockCommerceService) UpdateRole(ctx context.Context, businessID, roleID string, input services.UpsertRoleInput) (*models.Role, error) {
	args := m.Called(ctx, businessID, roleID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Role), args.Error(1)
}

func (m *MockCommerceService) DeleteRole(ctx context.Context, businessID, roleID string) error {
	args := m.Called(ctx, businessID, roleID)
	return args.Error(0)
}

func (m *MockCommerceService) ListBranches(ctx context.Context, businessID string) ([]*models.Branch, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Branch), args.Error(1)
}

func (m *MockCommerceService) CreateBranch(ctx context.Context, input services.UpsertBranchInput) (*models.Branch, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Branch), args.Error(1)
}

func (m *MockCommerceService) UpdateBranch(ctx context.Context, businessID, branchID string, input services.UpsertBranchInput) (*models.Branch, error) {
	args := m.Called(ctx, businessID, branchID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Branch), args.Error(1)
}

func (m *MockCommerceService) DeleteBranch(ctx context.Context, businessID, branchID string) error {
	args := m.Called(ctx, businessID, branchID)
	return args.Error(0)
}

func (m *MockCommerceService) ListDriveAssets(ctx context.Context, businessID string) ([]*models.DriveAsset, int64, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, 0, args.Error(2)
	}
	return args.Get(0).([]*models.DriveAsset), args.Get(1).(int64), args.Error(2)
}

func (m *MockCommerceService) CreateDriveUpload(ctx context.Context, businessID, userID string, input services.CreateDriveAssetInput) (*services.DriveUploadSession, error) {
	args := m.Called(ctx, businessID, userID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.DriveUploadSession), args.Error(1)
}

func (m *MockCommerceService) DeleteDriveAsset(ctx context.Context, businessID, assetID string) error {
	args := m.Called(ctx, businessID, assetID)
	return args.Error(0)
}

func (m *MockCommerceService) GetWhatsAppConfig(ctx context.Context, businessID string) (*models.WhatsAppConfig, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.WhatsAppConfig), args.Error(1)
}

func (m *MockCommerceService) UpsertWhatsAppConfig(ctx context.Context, businessID string, input services.UpsertWhatsAppConfigInput) (*models.WhatsAppConfig, error) {
	args := m.Called(ctx, businessID, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.WhatsAppConfig), args.Error(1)
}

func (m *MockCommerceService) ListNotificationDeliveries(ctx context.Context, businessID string, limit int) ([]*models.NotificationDelivery, error) {
	args := m.Called(ctx, businessID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.NotificationDelivery), args.Error(1)
}

// =============================================================================
// Test Commerce Handler Wrapper
// =============================================================================

type TestableCommerceHandler struct {
	svc *MockCommerceService
	log *logger.Logger
}

func NewTestableCommerceHandler(svc *MockCommerceService, log *logger.Logger) *TestableCommerceHandler {
	return &TestableCommerceHandler{svc: svc, log: log}
}

func (h *TestableCommerceHandler) ListStorefronts(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	storefronts, err := h.svc.ListStorefronts(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, storefronts)
}

func (h *TestableCommerceHandler) CreateStorefront(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertStorefrontInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	storefront, err := h.svc.CreateStorefront(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, storefront)
}

func (h *TestableCommerceHandler) GetStorefront(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	storefront, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, storefront)
}

func (h *TestableCommerceHandler) UpdateStorefrontSettings(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertStorefrontInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	storefront, err := h.svc.UpdateStorefrontSettings(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, storefront)
}

func (h *TestableCommerceHandler) ListStorefrontProducts(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	items, err := h.svc.ListStorefrontProducts(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *TestableCommerceHandler) ReplaceStorefrontProducts(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	var input []services.UpsertStorefrontProductInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, err := h.svc.ReplaceStorefrontProducts(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *TestableCommerceHandler) ListStorefrontCoupons(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	coupons, err := h.svc.ListStorefrontCoupons(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, coupons)
}

func (h *TestableCommerceHandler) CreateStorefrontCoupon(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	var input services.UpsertStorefrontCouponInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	coupon, err := h.svc.CreateStorefrontCoupon(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, coupon)
}

func (h *TestableCommerceHandler) UpdateStorefrontCoupon(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	var input services.UpsertStorefrontCouponInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	coupon, err := h.svc.UpdateStorefrontCoupon(c.Request.Context(), c.Param("id"), c.Param("coupon_id"), input)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, coupon)
}

func (h *TestableCommerceHandler) ListStorefrontOrders(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page := 1
	limit := 20
	orders, total, err := h.svc.ListStorefrontOrders(c.Request.Context(), businessID, c.Param("id"), c.Query("status"), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": orders, "total": total})
}

func (h *TestableCommerceHandler) ApproveStorefrontOrder(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	order, err := h.svc.ApproveStoreOrder(c.Request.Context(), businessID, c.Param("id"), c.Param("order_id"))
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, order)
}

func (h *TestableCommerceHandler) CancelStorefrontOrder(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&payload)
	order, err := h.svc.CancelStoreOrder(c.Request.Context(), businessID, c.Param("id"), c.Param("order_id"), payload.Reason)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, order)
}

func (h *TestableCommerceHandler) PublicCatalog(c *gin.Context) {
	catalog, err := h.svc.GetCatalog(c.Request.Context(), c.Param("slug"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog)
}

func (h *TestableCommerceHandler) PublicValidateCoupon(c *gin.Context) {
	var input services.ValidateCouponInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.ValidateCoupon(c.Request.Context(), c.Param("slug"), input)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TestableCommerceHandler) PublicCategories(c *gin.Context) {
	catalog, err := h.svc.GetCatalog(c.Request.Context(), c.Param("slug"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog.Categories)
}

func (h *TestableCommerceHandler) PublicCheckout(c *gin.Context) {
	var input services.StorefrontCheckoutInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.Checkout(c.Request.Context(), c.Param("slug"), "test-idempotency-key", input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *TestableCommerceHandler) PublicOrder(c *gin.Context) {
	order, err := h.svc.GetPublicOrder(c.Request.Context(), c.Param("slug"), c.Param("token"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, order)
}

func (h *TestableCommerceHandler) ListEntitlements(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	entitlements, err := h.svc.ListFeatureEntitlements(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entitlements)
}

func (h *TestableCommerceHandler) SyncEntitlements(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	entitlements, err := h.svc.SyncFeatureEntitlements(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entitlements)
}

func (h *TestableCommerceHandler) ListRoles(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	roles, err := h.svc.ListRoles(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, roles)
}

func (h *TestableCommerceHandler) CreateRole(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertRoleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	role, err := h.svc.CreateRole(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, role)
}

func (h *TestableCommerceHandler) UpdateRole(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertRoleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role, err := h.svc.UpdateRole(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, role)
}

func (h *TestableCommerceHandler) DeleteRole(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteRole(c.Request.Context(), businessID, c.Param("id")); err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *TestableCommerceHandler) ListBranches(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	branches, err := h.svc.ListBranches(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, branches)
}

func (h *TestableCommerceHandler) CreateBranch(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertBranchInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	branch, err := h.svc.CreateBranch(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, branch)
}

func (h *TestableCommerceHandler) UpdateBranch(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertBranchInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	branch, err := h.svc.UpdateBranch(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, branch)
}

func (h *TestableCommerceHandler) DeleteBranch(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteBranch(c.Request.Context(), businessID, c.Param("id")); err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *TestableCommerceHandler) ListDriveAssets(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	assets, usage, err := h.svc.ListDriveAssets(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": assets, "usage_bytes": usage})
}

func (h *TestableCommerceHandler) CreateDriveUpload(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.CreateDriveAssetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input.BusinessID = businessID
	session, err := h.svc.CreateDriveUpload(c.Request.Context(), businessID, userID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, session)
}

func (h *TestableCommerceHandler) DeleteDriveAsset(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteDriveAsset(c.Request.Context(), businessID, c.Param("id")); err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *TestableCommerceHandler) GetWhatsAppConfig(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	config, err := h.svc.GetWhatsAppConfig(c.Request.Context(), businessID)
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, config)
}

func (h *TestableCommerceHandler) UpsertWhatsAppConfig(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertWhatsAppConfigInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config, err := h.svc.UpsertWhatsAppConfig(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, config)
}

func (h *TestableCommerceHandler) ListNotificationDeliveries(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	deliveries, err := h.svc.ListNotificationDeliveries(c.Request.Context(), businessID, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, deliveries)
}

func requestContextWithActor(c *gin.Context) {
	// No-op for testing
}

func requireBusinessScope(c *gin.Context) (string, bool) {
	businessID, exists := c.Get("business_id")
	if !exists || businessID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return "", false
	}
	return businessID.(string), true
}

func requireUserScope(c *gin.Context) (string, bool) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user scope required"})
		return "", false
	}
	return userID.(string), true
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

// =============================================================================
// ListStorefronts Tests
// =============================================================================

func TestListStorefronts_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefronts := []*models.Storefront{
		{ID: "sf-1", Name: "Store 1", Slug: "store-1", Status: "active", BusinessID: "biz-123"},
		{ID: "sf-2", Name: "Store 2", Slug: "store-2", Status: "draft", BusinessID: "biz-123"},
	}
	mockSvc.On("ListStorefronts", mock.Anything, "biz-123").Return(storefronts, nil)

	router := gin.New()
	router.GET("/storefronts", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefronts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListStorefronts_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/storefronts", func(c *gin.Context) {
		// No business_id set
		handler.ListStorefronts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// =============================================================================
// CreateStorefront Tests
// =============================================================================

func TestCreateStorefront_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{
		ID:         "sf-123",
		Name:       "My Store",
		Slug:       "my-store",
		Status:     "draft",
		BusinessID: "biz-123",
	}
	mockSvc.On("CreateStorefront", mock.Anything, mock.MatchedBy(func(input services.UpsertStorefrontInput) bool {
		return input.Name == "My Store" && input.BusinessID == "biz-123"
	})).Return(storefront, nil)

	router := gin.New()
	router.POST("/storefronts", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateStorefront(c)
	})

	reqBody := map[string]interface{}{
		"name": "My Store",
		"slug": "my-store",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateStorefront_BadRequest(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.POST("/storefronts", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateStorefront(c)
	})

	// Missing required "name" field
	reqBody := map[string]interface{}{
		"slug": "my-store",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// GetStorefront Tests
// =============================================================================

func TestGetStorefront_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", Name: "My Store", Slug: "my-store", BusinessID: "biz-123"}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)

	router := gin.New()
	router.GET("/storefronts/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.GetStorefront(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetStorefront_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "non-existent").Return((*models.Storefront)(nil), errors.New("record not found"))

	router := gin.New()
	router.GET("/storefronts/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.GetStorefront(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/non-existent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// UpdateStorefrontSettings Tests
// =============================================================================

func TestUpdateStorefrontSettings_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", Name: "Updated Store", Status: "active", BusinessID: "biz-123"}
	mockSvc.On("UpdateStorefrontSettings", mock.Anything, "biz-123", "sf-123", mock.Anything).Return(storefront, nil)

	router := gin.New()
	router.PUT("/storefronts/:id/settings", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateStorefrontSettings(c)
	})

	reqBody := map[string]interface{}{
		"name":   "Updated Store",
		"status": "active",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/settings", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListStorefrontProducts Tests
// =============================================================================

func TestListStorefrontProducts_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	products := []*models.StorefrontProduct{
		{ID: "sp-1", StorefrontID: "sf-123", ProductID: "prod-1", DisplayPrice: 100.00},
	}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("ListStorefrontProducts", mock.Anything, "biz-123", "sf-123").Return(products, nil)

	router := gin.New()
	router.GET("/storefronts/:id/products", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontProducts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/products", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListStorefrontProducts_StorefrontNotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "non-existent").Return((*models.Storefront)(nil), errors.New("record not found"))

	router := gin.New()
	router.GET("/storefronts/:id/products", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontProducts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/non-existent/products", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ReplaceStorefrontProducts Tests
// =============================================================================

func TestReplaceStorefrontProducts_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	products := []*models.StorefrontProduct{
		{ID: "sp-1", StorefrontID: "sf-123", ProductID: "prod-1"},
	}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("ReplaceStorefrontProducts", mock.Anything, "biz-123", "sf-123", mock.Anything).Return(products, nil)

	router := gin.New()
	router.PUT("/storefronts/:id/products", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ReplaceStorefrontProducts(c)
	})

	reqBody := []map[string]interface{}{
		{"product_id": "11111111-1111-1111-1111-111111111111", "display_price": 100.00},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/products", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListStorefrontCoupons Tests
// =============================================================================

func TestListStorefrontCoupons_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	coupons := []*models.StorefrontCoupon{
		{ID: "cp-1", StorefrontID: "sf-123", Code: "SAVE10", DiscountType: "percentage", DiscountValue: 10},
	}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("ListStorefrontCoupons", mock.Anything, "sf-123").Return(coupons, nil)

	router := gin.New()
	router.GET("/storefronts/:id/coupons", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontCoupons(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/coupons", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// CreateStorefrontCoupon Tests
// =============================================================================

func TestCreateStorefrontCoupon_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	coupon := &models.StorefrontCoupon{ID: "cp-1", StorefrontID: "sf-123", Code: "SAVE20", DiscountType: "percentage", DiscountValue: 20}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("CreateStorefrontCoupon", mock.Anything, "sf-123", mock.MatchedBy(func(input services.UpsertStorefrontCouponInput) bool {
		return input.Code == "SAVE20" && input.DiscountType == "percentage"
	})).Return(coupon, nil)

	router := gin.New()
	router.POST("/storefronts/:id/coupons", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateStorefrontCoupon(c)
	})

	reqBody := map[string]interface{}{
		"code":           "SAVE20",
		"discount_type":  "percentage",
		"discount_value": 20,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/coupons", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// UpdateStorefrontCoupon Tests
// =============================================================================

func TestUpdateStorefrontCoupon_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	coupon := &models.StorefrontCoupon{ID: "cp-1", StorefrontID: "sf-123", Code: "SAVE30", DiscountType: "fixed", DiscountValue: 30}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("UpdateStorefrontCoupon", mock.Anything, "sf-123", "cp-1", mock.Anything).Return(coupon, nil)

	router := gin.New()
	router.PUT("/storefronts/:id/coupons/:coupon_id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateStorefrontCoupon(c)
	})

	reqBody := map[string]interface{}{
		"code":           "SAVE30",
		"discount_type":  "fixed",
		"discount_value": 30,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/coupons/cp-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListStorefrontOrders Tests
// =============================================================================

func TestListStorefrontOrders_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	orders := []*models.StoreOrder{
		{ID: "ord-1", StorefrontID: "sf-123", Status: "pending", Total: 500.00},
	}
	mockSvc.On("ListStorefrontOrders", mock.Anything, "biz-123", "sf-123", "", 1, 20).Return(orders, int64(1), nil)

	router := gin.New()
	router.GET("/storefronts/:id/orders", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontOrders(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/orders", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListStorefrontOrders_WithStatusFilter(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	orders := []*models.StoreOrder{
		{ID: "ord-1", StorefrontID: "sf-123", Status: "completed", Total: 500.00},
	}
	mockSvc.On("ListStorefrontOrders", mock.Anything, "biz-123", "sf-123", "completed", 1, 20).Return(orders, int64(1), nil)

	router := gin.New()
	router.GET("/storefronts/:id/orders", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontOrders(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/orders?status=completed", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ApproveStorefrontOrder Tests
// =============================================================================

func TestApproveStorefrontOrder_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	order := &models.StoreOrder{ID: "ord-1", StorefrontID: "sf-123", Status: "approved", Total: 500.00}
	mockSvc.On("ApproveStoreOrder", mock.Anything, "biz-123", "sf-123", "ord-1").Return(order, nil)

	router := gin.New()
	router.POST("/storefronts/:id/orders/:order_id/approve", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ApproveStorefrontOrder(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/orders/ord-1/approve", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestApproveStorefrontOrder_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("ApproveStoreOrder", mock.Anything, "biz-123", "sf-123", "non-existent").Return((*models.StoreOrder)(nil), errors.New("order not found"))

	router := gin.New()
	router.POST("/storefronts/:id/orders/:order_id/approve", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ApproveStorefrontOrder(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/orders/non-existent/approve", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// CancelStorefrontOrder Tests
// =============================================================================

func TestCancelStorefrontOrder_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	order := &models.StoreOrder{ID: "ord-1", StorefrontID: "sf-123", Status: "cancelled", Total: 500.00}
	mockSvc.On("CancelStoreOrder", mock.Anything, "biz-123", "sf-123", "ord-1", "Customer request").Return(order, nil)

	router := gin.New()
	router.POST("/storefronts/:id/orders/:order_id/cancel", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CancelStorefrontOrder(c)
	})

	reqBody := map[string]interface{}{
		"reason": "Customer request",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/orders/ord-1/cancel", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Additional Storefront Tests - Error/Edge Cases
// =============================================================================

// ListStorefronts - Service Error
func TestListStorefronts_ServiceError(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("ListStorefronts", mock.Anything, "biz-123").Return(nil, errors.New("database error"))

	router := gin.New()
	router.GET("/storefronts", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefronts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// CreateStorefront - Unauthorized
func TestCreateStorefront_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.POST("/storefronts", func(c *gin.Context) {
		handler.CreateStorefront(c)
	})

	reqBody := map[string]interface{}{"name": "My Store", "slug": "my-store"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// GetStorefront - Unauthorized
func TestGetStorefront_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/storefronts/:id", func(c *gin.Context) {
		handler.GetStorefront(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// UpdateStorefrontSettings - Unauthorized
func TestUpdateStorefrontSettings_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.PUT("/storefronts/:id/settings", func(c *gin.Context) {
		handler.UpdateStorefrontSettings(c)
	})

	reqBody := map[string]interface{}{"name": "Updated Store"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/settings", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// UpdateStorefrontSettings - Storefront Not Found
func TestUpdateStorefrontSettings_StorefrontNotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("UpdateStorefrontSettings", mock.Anything, "biz-123", "non-existent", mock.Anything).Return(nil, errors.New("storefront not found"))

	router := gin.New()
	router.PUT("/storefronts/:id/settings", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateStorefrontSettings(c)
	})

	reqBody := map[string]interface{}{"name": "Updated"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/non-existent/settings", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// ListStorefrontProducts - Unauthorized
func TestListStorefrontProducts_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/storefronts/:id/products", func(c *gin.Context) {
		handler.ListStorefrontProducts(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/products", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// ReplaceStorefrontProducts - Unauthorized
func TestReplaceStorefrontProducts_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.PUT("/storefronts/:id/products", func(c *gin.Context) {
		handler.ReplaceStorefrontProducts(c)
	})

	reqBody := []map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/products", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// ReplaceStorefrontProducts - Storefront Not Found
func TestReplaceStorefrontProducts_StorefrontNotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "non-existent").Return(nil, errors.New("storefront not found"))

	router := gin.New()
	router.PUT("/storefronts/:id/products", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ReplaceStorefrontProducts(c)
	})

	reqBody := []map[string]interface{}{{"product_id": "11111111-1111-1111-1111-111111111111"}}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/non-existent/products", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// ReplaceStorefrontProducts - Bad Request
func TestReplaceStorefrontProducts_BadRequest(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)

	router := gin.New()
	router.PUT("/storefronts/:id/products", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ReplaceStorefrontProducts(c)
	})

	// Send invalid JSON to trigger binding error
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/products", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// ReplaceStorefrontProducts - Service Error
func TestReplaceStorefrontProducts_ServiceError(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("ReplaceStorefrontProducts", mock.Anything, "biz-123", "sf-123", mock.Anything).Return(nil, errors.New("database error"))

	router := gin.New()
	router.PUT("/storefronts/:id/products", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ReplaceStorefrontProducts(c)
	})

	reqBody := []map[string]interface{}{{"product_id": "11111111-1111-1111-1111-111111111111"}}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/products", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// ListStorefrontCoupons - Unauthorized
func TestListStorefrontCoupons_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/storefronts/:id/coupons", func(c *gin.Context) {
		handler.ListStorefrontCoupons(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/coupons", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// ListStorefrontCoupons - Storefront Not Found
func TestListStorefrontCoupons_StorefrontNotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "non-existent").Return(nil, errors.New("storefront not found"))

	router := gin.New()
	router.GET("/storefronts/:id/coupons", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListStorefrontCoupons(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/non-existent/coupons", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// CreateStorefrontCoupon - Unauthorized
func TestCreateStorefrontCoupon_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.POST("/storefronts/:id/coupons", func(c *gin.Context) {
		handler.CreateStorefrontCoupon(c)
	})

	reqBody := map[string]interface{}{"code": "SAVE20", "discount_type": "percentage", "discount_value": 20}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/coupons", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// CreateStorefrontCoupon - Storefront Not Found
func TestCreateStorefrontCoupon_StorefrontNotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "non-existent").Return(nil, errors.New("storefront not found"))

	router := gin.New()
	router.POST("/storefronts/:id/coupons", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateStorefrontCoupon(c)
	})

	reqBody := map[string]interface{}{"code": "SAVE20", "discount_type": "percentage", "discount_value": 20}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts/non-existent/coupons", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// CreateStorefrontCoupon - Bad Request
func TestCreateStorefrontCoupon_BadRequest(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)

	router := gin.New()
	router.POST("/storefronts/:id/coupons", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateStorefrontCoupon(c)
	})

	// Missing required fields
	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/coupons", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

// UpdateStorefrontCoupon - Unauthorized
func TestUpdateStorefrontCoupon_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.PUT("/storefronts/:id/coupons/:coupon_id", func(c *gin.Context) {
		handler.UpdateStorefrontCoupon(c)
	})

	reqBody := map[string]interface{}{"code": "SAVE30", "discount_type": "fixed", "discount_value": 30}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/coupons/cp-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// UpdateStorefrontCoupon - Storefront Not Found
func TestUpdateStorefrontCoupon_StorefrontNotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "non-existent").Return(nil, errors.New("storefront not found"))

	router := gin.New()
	router.PUT("/storefronts/:id/coupons/:coupon_id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateStorefrontCoupon(c)
	})

	reqBody := map[string]interface{}{"code": "SAVE30", "discount_type": "fixed", "discount_value": 30}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/non-existent/coupons/cp-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// UpdateStorefrontCoupon - Coupon Not Found
func TestUpdateStorefrontCoupon_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	storefront := &models.Storefront{ID: "sf-123", BusinessID: "biz-123"}
	mockSvc.On("GetStorefront", mock.Anything, "biz-123", "sf-123").Return(storefront, nil)
	mockSvc.On("UpdateStorefrontCoupon", mock.Anything, "sf-123", "non-existent", mock.Anything).Return(nil, errors.New("coupon not found"))

	router := gin.New()
	router.PUT("/storefronts/:id/coupons/:coupon_id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateStorefrontCoupon(c)
	})

	reqBody := map[string]interface{}{"code": "SAVE30", "discount_type": "percentage", "discount_value": 30}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/storefronts/sf-123/coupons/non-existent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// ListStorefrontOrders - Unauthorized
func TestListStorefrontOrders_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/storefronts/:id/orders", func(c *gin.Context) {
		handler.ListStorefrontOrders(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/storefronts/sf-123/orders", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// ApproveStorefrontOrder - Unauthorized
func TestApproveStorefrontOrder_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.POST("/storefronts/:id/orders/:order_id/approve", func(c *gin.Context) {
		handler.ApproveStorefrontOrder(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/orders/ord-1/approve", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// CancelStorefrontOrder - Unauthorized
func TestCancelStorefrontOrder_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.POST("/storefronts/:id/orders/:order_id/cancel", func(c *gin.Context) {
		handler.CancelStorefrontOrder(c)
	})

	reqBody := map[string]interface{}{"reason": "Customer request"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/orders/ord-1/cancel", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// CancelStorefrontOrder - Not Found
func TestCancelStorefrontOrder_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("CancelStoreOrder", mock.Anything, "biz-123", "sf-123", "non-existent", "Customer request").Return(nil, errors.New("order not found"))

	router := gin.New()
	router.POST("/storefronts/:id/orders/:order_id/cancel", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CancelStorefrontOrder(c)
	})

	reqBody := map[string]interface{}{"reason": "Customer request"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/storefronts/sf-123/orders/non-existent/cancel", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListEntitlements Tests
// =============================================================================

func TestListEntitlements_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	entitlements := []*models.FeatureEntitlement{
		{ID: "ent-1", BusinessID: "biz-123", FeatureKey: "online_store", Enabled: true},
		{ID: "ent-2", BusinessID: "biz-123", FeatureKey: "multi_currency", Enabled: false},
	}
	mockSvc.On("ListFeatureEntitlements", mock.Anything, "biz-123").Return(entitlements, nil)

	router := gin.New()
	router.GET("/subscriptions/entitlements", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListEntitlements(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/subscriptions/entitlements", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListEntitlements_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/subscriptions/entitlements", func(c *gin.Context) {
		handler.ListEntitlements(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/subscriptions/entitlements", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

func TestListEntitlements_InternalError(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("ListFeatureEntitlements", mock.Anything, "biz-123").Return(nil, errors.New("database error"))

	router := gin.New()
	router.GET("/subscriptions/entitlements", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListEntitlements(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/subscriptions/entitlements", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// SyncEntitlements Tests
// =============================================================================

func TestSyncEntitlements_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	entitlements := []*models.FeatureEntitlement{
		{ID: "ent-1", BusinessID: "biz-123", FeatureKey: "online_store", Enabled: true},
	}
	mockSvc.On("SyncFeatureEntitlements", mock.Anything, "biz-123").Return(entitlements, nil)

	router := gin.New()
	router.POST("/subscriptions/entitlements/sync", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.SyncEntitlements(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/subscriptions/entitlements/sync", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestSyncEntitlements_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.POST("/subscriptions/entitlements/sync", func(c *gin.Context) {
		handler.SyncEntitlements(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/subscriptions/entitlements/sync", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// =============================================================================
// ListRoles Tests
// =============================================================================

func TestListRoles_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	roles := []*models.Role{
		{ID: "role-1", BusinessID: "biz-123", Name: "Admin", Key: "admin"},
		{ID: "role-2", BusinessID: "biz-123", Name: "Viewer", Key: "viewer"},
	}
	mockSvc.On("ListRoles", mock.Anything, "biz-123").Return(roles, nil)

	router := gin.New()
	router.GET("/roles", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListRoles(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/roles", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListRoles_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/roles", func(c *gin.Context) {
		handler.ListRoles(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/roles", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// =============================================================================
// CreateRole Tests
// =============================================================================

func TestCreateRole_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	role := &models.Role{ID: "role-1", BusinessID: "biz-123", Name: "Manager", Key: "manager"}
	mockSvc.On("CreateRole", mock.Anything, mock.MatchedBy(func(input services.UpsertRoleInput) bool {
		return input.Name == "Manager" && input.BusinessID == "biz-123"
	})).Return(role, nil)

	router := gin.New()
	router.POST("/roles", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateRole(c)
	})

	reqBody := map[string]interface{}{
		"name":        "Manager",
		"key":         "manager",
		"permissions": []string{"read", "write"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/roles", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateRole_BadRequest(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.POST("/roles", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateRole(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/roles", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// UpdateRole Tests
// =============================================================================

func TestUpdateRole_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	role := &models.Role{ID: "role-1", BusinessID: "biz-123", Name: "Updated Manager"}
	mockSvc.On("UpdateRole", mock.Anything, "biz-123", "role-1", mock.Anything).Return(role, nil)

	router := gin.New()
	router.PUT("/roles/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateRole(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Manager",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/roles/role-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestUpdateRole_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("UpdateRole", mock.Anything, "biz-123", "non-existent", mock.Anything).Return(nil, errors.New("role not found"))

	router := gin.New()
	router.PUT("/roles/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateRole(c)
	})

	reqBody := map[string]interface{}{"name": "Updated"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/roles/non-existent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DeleteRole Tests
// =============================================================================

func TestDeleteRole_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("DeleteRole", mock.Anything, "biz-123", "role-1").Return(nil)

	router := gin.New()
	router.DELETE("/roles/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.DeleteRole(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/roles/role-1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestDeleteRole_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("DeleteRole", mock.Anything, "biz-123", "non-existent").Return(errors.New("role not found"))

	router := gin.New()
	router.DELETE("/roles/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.DeleteRole(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/roles/non-existent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListBranches Tests
// =============================================================================

func TestListBranches_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	branches := []*models.Branch{
		{ID: "branch-1", BusinessID: "biz-123", Name: "Main Branch", Code: "MAIN"},
	}
	mockSvc.On("ListBranches", mock.Anything, "biz-123").Return(branches, nil)

	router := gin.New()
	router.GET("/branches", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListBranches(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/branches", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListBranches_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/branches", func(c *gin.Context) {
		handler.ListBranches(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/branches", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// =============================================================================
// CreateBranch Tests
// =============================================================================

func TestCreateBranch_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	branch := &models.Branch{ID: "branch-1", BusinessID: "biz-123", Name: "New Branch", Code: "NEW"}
	mockSvc.On("CreateBranch", mock.Anything, mock.MatchedBy(func(input services.UpsertBranchInput) bool {
		return input.Name == "New Branch" && input.Code == "NEW" && input.BusinessID == "biz-123"
	})).Return(branch, nil)

	router := gin.New()
	router.POST("/branches", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateBranch(c)
	})

	reqBody := map[string]interface{}{
		"name": "New Branch",
		"code": "NEW",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateBranch_BadRequest(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.POST("/branches", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateBranch(c)
	})

	reqBody := map[string]interface{}{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/branches", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// UpdateBranch Tests
// =============================================================================

func TestUpdateBranch_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	branch := &models.Branch{ID: "branch-1", BusinessID: "biz-123", Name: "Updated Branch"}
	mockSvc.On("UpdateBranch", mock.Anything, "biz-123", "branch-1", mock.Anything).Return(branch, nil)

	router := gin.New()
	router.PUT("/branches/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateBranch(c)
	})

	reqBody := map[string]interface{}{
		"name": "Updated Branch",
		"code": "UPD",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/branches/branch-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestUpdateBranch_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("UpdateBranch", mock.Anything, "biz-123", "non-existent", mock.Anything).Return(nil, errors.New("branch not found"))

	router := gin.New()
	router.PUT("/branches/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpdateBranch(c)
	})

	reqBody := map[string]interface{}{"name": "Updated", "code": "UPD"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/branches/non-existent", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DeleteBranch Tests
// =============================================================================

func TestDeleteBranch_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("DeleteBranch", mock.Anything, "biz-123", "branch-1").Return(nil)

	router := gin.New()
	router.DELETE("/branches/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.DeleteBranch(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/branches/branch-1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListDriveAssets Tests
// =============================================================================

func TestListDriveAssets_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	assets := []*models.DriveAsset{
		{ID: "asset-1", BusinessID: "biz-123", Name: "document.pdf", SizeBytes: 1024},
	}
	mockSvc.On("ListDriveAssets", mock.Anything, "biz-123").Return(assets, int64(1024), nil)

	router := gin.New()
	router.GET("/drive", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListDriveAssets(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/drive", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListDriveAssets_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/drive", func(c *gin.Context) {
		handler.ListDriveAssets(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/drive", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}

// =============================================================================
// CreateDriveUpload Tests
// =============================================================================

func TestCreateDriveUpload_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	session := &services.DriveUploadSession{
		Asset:     &models.DriveAsset{ID: "asset-1", BusinessID: "biz-123", Name: "new-file.pdf"},
		UploadURL: "https://storage.example.com/presigned-url",
	}
	mockSvc.On("CreateDriveUpload", mock.Anything, "biz-123", "user-1", mock.Anything).Return(session, nil)

	router := gin.New()
	router.POST("/drive/presign", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		c.Set("user_id", "user-1")
		handler.CreateDriveUpload(c)
	})

	reqBody := map[string]interface{}{
		"name":         "new-file.pdf",
		"content_type": "application/pdf",
		"size_bytes":   2048,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/drive/presign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestCreateDriveUpload_UnauthorizedUser(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.POST("/drive/presign", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.CreateDriveUpload(c)
	})

	reqBody := map[string]interface{}{"name": "file.pdf", "content_type": "application/pdf", "size_bytes": 100}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/drive/presign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

// =============================================================================
// DeleteDriveAsset Tests
// =============================================================================

func TestDeleteDriveAsset_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("DeleteDriveAsset", mock.Anything, "biz-123", "asset-1").Return(nil)

	router := gin.New()
	router.DELETE("/drive/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.DeleteDriveAsset(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/drive/asset-1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

func TestDeleteDriveAsset_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("DeleteDriveAsset", mock.Anything, "biz-123", "non-existent").Return(errors.New("asset not found"))

	router := gin.New()
	router.DELETE("/drive/:id", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.DeleteDriveAsset(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/drive/non-existent", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetWhatsAppConfig Tests
// =============================================================================

func TestGetWhatsAppConfig_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	config := &models.WhatsAppConfig{ID: "wa-1", BusinessID: "biz-123", PhoneNumberID: "+1234567890", Enabled: true}
	mockSvc.On("GetWhatsAppConfig", mock.Anything, "biz-123").Return(config, nil)

	router := gin.New()
	router.GET("/whatsapp/config", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.GetWhatsAppConfig(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/whatsapp/config", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestGetWhatsAppConfig_NotFound(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	mockSvc.On("GetWhatsAppConfig", mock.Anything, "biz-123").Return(nil, errors.New("config not found"))

	router := gin.New()
	router.GET("/whatsapp/config", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.GetWhatsAppConfig(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/whatsapp/config", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}

	mockSvc.AssertExpectations(t)
}

// =============================================================================
// UpsertWhatsAppConfig Tests
// =============================================================================

func TestUpsertWhatsAppConfig_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	config := &models.WhatsAppConfig{ID: "wa-1", BusinessID: "biz-123", PhoneNumberID: "+1234567890", Enabled: true}
	mockSvc.On("UpsertWhatsAppConfig", mock.Anything, "biz-123", mock.Anything).Return(config, nil)

	router := gin.New()
	router.PUT("/whatsapp/config", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpsertWhatsAppConfig(c)
	})

	reqBody := map[string]interface{}{
		"phone_number": "+1234567890",
		"status":       "active",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/whatsapp/config", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestUpsertWhatsAppConfig_BadRequest(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.PUT("/whatsapp/config", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.UpsertWhatsAppConfig(c)
	})

	req := httptest.NewRequest(http.MethodPut, "/whatsapp/config", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

// =============================================================================
// ListNotificationDeliveries Tests
// =============================================================================

func TestListNotificationDeliveries_Success(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	deliveries := []*models.NotificationDelivery{
		{ID: "del-1", BusinessID: "biz-123", Channel: "whatsapp", Status: "delivered"},
	}
	mockSvc.On("ListNotificationDeliveries", mock.Anything, "biz-123", 50).Return(deliveries, nil)

	router := gin.New()
	router.GET("/whatsapp/deliveries", func(c *gin.Context) {
		c.Set("business_id", "biz-123")
		handler.ListNotificationDeliveries(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/whatsapp/deliveries", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	mockSvc.AssertExpectations(t)
}

func TestListNotificationDeliveries_Unauthorized(t *testing.T) {
	mockSvc := new(MockCommerceService)
	log := logger.New()
	handler := NewTestableCommerceHandler(mockSvc, log)

	router := gin.New()
	router.GET("/whatsapp/deliveries", func(c *gin.Context) {
		handler.ListNotificationDeliveries(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/whatsapp/deliveries", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.Code)
	}
}
