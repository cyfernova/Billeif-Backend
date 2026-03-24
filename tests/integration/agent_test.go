//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	baseURL := getTestServerURL(t)
	client := NewAPIClient(baseURL)
	env := SetupTestEnv(t)

	uniqueID := time.Now().UnixNano()
	email := fmt.Sprintf("agent-test-%d@example.com", uniqueID)
	password := "TestPass123!"

	var userID string
	var businessID string

	t.Run("Register User", func(t *testing.T) {
		body := map[string]string{
			"email":    email,
			"password": password,
			"name":     "Agent Test User",
		}
		resp, _ := client.Post(t, "/api/v1/auth/register", body)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	t.Run("Confirm User", func(t *testing.T) {
		_, err := env.AWSClients.Cognito.AdminConfirmSignUp(env.Context(), &cognitoidentityprovider.AdminConfirmSignUpInput{
			UserPoolId: aws.String(env.Config.Cognito.UserPoolID),
			Username:   aws.String(email),
		})
		require.NoError(t, err)
	})

	t.Run("Login", func(t *testing.T) {
		body := map[string]string{
			"email":    email,
			"password": password,
		}
		resp, res := client.Post(t, "/api/v1/auth/login", body)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		token, ok := res["access_token"].(string)
		require.True(t, ok)
		client.SetToken(token)

		if user, ok := res["user"].(map[string]interface{}); ok {
			userID = user["id"].(string)
		}
		assert.NotEmpty(t, userID)
	})

	t.Run("Create Business Profile", func(t *testing.T) {
		body := map[string]interface{}{
			"company_name": "Agent Test Business LLC",
			"tax_id":       "US-987654321",
			"address":      "456 Agent Street, Test City",
			"currency":     "USD",
		}
		resp, res := client.Post(t, "/api/v1/business-profiles", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		businessID = res["id"].(string)
		assert.NotEmpty(t, businessID)
	})

	var shoppingAgentID string
	t.Run("Create Shopping Agent", func(t *testing.T) {
		body := map[string]interface{}{
			"type":        "shopping",
			"name":        "My Shopping Agent",
			"description": "Personal shopping assistant",
			"business_id": businessID,
		}
		resp, res := client.Post(t, "/api/v1/agents", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		shoppingAgentID = res["id"].(string)
		assert.NotEmpty(t, shoppingAgentID)
		assert.Equal(t, "shopping", res["type"])
		assert.Equal(t, "My Shopping Agent", res["name"])
	})

	var merchantAgentID string
	t.Run("Create Merchant Agent", func(t *testing.T) {
		body := map[string]interface{}{
			"type":        "merchant",
			"name":        "My Merchant Agent",
			"description": "Business merchant agent",
			"business_id": businessID,
			"config": map[string]interface{}{
				"product_ids": []string{},
			},
		}
		resp, res := client.Post(t, "/api/v1/agents", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		merchantAgentID = res["id"].(string)
		assert.NotEmpty(t, merchantAgentID)
		assert.Equal(t, "merchant", res["type"])
	})

	t.Run("Create Buyer Agent", func(t *testing.T) {
		body := map[string]interface{}{
			"type":        "buyer",
			"name":        "Buyer Agent 1",
			"description": "Buyer agent for negotiations",
			"config": map[string]interface{}{
				"volatility": 0.3,
				"strategy":   "aggressive",
			},
			"business_id": businessID,
		}
		resp, res := client.Post(t, "/api/v1/agents", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.Equal(t, "shopping", res["type"])
		assert.Equal(t, "buyer", res["marketplace_role"])
	})

	t.Run("Create Seller Agent", func(t *testing.T) {
		body := map[string]interface{}{
			"type":        "seller",
			"name":        "Seller Agent 1",
			"description": "Seller agent for negotiations",
			"config": map[string]interface{}{
				"volatility": 0.7,
				"strategy":   "conservative",
			},
			"business_id": businessID,
		}
		resp, res := client.Post(t, "/api/v1/agents", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.Equal(t, "merchant", res["type"])
		assert.Equal(t, "seller", res["marketplace_role"])
	})

	t.Run("Create Agent - Invalid Type", func(t *testing.T) {
		body := map[string]interface{}{
			"type":        "invalid_type",
			"name":        "Invalid Agent",
			"business_id": businessID,
		}
		resp, _ := client.Post(t, "/api/v1/agents", body)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("Create Agent - Missing Required Fields", func(t *testing.T) {
		body := map[string]interface{}{
			"description": "Agent without name or type",
		}
		resp, _ := client.Post(t, "/api/v1/agents", body)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("List Agents by User", func(t *testing.T) {
		resp, res := client.Get(t, "/api/v1/agents")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, res, "data")
		assert.Contains(t, res, "total")
		assert.Contains(t, res, "page")
		assert.Contains(t, res, "limit")

		data, ok := res["data"].([]interface{})
		require.True(t, ok)
		assert.Greater(t, len(data), 0)
	})

	t.Run("List Agents by Business", func(t *testing.T) {
		resp, res := client.Get(t, fmt.Sprintf("/api/v1/agents?business_id=%s", businessID))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, res, "data")
		assert.Contains(t, res, "total")
	})

	t.Run("Get Agent by ID", func(t *testing.T) {
		resp, res := client.Get(t, fmt.Sprintf("/api/v1/agents/%s", shoppingAgentID))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, shoppingAgentID, res["id"])
		assert.Equal(t, "shopping", res["type"])
	})

	t.Run("Get Agent - Not Found", func(t *testing.T) {
		resp, _ := client.Get(t, "/api/v1/agents/non-existent-uuid")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("Update Agent", func(t *testing.T) {
		body := map[string]interface{}{
			"name":        "Updated Shopping Agent",
			"description": "Updated description",
			"is_active":   true,
		}
		resp, res := client.Put(t, fmt.Sprintf("/api/v1/agents/%s", shoppingAgentID), body)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "agent updated successfully", res["message"])

		resp, res = client.Get(t, fmt.Sprintf("/api/v1/agents/%s", shoppingAgentID))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "Updated Shopping Agent", res["name"])
	})

	t.Run("Add Capability to Agent", func(t *testing.T) {
		body := map[string]interface{}{
			"capability_type": "search",
			"description":     "Product search capability",
			"config": map[string]interface{}{
				"max_results": 10,
			},
		}
		resp, res := client.Post(t, fmt.Sprintf("/api/v1/agents/%s/capabilities", shoppingAgentID), body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.Equal(t, "capability added successfully", res["message"])
	})

	t.Run("Get Agent Capabilities", func(t *testing.T) {
		resp, capabilities := client.GetArray(t, fmt.Sprintf("/api/v1/agents/%s/capabilities", shoppingAgentID))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.GreaterOrEqual(t, len(capabilities), 1)
	})

	var capabilityID string
	t.Run("Get Capability ID from List", func(t *testing.T) {
		resp, capabilities := client.GetArray(t, fmt.Sprintf("/api/v1/agents/%s/capabilities", shoppingAgentID))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		if len(capabilities) > 0 {
			cap, ok := capabilities[0].(map[string]interface{})
			require.True(t, ok)
			capabilityID = cap["id"].(string)
		}
		assert.NotEmpty(t, capabilityID)
	})

	t.Run("Remove Capability from Agent", func(t *testing.T) {
		if capabilityID == "" {
			t.Skip("No capability ID available")
		}
		resp, res := client.Delete(t, fmt.Sprintf("/api/v1/agents/%s/capabilities/%s", shoppingAgentID, capabilityID))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "capability removed successfully", res["message"])
	})

	t.Run("Get Active Agents - Shopping", func(t *testing.T) {
		resp, agents := client.GetArray(t, "/api/v1/agents/active?type=shopping")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.GreaterOrEqual(t, len(agents), 1)
	})

	t.Run("Get Active Agents - Missing Type", func(t *testing.T) {
		resp, _ := client.Get(t, "/api/v1/agents/active")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("Get Agent By Type - Shopping", func(t *testing.T) {
		resp, res := client.Get(t, "/api/v1/agents/type/shopping")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, res, "data")
		assert.Contains(t, res, "total")
	})

	t.Run("Get Agent By Type - Merchant", func(t *testing.T) {
		resp, res := client.Get(t, "/api/v1/agents/type/merchant")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, res, "data")
		data, ok := res["data"].([]interface{})
		require.True(t, ok)
		assert.GreaterOrEqual(t, len(data), 1)
	})

	t.Run("Get Agent By Type - Invalid Type", func(t *testing.T) {
		resp, _ := client.Get(t, "/api/v1/agents/type/invalid")
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("Validate Agent Permissions - Own Agent", func(t *testing.T) {
		resp, res := client.Post(t, fmt.Sprintf("/api/v1/agents/validate-permissions/%s", shoppingAgentID), nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, true, res["has_permission"])
	})

	t.Run("Validate Agent Permissions - Non-existent Agent", func(t *testing.T) {
		resp, res := client.Post(t, "/api/v1/agents/validate-permissions/non-existent", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, false, res["has_permission"])
	})

	t.Run("Delete Agent", func(t *testing.T) {
		resp, _ := client.Delete(t, fmt.Sprintf("/api/v1/agents/%s", shoppingAgentID))
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		resp, _ = client.Get(t, fmt.Sprintf("/api/v1/agents/%s", shoppingAgentID))
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("Pagination - List Agents", func(t *testing.T) {
		resp, res := client.Get(t, "/api/v1/agents?page=1&limit=5")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, float64(1), res["page"])
		assert.Equal(t, float64(5), res["limit"])
	})
}

func TestAgentEndpointUnauthenticated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	baseURL := getTestServerURL(t)
	client := NewAPIClient(baseURL)

	t.Run("GET /api/v1/agents - Without Auth", func(t *testing.T) {
		resp, _ := client.Get(t, "/api/v1/agents")
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("GET /api/v1/agents/type/shopping - Without Auth", func(t *testing.T) {
		resp, _ := client.Get(t, "/api/v1/agents/type/shopping")
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("GET /api/v1/agents/active?type=shopping - Without Auth", func(t *testing.T) {
		resp, _ := client.Get(t, "/api/v1/agents/active?type=shopping")
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("POST /api/v1/agents - Without Auth", func(t *testing.T) {
		body := map[string]interface{}{
			"type": "shopping",
			"name": "Test Agent",
		}
		resp, _ := client.Post(t, "/api/v1/agents", body)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("GET /api/v1/agents/:id - Without Auth", func(t *testing.T) {
		resp, _ := client.Get(t, "/api/v1/agents/some-uuid")
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("PUT /api/v1/agents/:id - Without Auth", func(t *testing.T) {
		body := map[string]interface{}{"name": "Updated"}
		resp, _ := client.Put(t, "/api/v1/agents/some-uuid", body)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("DELETE /api/v1/agents/:id - Without Auth", func(t *testing.T) {
		resp, _ := client.Delete(t, "/api/v1/agents/some-uuid")
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("POST /api/v1/agents/:id/capabilities - Without Auth", func(t *testing.T) {
		body := map[string]interface{}{
			"capability_type": "search",
			"description":     "Test capability",
		}
		resp, _ := client.Post(t, "/api/v1/agents/some-uuid/capabilities", body)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("GET /api/v1/agents/:id/capabilities - Without Auth", func(t *testing.T) {
		resp, _ := client.Get(t, "/api/v1/agents/some-uuid/capabilities")
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("DELETE /api/v1/agents/:id/capabilities/:capability_id - Without Auth", func(t *testing.T) {
		resp, _ := client.Delete(t, "/api/v1/agents/some-uuid/capabilities/some-cap-id")
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("POST /api/v1/agents/validate-permissions/:id - Without Auth", func(t *testing.T) {
		resp, _ := client.Post(t, "/api/v1/agents/validate-permissions/some-uuid", nil)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}
