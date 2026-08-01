package services

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockUserRepo struct {
	getByID          func(ctx context.Context, id string) (*models.User, error)
	getByEmail       func(ctx context.Context, email string) (*models.User, error)
	getByPhoneNumber func(ctx context.Context, phone string) (*models.User, error)
	getByCognitoID   func(ctx context.Context, cognitoID string) (*models.User, error)
	create           func(ctx context.Context, user *models.User) error
	update           func(ctx context.Context, user *models.User) error
}

func (m *mockUserRepo) Create(ctx context.Context, user *models.User) error {
	if m.create != nil {
		return m.create(ctx, user)
	}
	return nil
}

func (m *mockUserRepo) GetByID(ctx context.Context, id string) (*models.User, error) {
	if m.getByID != nil {
		return m.getByID(ctx, id)
	}
	return nil, errors.New("user not found")
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	if m.getByEmail != nil {
		return m.getByEmail(ctx, email)
	}
	return nil, errors.New("user not found")
}

func (m *mockUserRepo) GetByPhoneNumber(ctx context.Context, phone string) (*models.User, error) {
	if m.getByPhoneNumber != nil {
		return m.getByPhoneNumber(ctx, phone)
	}
	return nil, errors.New("user not found")
}

func (m *mockUserRepo) GetByCognitoID(ctx context.Context, cognitoID string) (*models.User, error) {
	if m.getByCognitoID != nil {
		return m.getByCognitoID(ctx, cognitoID)
	}
	return nil, errors.New("user not found")
}

func (m *mockUserRepo) Update(ctx context.Context, user *models.User) error {
	if m.update != nil {
		return m.update(ctx, user)
	}
	return nil
}

func (m *mockUserRepo) Delete(ctx context.Context, id string) error {
	return nil
}

func (m *mockUserRepo) List(ctx context.Context, businessID string, page, limit int) ([]*models.User, int64, error) {
	return nil, 0, nil
}

type mockCognitoClient struct {
	signUp                 func(ctx context.Context, params *cognitoidentityprovider.SignUpInput) (*cognitoidentityprovider.SignUpOutput, error)
	initiateAuth           func(ctx context.Context, params *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error)
	respondToAuthChallenge func(ctx context.Context, params *cognitoidentityprovider.RespondToAuthChallengeInput) (*cognitoidentityprovider.RespondToAuthChallengeOutput, error)
}

func (m *mockCognitoClient) ChangePassword(ctx context.Context, params *cognitoidentityprovider.ChangePasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ChangePasswordOutput, error) {
	return &cognitoidentityprovider.ChangePasswordOutput{}, nil
}

func (m *mockCognitoClient) ConfirmForgotPassword(ctx context.Context, params *cognitoidentityprovider.ConfirmForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmForgotPasswordOutput, error) {
	return &cognitoidentityprovider.ConfirmForgotPasswordOutput{}, nil
}

func (m *mockCognitoClient) ConfirmSignUp(ctx context.Context, params *cognitoidentityprovider.ConfirmSignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmSignUpOutput, error) {
	return &cognitoidentityprovider.ConfirmSignUpOutput{}, nil
}

func (m *mockCognitoClient) ForgotPassword(ctx context.Context, params *cognitoidentityprovider.ForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ForgotPasswordOutput, error) {
	return &cognitoidentityprovider.ForgotPasswordOutput{}, nil
}

func (m *mockCognitoClient) GlobalSignOut(ctx context.Context, params *cognitoidentityprovider.GlobalSignOutInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GlobalSignOutOutput, error) {
	return &cognitoidentityprovider.GlobalSignOutOutput{}, nil
}

func (m *mockCognitoClient) GetUser(ctx context.Context, params *cognitoidentityprovider.GetUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GetUserOutput, error) {
	return &cognitoidentityprovider.GetUserOutput{}, nil
}

func (m *mockCognitoClient) InitiateAuth(ctx context.Context, params *cognitoidentityprovider.InitiateAuthInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.InitiateAuthOutput, error) {
	if m.initiateAuth != nil {
		return m.initiateAuth(ctx, params)
	}
	return &cognitoidentityprovider.InitiateAuthOutput{}, nil
}

func (m *mockCognitoClient) ResendConfirmationCode(ctx context.Context, params *cognitoidentityprovider.ResendConfirmationCodeInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ResendConfirmationCodeOutput, error) {
	return &cognitoidentityprovider.ResendConfirmationCodeOutput{}, nil
}

func (m *mockCognitoClient) RespondToAuthChallenge(ctx context.Context, params *cognitoidentityprovider.RespondToAuthChallengeInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.RespondToAuthChallengeOutput, error) {
	if m.respondToAuthChallenge != nil {
		return m.respondToAuthChallenge(ctx, params)
	}
	return &cognitoidentityprovider.RespondToAuthChallengeOutput{}, nil
}

func (m *mockCognitoClient) SignUp(ctx context.Context, params *cognitoidentityprovider.SignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.SignUpOutput, error) {
	if m.signUp != nil {
		return m.signUp(ctx, params)
	}
	return &cognitoidentityprovider.SignUpOutput{}, nil
}

func newPhoneAuthService(t *testing.T, repo *mockUserRepo, phoneClient *mockCognitoClient) *AuthService {
	t.Helper()

	return &AuthService{
		cfg: &config.Config{
			Cognito: config.CognitoConfig{
				Phone: config.CognitoPhoneConfig{
					UserPoolID: "ap-south-1_phonepool",
					ClientID:   "phone-client-id",
					Region:     "ap-south-1",
				},
			},
		},
		userRepo:     repo,
		cognito:      &mockCognitoClient{},
		cognitoPhone: phoneClient,
		log:          logger.New(),
	}
}

func TestNormalizeIndianPhoneNumber(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "ten digits", input: "9876543210", want: "+919876543210"},
		{name: "with country code", input: "919876543210", want: "+919876543210"},
		{name: "with plus prefix", input: "+919876543210", want: "+919876543210"},
		{name: "with spaces and dashes", input: "+91 98765-43210", want: "+919876543210"},
		{name: "invalid mobile prefix", input: "5876543210", wantErr: true},
		{name: "too short", input: "987654321", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeIndianPhoneNumber(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPhoneRegisterRejectsEmailPrebinding(t *testing.T) {
	t.Parallel()

	repo := &mockUserRepo{}
	svc := newPhoneAuthService(t, repo, &mockCognitoClient{})
	result, err := svc.PhoneRegister(context.Background(), PhoneRegisterInput{
		PhoneNumber: "9876543210",
		Name:        "Phone User",
		Email:       "existing@example.com",
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), phoneAuthEmailUnsupported)
}

func TestPhoneRegisterRejectsExistingPhone(t *testing.T) {
	t.Parallel()

	repo := &mockUserRepo{
		getByPhoneNumber: func(ctx context.Context, phone string) (*models.User, error) {
			return &models.User{ID: "existing-user"}, nil
		},
	}

	svc := newPhoneAuthService(t, repo, &mockCognitoClient{})
	result, err := svc.PhoneRegister(context.Background(), PhoneRegisterInput{
		PhoneNumber: "+919876543210",
		Name:        "Phone User",
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), phoneAuthConflictMessage)
}

func TestPhoneRegisterUsesNormalizedPhoneAsUsername(t *testing.T) {
	t.Parallel()

	phoneClient := &mockCognitoClient{
		signUp: func(ctx context.Context, params *cognitoidentityprovider.SignUpInput) (*cognitoidentityprovider.SignUpOutput, error) {
			require.Equal(t, "phone-client-id", aws.ToString(params.ClientId))
			require.Equal(t, "+919876543210", aws.ToString(params.Username))
			return &cognitoidentityprovider.SignUpOutput{
				UserSub: aws.String("phone-sub-1"),
			}, nil
		},
	}

	repo := &mockUserRepo{
		create: func(ctx context.Context, user *models.User) error {
			require.Equal(t, "+919876543210", user.PhoneNumber)
			return nil
		},
	}

	svc := newPhoneAuthService(t, repo, phoneClient)
	result, err := svc.PhoneRegister(context.Background(), PhoneRegisterInput{
		PhoneNumber: "9876543210",
		Name:        "Phone User",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "+919876543210", result.PhoneNumber)
}

func TestPhoneRegisterDoesNotPersistUnverifiedEmail(t *testing.T) {
	t.Parallel()

	phoneClient := &mockCognitoClient{
		signUp: func(ctx context.Context, params *cognitoidentityprovider.SignUpInput) (*cognitoidentityprovider.SignUpOutput, error) {
			return &cognitoidentityprovider.SignUpOutput{UserSub: aws.String("phone-sub-1")}, nil
		},
	}
	repo := &mockUserRepo{
		create: func(ctx context.Context, user *models.User) error {
			require.Empty(t, user.Email)
			return nil
		},
	}

	svc := newPhoneAuthService(t, repo, phoneClient)
	_, err := svc.PhoneRegister(context.Background(), PhoneRegisterInput{
		PhoneNumber: "9876543210",
		Name:        "Phone User",
	})
	require.NoError(t, err)
}

func TestSyncGoogleUserRejectsPhonePreboundEmailRelink(t *testing.T) {
	t.Parallel()

	repo := &mockUserRepo{
		getByEmail: func(ctx context.Context, email string) (*models.User, error) {
			return &models.User{
				ID:        "phone-user",
				Email:     email,
				CognitoID: "ap-south-1_phonepool:phone-sub-1",
				Name:      "Phone User",
			}, nil
		},
	}
	svc := newPhoneAuthService(t, repo, &mockCognitoClient{})

	user, err := svc.SyncGoogleUser(context.Background(), SyncGoogleUserInput{
		Email:         "victim@example.com",
		EmailVerified: true,
		CognitoID:     "google-sub-1",
		Name:          "Victim",
	})

	require.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "cannot relink user across identity providers")
}

func TestSyncGoogleUserRejectsUnverifiedEmail(t *testing.T) {
	t.Parallel()

	repo := &mockUserRepo{
		create: func(ctx context.Context, user *models.User) error {
			t.Fatal("unverified Google email must not create or link a user")
			return nil
		},
	}
	svc := newPhoneAuthService(t, repo, &mockCognitoClient{})

	user, err := svc.SyncGoogleUser(context.Background(), SyncGoogleUserInput{
		Email:     "unverified@example.com",
		CognitoID: "google-sub-unverified",
		Name:      "Unverified User",
	})

	require.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "verified")
}

func TestSyncGoogleUserUsesEmailLocalPartWhenGoogleNameMissing(t *testing.T) {
	t.Parallel()

	repo := &mockUserRepo{
		create: func(ctx context.Context, user *models.User) error {
			require.Equal(t, "new.google@example.com", user.Email)
			require.Equal(t, "google-sub-1", user.CognitoID)
			require.Equal(t, "new google", user.Name)
			return nil
		},
	}
	svc := newPhoneAuthService(t, repo, &mockCognitoClient{})

	user, err := svc.SyncGoogleUser(context.Background(), SyncGoogleUserInput{
		Email:         "new.google@example.com",
		EmailVerified: true,
		CognitoID:     "google-sub-1",
	})

	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, "new google", user.Name)
}

func TestUpdateUserCognitoIDRejectsCrossProviderRelink(t *testing.T) {
	t.Parallel()

	repo := &mockUserRepo{
		getByID: func(ctx context.Context, id string) (*models.User, error) {
			return &models.User{ID: id, CognitoID: "ap-south-1_phonepool:phone-sub-1"}, nil
		},
		update: func(ctx context.Context, user *models.User) error {
			t.Fatal("Update should not be called for cross-provider relink")
			return nil
		},
	}
	svc := newPhoneAuthService(t, repo, &mockCognitoClient{})

	err := svc.UpdateUserCognitoID(context.Background(), "phone-user", "email-sub-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot relink user across identity providers")
}

func TestPhoneLoginUsesUserAuthSMSOTP(t *testing.T) {
	t.Parallel()

	phoneClient := &mockCognitoClient{
		initiateAuth: func(ctx context.Context, params *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error) {
			require.Equal(t, "phone-client-id", aws.ToString(params.ClientId))
			require.Equal(t, cognitotypes.AuthFlowTypeUserAuth, params.AuthFlow)
			require.Equal(t, "+919876543210", params.AuthParameters["USERNAME"])
			require.Equal(t, "SMS_OTP", params.AuthParameters["PREFERRED_CHALLENGE"])

			return &cognitoidentityprovider.InitiateAuthOutput{
				ChallengeName: cognitotypes.ChallengeNameTypeSmsOtp,
				Session:       aws.String("session-123"),
			}, nil
		},
	}

	repo := &mockUserRepo{
		getByPhoneNumber: func(ctx context.Context, phone string) (*models.User, error) {
			return &models.User{ID: "user-1", PhoneNumber: phone}, nil
		},
	}

	svc := newPhoneAuthService(t, repo, phoneClient)
	result, err := svc.PhoneLogin(context.Background(), PhoneLoginInput{PhoneNumber: "9876543210"})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "session-123", result.Session)
	assert.Equal(t, "SMS_OTP", result.ChallengeName)
}

func TestPhoneLoginRejectsUnregisteredNumber(t *testing.T) {
	t.Parallel()

	repo := &mockUserRepo{
		getByPhoneNumber: func(ctx context.Context, phone string) (*models.User, error) {
			return nil, errors.New("user not found")
		},
	}

	phoneClient := &mockCognitoClient{
		initiateAuth: func(ctx context.Context, params *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error) {
			t.Fatalf("initiateAuth should not be called for unregistered numbers")
			return nil, nil
		},
	}

	svc := newPhoneAuthService(t, repo, phoneClient)
	result, err := svc.PhoneLogin(context.Background(), PhoneLoginInput{PhoneNumber: "9876543210"})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "phone number not registered")
}

func TestPhoneVerifyLoginReturnsTokens(t *testing.T) {
	t.Parallel()

	phoneClient := &mockCognitoClient{
		respondToAuthChallenge: func(ctx context.Context, params *cognitoidentityprovider.RespondToAuthChallengeInput) (*cognitoidentityprovider.RespondToAuthChallengeOutput, error) {
			require.Equal(t, "phone-client-id", aws.ToString(params.ClientId))
			require.Equal(t, cognitotypes.ChallengeNameTypeSmsOtp, params.ChallengeName)
			require.Equal(t, "+919876543210", params.ChallengeResponses["USERNAME"])
			require.Equal(t, "123456", params.ChallengeResponses["SMS_OTP_CODE"])
			require.Equal(t, "session-123", aws.ToString(params.Session))

			return &cognitoidentityprovider.RespondToAuthChallengeOutput{
				AuthenticationResult: &cognitotypes.AuthenticationResultType{
					AccessToken:  aws.String("access"),
					RefreshToken: aws.String("refresh"),
					TokenType:    aws.String("Bearer"),
					ExpiresIn:    3600,
				},
			}, nil
		},
	}

	svc := newPhoneAuthService(t, &mockUserRepo{}, phoneClient)
	result, err := svc.PhoneVerifyLogin(context.Background(), PhoneVerifyLoginInput{
		PhoneNumber: "919876543210",
		Code:        "123456",
		Session:     "session-123",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "access", result.AccessToken)
	assert.Equal(t, "refresh", result.RefreshToken)
	assert.Equal(t, int32(3600), result.ExpiresIn)
}

func TestClassifyPhoneAuthErrorRateLimit(t *testing.T) {
	t.Parallel()

	err := classifyPhoneAuthError(&cognitotypes.LimitExceededException{})
	require.Error(t, err)
	assert.Equal(t, phoneAuthRateLimitMessage, err.Error())
}

func TestClassifyPhoneAuthErrorOTPValidation(t *testing.T) {
	t.Parallel()

	invalidCodeErr := classifyPhoneAuthError(&cognitotypes.CodeMismatchException{})
	require.Error(t, invalidCodeErr)
	assert.Equal(t, "invalid OTP code", invalidCodeErr.Error())

	expiredCodeErr := classifyPhoneAuthError(&cognitotypes.ExpiredCodeException{})
	require.Error(t, expiredCodeErr)
	assert.Equal(t, "OTP code expired, please request a new code", expiredCodeErr.Error())
}
