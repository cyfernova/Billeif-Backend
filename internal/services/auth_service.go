package services

import (
	"context"
	"fmt"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

type AuthService struct {
	cfg      *config.Config
	userRepo interfaces.UserRepository
	cognito  *cognitoidentityprovider.Client
	email    *EmailService
	log      *logger.Logger
}

func NewAuthService(cfg *config.Config, userRepo interfaces.UserRepository, aws *awsclients.Config, email *EmailService, log *logger.Logger) *AuthService {
	return &AuthService{
		cfg:      cfg,
		userRepo: userRepo,
		cognito:  aws.Cognito,
		email:    email,
		log:      log,
	}
}

type RegisterInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Name     string `json:"name" binding:"required,min=2"`
}

type RegisterOutput struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
}

func (s *AuthService) Register(ctx context.Context, input RegisterInput) (*RegisterOutput, error) {
	_, err := s.cognito.SignUp(ctx, &cognitoidentityprovider.SignUpInput{
		ClientId: aws.String(s.cfg.Cognito.ClientID),
		Username: aws.String(input.Email),
		Password: aws.String(input.Password),
		UserAttributes: []types.AttributeType{
			{Name: aws.String("email"), Value: aws.String(input.Email)},
			{Name: aws.String("name"), Value: aws.String(input.Name)},
		},
	})
	if err != nil {
		s.log.Error("cognito signup failed", "error", err)
		return nil, fmt.Errorf("registration failed: %w", err)
	}

	user := &models.User{
		Email:     input.Email,
		CognitoID: input.Email,
		Name:      input.Name,
		Role:      "viewer",
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		s.log.Error("failed to create user record", "error", err)
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &RegisterOutput{
		UserID:  user.ID,
		Message: "Please verify your email address",
	}, nil
}

type LoginInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type LoginOutput struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int32  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func (s *AuthService) Login(ctx context.Context, input LoginInput) (*LoginOutput, error) {
	result, err := s.cognito.InitiateAuth(ctx, &cognitoidentityprovider.InitiateAuthInput{
		ClientId: aws.String(s.cfg.Cognito.ClientID),
		AuthFlow: types.AuthFlowTypeUserPasswordAuth,
		AuthParameters: map[string]string{
			"USERNAME": input.Email,
			"PASSWORD": input.Password,
		},
	})
	if err != nil {
		s.log.Warn("login failed", "email", input.Email, "error", err)
		return nil, fmt.Errorf("authentication failed")
	}

	if result.AuthenticationResult == nil {
		return nil, fmt.Errorf("authentication result is nil")
	}

	return &LoginOutput{
		AccessToken:  *result.AuthenticationResult.AccessToken,
		RefreshToken: *result.AuthenticationResult.RefreshToken,
		ExpiresIn:    result.AuthenticationResult.ExpiresIn,
		TokenType:    *result.AuthenticationResult.TokenType,
	}, nil
}

type RefreshInput struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

func (s *AuthService) Refresh(ctx context.Context, input RefreshInput) (*LoginOutput, error) {
	result, err := s.cognito.InitiateAuth(ctx, &cognitoidentityprovider.InitiateAuthInput{
		ClientId: aws.String(s.cfg.Cognito.ClientID),
		AuthFlow: types.AuthFlowTypeRefreshTokenAuth,
		AuthParameters: map[string]string{
			"REFRESH_TOKEN": input.RefreshToken,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}

	if result.AuthenticationResult == nil {
		return nil, fmt.Errorf("no authentication result")
	}

	return &LoginOutput{
		AccessToken:  *result.AuthenticationResult.AccessToken,
		RefreshToken: input.RefreshToken,
		ExpiresIn:    result.AuthenticationResult.ExpiresIn,
		TokenType:    *result.AuthenticationResult.TokenType,
	}, nil
}

func (s *AuthService) Logout(ctx context.Context, accessToken string) error {
	_, err := s.cognito.GlobalSignOut(ctx, &cognitoidentityprovider.GlobalSignOutInput{
		AccessToken: aws.String(accessToken),
	})
	return err
}

type ForgotPasswordInput struct {
	Email string `json:"email" binding:"required,email"`
}

func (s *AuthService) ForgotPassword(ctx context.Context, input ForgotPasswordInput) error {
	_, err := s.cognito.ForgotPassword(ctx, &cognitoidentityprovider.ForgotPasswordInput{
		ClientId: aws.String(s.cfg.Cognito.ClientID),
		Username: aws.String(input.Email),
	})
	if err != nil {
		s.log.Error("forgot password failed", "error", err)
	}
	return nil
}

type ResetPasswordInput struct {
	Email            string `json:"email" binding:"required,email"`
	ConfirmationCode string `json:"confirmation_code" binding:"required"`
	NewPassword      string `json:"new_password" binding:"required,min=8"`
}

func (s *AuthService) ResetPassword(ctx context.Context, input ResetPasswordInput) error {
	_, err := s.cognito.ConfirmForgotPassword(ctx, &cognitoidentityprovider.ConfirmForgotPasswordInput{
		ClientId:         aws.String(s.cfg.Cognito.ClientID),
		Username:         aws.String(input.Email),
		ConfirmationCode: aws.String(input.ConfirmationCode),
		Password:         aws.String(input.NewPassword),
	})
	return err
}

type VerifyEmailInput struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required"`
}

func (s *AuthService) VerifyEmail(ctx context.Context, input VerifyEmailInput) error {
	_, err := s.cognito.ConfirmSignUp(ctx, &cognitoidentityprovider.ConfirmSignUpInput{
		ClientId:         aws.String(s.cfg.Cognito.ClientID),
		Username:         aws.String(input.Email),
		ConfirmationCode: aws.String(input.Code),
	})
	return err
}

func (s *AuthService) ResendVerification(ctx context.Context, email string) error {
	_, err := s.cognito.ResendConfirmationCode(ctx, &cognitoidentityprovider.ResendConfirmationCodeInput{
		ClientId: aws.String(s.cfg.Cognito.ClientID),
		Username: aws.String(email),
	})
	return err
}

func (s *AuthService) GetUser(ctx context.Context, userID string) (*models.User, error) {
	return s.userRepo.GetByID(ctx, userID)
}

func (s *AuthService) GetUserByCognitoID(ctx context.Context, cognitoID string) (*models.User, error) {
	return s.userRepo.GetByCognitoID(ctx, cognitoID)
}

type UpdateProfileInput struct {
	Name string `json:"name" binding:"required,min=2"`
}

func (s *AuthService) UpdateProfile(ctx context.Context, userID string, input UpdateProfileInput) (*models.User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	user.Name = input.Name
	user.UpdatedAt = time.Now()

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

type ChangePasswordInput struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

func (s *AuthService) ChangePassword(ctx context.Context, accessToken string, input ChangePasswordInput) error {
	_, err := s.cognito.ChangePassword(ctx, &cognitoidentityprovider.ChangePasswordInput{
		AccessToken:      aws.String(accessToken),
		PreviousPassword: aws.String(input.OldPassword),
		ProposedPassword: aws.String(input.NewPassword),
	})
	return err
}
