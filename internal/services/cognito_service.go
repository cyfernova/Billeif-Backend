package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/cyfernova/invoice-backend/internal/config"
	"github.com/cyfernova/invoice-backend/pkg/awsclients"
)

// CognitoService handles all Cognito operations
type CognitoService struct {
	client *awsclients.CognitoClient
	config config.CognitoConfig
}

// NewCognitoService creates a new Cognito service
func NewCognitoService(client *awsclients.CognitoClient, cfg config.CognitoConfig) *CognitoService {
	return &CognitoService{
		client: client,
		config: cfg,
	}
}

// AuthenticationResult holds the tokens from Cognito
type AuthenticationResult struct {
	AccessToken  string
	IDToken      string
	RefreshToken string
	ExpiresIn    int32
}

// CognitoUser represents user details from Cognito
type CognitoUser struct {
	Username string
	Sub      string
	Email    string
}

// SignUp registers a new user in Cognito
func (s *CognitoService) SignUp(ctx context.Context, email, password, firstName, lastName string) error {
	input := &cognitoidentityprovider.SignUpInput{
		ClientId: aws.String(s.config.ClientID),
		Username: aws.String(email),
		Password: aws.String(password),
		UserAttributes: []types.AttributeType{
			{Name: aws.String("email"), Value: aws.String(email)},
			{Name: aws.String("given_name"), Value: aws.String(firstName)},
			{Name: aws.String("family_name"), Value: aws.String(lastName)},
		},
	}

	_, err := s.client.SignUp(ctx, input)
	return err
}

// ConfirmSignUp confirms user registration with verification code
func (s *CognitoService) ConfirmSignUp(ctx context.Context, email, confirmationCode string) error {
	input := &cognitoidentityprovider.ConfirmSignUpInput{
		ClientId:         aws.String(s.config.ClientID),
		Username:         aws.String(email),
		ConfirmationCode: aws.String(confirmationCode),
	}

	_, err := s.client.ConfirmSignUp(ctx, input)
	return err
}

// SignIn authenticates user and returns tokens
func (s *CognitoService) SignIn(ctx context.Context, email, password string) (*AuthenticationResult, error) {
	authParams := map[string]string{
		"USERNAME": email,
		"PASSWORD": password,
	}

	// Add SECRET_HASH if client secret is configured
	if s.config.ClientSecret != "" {
		authParams["SECRET_HASH"] = calculateSecretHash(email, s.config.ClientSecret)
	}

	input := &cognitoidentityprovider.InitiateAuthInput{
		ClientId:   aws.String(s.config.ClientID),
		AuthFlow:   types.AuthFlowTypeUserPasswordAuth,
		AuthParameters: authParams,
	}

	resp, err := s.client.InitiateAuth(ctx, input)
	if err != nil {
		return nil, err
	}

	if resp.AuthenticationResult == nil {
		return nil, fmt.Errorf("no authentication result returned")
	}

	return &AuthenticationResult{
		AccessToken:  *resp.AuthenticationResult.AccessToken,
		IDToken:      *resp.AuthenticationResult.IdToken,
		RefreshToken: *resp.AuthenticationResult.RefreshToken,
		ExpiresIn:    resp.AuthenticationResult.ExpiresIn,
	}, nil
}

// RefreshToken refreshes access token using refresh token
func (s *CognitoService) RefreshToken(ctx context.Context, refreshToken string) (*AuthenticationResult, error) {
	authParams := map[string]string{
		"REFRESH_TOKEN": refreshToken,
	}

	// Add SECRET_HASH if client secret is configured
	if s.config.ClientSecret != "" {
		// For refresh, we need the username which should be stored with the session
		// This is a simplified version
	}

	input := &cognitoidentityprovider.InitiateAuthInput{
		ClientId:   aws.String(s.config.ClientID),
		AuthFlow:   types.AuthFlowTypeRefreshTokenAuth,
		AuthParameters: authParams,
	}

	resp, err := s.client.InitiateAuth(ctx, input)
	if err != nil {
		return nil, err
	}

	if resp.AuthenticationResult == nil {
		return nil, fmt.Errorf("no authentication result returned")
	}

	return &AuthenticationResult{
		AccessToken: *resp.AuthenticationResult.AccessToken,
		IDToken:     *resp.AuthenticationResult.IdToken,
		ExpiresIn:   resp.AuthenticationResult.ExpiresIn,
	}, nil
}

// SignOut logs out user
func (s *CognitoService) SignOut(ctx context.Context, accessToken string) error {
	_, err := s.client.GlobalSignOut(ctx, accessToken)
	return err
}

// ForgotPassword initiates password recovery
func (s *CognitoService) ForgotPassword(ctx context.Context, email string) error {
	input := &cognitoidentityprovider.ForgotPasswordInput{
		ClientId: aws.String(s.config.ClientID),
		Username: aws.String(email),
	}

	_, err := s.client.ForgotPassword(ctx, input)
	return err
}

// ConfirmForgotPassword resets password with confirmation code
func (s *CognitoService) ConfirmForgotPassword(ctx context.Context, email, confirmationCode, newPassword string) error {
	input := &cognitoidentityprovider.ConfirmForgotPasswordInput{
		ClientId:         aws.String(s.config.ClientID),
		Username:         aws.String(email),
		ConfirmationCode: aws.String(confirmationCode),
		Password:         aws.String(newPassword),
	}

	_, err := s.client.ConfirmForgotPassword(ctx, input)
	return err
}

// ChangePassword changes authenticated user's password
func (s *CognitoService) ChangePassword(ctx context.Context, accessToken, oldPassword, newPassword string) error {
	input := &cognitoidentityprovider.ChangePasswordInput{
		AccessToken:      aws.String(accessToken),
		PreviousPassword: aws.String(oldPassword),
		ProposedPassword: aws.String(newPassword),
	}

	_, err := s.client.ChangePassword(ctx, input)
	return err
}

// GetUser retrieves user details from Cognito (using access token)
func (s *CognitoService) GetUser(ctx context.Context, accessToken string) (*CognitoUser, error) {
	resp, err := s.client.GetUser(ctx, accessToken)
	if err != nil {
		return nil, err
	}

	user := &CognitoUser{
		Username: *resp.Username,
	}

	for _, attr := range resp.UserAttributes {
		if *attr.Name == "sub" {
			user.Sub = *attr.Value
		}
		if *attr.Name == "email" {
			user.Email = *attr.Value
		}
	}

	return user, nil
}

// GetUserByUsername retrieves user details from Cognito using username (admin)
func (s *CognitoService) GetUserByUsername(ctx context.Context, username string) (*CognitoUser, error) {
	resp, err := s.client.AdminGetUser(ctx, s.config.UserPoolID, username)
	if err != nil {
		return nil, err
	}

	user := &CognitoUser{
		Username: *resp.Username,
	}

	for _, attr := range resp.UserAttributes {
		if *attr.Name == "sub" {
			user.Sub = *attr.Value
		}
		if *attr.Name == "email" {
			user.Email = *attr.Value
		}
	}

	return user, nil
}

// AdminDeleteUser deletes a user from Cognito
func (s *CognitoService) AdminDeleteUser(ctx context.Context, username string) error {
	return s.client.AdminDeleteUser(ctx, s.config.UserPoolID, username)
}

// AddUserToGroup adds user to a Cognito group
func (s *CognitoService) AddUserToGroup(ctx context.Context, username, groupName string) error {
	return s.client.AdminAddUserToGroup(ctx, s.config.UserPoolID, username, groupName)
}

// ListUserGroups lists groups a user belongs to
func (s *CognitoService) ListUserGroups(ctx context.Context, username string) ([]string, error) {
	resp, err := s.client.AdminListGroupsForUser(ctx, s.config.UserPoolID, username)
	if err != nil {
		return nil, err
	}

	var groups []string
	for _, group := range resp.Groups {
		if group.GroupName != nil {
			groups = append(groups, *group.GroupName)
		}
	}

	return groups, nil
}

// calculateSecretHash calculates the SECRET_HASH for Cognito
func calculateSecretHash(username, clientSecret string) string {
	key := []byte(clientSecret)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
