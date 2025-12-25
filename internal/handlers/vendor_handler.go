package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/cyfernova/invoice-backend/internal/middleware"
	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/internal/services"
)

// VendorHandler handles vendor management endpoints
type VendorHandler struct {
	*Handler
	vendorService *services.VendorService
}

// NewVendorHandler creates a new vendor handler
func NewVendorHandler(vendorService *services.VendorService) *VendorHandler {
	return &VendorHandler{
		Handler:      &Handler{},
		vendorService: vendorService,
	}
}

// CreateVendorRequest represents vendor creation request
type CreateVendorRequest struct {
	BusinessID    string `json:"business_id" binding:"required"`
	Name          string `json:"name" binding:"required,min=2,max=255"`
	Type          string `json:"type" binding:"required"`
	Phone         string `json:"phone" binding:"omitempty,max=20"`
	Email         string `json:"email" binding:"omitempty,email,max=255"`
	Address       string `json:"address" binding:"omitempty,max=500"`
	City          string `json:"city" binding:"omitempty,max=100"`
	State         string `json:"state" binding:"omitempty,max=100"`
	Pincode       string `json:"pincode" binding:"omitempty,max=20"`
	GSTIN         string `json:"gstin" binding:"omitempty,len=15"`
	PAN           string `json:"pan" binding:"omitempty,len=10"`
	CreditLimit   string `json:"credit_limit"`
	CreditPeriod  int    `json:"credit_period" binding:"omitempty,min=0,max=365"`
	PaymentTerms  string `json:"payment_terms" binding:"omitempty,max=100"`
	BankAccountNo string `json:"bank_account_no" binding:"omitempty,max=50"`
	BankIFSC      string `json:"bank_ifsc" binding:"omitempty,max=20"`
	BankName      string `json:"bank_name" binding:"omitempty,max=100"`
}

// UpdateVendorRequest represents vendor update request
type UpdateVendorRequest struct {
	Name          *string `json:"name" binding:"omitempty,min=2,max=255"`
	Type          *string `json:"type" binding:"omitempty"`
	Phone         *string `json:"phone" binding:"omitempty,max=20"`
	Email         *string `json:"email" binding:"omitempty,email,max=255"`
	Address       *string `json:"address" binding:"omitempty,max=500"`
	City          *string `json:"city" binding:"omitempty,max=100"`
	State         *string `json:"state" binding:"omitempty,max=100"`
	Pincode       *string `json:"pincode" binding:"omitempty,max=20"`
	GSTIN         *string `json:"gstin" binding:"omitempty,len=15"`
	PAN           *string `json:"pan" binding:"omitempty,len=10"`
	CreditLimit   *string `json:"credit_limit"`
	CreditPeriod  *int    `json:"credit_period" binding:"omitempty,min=0,max=365"`
	PaymentTerms  *string `json:"payment_terms" binding:"omitempty,max=100"`
	BankAccountNo *string `json:"bank_account_no" binding:"omitempty,max=50"`
	BankIFSC      *string `json:"bank_ifsc" binding:"omitempty,max=20"`
	BankName      *string `json:"bank_name" binding:"omitempty,max=100"`
	IsActive      *bool   `json:"is_active"`
}

// UpdateVendorBalanceRequest represents balance update request
type UpdateVendorBalanceRequest struct {
	Amount           string `json:"amount" binding:"required"`
	Description      string `json:"description"`
	TransactionType  string `json:"transaction_type" binding:"required,oneof=credit debit payment_made purchase_created adjustment"`
	ReferenceID      *string `json:"reference_id"`
	ReferenceType    string `json:"reference_type"`
}

// Create creates a new vendor
// @Summary Create vendor
// @Description Create a new vendor
// @Tags vendors
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateVendorRequest true "Vendor details"
// @Success 201 {object} utils.Response
// @Router /api/v1/vendors [post]
func (h *VendorHandler) Create(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var req CreateVendorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}

	createReq := &services.CreateVendorRequest{
		BusinessID:    req.BusinessID,
		Name:          req.Name,
		Type:          models.VendorType(req.Type),
		Phone:         req.Phone,
		Email:         req.Email,
		Address:       req.Address,
		City:          req.City,
		State:         req.State,
		Pincode:       req.Pincode,
		GSTIN:         req.GSTIN,
		PAN:           req.PAN,
		CreditPeriod:  req.CreditPeriod,
		PaymentTerms:  req.PaymentTerms,
		BankAccountNo: req.BankAccountNo,
		BankIFSC:      req.BankIFSC,
		BankName:      req.BankName,
	}

	if req.CreditLimit != "" {
		creditLimit, err := services.ParseDecimal(req.CreditLimit)
		if err != nil {
			h.BadRequest(c, "Invalid credit limit format")
			return
		}
		createReq.CreditLimit = creditLimit
	}

	vendor, err := h.vendorService.Create(c.Request.Context(), createReq, userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Created(c, vendor)
}

// GetByID retrieves a vendor by ID
// @Summary Get vendor by ID
// @Description Get vendor by ID
// @Tags vendors
// @Produce json
// @Security Bearer
// @Param id path string true "Vendor ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/vendors/{id} [get]
func (h *VendorHandler) GetByID(c *gin.Context) {
	id := c.Param("id")

	vendor, err := h.vendorService.GetByID(c.Request.Context(), id)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, vendor)
}

// Update updates an existing vendor
// @Summary Update vendor
// @Description Update an existing vendor
// @Tags vendors
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Vendor ID"
// @Param request body UpdateVendorRequest true "Vendor details"
// @Success 200 {object} utils.Response
// @Router /api/v1/vendors/{id} [put]
func (h *VendorHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var req UpdateVendorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}

	updateReq := &services.UpdateVendorRequest{
		Name:          req.Name,
		Type:          (*models.VendorType)(req.Type),
		Phone:         req.Phone,
		Email:         req.Email,
		Address:       req.Address,
		City:          req.City,
		State:         req.State,
		Pincode:       req.Pincode,
		GSTIN:         req.GSTIN,
		PAN:           req.PAN,
		CreditPeriod:  req.CreditPeriod,
		PaymentTerms:  req.PaymentTerms,
		BankAccountNo: req.BankAccountNo,
		BankIFSC:      req.BankIFSC,
		BankName:      req.BankName,
		IsActive:      req.IsActive,
	}

	if req.CreditLimit != nil {
		creditLimit, err := services.ParseDecimal(*req.CreditLimit)
		if err != nil {
			h.BadRequest(c, "Invalid credit limit format")
			return
		}
		updateReq.CreditLimit = &creditLimit
	}

	vendor, err := h.vendorService.Update(c.Request.Context(), id, updateReq)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, vendor)
}

// Delete soft deletes a vendor
// @Summary Delete vendor
// @Description Soft delete a vendor
// @Tags vendors
// @Produce json
// @Security Bearer
// @Param id path string true "Vendor ID"
// @Success 204 {object} utils.Response
// @Router /api/v1/vendors/{id} [delete]
func (h *VendorHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	if err := h.vendorService.Delete(c.Request.Context(), id); err != nil {
		h.Error(c, err)
		return
	}

	h.NoContent(c)
}

// List retrieves vendors for a business with pagination
// @Summary List vendors
// @Description List vendors for a business with pagination
// @Tags vendors
// @Produce json
// @Security Bearer
// @Param business_id query string true "Business ID"
// @Param page query int false "Page number" default(1)
// @Param per_page query int false "Items per page" default(20)
// @Success 200 {object} utils.Response
// @Router /api/v1/vendors [get]
func (h *VendorHandler) List(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		h.BadRequest(c, "business_id is required")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	vendors, total, err := h.vendorService.List(c.Request.Context(), businessID, page, perPage)
	if err != nil {
		h.Error(c, err)
		return
	}

	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}

	h.Success(c, gin.H{
		"vendors": vendors,
		"meta": gin.H{
			"page":        page,
			"per_page":    perPage,
			"total_pages": totalPages,
			"total_count": total,
		},
	})
}

// Search searches vendors by name
// @Summary Search vendors
// @Description Search vendors by name
// @Tags vendors
// @Produce json
// @Security Bearer
// @Param business_id query string true "Business ID"
// @Param q query string true "Search query"
// @Param page query int false "Page number" default(1)
// @Param per_page query int false "Items per page" default(20)
// @Success 200 {object} utils.Response
// @Router /api/v1/vendors/search [get]
func (h *VendorHandler) Search(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		h.BadRequest(c, "business_id is required")
		return
	}

	query := c.Query("q")
	if query == "" {
		h.BadRequest(c, "search query (q) is required")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	vendors, total, err := h.vendorService.Search(c.Request.Context(), businessID, query, page, perPage)
	if err != nil {
		h.Error(c, err)
		return
	}

	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}

	h.Success(c, gin.H{
		"vendors": vendors,
		"meta": gin.H{
			"page":        page,
			"per_page":    perPage,
			"total_pages": totalPages,
			"total_count": total,
		},
	})
}

// FilterByType filters vendors by type
// @Summary Filter vendors by type
// @Description Filter vendors by type
// @Tags vendors
// @Produce json
// @Security Bearer
// @Param business_id query string true "Business ID"
// @Param type query string true "Vendor type" Enums(manufacturer, distributor, wholesaler, retailer, service, other)
// @Param page query int false "Page number" default(1)
// @Param per_page query int false "Items per page" default(20)
// @Success 200 {object} utils.Response
// @Router /api/v1/vendors/filter [get]
func (h *VendorHandler) FilterByType(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		h.BadRequest(c, "business_id is required")
		return
	}

	vendorType := c.Query("type")
	if vendorType == "" {
		h.BadRequest(c, "type is required")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	vendors, total, err := h.vendorService.FilterByType(c.Request.Context(), businessID, models.VendorType(vendorType), page, perPage)
	if err != nil {
		h.Error(c, err)
		return
	}

	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}

	h.Success(c, gin.H{
		"vendors": vendors,
		"meta": gin.H{
			"page":        page,
			"per_page":    perPage,
			"total_pages": totalPages,
			"total_count": total,
		},
	})
}

// GetOutstanding retrieves vendors with outstanding balances
// @Summary Get outstanding vendors
// @Description Get vendors with outstanding balances
// @Tags vendors
// @Produce json
// @Security Bearer
// @Param business_id query string true "Business ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/vendors/outstanding [get]
func (h *VendorHandler) GetOutstanding(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		h.BadRequest(c, "business_id is required")
		return
	}

	vendors, err := h.vendorService.GetOutstanding(c.Request.Context(), businessID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{
		"vendors": vendors,
	})
}

// GetStats retrieves vendor statistics
// @Summary Get vendor statistics
// @Description Get vendor statistics
// @Tags vendors
// @Produce json
// @Security Bearer
// @Param business_id query string true "Business ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/vendors/stats [get]
func (h *VendorHandler) GetStats(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		h.BadRequest(c, "business_id is required")
		return
	}

	stats, err := h.vendorService.GetStats(c.Request.Context(), businessID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, stats)
}

// UpdateBalance updates vendor balance
// @Summary Update vendor balance
// @Description Update vendor balance
// @Tags vendors
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Vendor ID"
// @Param request body UpdateVendorBalanceRequest true "Balance details"
// @Success 200 {object} utils.Response
// @Router /api/v1/vendors/{id}/balance [patch]
func (h *VendorHandler) UpdateBalance(c *gin.Context) {
	id := c.Param("id")

	var req UpdateVendorBalanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}

	amount, err := services.ParseDecimal(req.Amount)
	if err != nil {
		h.BadRequest(c, "Invalid amount format")
		return
	}

	updateReq := &services.VendorUpdateBalanceRequest{
		Amount:           amount,
		Description:      req.Description,
		TransactionType:  req.TransactionType,
		ReferenceID:      req.ReferenceID,
		ReferenceType:    req.ReferenceType,
	}

	if err := h.vendorService.UpdateBalance(c.Request.Context(), id, updateReq); err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{
		"message": "Balance updated successfully",
	})
}
