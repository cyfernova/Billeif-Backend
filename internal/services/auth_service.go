package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/cyfernova/invoice-backend/internal/config"
	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/internal/repositories"
	"github.com/cyfernova/invoice-backend/internal/utils"
	"github.com/cyfernova/invoice-backend/pkg/logger"
)

// AuthService handles authentication operations
type AuthService struct {
	cognitoService *CognitoService
	userRepo       repositories.UserRepository
	sessionRepo    repositories.SessionRepository
	config         config.JWTConfig
	logger         *logger.Logger
}

// NewAuthService creates a new auth service
func NewAuthService(
	cognito *CognitoService,
	userRepo repositories.UserRepository,
	sessionRepo repositories.SessionRepository,
	cfg config.JWTConfig,
	log *logger.Logger,
) *AuthService {
	return &AuthService{
		cognitoService: cognito,
		userRepo:       userRepo,
		sessionRepo:    sessionRepo,
		config:         cfg,
		logger:         log,
	}
}

// RegisterRequest represents user registration request
type RegisterRequest struct {
	Email     string
	Password  string
	FirstName string
	LastName  string
}

// LoginRequest represents user login request
type LoginRequest struct {
	Email    string
	Password string
}

// AuthResponse represents authentication response
type AuthResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	TokenType    string       `json:"token_type"`
	ExpiresIn    int64        `json:"expires_in"`
	User         *UserResponse `json:"user"`
}

// UserResponse represents user response
type UserResponse struct {
	ID                string            `json:"id"`
	Email             string            `json:"email"`
	FirstName         string            `json:"first_name"`
	LastName          string            `json:"last_name"`
	Phone             string            `json:"phone,omitempty"`
	ProfilePictureURL string            `json:"profile_picture_url,omitempty"`
	Role              models.UserRole   `json:"role"`
	EmailVerified     bool              `json:"email_verified"`
	Groups            []string          `json:"groups,omitempty"`
}

// Register handles user registration
func (s *AuthService) Register(ctx context.Context, req *RegisterRequest) error {
	// Validate input
	if req.Email == "" || req.Password == "" || req.FirstName == "" || req.LastName == "" {
		return utils.ErrBadRequest
	}

	// Check if user already exists in our database
	existingUser, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err == nil && existingUser != nil {
		return utils.NewAppError(409, "User already exists", nil)
	}

	// Register in Cognito
	err = s.cognitoService.SignUp(ctx, req.Email, req.Password, req.FirstName, req.LastName)
	if err != nil {
		s.logger.Error().Err(err).Str("email", req.Email).Msg("Failed to register user in Cognito")
		return utils.NewAppError(500, "Failed to register user", err)
	}

	return nil
}

// VerifyEmail handles email verification
func (s *AuthService) VerifyEmail(ctx context.Context, email, code string) error {
	err := s.cognitoService.ConfirmSignUp(ctx, email, code)
	if err != nil {
		return utils.NewAppError(400, "Invalid verification code", err)
	}

	// Update user in database
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil // User might not be in database yet
	}

	user.EmailVerified = true
	return s.userRepo.Update(ctx, user)
}

// ResendVerification resends verification email
func (s *AuthService) ResendVerification(ctx context.Context, email string) error {
	return s.cognitoService.ForgotPassword(ctx, email)
}

// Login handles user authentication
func (s *AuthService) Login(ctx context.Context, req *LoginRequest, ip, userAgent string) (*AuthResponse, error) {
	// Authenticate with Cognito
	authResult, err := s.cognitoService.SignIn(ctx, req.Email, req.Password)
	if err != nil {
		return nil, utils.ErrUnauthorized
	}

	// Get user from Cognito
	cognitoUser, err := s.cognitoService.GetUser(ctx, authResult.AccessToken)
	if err != nil {
		return nil, utils.ErrInternalServer
	}

	// Get or create user in database
	user, err := s.userRepo.GetByCognitoID(ctx, cognitoUser.Sub)
	if err != nil || user == nil {
		// Create new user in database
		user = &models.User{
			CognitoID:     cognitoUser.Sub,
			Email:         cognitoUser.Email,
			EmailVerified: true,
			Role:          models.RoleUser,
		}
		if err := s.userRepo.Create(ctx, user); err != nil {
			return nil, utils.ErrInternalServer
		}
	}

	// Get user groups
	groups, _ := s.cognitoService.ListUserGroups(ctx, cognitoUser.Username)

	// Store session in DynamoDB
	sessionID := generateSessionID()
	expiresAt := time.Now().Add(time.Duration(s.config.RefreshTokenExp) * time.Minute)

	session := &models.Session{
		SessionID:    sessionID,
		UserID:       user.ID.String(),
		RefreshToken: authResult.RefreshToken,
		IPAddress:    ip,
		UserAgent:    userAgent,
		ExpiresAt:    expiresAt,
		CreatedAt:    time.Now(),
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		s.logger.Error().Err(err).Msg("Failed to store session")
	}

	return &AuthResponse{
		AccessToken:  authResult.AccessToken,
		RefreshToken: sessionID, // Return session ID instead of actual refresh token
		TokenType:    "Bearer",
		ExpiresIn:    int64(authResult.ExpiresIn),
		User:         s.toUserResponse(user, groups),
	}, nil
}

// RefreshToken handles token refresh
func (s *AuthService) RefreshToken(ctx context.Context, sessionID string) (*AuthResponse, error) {
	session, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil {
		return nil, utils.ErrUnauthorized
	}

	if time.Now().After(session.ExpiresAt) {
		return nil, utils.ErrUnauthorized
	}

	// Refresh with Cognito
	authResult, err := s.cognitoService.RefreshToken(ctx, session.RefreshToken)
	if err != nil {
		return nil, utils.ErrUnauthorized
	}

	// Get user
	user, err := s.userRepo.GetByID(ctx, session.UserID)
	if err != nil {
		return nil, utils.ErrInternalServer
	}

	// Get user groups
	groups, _ := s.cognitoService.ListUserGroups(ctx, user.Email)

	return &AuthResponse{
		AccessToken:  authResult.AccessToken,
		RefreshToken: sessionID,
		TokenType:    "Bearer",
		ExpiresIn:    int64(authResult.ExpiresIn),
		User:         s.toUserResponse(user, groups),
	}, nil
}

// Logout handles user logout
func (s *AuthService) Logout(ctx context.Context, accessToken, sessionID string) error {
	// Sign out from Cognito
	if accessToken != "" {
		if err := s.cognitoService.SignOut(ctx, accessToken); err != nil {
			s.logger.Error().Err(err).Msg("Failed to sign out from Cognito")
		}
	}

	// Delete session
	if sessionID != "" {
		if err := s.sessionRepo.Delete(ctx, sessionID); err != nil {
			s.logger.Error().Err(err).Msg("Failed to delete session")
		}
	}

	return nil
}

// ForgotPassword initiates password reset
func (s *AuthService) ForgotPassword(ctx context.Context, email string) error {
	return s.cognitoService.ForgotPassword(ctx, email)
}

// ResetPassword completes password reset
func (s *AuthService) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	return s.cognitoService.ConfirmForgotPassword(ctx, email, code, newPassword)
}

// ChangePassword changes user password
func (s *AuthService) ChangePassword(ctx context.Context, accessToken, oldPassword, newPassword string) error {
	return s.cognitoService.ChangePassword(ctx, accessToken, oldPassword, newPassword)
}

// GetMe gets the current user
func (s *AuthService) GetMe(ctx context.Context, userID string) (*UserResponse, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, utils.ErrNotFound
	}

	// Get user groups from Cognito (using email as username)
	groups, _ := s.cognitoService.ListUserGroups(ctx, user.Email)

	return s.toUserResponse(user, groups), nil
}

func (s *AuthService) toUserResponse(user *models.User, groups []string) *UserResponse {
	return &UserResponse{
		ID:                user.ID.String(),
		Email:             user.Email,
		FirstName:         user.FirstName,
		LastName:          user.LastName,
		Phone:             user.Phone,
		ProfilePictureURL: user.ProfilePictureURL,
		Role:              user.Role,
		EmailVerified:     user.EmailVerified,
		Groups:            groups,
	}
}

func generateSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}
