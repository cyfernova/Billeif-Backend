package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/cyfernova/invoice-backend/internal/middleware"
	"github.com/cyfernova/invoice-backend/internal/services"
)

// UserHandler handles user management endpoints
type UserHandler struct {
	*Handler
	userService *services.UserService
}

// NewUserHandler creates a new user handler
func NewUserHandler(userService *services.UserService) *UserHandler {
	return &UserHandler{
		Handler:     &Handler{},
		userService: userService,
	}
}

// UpdateProfileRequest represents profile update request
type UpdateProfileRequest struct {
	FirstName        *string `json:"first_name"`
	LastName         *string `json:"last_name"`
	Phone            *string `json:"phone"`
	ProfilePictureURL *string `json:"profile_picture_url"`
}

// GetProfile gets user profile
// @Summary Get user profile
// @Description Get user profile by ID
// @Tags users
// @Produce json
// @Security Bearer
// @Param id path string true "User ID"
// @Success 200 {object} utils.Response
// @Router /api/v1/users/{id} [get]
func (h *UserHandler) GetProfile(c *gin.Context) {
	userID := c.Param("id")
	currentUserID := middleware.GetUserID(c)
	currentGroups := middleware.GetUserGroups(c)

	// Check permission: admin or own profile
	isOwnProfile := userID == currentUserID
	isAdmin := false
	for _, g := range currentGroups {
		if g == "admin" {
			isAdmin = true
			break
		}
	}

	if !isOwnProfile && !isAdmin {
		h.Forbidden(c, "You don't have permission to view this profile")
		return
	}

	user, err := h.userService.GetByID(c.Request.Context(), userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, user)
}

// UpdateProfile updates authenticated user profile
// @Summary Update user profile
// @Description Update authenticated user profile
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body UpdateProfileRequest true "Profile details"
// @Success 200 {object} utils.Response
// @Router /api/v1/users/profile [put]
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body")
		return
	}

	user, err := h.userService.UpdateProfile(c.Request.Context(), userID, &services.UpdateProfileRequest{
		FirstName:        req.FirstName,
		LastName:         req.LastName,
		Phone:            req.Phone,
		ProfilePictureURL: req.ProfilePictureURL,
	})

	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, user)
}

// GetMyProfile gets authenticated user profile
// @Summary Get my profile
// @Description Get authenticated user profile
// @Tags users
// @Produce json
// @Security Bearer
// @Success 200 {object} utils.Response
// @Router /api/v1/users/profile [get]
func (h *UserHandler) GetMyProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)

	user, err := h.userService.GetByID(c.Request.Context(), userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, user)
}

// ListUsers lists all users (admin only)
// @Summary List users
// @Description List all users (admin only)
// @Tags users
// @Produce json
// @Security Bearer
// @Param page query int false "Page number" default(1)
// @Param per_page query int false "Items per page" default(20)
// @Success 200 {object} utils.Response
// @Router /api/v1/users [get]
func (h *UserHandler) ListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	users, total, err := h.userService.List(c.Request.Context(), page, perPage)
	if err != nil {
		h.Error(c, err)
		return
	}

	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}

	h.Success(c, gin.H{
		"users": users,
		"meta": gin.H{
			"page":        page,
			"per_page":    perPage,
			"total_pages": totalPages,
			"total_count": total,
		},
	})
}
