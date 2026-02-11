package handlers

import (
	"invoice-backend/internal/models"
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type CustomerHandler struct {
	svc *services.CustomerService
	log *logger.Logger
}

func NewCustomerHandler(svc *services.CustomerService, log *logger.Logger) *CustomerHandler {
	return &CustomerHandler{svc: svc, log: log}
}

// Create creates a new customer
// @Summary Create customer
// @Description Create a new customer for a business.
// @Tags Customers
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateCustomerInput true "Customer details"
// @Success 201 {object} models.Customer
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /customers [post]
func (h *CustomerHandler) Create(c *gin.Context) {
	var input services.CreateCustomerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var customer *models.Customer
	customer, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, customer)
}

// Get retrieves a customer by ID
// @Summary Get customer
// @Description Returns the details of a specific customer.
// @Tags Customers
// @Produce json
// @Security BearerAuth
// @Param id path string true "Customer ID"
// @Success 200 {object} models.Customer
// @Failure 404 {object} map[string]string
// @Router /customers/{id} [get]
func (h *CustomerHandler) Get(c *gin.Context) {
	id := c.Param("id")
	var customer *models.Customer
	customer, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "customer not found"})
		return
	}

	c.JSON(http.StatusOK, customer)
}

// List retrieves all customers for a business
// @Summary List customers
// @Description Returns a list of customers belonging to a specific business.
// @Tags Customers
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(10)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /customers [get]
func (h *CustomerHandler) List(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	page, limit := utils.ParsePagination(c)

	customers, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  customers,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// Update updates a customer's information
// @Summary Update customer
// @Description Update the details of a specific customer.
// @Tags Customers
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Customer ID"
// @Param input body services.UpdateCustomerInput true "Customer updates"
// @Success 200 {object} models.Customer
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /customers/{id} [put]
func (h *CustomerHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input services.UpdateCustomerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var customer *models.Customer
	customer, err := h.svc.Update(c.Request.Context(), id, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, customer)
}

// Delete deletes a customer
// @Summary Delete customer
// @Description Remove a specific customer.
// @Tags Customers
// @Produce json
// @Security BearerAuth
// @Param id path string true "Customer ID"
// @Success 204 "No Content"
// @Failure 500 {object} map[string]string
// @Router /customers/{id} [delete]
func (h *CustomerHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// Import imports multiple customers
// @Summary Import customers
// @Description Batch import multiple customers for a business.
// @Tags Customers
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Param input body []services.CreateCustomerInput true "List of customers"
// @Success 200 {object} map[string]int
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /customers/import [post]
func (h *CustomerHandler) Import(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	var customers []services.CreateCustomerInput
	if err := c.ShouldBindJSON(&customers); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	count, err := h.svc.Import(c.Request.Context(), businessID, customers)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"imported": count})
}

// Export exports all customers for a business
// @Summary Export customers
// @Description Returns a list of all customers for a business in JSON format.
// @Tags Customers
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Business ID"
// @Success 200 {array} models.Customer
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /customers/export [get]
func (h *CustomerHandler) Export(c *gin.Context) {
	businessID := c.Query("business_id")
	if businessID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "business_id is required"})
		return
	}

	var customers []*models.Customer
	customers, err := h.svc.Export(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, customers)
}
