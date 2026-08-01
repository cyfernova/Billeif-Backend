package unit

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func testStringPtr(value string) *string {
	return &value
}

// MockUserRepository mocks the UserRepository interface
type MockUserRepository struct {
	mock.Mock
}

func (m *MockUserRepository) Create(ctx context.Context, user *models.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockUserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) GetByPhoneNumber(ctx context.Context, phoneNumber string) (*models.User, error) {
	args := m.Called(ctx, phoneNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) GetByCognitoID(ctx context.Context, cognitoID string) (*models.User, error) {
	args := m.Called(ctx, cognitoID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) Update(ctx context.Context, user *models.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockUserRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockUserRepository) List(ctx context.Context, businessID string, page, limit int) ([]*models.User, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.User), args.Get(1).(int64), args.Error(2)
}

// MockCognitoIdentityProviderAPI mocks the cognitoIdentityProviderAPI interface
type MockCognitoIdentityProviderAPI struct {
	mock.Mock
}

func (m *MockCognitoIdentityProviderAPI) SignUp(ctx context.Context, params *cognitoidentityprovider.SignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.SignUpOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cognitoidentityprovider.SignUpOutput), args.Error(1)
}

func (m *MockCognitoIdentityProviderAPI) InitiateAuth(ctx context.Context, params *cognitoidentityprovider.InitiateAuthInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.InitiateAuthOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cognitoidentityprovider.InitiateAuthOutput), args.Error(1)
}

func (m *MockCognitoIdentityProviderAPI) GlobalSignOut(ctx context.Context, params *cognitoidentityprovider.GlobalSignOutInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GlobalSignOutOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cognitoidentityprovider.GlobalSignOutOutput), args.Error(1)
}

func (m *MockCognitoIdentityProviderAPI) ForgotPassword(ctx context.Context, params *cognitoidentityprovider.ForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ForgotPasswordOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cognitoidentityprovider.ForgotPasswordOutput), args.Error(1)
}

func (m *MockCognitoIdentityProviderAPI) ConfirmForgotPassword(ctx context.Context, params *cognitoidentityprovider.ConfirmForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmForgotPasswordOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cognitoidentityprovider.ConfirmForgotPasswordOutput), args.Error(1)
}

func (m *MockCognitoIdentityProviderAPI) ConfirmSignUp(ctx context.Context, params *cognitoidentityprovider.ConfirmSignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmSignUpOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cognitoidentityprovider.ConfirmSignUpOutput), args.Error(1)
}

func (m *MockCognitoIdentityProviderAPI) ResendConfirmationCode(ctx context.Context, params *cognitoidentityprovider.ResendConfirmationCodeInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ResendConfirmationCodeOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cognitoidentityprovider.ResendConfirmationCodeOutput), args.Error(1)
}

func (m *MockCognitoIdentityProviderAPI) ChangePassword(ctx context.Context, params *cognitoidentityprovider.ChangePasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ChangePasswordOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cognitoidentityprovider.ChangePasswordOutput), args.Error(1)
}

func (m *MockCognitoIdentityProviderAPI) GetUser(ctx context.Context, params *cognitoidentityprovider.GetUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GetUserOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cognitoidentityprovider.GetUserOutput), args.Error(1)
}

func (m *MockCognitoIdentityProviderAPI) RespondToAuthChallenge(ctx context.Context, params *cognitoidentityprovider.RespondToAuthChallengeInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.RespondToAuthChallengeOutput, error) {
	args := m.Called(ctx, params, optFns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cognitoidentityprovider.RespondToAuthChallengeOutput), args.Error(1)
}

// TestAuthService_Register tests the Register method
func TestAuthService_Register(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.RegisterInput{
		Email:    "test@example.com",
		Password: "password123!",
		Name:     "Test User",
	}

	userSub := "user-sub-123"
	mockCognito.On("SignUp", ctx, mock.AnythingOfType("*cognitoidentityprovider.SignUpInput"), mock.Anything).Return(&cognitoidentityprovider.SignUpOutput{
		UserSub: aws.String(userSub),
		CodeDeliveryDetails: &types.CodeDeliveryDetailsType{
			DeliveryMedium: types.DeliveryMediumTypeEmail,
		},
	}, nil)

	mockUserRepo.On("Create", ctx, mock.AnythingOfType("*models.User")).Return(nil)

	result, err := svc.Register(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Please verify your email address", result.Message)
	mockCognito.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
}

func TestAuthService_Register_TriggersFallbackResendWhenDeliveryMissing(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.RegisterInput{
		Email:    "test@example.com",
		Password: "password123!",
		Name:     "Test User",
	}

	userSub := "user-sub-123"
	mockCognito.On("SignUp", ctx, mock.AnythingOfType("*cognitoidentityprovider.SignUpInput"), mock.Anything).Return(&cognitoidentityprovider.SignUpOutput{
		UserSub: aws.String(userSub),
	}, nil)
	mockCognito.On("ResendConfirmationCode", ctx, mock.AnythingOfType("*cognitoidentityprovider.ResendConfirmationCodeInput"), mock.Anything).Return(&cognitoidentityprovider.ResendConfirmationCodeOutput{}, nil)
	mockUserRepo.On("Create", ctx, mock.AnythingOfType("*models.User")).Return(nil)

	result, err := svc.Register(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Please verify your email address", result.Message)
	mockCognito.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
}

func TestAuthService_Register_ExistingUnverifiedUserReturnsVerificationMessage(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.RegisterInput{
		Email:    "test@example.com",
		Password: "password123!",
		Name:     "Test User",
	}

	mockCognito.On("SignUp", ctx, mock.AnythingOfType("*cognitoidentityprovider.SignUpInput"), mock.Anything).Return(nil, &types.UsernameExistsException{})
	mockCognito.On("ResendConfirmationCode", ctx, mock.AnythingOfType("*cognitoidentityprovider.ResendConfirmationCodeInput"), mock.Anything).Return(&cognitoidentityprovider.ResendConfirmationCodeOutput{}, nil)

	result, err := svc.Register(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Contains(t, result.Message, "verify your email")
	mockCognito.AssertExpectations(t)
}

func TestAuthService_Register_ExistingConfirmedUserReturnsConflictError(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.RegisterInput{
		Email:    "test@example.com",
		Password: "password123!",
		Name:     "Test User",
	}

	mockCognito.On("SignUp", ctx, mock.AnythingOfType("*cognitoidentityprovider.SignUpInput"), mock.Anything).Return(nil, &types.UsernameExistsException{})
	mockCognito.On("ResendConfirmationCode", ctx, mock.AnythingOfType("*cognitoidentityprovider.ResendConfirmationCodeInput"), mock.Anything).Return(nil, &types.InvalidParameterException{
		Message: aws.String("User is already confirmed"),
	})

	result, err := svc.Register(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "email already registered")
	mockCognito.AssertExpectations(t)
}

// TestAuthService_Register_CognitoError tests Register when Cognito fails
func TestAuthService_Register_CognitoError(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.RegisterInput{
		Email:    "test@example.com",
		Password: "password123!",
		Name:     "Test User",
	}

	mockCognito.On("SignUp", ctx, mock.AnythingOfType("*cognitoidentityprovider.SignUpInput"), mock.Anything).Return(nil, errors.New("cognito error"))

	result, err := svc.Register(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "registration failed")
	mockCognito.AssertExpectations(t)
}

// TestAuthService_Login tests the Login method
func TestAuthService_Login(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.LoginInput{
		Email:    "test@example.com",
		Password: "password123!",
	}

	mockCognito.On("InitiateAuth", ctx, mock.AnythingOfType("*cognitoidentityprovider.InitiateAuthInput"), mock.Anything).Return(&cognitoidentityprovider.InitiateAuthOutput{
		AuthenticationResult: &types.AuthenticationResultType{
			AccessToken:  aws.String("access-token"),
			RefreshToken: aws.String("refresh-token"),
			ExpiresIn:    3600,
			TokenType:    aws.String("Bearer"),
		},
	}, nil)
	mockCognito.On("GetUser", ctx, mock.AnythingOfType("*cognitoidentityprovider.GetUserInput"), mock.Anything).Return(&cognitoidentityprovider.GetUserOutput{
		UserAttributes: []types.AttributeType{
			{Name: aws.String("sub"), Value: aws.String("cognito-sub-123")},
			{Name: aws.String("email"), Value: aws.String("test@example.com")},
			{Name: aws.String("name"), Value: aws.String("Test User")},
		},
	}, nil)
	mockUserRepo.On("GetByCognitoID", ctx, "cognito-sub-123").Return(nil, errors.New("user not found"))
	mockUserRepo.On("GetByEmail", ctx, "test@example.com").Return(nil, errors.New("user not found"))
	mockUserRepo.On("Create", ctx, mock.AnythingOfType("*models.User")).Return(nil)

	result, err := svc.Login(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "access-token", result.AccessToken)
	assert.Equal(t, "refresh-token", result.RefreshToken)
	assert.Equal(t, int32(3600), result.ExpiresIn)
	assert.Equal(t, "Bearer", result.TokenType)
	mockCognito.AssertExpectations(t)
}

// TestAuthService_Login_InvalidCredentials tests Login with invalid credentials
func TestAuthService_Login_InvalidCredentials(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.LoginInput{
		Email:    "test@example.com",
		Password: "wrongpassword",
	}

	mockCognito.On("InitiateAuth", ctx, mock.AnythingOfType("*cognitoidentityprovider.InitiateAuthInput"), mock.Anything).Return(nil, &types.NotAuthorizedException{})

	result, err := svc.Login(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, "incorrect email or password", err.Error())
	mockCognito.AssertExpectations(t)
}

func TestAuthService_Login_UnverifiedEmail(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.LoginInput{
		Email:    "test@example.com",
		Password: "password123!",
	}

	mockCognito.On("InitiateAuth", ctx, mock.AnythingOfType("*cognitoidentityprovider.InitiateAuthInput"), mock.Anything).Return(nil, &types.UserNotConfirmedException{})

	result, err := svc.Login(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, "Please verify your email address", err.Error())
	mockCognito.AssertExpectations(t)
}

// TestAuthService_Logout tests the Logout method
func TestAuthService_Logout(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	accessToken := "valid-access-token"

	mockCognito.On("GlobalSignOut", ctx, mock.AnythingOfType("*cognitoidentityprovider.GlobalSignOutInput"), mock.Anything).Return(&cognitoidentityprovider.GlobalSignOutOutput{}, nil)

	err := svc.Logout(ctx, accessToken)

	assert.NoError(t, err)
	mockCognito.AssertExpectations(t)
}

// TestAuthService_Refresh tests the Refresh method
func TestAuthService_Refresh(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.RefreshInput{
		RefreshToken: "valid-refresh-token",
	}

	mockCognito.On("InitiateAuth", ctx, mock.AnythingOfType("*cognitoidentityprovider.InitiateAuthInput"), mock.Anything).Return(&cognitoidentityprovider.InitiateAuthOutput{
		AuthenticationResult: &types.AuthenticationResultType{
			AccessToken:  aws.String("new-access-token"),
			RefreshToken: aws.String("new-refresh-token"),
			ExpiresIn:    3600,
			TokenType:    aws.String("Bearer"),
		},
	}, nil)

	result, err := svc.Refresh(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "new-access-token", result.AccessToken)
	assert.Equal(t, "new-refresh-token", result.RefreshToken)
	mockCognito.AssertExpectations(t)
}

// TestAuthService_ForgotPassword tests the ForgotPassword method
func TestAuthService_ForgotPassword(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.ForgotPasswordInput{
		Email: "test@example.com",
	}

	mockCognito.On("ForgotPassword", ctx, mock.AnythingOfType("*cognitoidentityprovider.ForgotPasswordInput"), mock.Anything).Return(&cognitoidentityprovider.ForgotPasswordOutput{}, nil)

	err := svc.ForgotPassword(ctx, input)

	assert.NoError(t, err)
	mockCognito.AssertExpectations(t)
}

// TestAuthService_ResetPassword tests the ResetPassword method
func TestAuthService_ResetPassword(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.ResetPasswordInput{
		Email:            "test@example.com",
		ConfirmationCode: "123456",
		NewPassword:      "newpassword123!",
	}

	mockCognito.On("ConfirmForgotPassword", ctx, mock.AnythingOfType("*cognitoidentityprovider.ConfirmForgotPasswordInput"), mock.Anything).Return(&cognitoidentityprovider.ConfirmForgotPasswordOutput{}, nil)

	err := svc.ResetPassword(ctx, input)

	assert.NoError(t, err)
	mockCognito.AssertExpectations(t)
}

// TestAuthService_VerifyEmail tests the VerifyEmail method
func TestAuthService_VerifyEmail(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.VerifyEmailInput{
		Email: "test@example.com",
		Code:  "123456",
	}

	mockCognito.On("ConfirmSignUp", ctx, mock.AnythingOfType("*cognitoidentityprovider.ConfirmSignUpInput"), mock.Anything).Return(&cognitoidentityprovider.ConfirmSignUpOutput{}, nil)

	err := svc.VerifyEmail(ctx, input)

	assert.NoError(t, err)
	mockCognito.AssertExpectations(t)
}

// TestAuthService_ResendVerification tests the ResendVerification method
func TestAuthService_ResendVerification(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	email := "test@example.com"

	mockCognito.On("ResendConfirmationCode", ctx, mock.AnythingOfType("*cognitoidentityprovider.ResendConfirmationCodeInput"), mock.Anything).Return(&cognitoidentityprovider.ResendConfirmationCodeOutput{}, nil)

	err := svc.ResendVerification(ctx, email)

	assert.NoError(t, err)
	mockCognito.AssertExpectations(t)
}

// TestAuthService_GetUser tests the GetUser method
func TestAuthService_GetUser(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	userID := "user-123"
	expectedUser := &models.User{
		ID:    userID,
		Email: "test@example.com",
		Name:  "Test User",
		Role:  "viewer",
	}

	mockUserRepo.On("GetByID", ctx, userID).Return(expectedUser, nil)

	user, err := svc.GetUser(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, expectedUser.ID, user.ID)
	assert.Equal(t, expectedUser.Email, user.Email)
	mockUserRepo.AssertExpectations(t)
}

// TestAuthService_GetUser_NotFound tests GetUser when user doesn't exist
func TestAuthService_GetUser_NotFound(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	userID := "nonexistent-user"

	mockUserRepo.On("GetByID", ctx, userID).Return(nil, errors.New("user not found"))

	user, err := svc.GetUser(ctx, userID)

	assert.Error(t, err)
	assert.Nil(t, user)
	mockUserRepo.AssertExpectations(t)
}

// TestAuthService_GetUserByEmail tests the GetUserByEmail method
func TestAuthService_GetUserByEmail(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	email := "test@example.com"
	expectedUser := &models.User{
		ID:    "user-123",
		Email: email,
		Name:  "Test User",
	}

	mockUserRepo.On("GetByEmail", ctx, email).Return(expectedUser, nil)

	user, err := svc.GetUserByEmail(ctx, email)

	assert.NoError(t, err)
	assert.Equal(t, expectedUser.Email, user.Email)
	mockUserRepo.AssertExpectations(t)
}

// TestAuthService_UpdateProfile tests the UpdateProfile method
func TestAuthService_UpdateProfile(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	userID := "user-123"
	existingUser := &models.User{
		ID:          userID,
		Email:       "test@example.com",
		PhoneNumber: "+919999999999",
		Name:        "Old Name",
		Role:        "viewer",
	}

	input := services.UpdateProfileInput{
		Name:        testStringPtr("New Name"),
		Email:       testStringPtr("new@example.com"),
		PhoneNumber: testStringPtr("9876543210"),
	}

	mockUserRepo.On("GetByID", ctx, userID).Return(existingUser, nil)
	mockUserRepo.On("GetByEmail", ctx, "new@example.com").Return(nil, errors.New("user not found"))
	mockUserRepo.On("GetByPhoneNumber", ctx, "+919876543210").Return(nil, errors.New("user not found"))
	mockUserRepo.On("Update", ctx, mock.AnythingOfType("*models.User")).Return(nil)

	user, err := svc.UpdateProfile(ctx, userID, input)

	assert.NoError(t, err)
	assert.Equal(t, "New Name", user.Name)
	assert.Equal(t, "new@example.com", user.Email)
	assert.Equal(t, "+919876543210", user.PhoneNumber)
	mockUserRepo.AssertExpectations(t)
}

func TestAuthService_UpdateProfileRejectsInvalidPhone(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	svc := services.NewAuthServiceWithMocks(&services.TestAuthConfig{}, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	userID := "user-123"
	existingUser := &models.User{
		ID:    userID,
		Email: "test@example.com",
		Name:  "Old Name",
		Role:  "viewer",
	}

	input := services.UpdateProfileInput{
		Name:        testStringPtr("New Name"),
		PhoneNumber: testStringPtr("12345"),
	}

	mockUserRepo.On("GetByID", ctx, userID).Return(existingUser, nil)

	user, err := svc.UpdateProfile(ctx, userID, input)

	assert.Nil(t, user)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "phone_number must be a valid Indian mobile number")
	mockUserRepo.AssertExpectations(t)
	mockUserRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

func TestAuthService_UpdateProfileAllowsPartialPhoneOnly(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	svc := services.NewAuthServiceWithMocks(&services.TestAuthConfig{}, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	userID := "user-123"
	existingUser := &models.User{
		ID:          userID,
		Email:       "test@example.com",
		PhoneNumber: "+919999999999",
		Name:        "Existing Name",
		Role:        "viewer",
	}

	mockUserRepo.On("GetByID", ctx, userID).Return(existingUser, nil)
	mockUserRepo.On("GetByPhoneNumber", ctx, "+919876543210").Return(nil, errors.New("user not found"))
	mockUserRepo.On("Update", ctx, mock.AnythingOfType("*models.User")).Return(nil)

	user, err := svc.UpdateProfile(ctx, userID, services.UpdateProfileInput{
		PhoneNumber: testStringPtr("9876543210"),
	})

	assert.NoError(t, err)
	assert.Equal(t, "Existing Name", user.Name)
	assert.Equal(t, "+919876543210", user.PhoneNumber)
	mockUserRepo.AssertExpectations(t)
}

func TestAuthService_UpdateProfileRejectsNoopPayload(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	svc := services.NewAuthServiceWithMocks(&services.TestAuthConfig{}, mockUserRepo, mockCognito, nil, nil, log)

	user, err := svc.UpdateProfile(context.Background(), "user-123", services.UpdateProfileInput{})

	assert.Nil(t, user)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one profile field")
	mockUserRepo.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything)
}

// TestAuthService_ChangePassword tests the ChangePassword method
func TestAuthService_ChangePassword(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{
		CognitoClientID: "test-client-id",
	}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	accessToken := "valid-access-token"
	input := services.ChangePasswordInput{
		OldPassword: "oldpassword123!",
		NewPassword: "newpassword123!",
	}

	mockCognito.On("ChangePassword", ctx, mock.AnythingOfType("*cognitoidentityprovider.ChangePasswordInput"), mock.Anything).Return(&cognitoidentityprovider.ChangePasswordOutput{}, nil)

	err := svc.ChangePassword(ctx, accessToken, input)

	assert.NoError(t, err)
	mockCognito.AssertExpectations(t)
}

// TestAuthService_SyncGoogleUser tests the SyncGoogleUser method for existing user
func TestAuthService_SyncGoogleUser_ExistingUser(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	existingUser := &models.User{
		ID:                "user-123",
		Email:             "test@example.com",
		Name:              "Old Name",
		Role:              "viewer",
		ProfilePictureURL: "",
	}

	input := services.SyncGoogleUserInput{
		Email:             "test@example.com",
		EmailVerified:     true,
		CognitoID:         "cognito-id-123",
		Name:              "New Name",
		ProfilePictureURL: "https://example.com/new-pic.jpg",
	}

	mockUserRepo.On("GetByCognitoID", ctx, input.CognitoID).Return(existingUser, nil)
	mockUserRepo.On("Update", ctx, mock.AnythingOfType("*models.User")).Return(nil)

	user, err := svc.SyncGoogleUser(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, existingUser.ID, user.ID)
	assert.Equal(t, input.ProfilePictureURL, user.ProfilePictureURL)
	mockUserRepo.AssertExpectations(t)
}

func TestAuthService_SyncGoogleUser_PreservesExistingProfilePicture(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	svc := services.NewAuthServiceWithMocks(&services.TestAuthConfig{}, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	existingUser := &models.User{
		ID:                "user-123",
		Email:             "test@example.com",
		Name:              "Existing User",
		Role:              "viewer",
		ProfilePictureURL: "https://example.com/current-pic.jpg",
	}
	input := services.SyncGoogleUserInput{
		Email:             "test@example.com",
		EmailVerified:     true,
		CognitoID:         "cognito-id-123",
		Name:              "Google User",
		ProfilePictureURL: "https://example.com/google-pic.jpg",
	}

	mockUserRepo.On("GetByCognitoID", ctx, input.CognitoID).Return(existingUser, nil)

	user, err := svc.SyncGoogleUser(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "https://example.com/current-pic.jpg", user.ProfilePictureURL)
	mockUserRepo.AssertExpectations(t)
	mockUserRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestAuthService_SyncGoogleUser_NewUser tests SyncGoogleUser when user doesn't exist
func TestAuthService_SyncGoogleUser_NewUser(t *testing.T) {
	mockCognito := new(MockCognitoIdentityProviderAPI)
	mockUserRepo := new(MockUserRepository)
	log := logger.New()

	cfg := &services.TestAuthConfig{}

	svc := services.NewAuthServiceWithMocks(cfg, mockUserRepo, mockCognito, nil, nil, log)

	ctx := context.Background()
	input := services.SyncGoogleUserInput{
		Email:         "newuser@example.com",
		EmailVerified: true,
		CognitoID:     "new-cognito-id",
		Name:          "New User",
	}

	mockUserRepo.On("GetByCognitoID", ctx, input.CognitoID).Return(nil, errors.New("user not found"))
	mockUserRepo.On("GetByEmail", ctx, input.Email).Return(nil, errors.New("user not found"))
	mockUserRepo.On("Create", ctx, mock.AnythingOfType("*models.User")).Return(nil)

	user, err := svc.SyncGoogleUser(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, input.Email, user.Email)
	assert.Equal(t, input.Name, user.Name)
	assert.Equal(t, "viewer", user.Role)
	mockUserRepo.AssertExpectations(t)
}

// TestAuthService_NormalizeIndianPhoneNumber tests phone number normalization
func TestAuthService_NormalizeIndianPhoneNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		hasError bool
	}{
		{
			name:     "Valid phone with +91 prefix",
			input:    "+919876543210",
			expected: "+919876543210",
			hasError: false,
		},
		{
			name:     "Valid phone without prefix",
			input:    "9876543210",
			expected: "+919876543210",
			hasError: false,
		},
		{
			name:     "Valid phone with spaces",
			input:    "98765 43210",
			expected: "+919876543210",
			hasError: false,
		},
		{
			name:     "Invalid phone - too short",
			input:    "98765",
			expected: "",
			hasError: true,
		},
		{
			name:     "Invalid phone - starts with 5",
			input:    "59876543210",
			expected: "",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := services.NormalizeIndianPhoneNumber(tt.input)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
