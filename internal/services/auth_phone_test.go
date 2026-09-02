package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

type mockPhoneLinkAuditRepository struct {
	repo   *mockUserRepo
	events []*models.SecurityAuditEvent
}

func (m *mockPhoneLinkAuditRepository) LinkUserPhoneWithAudit(ctx context.Context, userID, phoneNumber string, updatedAt time.Time, event *models.SecurityAuditEvent) (bool, error) {
	user, err := m.repo.GetByID(ctx, userID)
	if err != nil || user == nil || user.PhoneNumber != "" {
		return false, err
	}
	user.PhoneNumber = phoneNumber
	user.UpdatedAt = updatedAt
	if err := m.repo.Update(ctx, user); err != nil {
		return false, nil
	}
	m.events = append(m.events, event)
	return true, nil
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
	confirmSignUp          func(ctx context.Context, params *cognitoidentityprovider.ConfirmSignUpInput) (*cognitoidentityprovider.ConfirmSignUpOutput, error)
	resendConfirmationCode func(ctx context.Context, params *cognitoidentityprovider.ResendConfirmationCodeInput) (*cognitoidentityprovider.ResendConfirmationCodeOutput, error)
	initiateAuth           func(ctx context.Context, params *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error)
	respondToAuthChallenge func(ctx context.Context, params *cognitoidentityprovider.RespondToAuthChallengeInput) (*cognitoidentityprovider.RespondToAuthChallengeOutput, error)
	globalSignOut          func(ctx context.Context, params *cognitoidentityprovider.GlobalSignOutInput) (*cognitoidentityprovider.GlobalSignOutOutput, error)
}

func (m *mockCognitoClient) ChangePassword(ctx context.Context, params *cognitoidentityprovider.ChangePasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ChangePasswordOutput, error) {
	return &cognitoidentityprovider.ChangePasswordOutput{}, nil
}

func (m *mockCognitoClient) ConfirmForgotPassword(ctx context.Context, params *cognitoidentityprovider.ConfirmForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmForgotPasswordOutput, error) {
	return &cognitoidentityprovider.ConfirmForgotPasswordOutput{}, nil
}

func (m *mockCognitoClient) ConfirmSignUp(ctx context.Context, params *cognitoidentityprovider.ConfirmSignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmSignUpOutput, error) {
	if m.confirmSignUp != nil {
		return m.confirmSignUp(ctx, params)
	}
	return &cognitoidentityprovider.ConfirmSignUpOutput{}, nil
}

func (m *mockCognitoClient) ForgotPassword(ctx context.Context, params *cognitoidentityprovider.ForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ForgotPasswordOutput, error) {
	return &cognitoidentityprovider.ForgotPasswordOutput{}, nil
}

func (m *mockCognitoClient) GlobalSignOut(ctx context.Context, params *cognitoidentityprovider.GlobalSignOutInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GlobalSignOutOutput, error) {
	if m.globalSignOut != nil {
		return m.globalSignOut(ctx, params)
	}
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
	if m.resendConfirmationCode != nil {
		return m.resendConfirmationCode(ctx, params)
	}
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

func (m *mockCognitoClient) AssociateSoftwareToken(context.Context, *cognitoidentityprovider.AssociateSoftwareTokenInput, ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AssociateSoftwareTokenOutput, error) {
	return &cognitoidentityprovider.AssociateSoftwareTokenOutput{}, nil
}
func (m *mockCognitoClient) VerifySoftwareToken(context.Context, *cognitoidentityprovider.VerifySoftwareTokenInput, ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.VerifySoftwareTokenOutput, error) {
	return &cognitoidentityprovider.VerifySoftwareTokenOutput{}, nil
}
func (m *mockCognitoClient) SetUserMFAPreference(context.Context, *cognitoidentityprovider.SetUserMFAPreferenceInput, ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.SetUserMFAPreferenceOutput, error) {
	return &cognitoidentityprovider.SetUserMFAPreferenceOutput{}, nil
}
func (m *mockCognitoClient) ListDevices(context.Context, *cognitoidentityprovider.ListDevicesInput, ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ListDevicesOutput, error) {
	return &cognitoidentityprovider.ListDevicesOutput{}, nil
}
func (m *mockCognitoClient) ForgetDevice(context.Context, *cognitoidentityprovider.ForgetDeviceInput, ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ForgetDeviceOutput, error) {
	return &cognitoidentityprovider.ForgetDeviceOutput{}, nil
}
func (m *mockCognitoClient) UpdateDeviceStatus(context.Context, *cognitoidentityprovider.UpdateDeviceStatusInput, ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.UpdateDeviceStatusOutput, error) {
	return &cognitoidentityprovider.UpdateDeviceStatusOutput{}, nil
}

func newPhoneAuthService(t *testing.T, repo *mockUserRepo, phoneClient *mockCognitoClient) *AuthService {
	t.Helper()

	service := &AuthService{
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
	service.phoneLink = &mockPhoneLinkAuditRepository{repo: repo}
	return service
}

func TestEmailLoginCompletesSoftwareTokenMFAChallenge(t *testing.T) {
	client := &mockCognitoClient{
		initiateAuth: func(context.Context, *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error) {
			return &cognitoidentityprovider.InitiateAuthOutput{ChallengeName: cognitotypes.ChallengeNameTypeSoftwareTokenMfa, Session: aws.String("mfa-session")}, nil
		},
		respondToAuthChallenge: func(_ context.Context, input *cognitoidentityprovider.RespondToAuthChallengeInput) (*cognitoidentityprovider.RespondToAuthChallengeOutput, error) {
			if input.ChallengeName != cognitotypes.ChallengeNameTypeSoftwareTokenMfa || aws.ToString(input.Session) != "mfa-session" || input.ChallengeResponses["SOFTWARE_TOKEN_MFA_CODE"] != "123456" {
				t.Fatalf("challenge input=%+v", input)
			}
			return &cognitoidentityprovider.RespondToAuthChallengeOutput{AuthenticationResult: &cognitotypes.AuthenticationResultType{
				AccessToken: aws.String("access"), RefreshToken: aws.String("refresh"), TokenType: aws.String("Bearer"), ExpiresIn: 3600,
			}}, nil
		},
	}
	service := &AuthService{cfg: &config.Config{Cognito: config.CognitoConfig{ClientID: "client"}}, userRepo: &mockUserRepo{}, cognito: client, log: logger.New()}
	started, err := service.Login(context.Background(), LoginInput{Email: "user@example.com", Password: "password"})
	if err != nil || started.Challenge != string(cognitotypes.ChallengeNameTypeSoftwareTokenMfa) || started.Session != "mfa-session" {
		t.Fatalf("started=%+v err=%v", started, err)
	}
	completed, err := service.CompleteLoginMFA(context.Background(), LoginMFAInput{Username: "user@example.com", Session: started.Session, Code: "123456"})
	if err != nil || completed.AccessToken != "access" || completed.RefreshToken != "refresh" {
		t.Fatalf("completed=%+v err=%v", completed, err)
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

func TestPhoneRegisterDoesNotRevealExistingPhone(t *testing.T) {
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

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "+919876543210", result.PhoneNumber)
	assert.Equal(t, defaultPhoneRegisterPrompt, result.Message)
}

func TestPhoneRegisterNormalizesProviderCollisionResponse(t *testing.T) {
	t.Parallel()

	client := &mockCognitoClient{
		signUp: func(context.Context, *cognitoidentityprovider.SignUpInput) (*cognitoidentityprovider.SignUpOutput, error) {
			return nil, &cognitotypes.UsernameExistsException{}
		},
	}

	result, err := newPhoneAuthService(t, &mockUserRepo{}, client).PhoneRegister(context.Background(), PhoneRegisterInput{
		PhoneNumber: "9876543210", Name: "Phone User",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, defaultPhoneRegisterPrompt, result.Message)
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

func TestPhoneLoginDefersRegistrationKnowledgeToProvider(t *testing.T) {
	t.Parallel()

	repo := &mockUserRepo{
		getByPhoneNumber: func(ctx context.Context, phone string) (*models.User, error) {
			return nil, errors.New("user not found")
		},
	}

	phoneClient := &mockCognitoClient{
		initiateAuth: func(ctx context.Context, params *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error) {
			return nil, &cognitotypes.UserNotFoundException{}
		},
	}

	svc := newPhoneAuthService(t, repo, phoneClient)
	result, err := svc.PhoneLogin(context.Background(), PhoneLoginInput{PhoneNumber: "9876543210"})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, phoneAuthGenericFailure, err.Error())
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

func TestPhoneLoginDoesNotRevealWhetherPhoneIsRegistered(t *testing.T) {
	t.Parallel()

	providerCalled := false
	phoneClient := &mockCognitoClient{
		initiateAuth: func(context.Context, *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error) {
			providerCalled = true
			return nil, &cognitotypes.UserNotFoundException{}
		},
	}
	repo := &mockUserRepo{
		getByPhoneNumber: func(context.Context, string) (*models.User, error) {
			return nil, errors.New("user not found")
		},
	}

	result, err := newPhoneAuthService(t, repo, phoneClient).PhoneLogin(context.Background(), PhoneLoginInput{PhoneNumber: "9876543210"})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, providerCalled)
	assert.Equal(t, phoneAuthGenericFailure, err.Error())
}

func TestPhoneLoginFailsClosedWhenConfiguredCooldownStoreIsUnavailable(t *testing.T) {
	t.Parallel()

	client := &mockCognitoClient{
		initiateAuth: func(context.Context, *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error) {
			t.Fatal("provider must not be called without the configured cooldown store")
			return nil, nil
		},
	}
	svc := newPhoneAuthService(t, &mockUserRepo{}, client)
	svc.cfg.Cognito.Phone.OTPCooldownTable = "phone-cooldown"

	result, err := svc.PhoneLogin(context.Background(), PhoneLoginInput{PhoneNumber: "9876543210"})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "rate limit is unavailable")
}

func TestPhoneResendDoesNotRevealUnknownPhone(t *testing.T) {
	t.Parallel()

	for _, providerErr := range []error{&cognitotypes.UserNotFoundException{}, &cognitotypes.InvalidParameterException{}} {
		phoneClient := &mockCognitoClient{
			resendConfirmationCode: func(context.Context, *cognitoidentityprovider.ResendConfirmationCodeInput) (*cognitoidentityprovider.ResendConfirmationCodeOutput, error) {
				return nil, providerErr
			},
		}

		err := newPhoneAuthService(t, &mockUserRepo{}, phoneClient).PhoneResendConfirmation(context.Background(), "9876543210")

		require.NoError(t, err)
	}
}

func TestPhoneLinkConfirmRequiresPossessionAndUpdatesOnlyAuthenticatedUser(t *testing.T) {
	t.Parallel()

	phoneClient := &mockCognitoClient{
		confirmSignUp: func(_ context.Context, input *cognitoidentityprovider.ConfirmSignUpInput) (*cognitoidentityprovider.ConfirmSignUpOutput, error) {
			assert.Equal(t, "+919876543210", aws.ToString(input.Username))
			assert.Equal(t, "123456", aws.ToString(input.ConfirmationCode))
			return &cognitoidentityprovider.ConfirmSignUpOutput{}, nil
		},
	}
	repo := &mockUserRepo{
		getByID: func(context.Context, string) (*models.User, error) {
			return &models.User{ID: "email-user", CognitoID: "email-sub", Email: "owner@example.com", Name: "Owner"}, nil
		},
		getByPhoneNumber: func(context.Context, string) (*models.User, error) {
			return nil, errors.New("user not found")
		},
		update: func(_ context.Context, user *models.User) error {
			assert.Equal(t, "email-user", user.ID)
			assert.Equal(t, "+919876543210", user.PhoneNumber)
			return nil
		},
	}

	err := newPhoneAuthService(t, repo, phoneClient).PhoneLinkConfirm(context.Background(), "email-user", PhoneConfirmInput{
		PhoneNumber: "9876543210",
		Code:        "123456",
	})

	require.NoError(t, err)
}

func TestPhoneLinkRejectsCollisionBeforeProviderMutation(t *testing.T) {
	t.Parallel()

	providerCalled := false
	phoneClient := &mockCognitoClient{
		confirmSignUp: func(context.Context, *cognitoidentityprovider.ConfirmSignUpInput) (*cognitoidentityprovider.ConfirmSignUpOutput, error) {
			providerCalled = true
			return &cognitoidentityprovider.ConfirmSignUpOutput{}, nil
		},
	}
	repo := &mockUserRepo{
		getByID: func(context.Context, string) (*models.User, error) {
			return &models.User{ID: "email-user", CognitoID: "email-sub", Name: "Owner"}, nil
		},
		getByPhoneNumber: func(context.Context, string) (*models.User, error) {
			return &models.User{ID: "other-user", PhoneNumber: "+919876543210"}, nil
		},
	}

	err := newPhoneAuthService(t, repo, phoneClient).PhoneLinkConfirm(context.Background(), "email-user", PhoneConfirmInput{
		PhoneNumber: "9876543210",
		Code:        "123456",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPhoneLinkConflict)
	assert.False(t, providerCalled)
}

func TestPhoneRefreshAndLogoutDoNotExposeProviderErrors(t *testing.T) {
	t.Parallel()

	providerErr := errors.New("provider-secret-detail")
	phoneClient := &mockCognitoClient{
		initiateAuth: func(context.Context, *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error) {
			return nil, providerErr
		},
		globalSignOut: func(context.Context, *cognitoidentityprovider.GlobalSignOutInput) (*cognitoidentityprovider.GlobalSignOutOutput, error) {
			return nil, providerErr
		},
	}
	svc := newPhoneAuthService(t, &mockUserRepo{}, phoneClient)

	_, refreshErr := svc.PhoneRefresh(context.Background(), RefreshInput{RefreshToken: "opaque"})
	logoutErr := svc.PhoneLogout(context.Background(), "opaque")

	require.Error(t, refreshErr)
	require.Error(t, logoutErr)
	assert.NotContains(t, refreshErr.Error(), "provider-secret-detail")
	assert.NotContains(t, logoutErr.Error(), "provider-secret-detail")
}

func TestPhoneLinkAuditDoesNotPersistRawPhone(t *testing.T) {
	t.Parallel()

	repo := &mockUserRepo{
		getByID: func(context.Context, string) (*models.User, error) {
			return &models.User{ID: "email-user", CognitoID: "email-sub", Name: "Owner"}, nil
		},
		getByPhoneNumber: func(context.Context, string) (*models.User, error) {
			return nil, errors.New("user not found")
		},
	}
	svc := newPhoneAuthService(t, repo, &mockCognitoClient{})

	err := svc.PhoneLinkConfirm(context.Background(), "email-user", PhoneConfirmInput{PhoneNumber: "9876543210", Code: "123456"})

	require.NoError(t, err)
	audit := svc.phoneLink.(*mockPhoneLinkAuditRepository)
	require.Len(t, audit.events, 1)
	assert.Equal(t, "phone_link_confirmed", audit.events[0].EventType)
	assert.Equal(t, "email-user", audit.events[0].Subject)
	assert.NotContains(t, audit.events[0].ResourceID, "9876543210")
}

func TestPhoneLinkConfirmFailsClosedWithoutAtomicAuditRepository(t *testing.T) {
	t.Parallel()

	repo := &mockUserRepo{
		getByID: func(context.Context, string) (*models.User, error) {
			return &models.User{ID: "email-user", CognitoID: "email-sub", Name: "Owner"}, nil
		},
		getByPhoneNumber: func(context.Context, string) (*models.User, error) {
			return nil, errors.New("user not found")
		},
		update: func(context.Context, *models.User) error {
			t.Fatal("phone must not mutate without atomic audit persistence")
			return nil
		},
	}
	svc := newPhoneAuthService(t, repo, &mockCognitoClient{})
	svc.phoneLink = nil

	err := svc.PhoneLinkConfirm(context.Background(), "email-user", PhoneConfirmInput{PhoneNumber: "9876543210", Code: "123456"})

	require.Error(t, err)
	assert.Equal(t, phoneAuthGenericFailure, err.Error())
}

func TestPhoneLinkConfirmRejectsExpiredOrReplayedOTPWithoutMutation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "expired", err: &cognitotypes.ExpiredCodeException{}, want: "OTP code expired"},
		{name: "replayed", err: &cognitotypes.NotAuthorizedException{}, want: "verification session expired"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockUserRepo{
				getByID: func(context.Context, string) (*models.User, error) {
					return &models.User{ID: "email-user", CognitoID: "email-sub", Name: "Owner"}, nil
				},
				getByPhoneNumber: func(context.Context, string) (*models.User, error) {
					return nil, errors.New("user not found")
				},
				update: func(context.Context, *models.User) error {
					t.Fatal("expired or replayed OTP must not mutate the user")
					return nil
				},
			}
			client := &mockCognitoClient{
				confirmSignUp: func(context.Context, *cognitoidentityprovider.ConfirmSignUpInput) (*cognitoidentityprovider.ConfirmSignUpOutput, error) {
					return nil, tc.err
				},
			}

			err := newPhoneAuthService(t, repo, client).PhoneLinkConfirm(context.Background(), "email-user", PhoneConfirmInput{
				PhoneNumber: "9876543210", Code: "123456",
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestConcurrentPhoneLinkCollisionHasSingleWinner(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	users := map[string]*models.User{
		"user-a": {ID: "user-a", CognitoID: "email-a", Name: "User A"},
		"user-b": {ID: "user-b", CognitoID: "email-b", Name: "User B"},
	}
	repo := &mockUserRepo{
		getByID: func(_ context.Context, id string) (*models.User, error) {
			mu.Lock()
			defer mu.Unlock()
			copy := *users[id]
			return &copy, nil
		},
		getByPhoneNumber: func(_ context.Context, phone string) (*models.User, error) {
			mu.Lock()
			defer mu.Unlock()
			for _, user := range users {
				if user.PhoneNumber == phone {
					copy := *user
					return &copy, nil
				}
			}
			return nil, errors.New("user not found")
		},
		update: func(_ context.Context, candidate *models.User) error {
			mu.Lock()
			defer mu.Unlock()
			for _, user := range users {
				if user.ID != candidate.ID && user.PhoneNumber == candidate.PhoneNumber {
					return errors.New("duplicate phone")
				}
			}
			copy := *candidate
			users[candidate.ID] = &copy
			return nil
		},
	}
	svc := newPhoneAuthService(t, repo, &mockCognitoClient{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, userID := range []string{"user-a", "user-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.PhoneLinkConfirm(context.Background(), userID, PhoneConfirmInput{PhoneNumber: "9876543210", Code: "123456"})
		}()
	}
	wg.Wait()
	close(errs)

	var succeeded, conflicted int
	for err := range errs {
		if err == nil {
			succeeded++
		} else if errors.Is(err, ErrPhoneLinkConflict) {
			conflicted++
		}
	}
	assert.Equal(t, 1, succeeded)
	assert.Equal(t, 1, conflicted)
}
