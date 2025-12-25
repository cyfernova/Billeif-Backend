package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/cyfernova/invoice-backend/internal/middleware"
	"github.com/cyfernova/invoice-backend/internal/services"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	*Handler
	authService *services.AuthService
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(authService *services.AuthService) *AuthHandler {
	return &AuthHandler{
		Handler:     &Handler{},
		authService: authService,
	}
}

// RegisterRequest represents registration request
type RegisterRequest struct {
	Email     string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required,min=8"`
	FirstName string `json:"first_name" binding:"required"`
	LastName  string `json:"last_name" binding:"required"`
}

// LoginRequest represents login request
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// VerifyEmailRequest represents email verification request
type VerifyEmailRequest struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required"`
}

// ForgotPasswordRequest represents forgot password request
type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// ResetPasswordRequest represents reset password request
type ResetPasswordRequest struct {
	Email        string `json:"email" binding:"required,email"`
	Code         string `json:"code" binding:"required"`
	NewPassword  string `json:"new_password" binding:"required,min=8"`
}

// ChangePasswordRequest represents change password request
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

// Register handles user registration
// @Summary Register a new user
// @Description Register a new user account
// @Tags auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Registration details"
// @Success 201 {object} utils.Response
// @Router /api/v1/auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body")
		return
	}

	err := h.authService.Register(c.Request.Context(), &services.RegisterRequest{
		Email:     req.Email,
		Password:  req.Password,
		FirstName: req.FirstName,
		LastName:  req.LastName,
	})

	if err != nil {
		h.Error(c, err)
		return
	}

	c.Status(http.StatusCreated)
}

// VerifyEmail handles email verification
// @Summary Verify email address
// @Description Verify user email with confirmation code
// @Tags auth
// @Accept json
// @Produce json
// @Param request body VerifyEmailRequest true "Verification details"
// @Success 200 {object} utils.Response
// @Router /api/v1/auth/verify-email [post]
func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	var req VerifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body")
		return
	}

	err := h.authService.VerifyEmail(c.Request.Context(), req.Email, req.Code)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{"message": "Email verified successfully"})
}

// ResendVerification resends verification email
// @Summary Resend verification email
// @Description Resend email verification code
// @Tags auth
// @Accept json
// @Produce json
// @Param request body ForgotPasswordRequest true "Email address"
// @Success 200 {object} utils.Response
// @Router /api/v1/auth/resend-verification [post]
func (h *AuthHandler) ResendVerification(c *gin.Context) {
	var req ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body")
		return
	}

	err := h.authService.ResendVerification(c.Request.Context(), req.Email)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{"message": "Verification code sent"})
}

// Login handles user login
// @Summary Login user
// @Description Authenticate user with email and password
// @Tags auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login credentials"
// @Success 200 {object} utils.Response{data=services.AuthResponse}
// @Router /api/v1/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body")
		return
	}

	ip := c.ClientIP()
	userAgent := c.Request.UserAgent()

	resp, err := h.authService.Login(c.Request.Context(), &services.LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	}, ip, userAgent)

	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, resp)
}

// Logout handles user logout
// @Summary Logout user
// @Description Logout authenticated user
// @Tags auth
// @Produce json
// @Security Bearer
// @Success 204
// @Router /api/v1/auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	accessToken := c.GetHeader("Authorization")
	if len(accessToken) > 7 {
		accessToken = accessToken[7:]
	}

	sessionID := c.GetHeader("X-Session-ID")

	err := h.authService.Logout(c.Request.Context(), accessToken, sessionID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.NoContent(c)
}

// RefreshToken handles token refresh
// @Summary Refresh access token
// @Description Get new access token using refresh token
// @Tags auth
// @Produce json
// @Param X-Session-ID header string true "Session ID"
// @Success 200 {object} utils.Response{data=services.AuthResponse}
// @Router /api/v1/auth/refresh [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	sessionID := c.GetHeader("X-Session-ID")
	if sessionID == "" {
		h.BadRequest(c, "Missing session ID")
		return
	}

	resp, err := h.authService.RefreshToken(c.Request.Context(), sessionID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, resp)
}

// ForgotPassword handles forgot password
// @Summary Request password reset
// @Description Send password reset email
// @Tags auth
// @Accept json
// @Produce json
// @Param request body ForgotPasswordRequest true "Email address"
// @Success 200 {object} utils.Response
// @Router /api/v1/auth/forgot-password [post]
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body")
		return
	}

	err := h.authService.ForgotPassword(c.Request.Context(), req.Email)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{"message": "Password reset code sent"})
}

// ResetPassword handles password reset
// @Summary Reset password
// @Description Reset password with verification code
// @Tags auth
// @Accept json
// @Produce json
// @Param request body ResetPasswordRequest true "Reset details"
// @Success 200 {object} utils.Response
// @Router /api/v1/auth/reset-password [post]
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body")
		return
	}

	err := h.authService.ResetPassword(c.Request.Context(), req.Email, req.Code, req.NewPassword)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{"message": "Password reset successfully"})
}

// ChangePassword handles password change
// @Summary Change password
// @Description Change authenticated user password
// @Tags auth
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ChangePasswordRequest true "Password details"
// @Success 200 {object} utils.Response
// @Router /api/v1/auth/change-password [post]
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.BadRequest(c, "Invalid request body")
		return
	}

	accessToken := c.GetHeader("Authorization")
	if len(accessToken) > 7 {
		accessToken = accessToken[7:]
	}

	err := h.authService.ChangePassword(c.Request.Context(), accessToken, req.OldPassword, req.NewPassword)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, gin.H{"message": "Password changed successfully"})
}

// GetMe gets current user
// @Summary Get current user
// @Description Get authenticated user details
// @Tags auth
// @Produce json
// @Security Bearer
// @Success 200 {object} utils.Response{data=services.UserResponse}
// @Router /api/v1/auth/me [get]
func (h *AuthHandler) GetMe(c *gin.Context) {
	userID := middleware.GetUserID(c)

	user, err := h.authService.GetMe(c.Request.Context(), userID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Success(c, user)
}
