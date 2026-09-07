package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

const maxProfilePictureBytes int64 = services.MaxProfilePictureUploadBytes

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
		c.JSON(statusCodeForAuthError(err, http.StatusUnauthorized), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// CompleteLoginMFA completes Cognito's SOFTWARE_TOKEN_MFA challenge.
// @Summary Complete TOTP login challenge
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.LoginMFAInput true "Cognito MFA challenge"
// @Success 200 {object} services.LoginOutput
// @Failure 401 {object} map[string]string
// @Router /auth/login/mfa [post]
func (h *AuthHandler) CompleteLoginMFA(c *gin.Context) {
	var input services.LoginMFAInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid MFA challenge"})
		return
	}
	result, err := h.svc.CompleteLoginMFA(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "MFA challenge failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// PhoneRegister registers a user with an Indian mobile number.
// @Summary Register with phone OTP
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.PhoneRegisterInput true "Indian mobile registration"
// @Success 201 {object} services.PhoneRegisterOutput
// @Failure 400 {object} map[string]string
// @Failure 429 {object} map[string]string
// @Router /auth/phone/register [post]
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
// @Summary Confirm phone registration OTP
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.PhoneConfirmInput true "Phone and OTP"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /auth/phone/confirm [post]
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
// @Summary Resend phone registration OTP
// @Description Returns the same response whether or not the phone is registered.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.PhoneLinkInput true "Indian mobile number"
// @Success 200 {object} map[string]string
// @Failure 429 {object} map[string]string
// @Router /auth/phone/resend-confirmation [post]
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
// @Summary Start phone OTP login
// @Description Authentication failures do not reveal whether an account exists.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.PhoneLoginInput true "Indian mobile number"
// @Success 200 {object} services.PhoneLoginChallengeOutput
// @Failure 401 {object} map[string]string
// @Failure 429 {object} map[string]string
// @Router /auth/phone/login [post]
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
// @Summary Verify phone login OTP
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.PhoneVerifyLoginInput true "OTP challenge response"
// @Success 200 {object} services.LoginOutput
// @Failure 401 {object} map[string]string
// @Router /auth/phone/verify-login [post]
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

	c.JSON(http.StatusOK, gin.H{"message": "all Cognito sessions revoked"})
}

// BeginTOTP starts software-token enrollment.
// @Summary Start TOTP enrollment
// @Tags Authentication
// @Security BearerAuth
// @Success 201 {object} services.TOTPSetup
// @Failure 400 {object} map[string]string
// @Router /auth/totp/setup [post]
func (h *AuthHandler) BeginTOTP(c *gin.Context) {
	result, err := h.svc.BeginTOTP(c.Request.Context(), extractToken(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "TOTP setup failed"})
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ConfirmTOTP verifies and enables software-token MFA.
// @Summary Confirm TOTP enrollment
// @Tags Authentication
// @Security BearerAuth
// @Param input body services.TOTPConfirmInput true "TOTP confirmation"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /auth/totp/confirm [post]
func (h *AuthHandler) ConfirmTOTP(c *gin.Context) {
	var input services.TOTPConfirmInput
	if c.ShouldBindJSON(&input) != nil || h.svc.ConfirmTOTP(c.Request.Context(), extractToken(c), input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "TOTP verification failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "TOTP enabled"})
}

// DisableTOTP disables software-token MFA preference.
// @Summary Disable TOTP
// @Tags Authentication
// @Security BearerAuth
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /auth/totp [delete]
func (h *AuthHandler) DisableTOTP(c *gin.Context) {
	if h.svc.DisableTOTP(c.Request.Context(), extractToken(c)) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "TOTP preference update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "TOTP disabled"})
}

// ListDevices lists Cognito devices for the current session.
// @Summary List authentication devices
// @Tags Authentication
// @Security BearerAuth
// @Param next_token query string false "Pagination token"
// @Success 200 {object} services.AuthDevicePage
// @Failure 400 {object} map[string]string
// @Router /auth/devices [get]
func (h *AuthHandler) ListDevices(c *gin.Context) {
	result, err := h.svc.ListDevices(c.Request.Context(), extractToken(c), c.Query("next_token"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device listing failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ForgetDevice revokes one Cognito device.
// @Summary Revoke authentication device
// @Tags Authentication
// @Security BearerAuth
// @Param device_key path string true "Cognito device key"
// @Success 204
// @Failure 400 {object} map[string]string
// @Router /auth/devices/{device_key} [delete]
func (h *AuthHandler) ForgetDevice(c *gin.Context) {
	if h.svc.ForgetDevice(c.Request.Context(), extractToken(c), c.Param("device_key")) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device revocation failed"})
		return
	}
	c.Status(http.StatusNoContent)
}

// SetDeviceRemembered updates Cognito's remembered state.
// @Summary Update device remembered state
// @Tags Authentication
// @Security BearerAuth
// @Param device_key path string true "Cognito device key"
// @Param input body object true "Remembered state"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Router /auth/devices/{device_key} [put]
func (h *AuthHandler) SetDeviceRemembered(c *gin.Context) {
	var input struct {
		Remembered bool `json:"remembered"`
	}
	if c.ShouldBindJSON(&input) != nil || h.svc.SetDeviceRemembered(c.Request.Context(), extractToken(c), c.Param("device_key"), input.Remembered) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device status update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"remembered": input.Remembered})
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
// @Summary Refresh phone-auth session
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body services.RefreshInput true "Refresh token"
// @Success 200 {object} services.LoginOutput
// @Failure 401 {object} map[string]string
// @Router /auth/phone/refresh [post]
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
					if updateErr := h.svc.UpdateUserCognitoID(c.Request.Context(), user.ID, userID); updateErr != nil {
						err = updateErr
					} else {
						user.CognitoID = userID // Update in response
					}
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
	if !input.HasChanges() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one profile field is required"})
		return
	}

	user, err := h.svc.UpdateProfile(c.Request.Context(), userID, input)
	if err != nil {
		if errors.Is(err, services.ErrPhoneLinkRequired) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		// Try by Cognito ID (the JWT subject)
		user, err = h.svc.GetUserByCognitoID(c.Request.Context(), userID)
		if err != nil {
			// Fallback: try by email for legacy users who have email as cognito_id
			if email != "" {
				user, err = h.svc.GetUserByEmail(c.Request.Context(), email)
				if err == nil {
					// Auto-fix the cognito_id for this legacy user
					if updateErr := h.svc.UpdateUserCognitoID(c.Request.Context(), user.ID, userID); updateErr != nil {
						err = updateErr
					} else {
						user.CognitoID = userID
					}
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
			if errors.Is(err, services.ErrPhoneLinkRequired) {
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
				return
			}
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
// @Summary Globally revoke phone-auth sessions
// @Tags Authentication
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /auth/phone/logout [post]
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

// PhoneLinkStart starts explicit phone linking for the authenticated account.
// @Summary Start explicit phone link
// @Description Sends an OTP but does not mutate the account until confirmation.
// @Tags Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.PhoneLinkInput true "Phone to link"
// @Success 202 {object} services.PhoneLinkOutput
// @Failure 409 {object} map[string]string
// @Router /auth/phone/link [post]
func (h *AuthHandler) PhoneLinkStart(c *gin.Context) {
	userID, ok := h.authenticatedDatabaseUserID(c)
	if !ok {
		return
	}
	var input services.PhoneLinkInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.PhoneLinkStart(c.Request.Context(), userID, input)
	if err != nil {
		c.JSON(statusCodeForAuthError(err, http.StatusBadRequest), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, result)
}

// PhoneLinkConfirm confirms explicit phone linking with the possession OTP.
// @Summary Confirm explicit phone link
// @Tags Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.PhoneConfirmInput true "Phone and OTP"
// @Success 200 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /auth/phone/link/confirm [post]
func (h *AuthHandler) PhoneLinkConfirm(c *gin.Context) {
	userID, ok := h.authenticatedDatabaseUserID(c)
	if !ok {
		return
	}
	var input services.PhoneConfirmInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.PhoneLinkConfirm(c.Request.Context(), userID, input); err != nil {
		c.JSON(statusCodeForAuthError(err, http.StatusBadRequest), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "phone number linked successfully"})
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
	emailVerified := middleware.GetEmailVerified(c)
	name := middleware.GetName(c)
	picture := middleware.GetPicture(c)
	if userId == "" || email == "" || !emailVerified {
		h.log.Error("google login failed: invalid identity claims", "user_id", userId, "email", email, "email_verified", emailVerified)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
		return
	}

	input := services.SyncGoogleUserInput{
		Email:             email,
		EmailVerified:     emailVerified,
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
// @Description Returns a presigned S3 URL and the exact headers required to upload a profile picture. The non-multipart presign flow requires size_bytes; uploads are limited to 5 MiB.
// @Tags Authentication
// @Produce json
// @Security BearerAuth
// @Param Content-Type header string false "MIME type (default: image/png)"
// @Param size_bytes query int true "Exact upload size in bytes" minimum(1) maximum(5242880)
// @Success 200 {object} services.PresignedUpload
// @Failure 400 {object} map[string]string
// @Failure 413 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /auth/profile-picture [post]
func (h *AuthHandler) UploadProfilePicture(c *gin.Context) {
	userID, ok := h.authenticatedDatabaseUserID(c)
	if !ok {
		return
	}

	if c.ContentType() == "multipart/form-data" {
		h.uploadProfilePictureFile(c, userID)
		return
	}

	contentType, ok := validateImageContentType(c)
	if !ok {
		return
	}
	sizeBytes, ok := requireUploadSizeBytes(c, services.MaxProfilePictureUploadBytes)
	if !ok {
		return
	}

	upload, err := h.svc.GetProfilePictureUploadURL(c.Request.Context(), userID, contentType, sizeBytes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, upload)
}

func (h *AuthHandler) uploadProfilePictureFile(c *gin.Context, userID string) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxProfilePictureBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile picture file is required"})
		return
	}
	if fileHeader.Size > maxProfilePictureBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "profile picture must be 5MB or smaller"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to open profile picture"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read profile picture"})
		return
	}
	if len(data) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile picture file is empty"})
		return
	}
	if int64(len(data)) > maxProfilePictureBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "profile picture must be 5MB or smaller"})
		return
	}

	contentType := strings.ToLower(strings.TrimSpace(fileHeader.Header.Get("Content-Type")))
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	contentType, err = services.NormalizeImageUploadContentType(contentType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.svc.UploadProfilePicture(c.Request.Context(), userID, data, contentType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, user)
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
	userID, ok := h.authenticatedDatabaseUserID(c)
	if !ok {
		return
	}

	var input struct {
		ProfilePictureURL string `json:"profile_picture_url" binding:"required,url"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.svc.UpdateProfile(c.Request.Context(), userID, services.UpdateProfileInput{ProfilePictureURL: &input.ProfilePictureURL})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, user)
}

func (h *AuthHandler) authenticatedDatabaseUserID(c *gin.Context) (string, bool) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return "", false
	}

	resolvedID, err := h.resolveDatabaseUserID(
		c.Request.Context(),
		userID,
		middleware.GetEmail(c),
		c.GetString("phone_number"),
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return "", false
	}

	return resolvedID, true
}

func (h *AuthHandler) resolveDatabaseUserID(ctx context.Context, userID, email, phoneNumber string) (string, error) {
	user, err := h.svc.GetUser(ctx, userID)
	if err == nil {
		return user.ID, nil
	}

	user, err = h.svc.GetUserByCognitoID(ctx, userID)
	if err == nil {
		return user.ID, nil
	}

	if email != "" {
		user, err = h.svc.GetUserByEmail(ctx, email)
		if err == nil {
			if updateErr := h.svc.UpdateUserCognitoID(ctx, user.ID, userID); updateErr != nil {
				err = updateErr
			} else {
				return user.ID, nil
			}
		}
	}

	if phoneNumber != "" {
		user, err = h.svc.GetUserByPhoneNumber(ctx, phoneNumber)
		if err == nil {
			// Phone linking is explicit and possession-verified. Keep the primary
			// Cognito identity intact when resolving a linked phone-pool token.
			return user.ID, nil
		}
	}

	return "", err
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
	case strings.Contains(message, "linked to another account"):
		return http.StatusConflict
	case strings.Contains(message, "already registered"):
		return http.StatusConflict
	case strings.Contains(message, "aliasexistsexception"):
		return http.StatusConflict
	case strings.Contains(message, "phone number not registered"):
		return http.StatusNotFound
	case strings.Contains(message, "phone number not verified"):
		return http.StatusForbidden
	case strings.Contains(message, "verify your email"):
		return http.StatusForbidden
	case strings.Contains(message, "too many login attempts"):
		return http.StatusTooManyRequests
	case strings.Contains(message, "invalid otp code"):
		return http.StatusBadRequest
	case strings.Contains(message, "otp code expired"):
		return http.StatusBadRequest
	case strings.Contains(message, "verification session expired"):
		return http.StatusUnauthorized
	case strings.Contains(message, "too many otp requests"):
		return http.StatusTooManyRequests
	case strings.Contains(message, "rate limit is unavailable"):
		return http.StatusServiceUnavailable
	case strings.Contains(message, "unable to deliver otp sms"):
		return http.StatusServiceUnavailable
	case strings.Contains(message, "not configured"):
		return http.StatusServiceUnavailable
	default:
		return fallback
	}
}
