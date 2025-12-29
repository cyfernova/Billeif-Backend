//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "healthy", result["status"])
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
