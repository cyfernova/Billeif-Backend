package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/cyfernova/invoice-backend/internal/middleware"
	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/internal/services"
)

// CustomerHandler handles customer management endpoints
type CustomerHandler struct {
	*Handler
	customerService *services.CustomerService
}

// NewCustomerHandler creates a new customer handler
func NewCustomerHandler(customerService *services.CustomerService) *CustomerHandler {
	return &CustomerHandler{
		Handler:         &Handler{},
		customerService: customerService,
	}
}

// CreateCustomerRequest represents customer creation request
type CreateCustomerRequest struct {
	BusinessID   string  `json:"business_id" binding:"required"`
	Name         string  `json:"name" binding:"required,min=2,max=255"`
	Type         string  `json:"type" binding:"required"`
	Phone        string  `json:"phone" binding:"omitempty,max=20"`
	Email        string  `json:"email" binding:"omitempty,email,max=255"`
	Address      string  `json:"address" binding:"omitempty,max=500"`
	City         string  `json:"city" binding:"omitempty,max=100"`
	State        string  `json:"state" binding:"omitempty,max=100"`
	Pincode      string  `json:"pincode" binding:"omitempty,max=20"`
	GSTIN        string  `json:"gstin" binding:"omitempty,len=15"`
	PAN          string  `json:"pan" binding:"omitempty,len=10"`
	CreditLimit  string  `json:"credit_limit"`
	CreditPeriod int     `json:"credit_period" binding:"omitempty,min=0,max=365"`
}

// UpdateCustomerRequest represents customer update request
type UpdateCustomerRequest struct {
	Name         *string `json:"name" binding:"omitempty,min=2,max=255"`
	Type         *string `json:"type" binding:"omitempty"`
	Phone        *string `json:"phone" binding:"omitempty,max=20"`
	Email        *string `json:"email" binding:"omitempty,email,max=255"`
	Address      *string `json:"address" binding:"omitempty,max=500"`
	City         *string `json:"city" binding:"omitempty,max=100"`
	State        *string `json:"state" binding:"omitempty,max=100"`
	Pincode      *string `json:"pincode" binding:"omitempty,max=20"`
	GSTIN        *string `json:"gstin" binding:"omitempty,len=15"`
	PAN          *string `json:"pan" binding:"omitempty,len=10"`
	CreditLimit  *string `json:"credit_limit"`
	CreditPeriod *int    `json:"credit_period" binding:"omitempty,min=0,max=365"`
	IsActive     *bool   `json:"is_active"`
}

// UpdateBalanceRequest represents balance update request
type UpdateBalanceRequest struct {
	Amount          string  `json:"amount" binding:"required"`
	Description     string  `json:"description"`
	TransactionType string  `json:"transaction_type" binding:"required,oneof=credit debit payment_received invoice_created adjustment"`
	ReferenceID     *string `json:"reference_id"`
	ReferenceType   string  `json:"reference_type"`
}

// Create creates a new customer
// @Summary Create customer
// @Description Create a new customer
// @Tags customers
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateCustomerRequest true "Customer details"
// @Success 201 {object} utils.Response
// @Router /api/v1/customers [post]
func (h *CustomerHandler) Create(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var req CreateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}

	createReq := &services.CreateCustomerRequest{
		BusinessID:   req.BusinessID,
		Name:         req.Name,
		Type:         models.CustomerType(req.Type),
		Phone:        req.Phone,
		Email:        req.Email,
		Address:      req.Address,
		City:         req.City,
		State:        req.State,
		Pincode:      req.Pincode,
		GSTIN:        req.GSTIN,
		PAN:          req.PAN,
		CreditPeriod: req.CreditPeriod,
	}

	if req.CreditLimit != "" {
		creditLimit, err := services.ParseDecimal(req.CreditLimit)
		if err != nil {
			h.BadRequest(c, "Invalid credit limit format")
			return
		}
		createReq.CreditLimit = creditLimit
	}

	customer, err := h.customerService.Create(c.Request.Context(), createReq, userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Created(c, customer)
}

// GetByID retrieves a customer by ID
// @Summary Get customer by ID
// @Description Get customer by ID
// @Tags customers
// @Produce json
// @Security Bearer
// @Param id path string true "Customer ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/customers/{id} [get]
func (h *CustomerHandler) GetByID(c *gin.Context) {
	id := c.Param("id")

	customer, err := h.customerService.GetByID(c.Request.Context(), id)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, customer)
}

// Update updates an existing customer
// @Summary Update customer
// @Description Update an existing customer
// @Tags customers
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Customer ID"
// @Param request body UpdateCustomerRequest true "Customer details"
// @Success 200 {object} utils.Response
// @Router /api/v1/customers/{id} [put]
func (h *CustomerHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var req UpdateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}

	updateReq := &services.UpdateCustomerRequest{
		Name:         req.Name,
		Type:         (*models.CustomerType)(req.Type),
		Phone:        req.Phone,
		Email:        req.Email,
		Address:      req.Address,
		City:         req.City,
		State:        req.State,
		Pincode:      req.Pincode,
		GSTIN:        req.GSTIN,
		PAN:          req.PAN,
		CreditPeriod: req.CreditPeriod,
		IsActive:     req.IsActive,
	}

	if req.CreditLimit != nil {
		creditLimit, err := services.ParseDecimal(*req.CreditLimit)
		if err != nil {
			h.BadRequest(c, "Invalid credit limit format")
			return
		}
		updateReq.CreditLimit = &creditLimit
	}

	customer, err := h.customerService.Update(c.Request.Context(), id, updateReq)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, customer)
}

// Delete soft deletes a customer
// @Summary Delete customer
// @Description Soft delete a customer
// @Tags customers
// @Produce json
// @Security Bearer
// @Param id path string true "Customer ID"
// @Success 204 {object} utils.Response
// @Router /api/v1/customers/{id} [delete]
func (h *CustomerHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	if err := h.customerService.Delete(c.Request.Context(), id); err != nil {
		h.Error(c, err)
		return
	}

	h.NoContent(c)
}

// List retrieves customers for a business with pagination
// @Summary List customers
// @Description List customers for a business with pagination
// @Tags customers
// @Produce json
// @Security Bearer
// @Param business_id query string true "Business ID"
// @Param page query int false "Page number" default(1)
// @Param per_page query int false "Items per page" default(20)
// @Success 200 {object} utils.Response
// @Router /api/v1/customers [get]
func (h *CustomerHandler) List(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		h.BadRequest(c, "business_id is required")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	customers, total, err := h.customerService.List(c.Request.Context(), businessID, page, perPage)
	if err != nil {
		h.Error(c, err)
		return
	}

	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}

	h.Success(c, gin.H{
		"customers": customers,
		"meta": gin.H{
			"page":        page,
			"per_page":    perPage,
			"total_pages": totalPages,
			"total_count": total,
		},
	})
}

// Search searches customers by name
// @Summary Search customers
// @Description Search customers by name
// @Tags customers
// @Produce json
// @Security Bearer
// @Param business_id query string true "Business ID"
// @Param q query string true "Search query"
// @Param page query int false "Page number" default(1)
// @Param per_page query int false "Items per page" default(20)
// @Success 200 {object} utils.Response
// @Router /api/v1/customers/search [get]
func (h *CustomerHandler) Search(c *gin.Context) {
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

	customers, total, err := h.customerService.Search(c.Request.Context(), businessID, query, page, perPage)
	if err != nil {
		h.Error(c, err)
		return
	}

	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}

	h.Success(c, gin.H{
		"customers": customers,
		"meta": gin.H{
			"page":        page,
			"per_page":    perPage,
			"total_pages": totalPages,
			"total_count": total,
		},
	})
}

// FilterByType filters customers by type
// @Summary Filter customers by type
// @Description Filter customers by type
// @Tags customers
// @Produce json
// @Security Bearer
// @Param business_id query string true "Business ID"
// @Param type query string true "Customer type" Enums(retail, wholesale, b2b, government, other)
// @Param page query int false "Page number" default(1)
// @Param per_page query int false "Items per page" default(20)
// @Success 200 {object} utils.Response
// @Router /api/v1/customers/filter [get]
func (h *CustomerHandler) FilterByType(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		h.BadRequest(c, "business_id is required")
		return
	}

	customerType := c.Query("type")
	if customerType == "" {
		h.BadRequest(c, "type is required")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	customers, total, err := h.customerService.FilterByType(c.Request.Context(), businessID, models.CustomerType(customerType), page, perPage)
	if err != nil {
		h.Error(c, err)
		return
	}

	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}

	h.Success(c, gin.H{
		"customers": customers,
		"meta": gin.H{
			"page":        page,
			"per_page":    perPage,
			"total_pages": totalPages,
			"total_count": total,
		},
	})
}

// GetOutstanding retrieves customers with outstanding balances
// @Summary Get outstanding customers
// @Description Get customers with outstanding balances
// @Tags customers
// @Produce json
// @Security Bearer
// @Param business_id query string true "Business ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/customers/outstanding [get]
func (h *CustomerHandler) GetOutstanding(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		h.BadRequest(c, "business_id is required")
		return
	}

	customers, err := h.customerService.GetOutstanding(c.Request.Context(), businessID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{
		"customers": customers,
	})
}

// GetStats retrieves customer statistics
// @Summary Get customer statistics
// @Description Get customer statistics
// @Tags customers
// @Produce json
// @Security Bearer
// @Param business_id query string true "Business ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/customers/stats [get]
func (h *CustomerHandler) GetStats(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		h.BadRequest(c, "business_id is required")
		return
	}

	stats, err := h.customerService.GetStats(c.Request.Context(), businessID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, stats)
}

// UpdateBalance updates customer balance
// @Summary Update customer balance
// @Description Update customer balance
// @Tags customers
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Customer ID"
// @Param request body UpdateBalanceRequest true "Balance details"
// @Success 200 {object} utils.Response
// @Router /api/v1/customers/{id}/balance [patch]
func (h *CustomerHandler) UpdateBalance(c *gin.Context) {
	id := c.Param("id")

	var req UpdateBalanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}

	amount, err := services.ParseDecimal(req.Amount)
	if err != nil {
		h.BadRequest(c, "Invalid amount format")
		return
	}

	updateReq := &services.UpdateBalanceRequest{
		Amount:           amount,
		Description:      req.Description,
		TransactionType:  req.TransactionType,
		ReferenceID:      req.ReferenceID,
		ReferenceType:    req.ReferenceType,
	}

	if err := h.customerService.UpdateBalance(c.Request.Context(), id, updateReq); err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{
		"message": "Balance updated successfully",
	})
}
