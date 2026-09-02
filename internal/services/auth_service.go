package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

const (
	phoneSignupCooldownPurpose  = "signup"
	phoneResendCooldownPurpose  = "resend"
	phoneLoginCooldownPurpose   = "login"
	phoneOTPCooldownWindow      = time.Minute
	phoneAuthDisabledMessage    = "phone authentication is not configured"
	phoneAuthConflictMessage    = "phone number already registered"
	phoneAuthEmailConflict      = "email already registered with another account"
	phoneAuthEmailUnsupported   = "email cannot be set during phone registration"
	phoneAuthRateLimitMessage   = "too many OTP requests, please wait before trying again"
	phoneAuthGenericFailure     = "authentication failed"
	defaultPhoneRegisterPrompt  = "OTP sent to your phone number"
	defaultPhoneConfirmPrompt   = "phone number verified successfully"
	defaultPhoneResendPrompt    = "verification code resent"
	defaultPhoneChallengePrompt = "OTP sent to your phone number"
)

var indianMobileNumberPattern = regexp.MustCompile(`^[6-9][0-9]{9}$`)

type cognitoIdentityProviderAPI interface {
	ChangePassword(ctx context.Context, params *cognitoidentityprovider.ChangePasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ChangePasswordOutput, error)
	ConfirmForgotPassword(ctx context.Context, params *cognitoidentityprovider.ConfirmForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmForgotPasswordOutput, error)
	ConfirmSignUp(ctx context.Context, params *cognitoidentityprovider.ConfirmSignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ConfirmSignUpOutput, error)
	ForgotPassword(ctx context.Context, params *cognitoidentityprovider.ForgotPasswordInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ForgotPasswordOutput, error)
	GlobalSignOut(ctx context.Context, params *cognitoidentityprovider.GlobalSignOutInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GlobalSignOutOutput, error)
	GetUser(ctx context.Context, params *cognitoidentityprovider.GetUserInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.GetUserOutput, error)
	InitiateAuth(ctx context.Context, params *cognitoidentityprovider.InitiateAuthInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.InitiateAuthOutput, error)
	ResendConfirmationCode(ctx context.Context, params *cognitoidentityprovider.ResendConfirmationCodeInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ResendConfirmationCodeOutput, error)
	RespondToAuthChallenge(ctx context.Context, params *cognitoidentityprovider.RespondToAuthChallengeInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.RespondToAuthChallengeOutput, error)
	SignUp(ctx context.Context, params *cognitoidentityprovider.SignUpInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.SignUpOutput, error)
	AssociateSoftwareToken(ctx context.Context, params *cognitoidentityprovider.AssociateSoftwareTokenInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.AssociateSoftwareTokenOutput, error)
	VerifySoftwareToken(ctx context.Context, params *cognitoidentityprovider.VerifySoftwareTokenInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.VerifySoftwareTokenOutput, error)
	SetUserMFAPreference(ctx context.Context, params *cognitoidentityprovider.SetUserMFAPreferenceInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.SetUserMFAPreferenceOutput, error)
	ListDevices(ctx context.Context, params *cognitoidentityprovider.ListDevicesInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ListDevicesOutput, error)
	ForgetDevice(ctx context.Context, params *cognitoidentityprovider.ForgetDeviceInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.ForgetDeviceOutput, error)
	UpdateDeviceStatus(ctx context.Context, params *cognitoidentityprovider.UpdateDeviceStatusInput, optFns ...func(*cognitoidentityprovider.Options)) (*cognitoidentityprovider.UpdateDeviceStatusOutput, error)
}

type AuthService struct {
	cfg          *config.Config
	userRepo     interfaces.UserRepository
	cognito      cognitoIdentityProviderAPI
	cognitoPhone cognitoIdentityProviderAPI
	dynamoDB     *dynamodb.Client
	email        *EmailService
	s3           *S3Service
	log          *logger.Logger
}

func NewAuthService(cfg *config.Config, userRepo interfaces.UserRepository, awsCfg *awsclients.Config, email *EmailService, s3 *S3Service, log *logger.Logger) *AuthService {
	svc := &AuthService{
		cfg:      cfg,
		userRepo: userRepo,
		cognito:  awsCfg.Cognito,
		dynamoDB: awsCfg.DynamoDB,
		email:    email,
		s3:       s3,
		log:      log,
	}

	if cfg != nil && cfg.Cognito.Phone.UserPoolID != "" && cfg.Cognito.Phone.ClientID != "" && awsCfg != nil {
		phoneSDKConfig := awsCfg.SDKConfig
		phoneSDKConfig.Region = cfg.Cognito.Phone.Region
		svc.cognitoPhone = cognitoidentityprovider.NewFromConfig(phoneSDKConfig, func(o *cognitoidentityprovider.Options) {
			if cfg.AWS.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.AWS.Endpoint)
			}
		})
	}

	return svc
}

type RegisterInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=12"`
	Name     string `json:"name" binding:"required,min=2"`
}

type RegisterOutput struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
}

func (s *AuthService) Register(ctx context.Context, input RegisterInput) (*RegisterOutput, error) {
	normalizedEmail := normalizeOptionalEmail(input.Email)
	normalizedName := strings.TrimSpace(input.Name)

	signUpResp, err := s.cognito.SignUp(ctx, &cognitoidentityprovider.SignUpInput{
		ClientId: aws.String(s.cfg.Cognito.ClientID),
		Username: aws.String(normalizedEmail),
		Password: aws.String(input.Password),
		UserAttributes: []types.AttributeType{
			{Name: aws.String("email"), Value: aws.String(normalizedEmail)},
			{Name: aws.String("name"), Value: aws.String(normalizedName)},
		},
	})
	if err != nil {
		var usernameExists *types.UsernameExistsException
		if errors.As(err, &usernameExists) {
			resendErr := s.ResendVerification(ctx, normalizedEmail)
			if resendErr == nil || shouldIgnoreVerificationResendError(resendErr) {
				if resendErr != nil {
					s.log.Warn("existing signup verification resend throttled", "email", normalizedEmail, "error", resendErr)
				}
				return &RegisterOutput{
					UserID:  "",
					Message: "Account already exists. Please verify your email address",
				}, nil
			}

			if isAlreadyConfirmedResendError(resendErr) {
				return nil, fmt.Errorf("email already registered")
			}

			s.log.Warn("existing signup verification resend failed", "email", normalizedEmail, "error", resendErr)
			return nil, fmt.Errorf("email already registered")
		}

		s.log.Error("cognito signup failed", "error", err)
		return nil, fmt.Errorf("registration failed: %w", err)
	}

	if signUpResp != nil && !signUpResp.UserConfirmed {
		s.ensureEmailVerificationCodeDispatched(ctx, normalizedEmail, signUpResp)
	}

	cognitoID := normalizedEmail
	if signUpResp.UserSub != nil {
		cognitoID = *signUpResp.UserSub
	}

	user := &models.User{
		Email:     normalizedEmail,
		CognitoID: cognitoID,
		Name:      normalizedName,
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

func (s *AuthService) ensureEmailVerificationCodeDispatched(ctx context.Context, email string, signUpResp *cognitoidentityprovider.SignUpOutput) {
	if signUpResp != nil && signUpResp.CodeDeliveryDetails != nil {
		return
	}

	if err := s.ResendVerification(ctx, email); err != nil {
		if shouldIgnoreVerificationResendError(err) {
			s.log.Warn("email verification fallback resend throttled; assuming code was already sent", "email", email, "error", err)
			return
		}
		s.log.Warn("email verification fallback resend failed", "email", email, "error", err)
	}
}

func shouldIgnoreVerificationResendError(err error) bool {
	var tooManyRequests *types.TooManyRequestsException
	if errors.As(err, &tooManyRequests) {
		return true
	}

	var limitExceeded *types.LimitExceededException
	return errors.As(err, &limitExceeded)
}

func isAlreadyConfirmedResendError(err error) bool {
	if err == nil {
		return false
	}

	var invalidParameter *types.InvalidParameterException
	if errors.As(err, &invalidParameter) {
		return strings.Contains(strings.ToLower(aws.ToString(invalidParameter.Message)), "already confirmed")
	}

	return strings.Contains(strings.ToLower(err.Error()), "already confirmed")
}

type LoginInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type LoginOutput struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int32  `json:"expires_in,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	Challenge    string `json:"challenge,omitempty"`
	Session      string `json:"session,omitempty"`
	Username     string `json:"username,omitempty"`
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
		return nil, classifyEmailAuthError(err)
	}
	if result.ChallengeName == types.ChallengeNameTypeSoftwareTokenMfa && strings.TrimSpace(aws.ToString(result.Session)) != "" {
		return &LoginOutput{Challenge: string(result.ChallengeName), Session: aws.ToString(result.Session), Username: strings.TrimSpace(input.Email)}, nil
	}

	// Ensure local user record exists (handles users created outside the app)
	if result.AuthenticationResult != nil && result.AuthenticationResult.AccessToken != nil {
		if err := s.ensureUserFromCognito(ctx, aws.ToString(result.AuthenticationResult.AccessToken)); err != nil {
			s.log.Warn("failed to ensure user exists", "error", err)
			// Don't block login if user sync fails — tokens are still valid
		}
	}

	return loginOutputFromAuthResult(result.AuthenticationResult)
}

type LoginMFAInput struct {
	Username string `json:"username" binding:"required"`
	Session  string `json:"session" binding:"required"`
	Code     string `json:"code" binding:"required,len=6"`
}

func (s *AuthService) CompleteLoginMFA(ctx context.Context, input LoginMFAInput) (*LoginOutput, error) {
	input.Username, input.Session, input.Code = strings.TrimSpace(input.Username), strings.TrimSpace(input.Session), strings.TrimSpace(input.Code)
	if input.Username == "" || input.Session == "" || !sixDigitCode(input.Code) {
		return nil, fmt.Errorf("invalid MFA challenge")
	}
	result, err := s.cognito.RespondToAuthChallenge(ctx, &cognitoidentityprovider.RespondToAuthChallengeInput{
		ClientId: aws.String(s.cfg.Cognito.ClientID), ChallengeName: types.ChallengeNameTypeSoftwareTokenMfa,
		Session: aws.String(input.Session), ChallengeResponses: map[string]string{"USERNAME": input.Username, "SOFTWARE_TOKEN_MFA_CODE": input.Code},
	})
	if err != nil || result == nil {
		return nil, fmt.Errorf("MFA challenge failed")
	}
	output, err := loginOutputFromAuthResult(result.AuthenticationResult)
	if err != nil {
		return nil, err
	}
	if err := s.ensureUserFromCognito(ctx, output.AccessToken); err != nil {
		s.log.Warn("failed to ensure MFA user exists", "error", err)
	}
	return output, nil
}

func sixDigitCode(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// CognitoAccessTokenSubject reads a token returned directly by Cognito in the
// same request. Callers must not use it for arbitrary client-supplied tokens.
func CognitoAccessTokenSubject(accessToken string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	parser := jwt.Parser{}
	if _, _, err := parser.ParseUnverified(strings.TrimSpace(accessToken), claims); err != nil || strings.TrimSpace(claims.Subject) == "" {
		return "", errors.New("Cognito access token subject is unavailable")
	}
	return strings.TrimSpace(claims.Subject), nil
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

	return loginOutputFromRefreshResult(result.AuthenticationResult, input.RefreshToken)
}

func (s *AuthService) Logout(ctx context.Context, accessToken string) error {
	client := s.cognitoClientForAccessToken(accessToken)
	_, err := client.GlobalSignOut(ctx, &cognitoidentityprovider.GlobalSignOutInput{
		AccessToken: aws.String(accessToken),
	})
	return err
}

type TOTPSetup struct {
	SecretCode string `json:"secret_code"`
	Session    string `json:"session,omitempty"`
}

func (s *AuthService) BeginTOTP(ctx context.Context, accessToken string) (*TOTPSetup, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("access token is required")
	}
	output, err := s.cognitoClientForAccessToken(accessToken).AssociateSoftwareToken(ctx, &cognitoidentityprovider.AssociateSoftwareTokenInput{AccessToken: aws.String(accessToken)})
	if err != nil || output == nil || strings.TrimSpace(aws.ToString(output.SecretCode)) == "" {
		return nil, fmt.Errorf("TOTP setup failed")
	}
	return &TOTPSetup{SecretCode: aws.ToString(output.SecretCode), Session: aws.ToString(output.Session)}, nil
}

type TOTPConfirmInput struct {
	Code               string `json:"code" binding:"required,len=6"`
	Session            string `json:"session"`
	FriendlyDeviceName string `json:"friendly_device_name"`
}

func (s *AuthService) ConfirmTOTP(ctx context.Context, accessToken string, input TOTPConfirmInput) error {
	accessToken, input.Code = strings.TrimSpace(accessToken), strings.TrimSpace(input.Code)
	if accessToken == "" || len(input.Code) != 6 {
		return fmt.Errorf("invalid TOTP confirmation")
	}
	client := s.cognitoClientForAccessToken(accessToken)
	verifyInput := &cognitoidentityprovider.VerifySoftwareTokenInput{
		UserCode: aws.String(input.Code), FriendlyDeviceName: aws.String(strings.TrimSpace(input.FriendlyDeviceName)),
	}
	if session := strings.TrimSpace(input.Session); session != "" {
		verifyInput.Session = aws.String(session)
	} else {
		verifyInput.AccessToken = aws.String(accessToken)
	}
	verified, err := client.VerifySoftwareToken(ctx, verifyInput)
	if err != nil || verified == nil || verified.Status != types.VerifySoftwareTokenResponseTypeSuccess {
		return fmt.Errorf("TOTP verification failed")
	}
	_, err = client.SetUserMFAPreference(ctx, &cognitoidentityprovider.SetUserMFAPreferenceInput{
		AccessToken:              aws.String(accessToken),
		SoftwareTokenMfaSettings: &types.SoftwareTokenMfaSettingsType{Enabled: true, PreferredMfa: true},
	})
	if err != nil {
		return fmt.Errorf("TOTP preference update failed")
	}
	return nil
}

func (s *AuthService) DisableTOTP(ctx context.Context, accessToken string) error {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return fmt.Errorf("access token is required")
	}
	_, err := s.cognitoClientForAccessToken(accessToken).SetUserMFAPreference(ctx, &cognitoidentityprovider.SetUserMFAPreferenceInput{
		AccessToken:              aws.String(accessToken),
		SoftwareTokenMfaSettings: &types.SoftwareTokenMfaSettingsType{Enabled: false, PreferredMfa: false},
	})
	if err != nil {
		return fmt.Errorf("TOTP preference update failed")
	}
	return nil
}

type AuthDevice struct {
	DeviceKey       string     `json:"device_key"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
	LastAccessedAt  *time.Time `json:"last_accessed_at,omitempty"`
	RememberedState string     `json:"remembered_state,omitempty"`
}

type AuthDevicePage struct {
	Devices   []AuthDevice `json:"devices"`
	NextToken string       `json:"next_token,omitempty"`
}

func (s *AuthService) ListDevices(ctx context.Context, accessToken, nextToken string) (*AuthDevicePage, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("access token is required")
	}
	output, err := s.cognitoClientForAccessToken(accessToken).ListDevices(ctx, &cognitoidentityprovider.ListDevicesInput{
		AccessToken: aws.String(accessToken), Limit: aws.Int32(20), PaginationToken: optionalString(nextToken),
	})
	if err != nil || output == nil {
		return nil, fmt.Errorf("device listing failed")
	}
	page := &AuthDevicePage{Devices: make([]AuthDevice, 0, len(output.Devices)), NextToken: aws.ToString(output.PaginationToken)}
	for _, device := range output.Devices {
		item := AuthDevice{DeviceKey: aws.ToString(device.DeviceKey), CreatedAt: device.DeviceCreateDate, LastAccessedAt: device.DeviceLastModifiedDate}
		for _, attribute := range device.DeviceAttributes {
			if aws.ToString(attribute.Name) == "device_status" {
				item.RememberedState = aws.ToString(attribute.Value)
			}
		}
		page.Devices = append(page.Devices, item)
	}
	return page, nil
}

func (s *AuthService) ForgetDevice(ctx context.Context, accessToken, deviceKey string) error {
	accessToken, deviceKey = strings.TrimSpace(accessToken), strings.TrimSpace(deviceKey)
	if accessToken == "" || deviceKey == "" || len(deviceKey) > 512 {
		return fmt.Errorf("invalid device request")
	}
	_, err := s.cognitoClientForAccessToken(accessToken).ForgetDevice(ctx, &cognitoidentityprovider.ForgetDeviceInput{AccessToken: aws.String(accessToken), DeviceKey: aws.String(deviceKey)})
	if err != nil {
		return fmt.Errorf("device revocation failed")
	}
	return nil
}

func (s *AuthService) SetDeviceRemembered(ctx context.Context, accessToken, deviceKey string, remembered bool) error {
	accessToken, deviceKey = strings.TrimSpace(accessToken), strings.TrimSpace(deviceKey)
	if accessToken == "" || deviceKey == "" || len(deviceKey) > 512 {
		return fmt.Errorf("invalid device request")
	}
	status := types.DeviceRememberedStatusTypeNotRemembered
	if remembered {
		status = types.DeviceRememberedStatusTypeRemembered
	}
	_, err := s.cognitoClientForAccessToken(accessToken).UpdateDeviceStatus(ctx, &cognitoidentityprovider.UpdateDeviceStatusInput{
		AccessToken: aws.String(accessToken), DeviceKey: aws.String(deviceKey), DeviceRememberedStatus: status,
	})
	if err != nil {
		return fmt.Errorf("device status update failed")
	}
	return nil
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return aws.String(value)
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
		return fmt.Errorf("failed to initiate password reset: %w", err)
	}
	return nil
}

type ResetPasswordInput struct {
	Email            string `json:"email" binding:"required,email"`
	ConfirmationCode string `json:"confirmation_code" binding:"required,min=6,max=6"`
	NewPassword      string `json:"new_password" binding:"required,min=12"`
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
	user, err := s.userRepo.GetByCognitoID(ctx, cognitoID)
	if err == nil {
		return user, nil
	}

	if rawSubject, ok := rawCognitoSubject(cognitoID); ok {
		return s.userRepo.GetByCognitoID(ctx, rawSubject)
	}

	return nil, err
}

func (s *AuthService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return s.userRepo.GetByEmail(ctx, email)
}

func (s *AuthService) GetUserByPhoneNumber(ctx context.Context, phoneNumber string) (*models.User, error) {
	return s.userRepo.GetByPhoneNumber(ctx, phoneNumber)
}

func (s *AuthService) UpdateUserCognitoID(ctx context.Context, userID string, cognitoID string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.ensureCognitoRelinkAllowed(user, cognitoID); err != nil {
		return err
	}
	user.CognitoID = cognitoID
	user.UpdatedAt = time.Now()
	return s.userRepo.Update(ctx, user)
}

type UpdateProfileInput struct {
	Name              *string `json:"name,omitempty" binding:"omitempty,min=2"`
	Email             *string `json:"email,omitempty" binding:"omitempty,email,max=255"`
	PhoneNumber       *string `json:"phone_number,omitempty" binding:"omitempty,max=20"`
	ProfilePictureURL *string `json:"profile_picture_url,omitempty"`
}

func (i UpdateProfileInput) HasChanges() bool {
	return i.Name != nil || i.Email != nil || i.PhoneNumber != nil || i.ProfilePictureURL != nil
}

func (s *AuthService) UpdateProfile(ctx context.Context, userID string, input UpdateProfileInput) (*models.User, error) {
	if !input.HasChanges() {
		return nil, fmt.Errorf("at least one profile field is required")
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if len(name) < 2 {
			return nil, fmt.Errorf("name must be at least 2 characters")
		}
		user.Name = name
	}
	if input.Email != nil {
		email := normalizeOptionalEmail(*input.Email)
		if email != "" && email != user.Email {
			existing, err := s.userRepo.GetByEmail(ctx, email)
			if err == nil && existing != nil && existing.ID != user.ID {
				return nil, fmt.Errorf("email already registered")
			}
			if err != nil && !isUserNotFoundError(err) {
				return nil, err
			}
			user.Email = email
		}
	}
	if input.PhoneNumber != nil {
		normalizedPhone, err := normalizeIndianPhoneNumber(*input.PhoneNumber)
		if err != nil {
			return nil, err
		}
		if normalizedPhone != user.PhoneNumber {
			existing, err := s.userRepo.GetByPhoneNumber(ctx, normalizedPhone)
			if err == nil && existing != nil && existing.ID != user.ID {
				return nil, errors.New(phoneAuthConflictMessage)
			}
			if err != nil && !isUserNotFoundError(err) {
				return nil, err
			}
			user.PhoneNumber = normalizedPhone
		}
	}
	if input.ProfilePictureURL != nil {
		user.ProfilePictureURL = strings.TrimSpace(*input.ProfilePictureURL)
	}
	user.UpdatedAt = time.Now()

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *AuthService) GetProfilePictureUploadURL(ctx context.Context, userID, contentType string, sizeBytes int64) (*PresignedUpload, error) {
	contentType, err := NormalizeImageUploadContentType(contentType)
	if err != nil {
		return nil, err
	}
	if err := validateUploadSize("profile picture", sizeBytes, MaxProfilePictureUploadBytes); err != nil {
		return nil, err
	}
	key := s.profilePictureKey(userID, contentType)
	return s.s3.GeneratePresignedUpload(ctx, s.profilePictureBucket(), key, contentType, sizeBytes, 3600)
}

func (s *AuthService) UploadProfilePicture(ctx context.Context, userID string, data []byte, contentType string) (*models.User, error) {
	if s.s3 == nil {
		return nil, fmt.Errorf("profile picture storage is not configured")
	}

	bucket := s.profilePictureBucket()
	key := s.profilePictureKey(userID, contentType)
	if err := s.s3.Upload(ctx, bucket, key, data, contentType); err != nil {
		return nil, fmt.Errorf("failed to upload profile picture: %w", err)
	}

	profilePictureURL := s.s3.GetObjectURL(bucket, key)
	return s.UpdateProfile(ctx, userID, UpdateProfileInput{ProfilePictureURL: &profilePictureURL})
}

func (s *AuthService) profilePictureKey(userID, contentType string) string {
	return fmt.Sprintf(
		"profile-pictures/%s/%s%s",
		userID,
		uuid.NewString(),
		profilePictureExtension(contentType),
	)
}

func (s *AuthService) profilePictureBucket() string {
	if s != nil && s.cfg != nil {
		if bucket := strings.TrimSpace(s.cfg.S3.BucketLogos); bucket != "" {
			return bucket
		}
	}
	return "business-logos"
}

func profilePictureExtension(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ""
	}
}

type ChangePasswordInput struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=12"`
}

func (s *AuthService) ChangePassword(ctx context.Context, accessToken string, input ChangePasswordInput) error {
	client := s.cognitoClientForAccessToken(accessToken)
	_, err := client.ChangePassword(ctx, &cognitoidentityprovider.ChangePasswordInput{
		AccessToken:      aws.String(accessToken),
		PreviousPassword: aws.String(input.OldPassword),
		ProposedPassword: aws.String(input.NewPassword),
	})
	return err
}

type SyncGoogleUserInput struct {
	Email             string
	EmailVerified     bool
	CognitoID         string
	Name              string
	ProfilePictureURL string
}

func (s *AuthService) SyncGoogleUser(ctx context.Context, input SyncGoogleUserInput) (*models.User, error) {
	if !input.EmailVerified {
		return nil, fmt.Errorf("google email must be verified")
	}

	email := normalizeOptionalEmail(input.Email)
	name := googleDisplayName(input.Name, email)
	profilePictureURL := strings.TrimSpace(input.ProfilePictureURL)
	cognitoID := strings.TrimSpace(input.CognitoID)

	user, err := s.userRepo.GetByCognitoID(ctx, cognitoID)
	if err == nil {
		if profilePictureURL != "" && strings.TrimSpace(user.ProfilePictureURL) == "" {
			user.ProfilePictureURL = profilePictureURL
			user.UpdatedAt = time.Now()
			if err := s.userRepo.Update(ctx, user); err != nil {
				s.log.Warn("failed to update profile picture", "error", err)
			}
		}
		return user, nil
	}

	user, err = s.userRepo.GetByEmail(ctx, email)
	if err == nil {
		if err := s.ensureCognitoRelinkAllowed(user, cognitoID); err != nil {
			return nil, err
		}
		user.CognitoID = cognitoID
		if strings.TrimSpace(input.Name) != "" {
			user.Name = name
		}
		if profilePictureURL != "" && strings.TrimSpace(user.ProfilePictureURL) == "" {
			user.ProfilePictureURL = profilePictureURL
		}
		user.UpdatedAt = time.Now()
		if err := s.userRepo.Update(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to link google user: %w", err)
		}
		return user, nil
	}

	newUser := &models.User{
		Email:             email,
		CognitoID:         cognitoID,
		Name:              name,
		ProfilePictureURL: profilePictureURL,
		Role:              "viewer",
	}

	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, fmt.Errorf("failed to create google user: %w", err)
	}

	return newUser, nil
}

func googleDisplayName(name, email string) string {
	trimmedName := strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	if utf8.RuneCountInString(trimmedName) >= 2 {
		return trimmedName
	}

	localPart := strings.TrimSpace(email)
	if at := strings.Index(localPart, "@"); at >= 0 {
		localPart = localPart[:at]
	}
	localPart = strings.NewReplacer(".", " ", "_", " ", "-", " ", "+", " ").Replace(localPart)
	localPart = strings.Join(strings.Fields(localPart), " ")
	if utf8.RuneCountInString(localPart) >= 2 {
		return localPart
	}

	return "Google User"
}

// ensureUserFromCognito fetches user attributes from Cognito using the access token,
// then finds or creates the local user record. This handles users who exist in Cognito
// but have no local record (e.g., users created via AWS console, Google OAuth, etc.).
func (s *AuthService) ensureUserFromCognito(ctx context.Context, accessToken string) error {
	userResp, err := s.cognito.GetUser(ctx, &cognitoidentityprovider.GetUserInput{
		AccessToken: aws.String(accessToken),
	})
	if err != nil {
		s.log.Warn("failed to get user from cognito", "error", err)
		return err
	}

	var email, name, sub string
	for _, attr := range userResp.UserAttributes {
		switch aws.ToString(attr.Name) {
		case "email":
			email = aws.ToString(attr.Value)
		case "name":
			name = aws.ToString(attr.Value)
		case "sub":
			sub = aws.ToString(attr.Value)
		}
	}

	// Try by Cognito sub first
	_, err = s.userRepo.GetByCognitoID(ctx, sub)
	if err == nil {
		return nil // user exists
	}

	// Try by email and link Cognito ID
	if email != "" {
		user, err := s.userRepo.GetByEmail(ctx, email)
		if err == nil {
			if err := s.ensureCognitoRelinkAllowed(user, sub); err != nil {
				return err
			}
			user.CognitoID = sub
			user.UpdatedAt = time.Now()
			return s.userRepo.Update(ctx, user)
		}
	}

	// Create new user
	newUser := &models.User{
		Email:     email,
		CognitoID: sub,
		Name:      name,
		Role:      "viewer",
	}
	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return fmt.Errorf("failed to create user from cognito: %w", err)
	}
	return nil
}

type PhoneRegisterInput struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
	Name        string `json:"name" binding:"required,min=2"`
	Email       string `json:"email,omitempty" binding:"omitempty,email"`
}

type PhoneRegisterOutput struct {
	UserID      string `json:"user_id"`
	PhoneNumber string `json:"phone_number"`
	Message     string `json:"message"`
}

func (s *AuthService) PhoneRegister(ctx context.Context, input PhoneRegisterInput) (*PhoneRegisterOutput, error) {
	if err := s.ensurePhoneAuthConfigured(); err != nil {
		return nil, err
	}

	normalizedPhone, err := normalizeIndianPhoneNumber(input.PhoneNumber)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(input.Email) != "" {
		return nil, errors.New(phoneAuthEmailUnsupported)
	}
	if _, err := s.userRepo.GetByPhoneNumber(ctx, normalizedPhone); err == nil {
		return nil, errors.New(phoneAuthConflictMessage)
	}

	if err := s.enforcePhoneOTPCooldown(ctx, phoneSignupCooldownPurpose, normalizedPhone); err != nil {
		return nil, err
	}

	userAttributes := []types.AttributeType{
		{Name: aws.String("name"), Value: aws.String(strings.TrimSpace(input.Name))},
		{Name: aws.String("phone_number"), Value: aws.String(normalizedPhone)},
	}

	signUpResp, err := s.cognitoPhone.SignUp(ctx, &cognitoidentityprovider.SignUpInput{
		ClientId: aws.String(s.cfg.Cognito.Phone.ClientID),
		// Keep username aligned with phone-based login/confirm calls.
		Username:       aws.String(normalizedPhone),
		UserAttributes: userAttributes,
	})
	if err != nil {
		s.log.Warn("phone registration failed", "phone_number", maskPhoneNumber(normalizedPhone), "error", err)
		return nil, fmt.Errorf("phone registration failed: %w", err)
	}
	if signUpResp.UserSub == nil || *signUpResp.UserSub == "" {
		return nil, fmt.Errorf("phone registration failed: missing user identity")
	}

	user := &models.User{
		PhoneNumber: normalizedPhone,
		CognitoID:   canonicalPhoneCognitoID(s.cfg.Cognito.Phone.UserPoolID, *signUpResp.UserSub),
		Name:        strings.TrimSpace(input.Name),
		Role:        "viewer",
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		s.log.Error("failed to create phone user record", "phone_number", maskPhoneNumber(normalizedPhone), "error", err)
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &PhoneRegisterOutput{
		UserID:      user.ID,
		PhoneNumber: normalizedPhone,
		Message:     defaultPhoneRegisterPrompt,
	}, nil
}

func (s *AuthService) ensureCognitoRelinkAllowed(user *models.User, nextCognitoID string) error {
	if user == nil {
		return nil
	}
	current := strings.TrimSpace(user.CognitoID)
	next := strings.TrimSpace(nextCognitoID)
	if current == "" || current == next {
		return nil
	}
	if s.isPhonePoolCognitoID(current) != s.isPhonePoolCognitoID(next) {
		return fmt.Errorf("cannot relink user across identity providers")
	}
	return nil
}

func (s *AuthService) isPhonePoolCognitoID(cognitoID string) bool {
	if s == nil || s.cfg == nil {
		return false
	}
	poolID := strings.TrimSpace(s.cfg.Cognito.Phone.UserPoolID)
	return poolID != "" && strings.HasPrefix(strings.TrimSpace(cognitoID), poolID+":")
}

type PhoneConfirmInput struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
	Code        string `json:"code" binding:"required,min=6,max=8"`
}

func (s *AuthService) PhoneConfirm(ctx context.Context, input PhoneConfirmInput) error {
	if err := s.ensurePhoneAuthConfigured(); err != nil {
		return err
	}

	normalizedPhone, err := normalizeIndianPhoneNumber(input.PhoneNumber)
	if err != nil {
		return err
	}

	_, err = s.cognitoPhone.ConfirmSignUp(ctx, &cognitoidentityprovider.ConfirmSignUpInput{
		ClientId:         aws.String(s.cfg.Cognito.Phone.ClientID),
		Username:         aws.String(normalizedPhone),
		ConfirmationCode: aws.String(strings.TrimSpace(input.Code)),
	})
	return err
}

func (s *AuthService) PhoneResendConfirmation(ctx context.Context, phoneNumber string) error {
	if err := s.ensurePhoneAuthConfigured(); err != nil {
		return err
	}

	normalizedPhone, err := normalizeIndianPhoneNumber(phoneNumber)
	if err != nil {
		return err
	}
	if err := s.enforcePhoneOTPCooldown(ctx, phoneResendCooldownPurpose, normalizedPhone); err != nil {
		return err
	}

	_, err = s.cognitoPhone.ResendConfirmationCode(ctx, &cognitoidentityprovider.ResendConfirmationCodeInput{
		ClientId: aws.String(s.cfg.Cognito.Phone.ClientID),
		Username: aws.String(normalizedPhone),
	})
	return err
}

type PhoneLoginInput struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
}

type PhoneLoginChallengeOutput struct {
	ChallengeName string `json:"challenge_name"`
	Session       string `json:"session"`
	Message       string `json:"message"`
}

func (s *AuthService) PhoneLogin(ctx context.Context, input PhoneLoginInput) (*PhoneLoginChallengeOutput, error) {
	if err := s.ensurePhoneAuthConfigured(); err != nil {
		return nil, err
	}

	normalizedPhone, err := normalizeIndianPhoneNumber(input.PhoneNumber)
	if err != nil {
		return nil, err
	}

	// Surface a clear error before contacting Cognito when the number isn't registered locally.
	if _, err := s.userRepo.GetByPhoneNumber(ctx, normalizedPhone); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "user not found") {
			return nil, fmt.Errorf("phone number not registered")
		}
		s.log.Error("phone login precheck failed", "phone_number", maskPhoneNumber(normalizedPhone), "error", err)
		return nil, errors.New(phoneAuthGenericFailure)
	}

	if err := s.enforcePhoneOTPCooldown(ctx, phoneLoginCooldownPurpose, normalizedPhone); err != nil {
		return nil, err
	}

	result, err := s.cognitoPhone.InitiateAuth(ctx, &cognitoidentityprovider.InitiateAuthInput{
		ClientId: aws.String(s.cfg.Cognito.Phone.ClientID),
		AuthFlow: types.AuthFlowTypeUserAuth,
		AuthParameters: map[string]string{
			"USERNAME":            normalizedPhone,
			"PREFERRED_CHALLENGE": "SMS_OTP",
		},
	})
	if err != nil {
		s.log.Warn("phone login failed", "phone_number", maskPhoneNumber(normalizedPhone), "error", err)
		return nil, classifyPhoneAuthError(err)
	}
	if result.Session == nil || *result.Session == "" {
		return nil, fmt.Errorf("authentication failed: missing challenge session")
	}

	challengeName := string(result.ChallengeName)
	if challengeName == "" {
		challengeName = "SMS_OTP"
	}

	return &PhoneLoginChallengeOutput{
		ChallengeName: challengeName,
		Session:       *result.Session,
		Message:       defaultPhoneChallengePrompt,
	}, nil
}

type PhoneVerifyLoginInput struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
	Code        string `json:"code" binding:"required,min=6,max=8"`
	Session     string `json:"session" binding:"required"`
}

func (s *AuthService) PhoneVerifyLogin(ctx context.Context, input PhoneVerifyLoginInput) (*LoginOutput, error) {
	if err := s.ensurePhoneAuthConfigured(); err != nil {
		return nil, err
	}

	normalizedPhone, err := normalizeIndianPhoneNumber(input.PhoneNumber)
	if err != nil {
		return nil, err
	}

	result, err := s.cognitoPhone.RespondToAuthChallenge(ctx, &cognitoidentityprovider.RespondToAuthChallengeInput{
		ClientId:      aws.String(s.cfg.Cognito.Phone.ClientID),
		ChallengeName: types.ChallengeNameTypeSmsOtp,
		Session:       aws.String(strings.TrimSpace(input.Session)),
		ChallengeResponses: map[string]string{
			"USERNAME":     normalizedPhone,
			"SMS_OTP_CODE": strings.TrimSpace(input.Code),
		},
	})
	if err != nil {
		s.log.Warn("phone OTP verification failed", "phone_number", maskPhoneNumber(normalizedPhone), "error", err)
		return nil, classifyPhoneAuthError(err)
	}

	return loginOutputFromAuthResult(result.AuthenticationResult)
}

func (s *AuthService) PhoneRefresh(ctx context.Context, input RefreshInput) (*LoginOutput, error) {
	if err := s.ensurePhoneAuthConfigured(); err != nil {
		return nil, err
	}

	result, err := s.cognitoPhone.InitiateAuth(ctx, &cognitoidentityprovider.InitiateAuthInput{
		ClientId: aws.String(s.cfg.Cognito.Phone.ClientID),
		AuthFlow: types.AuthFlowTypeRefreshTokenAuth,
		AuthParameters: map[string]string{
			"REFRESH_TOKEN": input.RefreshToken,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}

	return loginOutputFromRefreshResult(result.AuthenticationResult, input.RefreshToken)
}

func (s *AuthService) PhoneLogout(ctx context.Context, accessToken string) error {
	if err := s.ensurePhoneAuthConfigured(); err != nil {
		return err
	}

	_, err := s.cognitoPhone.GlobalSignOut(ctx, &cognitoidentityprovider.GlobalSignOutInput{
		AccessToken: aws.String(accessToken),
	})
	return err
}

func (s *AuthService) ensurePhoneAuthConfigured() error {
	if s.cognitoPhone == nil || s.cfg == nil || s.cfg.Cognito.Phone.UserPoolID == "" || s.cfg.Cognito.Phone.ClientID == "" {
		return errors.New(phoneAuthDisabledMessage)
	}
	return nil
}

func (s *AuthService) cognitoClientForAccessToken(accessToken string) cognitoIdentityProviderAPI {
	if s.cognitoPhone == nil || s.cfg == nil || s.cfg.Cognito.Phone.UserPoolID == "" || s.cfg.Cognito.Phone.Region == "" {
		return s.cognito
	}

	claims := &jwt.RegisteredClaims{}
	parser := jwt.Parser{}
	if _, _, err := parser.ParseUnverified(accessToken, claims); err == nil && claims.Issuer == phonePoolIssuer(s.cfg.Cognito.Phone.Region, s.cfg.Cognito.Phone.UserPoolID) {
		return s.cognitoPhone
	}

	return s.cognito
}

func (s *AuthService) enforcePhoneOTPCooldown(ctx context.Context, purpose, phoneNumber string) error {
	if s.dynamoDB == nil || s.cfg == nil || s.cfg.Cognito.Phone.OTPCooldownTable == "" {
		return nil
	}

	now := time.Now().Unix()
	expiresAt := time.Now().Add(phoneOTPCooldownWindow).Unix()
	cooldownKey := fmt.Sprintf("%s#%s", purpose, phoneNumber)

	_, err := s.dynamoDB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.cfg.Cognito.Phone.OTPCooldownTable),
		Item: map[string]dynamodbtypes.AttributeValue{
			"cooldown_key": &dynamodbtypes.AttributeValueMemberS{Value: cooldownKey},
			"phone_number": &dynamodbtypes.AttributeValueMemberS{Value: phoneNumber},
			"purpose":      &dynamodbtypes.AttributeValueMemberS{Value: purpose},
			"expires_at":   &dynamodbtypes.AttributeValueMemberN{Value: strconv.FormatInt(expiresAt, 10)},
		},
		ConditionExpression: aws.String("attribute_not_exists(cooldown_key) OR expires_at < :now"),
		ExpressionAttributeValues: map[string]dynamodbtypes.AttributeValue{
			":now": &dynamodbtypes.AttributeValueMemberN{Value: strconv.FormatInt(now, 10)},
		},
	})
	if err == nil {
		return nil
	}

	var conditionalErr *dynamodbtypes.ConditionalCheckFailedException
	if errors.As(err, &conditionalErr) {
		return errors.New(phoneAuthRateLimitMessage)
	}

	s.log.Error("failed to enforce phone OTP cooldown", "phone_number", maskPhoneNumber(phoneNumber), "purpose", purpose, "error", err)
	return fmt.Errorf("failed to apply phone OTP cooldown")
}

func loginOutputFromAuthResult(authResult *types.AuthenticationResultType) (*LoginOutput, error) {
	if authResult == nil {
		return nil, fmt.Errorf("authentication result is nil")
	}
	if authResult.AccessToken == nil || authResult.RefreshToken == nil || authResult.TokenType == nil {
		return nil, fmt.Errorf("authentication failed: incomplete response")
	}

	return &LoginOutput{
		AccessToken:  *authResult.AccessToken,
		RefreshToken: *authResult.RefreshToken,
		ExpiresIn:    authResult.ExpiresIn,
		TokenType:    *authResult.TokenType,
	}, nil
}

func loginOutputFromRefreshResult(authResult *types.AuthenticationResultType, refreshToken string) (*LoginOutput, error) {
	if authResult == nil {
		return nil, fmt.Errorf("no authentication result")
	}
	if authResult.AccessToken == nil || authResult.TokenType == nil {
		return nil, fmt.Errorf("token refresh failed: incomplete response")
	}

	newRefreshToken := refreshToken
	if authResult.RefreshToken != nil && *authResult.RefreshToken != "" {
		newRefreshToken = *authResult.RefreshToken
	}

	return &LoginOutput{
		AccessToken:  *authResult.AccessToken,
		RefreshToken: newRefreshToken,
		ExpiresIn:    authResult.ExpiresIn,
		TokenType:    *authResult.TokenType,
	}, nil
}

func normalizeIndianPhoneNumber(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	replacer := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")
	trimmed = replacer.Replace(trimmed)

	switch {
	case strings.HasPrefix(trimmed, "+91"):
		trimmed = strings.TrimPrefix(trimmed, "+91")
	case strings.HasPrefix(trimmed, "91") && len(trimmed) == 12:
		trimmed = strings.TrimPrefix(trimmed, "91")
	}

	if !indianMobileNumberPattern.MatchString(trimmed) {
		return "", fmt.Errorf("phone_number must be a valid Indian mobile number")
	}

	return "+91" + trimmed, nil
}

func normalizeOptionalEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func isUserNotFoundError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "user not found")
}

func canonicalPhoneCognitoID(userPoolID, subject string) string {
	return fmt.Sprintf("%s:%s", userPoolID, subject)
}

func rawCognitoSubject(cognitoID string) (string, bool) {
	parts := strings.SplitN(cognitoID, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func phonePoolIssuer(region, userPoolID string) string {
	return fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", region, userPoolID)
}

func classifyPhoneAuthError(err error) error {
	var tooManyRequests *types.TooManyRequestsException
	if errors.As(err, &tooManyRequests) {
		return errors.New(phoneAuthRateLimitMessage)
	}

	var limitExceeded *types.LimitExceededException
	if errors.As(err, &limitExceeded) {
		return errors.New(phoneAuthRateLimitMessage)
	}

	var codeDeliveryFailure *types.CodeDeliveryFailureException
	if errors.As(err, &codeDeliveryFailure) {
		return fmt.Errorf("unable to deliver OTP SMS right now")
	}

	var invalidSMSRoleAccess *types.InvalidSmsRoleAccessPolicyException
	if errors.As(err, &invalidSMSRoleAccess) {
		return fmt.Errorf("SMS delivery is not configured correctly")
	}

	var invalidSMSRoleTrust *types.InvalidSmsRoleTrustRelationshipException
	if errors.As(err, &invalidSMSRoleTrust) {
		return fmt.Errorf("SMS delivery is not configured correctly")
	}

	var userNotFound *types.UserNotFoundException
	if errors.As(err, &userNotFound) {
		return fmt.Errorf("phone number not registered")
	}

	var userNotConfirmed *types.UserNotConfirmedException
	if errors.As(err, &userNotConfirmed) {
		return fmt.Errorf("phone number not verified")
	}

	var codeMismatch *types.CodeMismatchException
	if errors.As(err, &codeMismatch) {
		return fmt.Errorf("invalid OTP code")
	}

	var expiredCode *types.ExpiredCodeException
	if errors.As(err, &expiredCode) {
		return fmt.Errorf("OTP code expired, please request a new code")
	}

	var notAuthorized *types.NotAuthorizedException
	if errors.As(err, &notAuthorized) {
		return fmt.Errorf("verification session expired or code is invalid")
	}

	return errors.New(phoneAuthGenericFailure)
}

func classifyEmailAuthError(err error) error {
	var tooManyRequests *types.TooManyRequestsException
	if errors.As(err, &tooManyRequests) {
		return fmt.Errorf("too many login attempts, please try again later")
	}

	var limitExceeded *types.LimitExceededException
	if errors.As(err, &limitExceeded) {
		return fmt.Errorf("too many login attempts, please try again later")
	}

	var userNotConfirmed *types.UserNotConfirmedException
	if errors.As(err, &userNotConfirmed) {
		return fmt.Errorf("Please verify your email address")
	}

	var notAuthorized *types.NotAuthorizedException
	if errors.As(err, &notAuthorized) {
		return fmt.Errorf("incorrect email or password")
	}

	var userNotFound *types.UserNotFoundException
	if errors.As(err, &userNotFound) {
		return fmt.Errorf("incorrect email or password")
	}

	return errors.New(phoneAuthGenericFailure)
}

func maskPhoneNumber(phoneNumber string) string {
	normalized := strings.TrimSpace(phoneNumber)
	if len(normalized) <= 4 {
		return "****"
	}
	return normalized[:3] + strings.Repeat("*", len(normalized)-7) + normalized[len(normalized)-4:]
}

// TestAuthConfig holds minimal config for testing AuthService
type TestAuthConfig struct {
	CognitoClientID string
	CognitoRegion   string
	PhoneUserPoolID string
	PhoneClientID   string
	PhoneRegion     string
}

// NewAuthServiceWithMocks creates an AuthService with injected mocks for testing
func NewAuthServiceWithMocks(cfg *TestAuthConfig, userRepo interfaces.UserRepository, cognito cognitoIdentityProviderAPI, cognitoPhone cognitoIdentityProviderAPI, dynamoDB *dynamodb.Client, log *logger.Logger) *AuthService {
	var phoneCognito cognitoIdentityProviderAPI
	if cognitoPhone != nil {
		phoneCognito = cognitoPhone
	}

	return &AuthService{
		cfg: &config.Config{
			Cognito: config.CognitoConfig{
				ClientID: cfg.CognitoClientID,
				Region:   cfg.CognitoRegion,
				Phone: config.CognitoPhoneConfig{
					UserPoolID: cfg.PhoneUserPoolID,
					ClientID:   cfg.PhoneClientID,
					Region:     cfg.PhoneRegion,
				},
			},
		},
		userRepo:     userRepo,
		cognito:      cognito,
		cognitoPhone: phoneCognito,
		dynamoDB:     dynamoDB,
		log:          log,
	}
}

// NormalizeIndianPhoneNumber normalizes an Indian phone number to E.164 format
func NormalizeIndianPhoneNumber(input string) (string, error) {
	return normalizeIndianPhoneNumber(input)
}
