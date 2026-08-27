package handlers

import (
	"context"
	"net/http"
	"strconv"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type publicOrderService interface {
	GetPublicOrder(ctx context.Context, slug, token string) (*models.StoreOrder, error)
}

type CommerceHandler struct {
	svc          *services.CommerceService
	publicOrders publicOrderService
	log          *logger.Logger
}

func NewCommerceHandler(svc *services.CommerceService, log *logger.Logger) *CommerceHandler {
	return &CommerceHandler{svc: svc, publicOrders: svc, log: log}
}

// ListEntitlements godoc
// @Summary List feature entitlements
// @Description Returns a list of all feature entitlements for a business
// @Tags Commerce
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /subscriptions/entitlements [get]
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

// SyncEntitlements godoc
// @Summary Sync feature entitlements
// @Description Syncs feature entitlements for a business from the subscription service
// @Tags Commerce
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /subscriptions/entitlements/sync [post]
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

// ListRoles godoc
// @Summary List roles
// @Description Returns a list of all roles for a business
// @Tags Commerce
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /roles [get]
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

// CreateRole godoc
// @Summary Create role
// @Description Creates a new role for a business
// @Tags Commerce
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param input body services.UpsertRoleInput true "Role input"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /roles [post]
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

// UpdateRole godoc
// @Summary Update role
// @Description Updates an existing role
// @Tags Commerce
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "Role ID"
// @Param input body services.UpsertRoleInput true "Role input"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /roles/{id} [put]
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

// DeleteRole godoc
// @Summary Delete role
// @Description Deletes an existing role
// @Tags Commerce
// @Security BearerAuth
// @Param id path string true "Role ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /roles/{id} [delete]
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

// ListBranches godoc
// @Summary List branches
// @Description Returns a list of all branches for a business
// @Tags Commerce
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /branches [get]
func (h *CommerceHandler) ListBranches(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	branchIDs, restricted := branchScopeFilter(c)
	if restricted && len(branchIDs) == 0 {
		c.JSON(http.StatusOK, []*models.Branch{})
		return
	}
	branches, err := h.svc.ListBranches(c.Request.Context(), businessID, branchIDs...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, branches)
}

// CreateBranch godoc
// @Summary Create branch
// @Description Creates a new branch for a business
// @Tags Commerce
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param input body services.UpsertBranchInput true "Branch input"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /branches [post]
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

// UpdateBranch godoc
// @Summary Update branch
// @Description Updates an existing branch
// @Tags Commerce
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "Branch ID"
// @Param input body services.UpsertBranchInput true "Branch input"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /branches/{id} [put]
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
	branch, err := h.svc.UpdateBranch(c.Request.Context(), businessID, branchRouteID(c), input)
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

// DeleteBranch godoc
// @Summary Delete branch
// @Description Deletes an existing branch
// @Tags Commerce
// @Security BearerAuth
// @Param id path string true "Branch ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /branches/{id} [delete]
func (h *CommerceHandler) DeleteBranch(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteBranch(c.Request.Context(), businessID, branchRouteID(c)); err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// ListStorefronts godoc
// @Summary List storefronts
// @Description Returns a list of all storefronts for a business
// @Tags Storefronts
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /storefronts [get]
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

// CreateStorefront godoc
// @Summary Create storefront
// @Description Creates a new storefront for a business
// @Tags Storefronts
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param input body services.UpsertStorefrontInput true "Storefront input"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /storefronts [post]
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

// GetStorefront godoc
// @Summary Get storefront
// @Description Returns the details of a specific storefront
// @Tags Storefronts
// @Security BearerAuth
// @Produce json
// @Param id path string true "Storefront ID"
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /storefronts/{id} [get]
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

// UpdateStorefrontSettings godoc
// @Summary Update storefront settings
// @Description Updates the settings of a specific storefront
// @Tags Storefronts
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "Storefront ID"
// @Param input body services.UpsertStorefrontInput true "Storefront settings"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /storefronts/{id}/settings [put]
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

// ListStorefrontProducts godoc
// @Summary List storefront products
// @Description Returns a list of all products for a specific storefront
// @Tags Storefronts
// @Security BearerAuth
// @Produce json
// @Param id path string true "Storefront ID"
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /storefronts/{id}/products [get]
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

// ReplaceStorefrontProducts godoc
// @Summary Replace storefront products
// @Description Replaces all products for a specific storefront
// @Tags Storefronts
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "Storefront ID"
// @Param input body []services.UpsertStorefrontProductInput true "Products input"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /storefronts/{id}/products [put]
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

// ListStorefrontCoupons godoc
// @Summary List storefront coupons
// @Description Returns a list of all coupons for a specific storefront
// @Tags Storefronts
// @Security BearerAuth
// @Produce json
// @Param id path string true "Storefront ID"
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /storefronts/{id}/coupons [get]
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

// CreateStorefrontCoupon godoc
// @Summary Create storefront coupon
// @Description Creates a new coupon for a specific storefront
// @Tags Storefronts
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "Storefront ID"
// @Param input body services.UpsertStorefrontCouponInput true "Coupon input"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /storefronts/{id}/coupons [post]
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

// UpdateStorefrontCoupon godoc
// @Summary Update storefront coupon
// @Description Updates an existing coupon for a specific storefront
// @Tags Storefronts
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "Storefront ID"
// @Param coupon_id path string true "Coupon ID"
// @Param input body services.UpsertStorefrontCouponInput true "Coupon input"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /storefronts/{id}/coupons/{coupon_id} [put]
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

// ListStorefrontOrders godoc
// @Summary List storefront orders
// @Description Returns a paginated list of orders for a specific storefront
// @Tags Storefronts
// @Security BearerAuth
// @Produce json
// @Param id path string true "Storefront ID"
// @Param status query string false "Filter by status"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /storefronts/{id}/orders [get]
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

// ApproveStorefrontOrder godoc
// @Summary Approve storefront order
// @Description Approves a pending order for a specific storefront
// @Tags Storefronts
// @Security BearerAuth
// @Produce json
// @Param id path string true "Storefront ID"
// @Param order_id path string true "Order ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /storefronts/{id}/orders/{order_id}/approve [post]
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

// CancelStorefrontOrder godoc
// @Summary Cancel storefront order
// @Description Cancels a pending order for a specific storefront
// @Tags Storefronts
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "Storefront ID"
// @Param order_id path string true "Order ID"
// @Param reason body string false "Cancellation reason"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /storefronts/{id}/orders/{order_id}/cancel [post]
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

// ListDriveAssets godoc
// @Summary List drive assets
// @Description Returns a list of all drive assets for a business with usage statistics
// @Tags Drive
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /drive [get]
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

// CreateDriveUpload godoc
// @Summary Create drive upload
// @Description Creates a presigned URL and exact required headers for uploading a drive asset up to 25 MiB
// @Tags Drive
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param input body services.CreateDriveAssetInput true "Drive asset input"
// @Success 201 {object} services.DriveUploadSession
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /drive/presign [post]
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

func (h *CommerceHandler) UpdateDriveAsset(c *gin.Context) {
	requestContextWithActor(c)
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpdateDriveAssetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	asset, err := h.svc.UpdateDriveAsset(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, asset)
}

// DeleteDriveAsset godoc
// @Summary Delete drive asset
// @Description Deletes a specific drive asset
// @Tags Drive
// @Security BearerAuth
// @Param id path string true "Drive asset ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /drive/{id} [delete]
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

// GetWhatsAppConfig godoc
// @Summary Get WhatsApp config
// @Description Returns the WhatsApp configuration for a business
// @Tags WhatsApp
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /whatsapp/config [get]
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

// UpsertWhatsAppConfig godoc
// @Summary Upsert WhatsApp config
// @Description Creates or updates the WhatsApp configuration for a business
// @Tags WhatsApp
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param input body services.UpsertWhatsAppConfigInput true "WhatsApp config input"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /whatsapp/config [put]
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

// ListNotificationDeliveries godoc
// @Summary List notification deliveries
// @Description Returns a list of notification deliveries for a business
// @Tags WhatsApp
// @Security BearerAuth
// @Produce json
// @Param limit query int false "Number of results" default(50)
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /whatsapp/deliveries [get]
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

// PublicCatalog godoc
// @Summary Get public catalog
// @Description Returns the public catalog for a storefront by slug
// @Tags Public Storefront
// @Produce json
// @Param slug path string true "Storefront slug"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /public/store/catalog/{slug} [get]
func (h *CommerceHandler) PublicCatalog(c *gin.Context) {
	catalog, err := h.svc.GetCatalog(c.Request.Context(), c.Param("slug"))
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		} else if err.Error() == "storefront is not accepting orders" {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog)
}

// PublicCategories godoc
// @Summary Get public categories
// @Description Returns the categories for a storefront by slug
// @Tags Public Storefront
// @Produce json
// @Param slug path string true "Storefront slug"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /public/store/categories/{slug} [get]
func (h *CommerceHandler) PublicCategories(c *gin.Context) {
	catalog, err := h.svc.GetCatalog(c.Request.Context(), c.Param("slug"))
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		} else if err.Error() == "storefront is not accepting orders" {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, catalog.Categories)
}

// PublicValidateCoupon godoc
// @Summary Validate coupon
// @Description Validates a coupon code for a storefront
// @Tags Public Storefront
// @Accept json
// @Produce json
// @Param slug path string true "Storefront slug"
// @Param input body services.ValidateCouponInput true "Coupon validation input"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /public/store/coupons/validate/{slug} [post]
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
		} else if err.Error() == "storefront is not accepting orders" {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// PublicCheckout godoc
// @Summary Public checkout
// @Description Processes checkout for a storefront order
// @Tags Public Storefront
// @Accept json
// @Produce json
// @Param slug path string true "Storefront slug"
// @Param input body services.StorefrontCheckoutInput true "Checkout input"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /public/store/checkout/{slug} [post]
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

// PublicOrder godoc
// @Summary Get public order
// @Description Returns order details using a public order token
// @Tags Public Storefront
// @Produce json
// @Param slug path string true "Storefront slug"
// @Param token path string true "Order token"
// @Success 200 {object} services.PublicStoreOrderResponse
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /public/store/{slug}/orders/{token} [get]
func (h *CommerceHandler) PublicOrder(c *gin.Context) {
	order, err := h.publicOrders.GetPublicOrder(c.Request.Context(), c.Param("slug"), c.Param("token"))
	if err != nil {
		status := http.StatusInternalServerError
		if isNotFoundErr(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, services.NewPublicStoreOrderResponse(order))
}

func branchRouteID(c *gin.Context) string {
	if id := c.Param("branch_id"); id != "" {
		return id
	}
	return c.Param("id")
}

func branchScopeFilter(c *gin.Context) ([]string, bool) {
	allBranches, branchIDs, ok := middleware.GetValidatedBranchScope(c)
	if !ok || allBranches {
		return nil, false
	}
	return branchIDs, true
}
