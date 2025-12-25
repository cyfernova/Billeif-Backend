package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/cyfernova/invoice-backend/internal/middleware"
	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/internal/services"
)

// BusinessProfileHandler handles business profile management endpoints
type BusinessProfileHandler struct {
	*Handler
	profileService *services.BusinessProfileService
}

// NewBusinessProfileHandler creates a new business profile handler
func NewBusinessProfileHandler(profileService *services.BusinessProfileService) *BusinessProfileHandler {
	return &BusinessProfileHandler{
		Handler:        &Handler{},
		profileService: profileService,
	}
}

// CreateBusinessProfileRequest represents business profile creation request
type CreateBusinessProfileRequest struct {
	Name               string `json:"name" binding:"required,min=2,max=255"`
	Type               string `json:"type" binding:"required"`
	Address            string `json:"address" binding:"omitempty,max=500"`
	City               string `json:"city" binding:"omitempty,max=100"`
	State              string `json:"state" binding:"omitempty,max=100"`
	StateCode          string `json:"state_code" binding:"omitempty,max=5"`
	Pincode            string `json:"pincode" binding:"omitempty,max=20"`
	GSTIN              string `json:"gstin" binding:"omitempty,len=15"`
	PAN                string `json:"pan" binding:"omitempty,len=10"`
	Email              string `json:"email" binding:"omitempty,email,max=255"`
	Phone              string `json:"phone" binding:"omitempty,max=20"`
	Website            string `json:"website" binding:"omitempty,url,max=255"`
	InvoicePrefix      string `json:"invoice_prefix" binding:"omitempty,max=20,alpha"`
	InvoiceStartingNo  int    `json:"invoice_starting_no" binding:"min=1"`
	GSTReturnFrequency string `json:"gst_return_frequency" binding:"omitempty,oneof=monthly quarterly annually"`
	BankName           string `json:"bank_name" binding:"omitempty,max=100"`
	BankAccountNo      string `json:"bank_account_no" binding:"omitempty,max=50"`
	BankIFSC           string `json:"bank_ifsc" binding:"omitempty,max=20"`
	BankBranch         string `json:"bank_branch" binding:"omitempty,max=100"`
	UPIID              string `json:"upi_id" binding:"omitempty,max=50"`
	TermsAndConditions string `json:"terms_and_conditions"`
	InvoiceNotes       string `json:"invoice_notes"`
	IsDefault          bool   `json:"is_default"`
}

// UpdateBusinessProfileRequest represents business profile update request
type UpdateBusinessProfileRequest struct {
	Name               *string `json:"name" binding:"omitempty,min=2,max=255"`
	Type               *string `json:"type" binding:"omitempty"`
	Address            *string `json:"address" binding:"omitempty,max=500"`
	City               *string `json:"city" binding:"omitempty,max=100"`
	State              *string `json:"state" binding:"omitempty,max=100"`
	StateCode          *string `json:"state_code" binding:"omitempty,max=5"`
	Pincode            *string `json:"pincode" binding:"omitempty,max=20"`
	GSTIN              *string `json:"gstin" binding:"omitempty,len=15"`
	PAN                *string `json:"pan" binding:"omitempty,len=10"`
	Email              *string `json:"email" binding:"omitempty,email,max=255"`
	Phone              *string `json:"phone" binding:"omitempty,max=20"`
	Website            *string `json:"website" binding:"omitempty,url,max=255"`
	LogoURL            *string `json:"logo_url"`
	InvoicePrefix      *string `json:"invoice_prefix" binding:"omitempty,max=20,alpha"`
	InvoiceStartingNo  *int    `json:"invoice_starting_no" binding:"omitempty,min=1"`
	GSTReturnFrequency *string `json:"gst_return_frequency" binding:"omitempty,oneof=monthly quarterly annually"`
	BankName           *string `json:"bank_name" binding:"omitempty,max=100"`
	BankAccountNo      *string `json:"bank_account_no" binding:"omitempty,max=50"`
	BankIFSC           *string `json:"bank_ifsc" binding:"omitempty,max=20"`
	BankBranch         *string `json:"bank_branch" binding:"omitempty,max=100"`
	UPIID              *string `json:"upi_id" binding:"omitempty,max=50"`
	TermsAndConditions *string `json:"terms_and_conditions"`
	InvoiceNotes       *string `json:"invoice_notes"`
	IsDefault          *bool   `json:"is_default"`
}

// Create creates a new business profile
// @Summary Create business profile
// @Description Create a new business profile
// @Tags business-profiles
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateBusinessProfileRequest true "Business profile details"
// @Success 201 {object} utils.Response
// @Router /api/v1/business-profiles [post]
func (h *BusinessProfileHandler) Create(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var req CreateBusinessProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}

	createReq := &services.CreateBusinessProfileRequest{
		Name:               req.Name,
		Type:               models.BusinessType(req.Type),
		Address:            req.Address,
		City:               req.City,
		State:              req.State,
		StateCode:          req.StateCode,
		Pincode:            req.Pincode,
		GSTIN:              req.GSTIN,
		PAN:                req.PAN,
		Email:              req.Email,
		Phone:              req.Phone,
		Website:            req.Website,
		InvoicePrefix:      req.InvoicePrefix,
		InvoiceStartingNo:  req.InvoiceStartingNo,
		GSTReturnFrequency: req.GSTReturnFrequency,
		BankName:           req.BankName,
		BankAccountNo:      req.BankAccountNo,
		BankIFSC:           req.BankIFSC,
		BankBranch:         req.BankBranch,
		UPIID:              req.UPIID,
		TermsAndConditions: req.TermsAndConditions,
		InvoiceNotes:       req.InvoiceNotes,
		IsDefault:          req.IsDefault,
	}

	profile, err := h.profileService.Create(c.Request.Context(), userID, createReq)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Created(c, profile)
}

// GetByID retrieves a business profile by ID
// @Summary Get business profile by ID
// @Description Get business profile by ID
// @Tags business-profiles
// @Produce json
// @Security Bearer
// @Param id path string true "Profile ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/business-profiles/{id} [get]
func (h *BusinessProfileHandler) GetByID(c *gin.Context) {
	id := c.Param("id")

	profile, err := h.profileService.GetByID(c.Request.Context(), id)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, profile)
}

// GetByUserID retrieves all profiles for the authenticated user
// @Summary Get my business profiles
// @Description Get all business profiles for the authenticated user
// @Tags business-profiles
// @Produce json
// @Security Bearer
// @Success 200 {object} utils.Response
// @Router /api/v1/business-profiles [get]
func (h *BusinessProfileHandler) GetByUserID(c *gin.Context) {
	userID := middleware.GetUserID(c)

	profiles, err := h.profileService.GetByUserID(c.Request.Context(), userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{
		"profiles": profiles,
	})
}

// GetDefault retrieves the default profile for the authenticated user
// @Summary Get default business profile
// @Description Get the default business profile for the authenticated user
// @Tags business-profiles
// @Produce json
// @Security Bearer
// @Success 200 {object} utils.Response
// @Router /api/v1/business-profiles/default [get]
func (h *BusinessProfileHandler) GetDefault(c *gin.Context) {
	userID := middleware.GetUserID(c)

	profile, err := h.profileService.GetDefault(c.Request.Context(), userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, profile)
}

// HasAny checks if user has any business profiles
// @Summary Check if user has business profiles
// @Description Check if the authenticated user has any business profiles
// @Tags business-profiles
// @Produce json
// @Security Bearer
// @Success 200 {object} utils.Response
// @Router /api/v1/business-profiles/has-any [get]
func (h *BusinessProfileHandler) HasAny(c *gin.Context) {
	userID := middleware.GetUserID(c)

	hasAny, err := h.profileService.HasAny(c.Request.Context(), userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{
		"has_any": hasAny,
	})
}

// Update updates an existing business profile
// @Summary Update business profile
// @Description Update an existing business profile
// @Tags business-profiles
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Profile ID"
// @Param request body UpdateBusinessProfileRequest true "Business profile details"
// @Success 200 {object} utils.Response
// @Router /api/v1/business-profiles/{id} [put]
func (h *BusinessProfileHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var req UpdateBusinessProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}

	updateReq := &services.UpdateBusinessProfileRequest{
		Name:               req.Name,
		Type:               (*models.BusinessType)(req.Type),
		Address:            req.Address,
		City:               req.City,
		State:              req.State,
		StateCode:          req.StateCode,
		Pincode:            req.Pincode,
		GSTIN:              req.GSTIN,
		PAN:                req.PAN,
		Email:              req.Email,
		Phone:              req.Phone,
		Website:            req.Website,
		LogoURL:            req.LogoURL,
		InvoicePrefix:      req.InvoicePrefix,
		InvoiceStartingNo:  req.InvoiceStartingNo,
		GSTReturnFrequency: req.GSTReturnFrequency,
		BankName:           req.BankName,
		BankAccountNo:      req.BankAccountNo,
		BankIFSC:           req.BankIFSC,
		BankBranch:         req.BankBranch,
		UPIID:              req.UPIID,
		TermsAndConditions: req.TermsAndConditions,
		InvoiceNotes:       req.InvoiceNotes,
		IsDefault:          req.IsDefault,
	}

	profile, err := h.profileService.Update(c.Request.Context(), id, updateReq)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, profile)
}

// Delete soft deletes a business profile
// @Summary Delete business profile
// @Description Soft delete a business profile
// @Tags business-profiles
// @Produce json
// @Security Bearer
// @Param id path string true "Profile ID"
// @Success 204 {object} utils.Response
// @Router /api/v1/business-profiles/{id} [delete]
func (h *BusinessProfileHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	if err := h.profileService.Delete(c.Request.Context(), id); err != nil {
		h.Error(c, err)
		return
	}

	h.NoContent(c)
}

// SetDefault sets a profile as default
// @Summary Set default business profile
// @Description Set a business profile as default for the user
// @Tags business-profiles
// @Produce json
// @Security Bearer
// @Param id path string true "Profile ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/business-profiles/{id}/set-default [put]
func (h *BusinessProfileHandler) SetDefault(c *gin.Context) {
	id := c.Param("id")

	profile, err := h.profileService.SetDefault(c.Request.Context(), id)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, profile)
}

// GetLogoUploadURL generates a pre-signed URL for logo upload
// @Summary Get logo upload URL
// @Description Generate a pre-signed URL for uploading business logo
// @Tags business-profiles
// @Produce json
// @Security Bearer
// @Param id path string true "Profile ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/business-profiles/{id}/logo-upload [get]
func (h *BusinessProfileHandler) GetLogoUploadURL(c *gin.Context) {
	id := c.Param("id")

	response, err := h.profileService.GetLogoUploadURL(c.Request.Context(), id)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, response)
}

// ConfirmLogoUpload confirms logo upload and updates profile
// @Summary Confirm logo upload
// @Description Confirm logo upload and update profile with logo URL
// @Tags business-profiles
// @Accept json
// @Produce json
// @Security Bearer
// @Param id path string true "Profile ID"
// @Param request body object{key=string} true "Logo key in S3"
// @Success 200 {object} utils.Response
// @Router /api/v1/business-profiles/{id}/logo-confirm [post]
func (h *BusinessProfileHandler) ConfirmLogoUpload(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Key string `json:"key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}

	profile, err := h.profileService.ConfirmLogoUpload(c.Request.Context(), id, req.Key)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, profile)
}

// GetNextInvoiceNumber generates the next invoice number for a profile
// @Summary Get next invoice number
// @Description Get the next invoice number for a business profile
// @Tags business-profiles
// @Produce json
// @Security Bearer
// @Param id path string true "Profile ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/business-profiles/{id}/next-invoice-number [get]
func (h *BusinessProfileHandler) GetNextInvoiceNumber(c *gin.Context) {
	id := c.Param("id")

	invoiceNumber, sequence, err := h.profileService.GetNextInvoiceNumber(c.Request.Context(), id)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{
		"invoice_number": invoiceNumber,
		"sequence":       sequence,
	})
}

// Search searches profiles by name
// @Summary Search business profiles
// @Description Search business profiles by name
// @Tags business-profiles
// @Produce json
// @Security Bearer
// @Param q query string true "Search query"
// @Success 200 {object} utils.Response
// @Router /api/v1/business-profiles/search [get]
func (h *BusinessProfileHandler) Search(c *gin.Context) {
	userID := middleware.GetUserID(c)
	name := c.Query("q")

	if name == "" {
		h.BadRequest(c, "search query (q) is required")
		return
	}

	profiles, err := h.profileService.Search(c.Request.Context(), userID, name)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{
		"profiles": profiles,
	})
}
