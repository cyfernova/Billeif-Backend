package handlers

import (
	"net/http"
	"strings"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	svc *services.AuthService
	log *logger.Logger
}

func NewAuthHandler(svc *services.AuthService, log *logger.Logger) *AuthHandler {
	return &AuthHandler{svc: svc, log: log}
}

// Register registers a new user
// @Summary Register a new user
// @Description Create a new user account with email and password.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.RegisterInput true "Registration details"
// @Success 201 {object} services.RegisterOutput
// @Failure 400 {object} map[string]string
// @Router /auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var input services.RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.svc.Register(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, result)
}

// Login authenticates a user
// @Summary Login user
// @Description Authenticate a user with email and password and return tokens.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.LoginInput true "Login credentials"
// @Success 200 {object} services.LoginOutput
// @Failure 401 {object} map[string]string
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var input services.LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.svc.Login(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// PhoneRegister registers a user with an Indian mobile number.
func (h *AuthHandler) PhoneRegister(c *gin.Context) {
	var input services.PhoneRegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.svc.PhoneRegister(c.Request.Context(), input)
	if err != nil {
		c.JSON(statusCodeForAuthError(err, http.StatusBadRequest), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, result)
}

// PhoneConfirm confirms a phone-based signup.
func (h *AuthHandler) PhoneConfirm(c *gin.Context) {
	var input services.PhoneConfirmInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.PhoneConfirm(c.Request.Context(), input); err != nil {
		c.JSON(statusCodeForAuthError(err, http.StatusBadRequest), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "phone number verified successfully"})
}

// PhoneResendConfirmation resends the sign-up OTP.
func (h *AuthHandler) PhoneResendConfirmation(c *gin.Context) {
	var input struct {
		PhoneNumber string `json:"phone_number" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.PhoneResendConfirmation(c.Request.Context(), input.PhoneNumber); err != nil {
		c.JSON(statusCodeForAuthError(err, http.StatusBadRequest), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "verification code resent"})
}

// PhoneLogin starts the SMS OTP challenge.
func (h *AuthHandler) PhoneLogin(c *gin.Context) {
	var input services.PhoneLoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.svc.PhoneLogin(c.Request.Context(), input)
	if err != nil {
		c.JSON(statusCodeForAuthError(err, http.StatusUnauthorized), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// PhoneVerifyLogin verifies the SMS OTP and returns tokens.
func (h *AuthHandler) PhoneVerifyLogin(c *gin.Context) {
	var input services.PhoneVerifyLoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.svc.PhoneVerifyLogin(c.Request.Context(), input)
	if err != nil {
		c.JSON(statusCodeForAuthError(err, http.StatusUnauthorized), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// Logout invalidates a user session
// @Summary Logout user
// @Description Revoke the user's access token and end the session.
// @Tags Authentication
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	token := extractToken(c)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no token provided"})
		return
	}

	if err := h.svc.Logout(c.Request.Context(), token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}

// Refresh renews an access token
// @Summary Refresh token
// @Description Get a new access token using a refresh token.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.RefreshInput true "Refresh token"
// @Success 200 {object} services.LoginOutput
// @Failure 401 {object} map[string]string
// @Router /auth/refresh [post]
func (h *AuthHandler) Refresh(c *gin.Context) {
	var input services.RefreshInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.svc.Refresh(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// PhoneRefresh renews a phone-auth access token.
func (h *AuthHandler) PhoneRefresh(c *gin.Context) {
	var input services.RefreshInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.svc.PhoneRefresh(c.Request.Context(), input)
	if err != nil {
		c.JSON(statusCodeForAuthError(err, http.StatusUnauthorized), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ForgotPassword initiates password reset
// @Summary Forgot password
// @Description Send a reset code to the user's email if it exists.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.ForgotPasswordInput true "User email"
// @Success 200 {object} map[string]string
// @Router /auth/forgot-password [post]
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var input services.ForgotPasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_ = h.svc.ForgotPassword(c.Request.Context(), input)
	c.JSON(http.StatusOK, gin.H{"message": "if the email exists, a reset code will be sent"})
}

// ResetPassword completes password reset
// @Summary Reset password
// @Description Reset the user's password using the code sent to their email.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.ResetPasswordInput true "New password and reset code"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /auth/reset-password [post]
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var input services.ResetPasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.ResetPassword(c.Request.Context(), input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password reset successfully"})
}

// VerifyEmail verifies user's email
// @Summary Verify email
// @Description Verify a user's email address using the verification code.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.VerifyEmailInput true "Verification code"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /auth/verify-email [post]
func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	var input services.VerifyEmailInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.VerifyEmail(c.Request.Context(), input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "email verified successfully"})
}

// ResendVerification resends verification code
// @Summary Resend verification
// @Description Resend the email verification code to the user.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body object{email=string} true "User email"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /auth/resend-verification [post]
func (h *AuthHandler) ResendVerification(c *gin.Context) {
	var input struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.ResendVerification(c.Request.Context(), input.Email); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "verification code resent"})
}

// Me returns current user profile
// @Summary Get current user
// @Description Returns the profile information of the authenticated user.
// @Tags Authentication
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /auth/me [get]
func (h *AuthHandler) Me(c *gin.Context) {
	userID := middleware.GetUserID(c)
	email := middleware.GetEmail(c)
	phoneNumber := c.GetString("phone_number")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// Try to find user by database ID first
	user, err := h.svc.GetUser(c.Request.Context(), userID)
	if err != nil {
		// Try by Cognito ID (the JWT subject)
		user, err = h.svc.GetUserByCognitoID(c.Request.Context(), userID)
		if err != nil {
			// Fallback: try by email for legacy users who have email as cognito_id
			if email != "" {
				user, err = h.svc.GetUserByEmail(c.Request.Context(), email)
				if err == nil {
					// Auto-fix the cognito_id for this legacy user
					_ = h.svc.UpdateUserCognitoID(c.Request.Context(), user.ID, userID)
					user.CognitoID = userID // Update in response
				}
			}
			if err != nil && phoneNumber != "" {
				user, err = h.svc.GetUserByPhoneNumber(c.Request.Context(), phoneNumber)
			}
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
				return
			}
		}
	}

	c.JSON(http.StatusOK, user)
}

// UpdateProfile updates user profile
// @Summary Update profile
// @Description Update the profile information of the authenticated user.
// @Tags Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.UpdateProfileInput true "Profile updates"
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /auth/profile [put]
func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)
	email := middleware.GetEmail(c)
	phoneNumber := c.GetString("phone_number")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var input services.UpdateProfileInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.svc.UpdateProfile(c.Request.Context(), userID, input)
	if err != nil {
		// Try by Cognito ID (the JWT subject)
		user, err = h.svc.GetUserByCognitoID(c.Request.Context(), userID)
		if err != nil {
			// Fallback: try by email for legacy users who have email as cognito_id
			if email != "" {
				user, err = h.svc.GetUserByEmail(c.Request.Context(), email)
				if err == nil {
					// Auto-fix the cognito_id for this legacy user
					_ = h.svc.UpdateUserCognitoID(c.Request.Context(), user.ID, userID)
					user.CognitoID = userID
				}
			}
			if err != nil && phoneNumber != "" {
				user, err = h.svc.GetUserByPhoneNumber(c.Request.Context(), phoneNumber)
			}
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
				return
			}
		}

		// Retry update with found user
		user, err = h.svc.UpdateProfile(c.Request.Context(), user.ID, input)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, user)
}

// ChangePassword changes user password
// @Summary Change password
// @Description Change the password of the authenticated user.
// @Tags Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.ChangePasswordInput true "Password change details"
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /auth/change-password [post]
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	token := extractToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var input services.ChangePasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.ChangePassword(c.Request.Context(), token, input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password changed successfully"})
}

// PhoneLogout invalidates a phone-authenticated session.
func (h *AuthHandler) PhoneLogout(c *gin.Context) {
	token := extractToken(c)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no token provided"})
		return
	}

	if err := h.svc.PhoneLogout(c.Request.Context(), token); err != nil {
		c.JSON(statusCodeForAuthError(err, http.StatusInternalServerError), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}

// GoogleLogin handles Google OAuth login
// @Summary Google login
// @Description Sync user data from Google after successful OAuth authentication.
// @Tags Authentication
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /auth/google [post]
func (h *AuthHandler) GoogleLogin(c *gin.Context) {
	userId := middleware.GetUserID(c)
	email := middleware.GetEmail(c)
	name := middleware.GetName(c)
	picture := middleware.GetPicture(c)
	if userId == "" || email == "" {
		h.log.Error("google login failed: missing user_id or email from token", "user_id", userId, "email", email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
		return
	}

	input := services.SyncGoogleUserInput{
		Email:             email,
		CognitoID:         userId,
		Name:              name,
		ProfilePictureURL: picture,
	}

	user, err := h.svc.SyncGoogleUser(c.Request.Context(), input)
	if err != nil {
		h.log.Error("failed to sync google user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process google login"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// UploadProfilePicture generates a presigned URL for profile picture upload
// @Summary Upload profile picture
// @Description Returns a presigned S3 URL to upload a profile picture.
// @Tags Authentication
// @Produce json
// @Security BearerAuth
// @Param Content-Type header string false "MIME type (default: image/png)"
// @Success 200 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /auth/profile-picture [post]
func (h *AuthHandler) UploadProfilePicture(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	contentType, ok := validateImageContentType(c)
	if !ok {
		return
	}

	url, err := h.svc.GetProfilePictureUploadURL(c.Request.Context(), userID, contentType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"upload_url": url})
}

// UpdateProfilePicture updates the user's profile picture URL
// @Summary Update profile picture URL
// @Description Updates the user's profile picture URL after successful upload.
// @Tags Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body object{profile_picture_url=string} true "Profile picture URL"
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /auth/profile-picture [put]
func (h *AuthHandler) UpdateProfilePicture(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var input struct {
		ProfilePictureURL string `json:"profile_picture_url" binding:"required,url"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.svc.UpdateProfile(c.Request.Context(), userID, services.UpdateProfileInput{
		Name:              "", // Only update profile picture
		ProfilePictureURL: input.ProfilePictureURL,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, user)
}

func extractToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func statusCodeForAuthError(err error, fallback int) int {
	if err == nil {
		return fallback
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "already registered"):
		return http.StatusConflict
	case strings.Contains(message, "aliasexistsexception"):
		return http.StatusConflict
	case strings.Contains(message, "phone number not registered"):
		return http.StatusNotFound
	case strings.Contains(message, "phone number not verified"):
		return http.StatusForbidden
	case strings.Contains(message, "invalid otp code"):
		return http.StatusBadRequest
	case strings.Contains(message, "otp code expired"):
		return http.StatusBadRequest
	case strings.Contains(message, "verification session expired"):
		return http.StatusUnauthorized
	case strings.Contains(message, "too many otp requests"):
		return http.StatusTooManyRequests
	case strings.Contains(message, "unable to deliver otp sms"):
		return http.StatusServiceUnavailable
	case strings.Contains(message, "not configured"):
		return http.StatusServiceUnavailable
	default:
		return fallback
	}
}
