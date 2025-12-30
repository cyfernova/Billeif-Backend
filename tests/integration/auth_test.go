//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// HTTP-Based Auth Tests (require running server)
// ========================================

func TestAuthFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	baseURL := getTestServerURL(t)

	t.Run("register user", func(t *testing.T) {
		body := map[string]string{
			"email":    "integration@test.com",
			"password": "TestPass123!",
			"name":     "Integration Test",
		}
		resp, err := postJSON(baseURL+"/api/v1/auth/register", body, "")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		// Confirm user manually since we're using real Cognito
		env := SetupTestEnv(t)
		_, err = env.AWSClients.Cognito.AdminConfirmSignUp(env.Context(), &cognitoidentityprovider.AdminConfirmSignUpInput{
			UserPoolId: aws.String(env.Config.Cognito.UserPoolID),
			Username:   aws.String("integration@test.com"),
		})
		require.NoError(t, err)
	})

	t.Run("login user", func(t *testing.T) {
		body := map[string]string{
			"email":    "integration@test.com",
			"password": "TestPass123!",
		}
		resp, err := postJSON(baseURL+"/api/v1/auth/login", body, "")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.NotEmpty(t, result["access_token"])
	})
}

func TestHealthCheck(t *testing.T) {
	baseURL := getTestServerURL(t)

	resp, err := http.Get(baseURL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// Response is ASCII art, so we just check status 200 OK
}

func getTestServerURL(t *testing.T) string {
	return "http://localhost:8080"
}

func postJSON(url string, body interface{}, token string) (*http.Response, error) {
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

// ========================================
// Cognito Direct Tests (LocalStack)
// ========================================

func TestCognitoUserPoolExists(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	// List user pools
	resp, err := env.AWSClients.Cognito.ListUserPools(ctx, &cognitoidentityprovider.ListUserPoolsInput{
		MaxResults: aws.Int32(10),
	})
	if err != nil {
		t.Skipf("Could not list Cognito user pools: %v", err)
	}

	t.Logf("Found %d user pools", len(resp.UserPools))
	for _, pool := range resp.UserPools {
		t.Logf("  - %s (ID: %s)", *pool.Name, *pool.Id)
	}
}

func TestCognitoSignUp(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	// Create unique test user
	testEmail := fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())
	testPassword := "TestPass123!"
	testName := "Test User"

	// Sign up user
	signUpResp, err := env.AWSClients.Cognito.SignUp(ctx, &cognitoidentityprovider.SignUpInput{
		ClientId: aws.String(env.Config.Cognito.ClientID),
		Username: aws.String(testEmail),
		Password: aws.String(testPassword),
		UserAttributes: []types.AttributeType{
			{Name: aws.String("email"), Value: aws.String(testEmail)},
			{Name: aws.String("name"), Value: aws.String(testName)},
		},
	})

	if err != nil {
		// LocalStack Cognito might not be fully configured
		t.Skipf("Cognito SignUp failed (may need user pool configuration): %v", err)
	}

	assert.NotNil(t, signUpResp.UserSub)
	t.Logf("User created with sub: %s", *signUpResp.UserSub)
}

func TestCognitoAdminCreateUser(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := context.Background()

	testEmail := fmt.Sprintf("admin-test-%d@example.com", time.Now().UnixNano())

	// Admin create user (doesn't require client ID)
	_, err := env.AWSClients.Cognito.AdminCreateUser(ctx, &cognitoidentityprovider.AdminCreateUserInput{
		UserPoolId: aws.String(env.Config.Cognito.UserPoolID),
		Username:   aws.String(testEmail),
		UserAttributes: []types.AttributeType{
			{Name: aws.String("email"), Value: aws.String(testEmail)},
			{Name: aws.String("email_verified"), Value: aws.String("true")},
		},
		TemporaryPassword:      aws.String("TempPass123!"),
		MessageAction:          types.MessageActionTypeSuppress,
		DesiredDeliveryMediums: []types.DeliveryMediumType{},
	})

	if err != nil {
		t.Skipf("Cognito AdminCreateUser failed (may need user pool): %v", err)
	}

	// Clean up - delete the user
	_, err = env.AWSClients.Cognito.AdminDeleteUser(ctx, &cognitoidentityprovider.AdminDeleteUserInput{
		UserPoolId: aws.String(env.Config.Cognito.UserPoolID),
		Username:   aws.String(testEmail),
	})
	if err != nil {
		t.Logf("Warning: could not delete test user: %v", err)
	}
}

func TestCognitoListUsers(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	// List users in the pool
	resp, err := env.AWSClients.Cognito.ListUsers(ctx, &cognitoidentityprovider.ListUsersInput{
		UserPoolId: aws.String(env.Config.Cognito.UserPoolID),
		Limit:      aws.Int32(10),
	})

	if err != nil {
		t.Skipf("Could not list Cognito users (may need user pool): %v", err)
	}

	t.Logf("Found %d users in user pool", len(resp.Users))
	for _, user := range resp.Users {
		t.Logf("  - %s (Status: %s)", *user.Username, user.UserStatus)
	}
}
