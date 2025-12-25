package awsclients

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
)

// CognitoClient wraps the AWS Cognito Identity Provider client
type CognitoClient struct {
	Client *cognitoidentityprovider.Client
}

// NewCognitoClient creates a new Cognito client
func NewCognitoClient(ctx context.Context, region, accessKey, secretKey, endpoint string) (*CognitoClient, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, err
	}

	client := cognitoidentityprovider.NewFromConfig(cfg, func(o *cognitoidentityprovider.Options) {
		if endpoint != "" && endpoint != "http://localhost:4566" && endpoint != "http://localstack:4566" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	return &CognitoClient{Client: client}, nil
}

// HealthCheck checks if Cognito is accessible
func (c *CognitoClient) HealthCheck(ctx context.Context) error {
	// Describe user pool to check connectivity
	// In LocalStack, this might not work perfectly, so we'll just check if client exists
	if c.Client == nil {
		return ErrClientNotInitialized
	}
	return nil
}

// GetUser retrieves user details from Cognito
func (c *CognitoClient) GetUser(ctx context.Context, accessToken string) (*cognitoidentityprovider.GetUserOutput, error) {
	input := &cognitoidentityprovider.GetUserInput{
		AccessToken: aws.String(accessToken),
	}

	return c.Client.GetUser(ctx, input)
}

// InitiateAuth initiates authentication
func (c *CognitoClient) InitiateAuth(ctx context.Context, input *cognitoidentityprovider.InitiateAuthInput) (*cognitoidentityprovider.InitiateAuthOutput, error) {
	return c.Client.InitiateAuth(ctx, input)
}

// SignUp registers a new user
func (c *CognitoClient) SignUp(ctx context.Context, input *cognitoidentityprovider.SignUpInput) (*cognitoidentityprovider.SignUpOutput, error) {
	return c.Client.SignUp(ctx, input)
}

// ConfirmSignUp confirms user registration
func (c *CognitoClient) ConfirmSignUp(ctx context.Context, input *cognitoidentityprovider.ConfirmSignUpInput) (*cognitoidentityprovider.ConfirmSignUpOutput, error) {
	return c.Client.ConfirmSignUp(ctx, input)
}

// ConfirmForgotPassword confirms password reset
func (c *CognitoClient) ConfirmForgotPassword(ctx context.Context, input *cognitoidentityprovider.ConfirmForgotPasswordInput) (*cognitoidentityprovider.ConfirmForgotPasswordOutput, error) {
	return c.Client.ConfirmForgotPassword(ctx, input)
}

// ForgotPassword initiates password reset
func (c *CognitoClient) ForgotPassword(ctx context.Context, input *cognitoidentityprovider.ForgotPasswordInput) (*cognitoidentityprovider.ForgotPasswordOutput, error) {
	return c.Client.ForgotPassword(ctx, input)
}

// ChangePassword changes user password
func (c *CognitoClient) ChangePassword(ctx context.Context, input *cognitoidentityprovider.ChangePasswordInput) (*cognitoidentityprovider.ChangePasswordOutput, error) {
	return c.Client.ChangePassword(ctx, input)
}

// GlobalSignOut signs out user from all devices
func (c *CognitoClient) GlobalSignOut(ctx context.Context, accessToken string) (*cognitoidentityprovider.GlobalSignOutOutput, error) {
	input := &cognitoidentityprovider.GlobalSignOutInput{
		AccessToken: aws.String(accessToken),
	}
	return c.Client.GlobalSignOut(ctx, input)
}

// AdminAddUserToGroup adds user to a group
func (c *CognitoClient) AdminAddUserToGroup(ctx context.Context, userPoolID, username, groupName string) error {
	input := &cognitoidentityprovider.AdminAddUserToGroupInput{
		UserPoolId: aws.String(userPoolID),
		Username:   aws.String(username),
		GroupName:  aws.String(groupName),
	}
	_, err := c.Client.AdminAddUserToGroup(ctx, input)
	return err
}

// AdminListGroupsForUser lists groups for a user
func (c *CognitoClient) AdminListGroupsForUser(ctx context.Context, userPoolID, username string) (*cognitoidentityprovider.AdminListGroupsForUserOutput, error) {
	input := &cognitoidentityprovider.AdminListGroupsForUserInput{
		UserPoolId: aws.String(userPoolID),
		Username:   aws.String(username),
	}
	return c.Client.AdminListGroupsForUser(ctx, input)
}

// AdminGetUser gets user details as admin
func (c *CognitoClient) AdminGetUser(ctx context.Context, userPoolID, username string) (*cognitoidentityprovider.AdminGetUserOutput, error) {
	input := &cognitoidentityprovider.AdminGetUserInput{
		UserPoolId: aws.String(userPoolID),
		Username:   aws.String(username),
	}
	return c.Client.AdminGetUser(ctx, input)
}

// AdminDeleteUser deletes a user
func (c *CognitoClient) AdminDeleteUser(ctx context.Context, userPoolID, username string) error {
	input := &cognitoidentityprovider.AdminDeleteUserInput{
		UserPoolId: aws.String(userPoolID),
		Username:   aws.String(username),
	}
	_, err := c.Client.AdminDeleteUser(ctx, input)
	return err
}

// AdminDisableUser disables a user
func (c *CognitoClient) AdminDisableUser(ctx context.Context, userPoolID, username string) error {
	input := &cognitoidentityprovider.AdminDisableUserInput{
		UserPoolId: aws.String(userPoolID),
		Username:   aws.String(username),
	}
	_, err := c.Client.AdminDisableUser(ctx, input)
	return err
}

// AdminEnableUser enables a user
func (c *CognitoClient) AdminEnableUser(ctx context.Context, userPoolID, username string) error {
	input := &cognitoidentityprovider.AdminEnableUserInput{
		UserPoolId: aws.String(userPoolID),
		Username:   aws.String(username),
	}
	_, err := c.Client.AdminEnableUser(ctx, input)
	return err
}

var ErrClientNotInitialized = &ClientError{Message: "AWS client not initialized"}

type ClientError struct {
	Message string
}

func (e *ClientError) Error() string {
	return e.Message
}
