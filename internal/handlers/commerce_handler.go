package handlers

import (
	"io"
	"net/http"
	"strconv"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type CommerceHandler struct {
	svc *services.CommerceService
	log *logger.Logger
}

func NewCommerceHandler(svc *services.CommerceService, log *logger.Logger) *CommerceHandler {
	return &CommerceHandler{svc: svc, log: log}
}

func (h *CommerceHandler) ListEntitlements(c *gin.Context) {
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

func (h *CommerceHandler) SyncEntitlements(c *gin.Context) {
	requestContextWithActor(c)
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

func (h *CommerceHandler) ListRoles(c *gin.Context) {
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

func (h *CommerceHandler) CreateRole(c *gin.Context) {
	requestContextWithActor(c)
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

func (h *CommerceHandler) UpdateRole(c *gin.Context) {
	requestContextWithActor(c)
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

func (h *CommerceHandler) DeleteRole(c *gin.Context) {
	requestContextWithActor(c)
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

func (h *CommerceHandler) ListBranches(c *gin.Context) {
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

func (h *CommerceHandler) CreateBranch(c *gin.Context) {
	requestContextWithActor(c)
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

func (h *CommerceHandler) UpdateBranch(c *gin.Context) {
	requestContextWithActor(c)
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

func (h *CommerceHandler) DeleteBranch(c *gin.Context) {
	requestContextWithActor(c)
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

func (h *CommerceHandler) ListStorefronts(c *gin.Context) {
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

func (h *CommerceHandler) CreateStorefront(c *gin.Context) {
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

func (h *CommerceHandler) GetStorefront(c *gin.Context) {
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

func (h *CommerceHandler) UpdateStorefrontSettings(c *gin.Context) {
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

func (h *CommerceHandler) ListStorefrontProducts(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	items, err := h.svc.ListStorefrontProducts(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *CommerceHandler) ReplaceStorefrontProducts(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
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

func (h *CommerceHandler) ListStorefrontCoupons(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	coupons, err := h.svc.ListStorefrontCoupons(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, coupons)
}

func (h *CommerceHandler) CreateStorefrontCoupon(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
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

func (h *CommerceHandler) UpdateStorefrontCoupon(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if _, err := h.svc.GetStorefront(c.Request.Context(), businessID, c.Param("id")); err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
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

func (h *CommerceHandler) ListStorefrontOrders(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	orders, total, err := h.svc.ListStorefrontOrders(c.Request.Context(), businessID, c.Param("id"), c.Query("status"), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": orders, "total": total})
}

func (h *CommerceHandler) ApproveStorefrontOrder(c *gin.Context) {
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

func (h *CommerceHandler) CancelStorefrontOrder(c *gin.Context) {
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

func (h *CommerceHandler) ListDriveAssets(c *gin.Context) {
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

func (h *CommerceHandler) CreateDriveUpload(c *gin.Context) {
	requestContextWithActor(c)
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

func (h *CommerceHandler) DeleteDriveAsset(c *gin.Context) {
	requestContextWithActor(c)
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

func (h *CommerceHandler) GetWhatsAppConfig(c *gin.Context) {
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

func (h *CommerceHandler) UpsertWhatsAppConfig(c *gin.Context) {
	requestContextWithActor(c)
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

func (h *CommerceHandler) ListNotificationDeliveries(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	deliveries, err := h.svc.ListNotificationDeliveries(c.Request.Context(), businessID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, deliveries)
}

func (h *CommerceHandler) PublicCatalog(c *gin.Context) {
	catalog, err := h.svc.GetCatalog(c.Request.Context(), c.Param("slug"))
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog)
}

func (h *CommerceHandler) PublicCategories(c *gin.Context) {
	catalog, err := h.svc.GetCatalog(c.Request.Context(), c.Param("slug"))
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog.Categories)
}

func (h *CommerceHandler) PublicValidateCoupon(c *gin.Context) {
	var input services.ValidateCouponInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.ValidateCoupon(c.Request.Context(), c.Param("slug"), input)
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *CommerceHandler) PublicCheckout(c *gin.Context) {
	idempotencyKey, ok := requireIdempotencyKey(c)
	if !ok {
		return
	}
	var input services.StorefrontCheckoutInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.Checkout(c.Request.Context(), c.Param("slug"), idempotencyKey, input)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *CommerceHandler) PublicOrder(c *gin.Context) {
	order, err := h.svc.GetPublicOrder(c.Request.Context(), c.Param("slug"), c.Param("token"))
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, order)
}

func (h *CommerceHandler) PublicRazorpayWebhook(c *gin.Context) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}
	if err := h.svc.HandleRazorpayWebhook(c.Request.Context(), c.GetHeader("X-Razorpay-Signature"), rawBody); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
