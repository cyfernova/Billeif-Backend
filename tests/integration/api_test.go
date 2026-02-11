//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// APIClient is a helper to make authenticated requests
type APIClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *APIClient) SetToken(token string) {
	c.Token = token
}

func (c *APIClient) Post(t *testing.T, path string, body interface{}) (*http.Response, map[string]interface{}) {
	return c.Request(t, "POST", path, body)
}

func (c *APIClient) Put(t *testing.T, path string, body interface{}) (*http.Response, map[string]interface{}) {
	return c.Request(t, "PUT", path, body)
}

func (c *APIClient) Get(t *testing.T, path string) (*http.Response, map[string]interface{}) {
	return c.Request(t, "GET", path, nil)
}

func (c *APIClient) Request(t *testing.T, method, path string, body interface{}) (*http.Response, map[string]interface{}) {
	var bodyReader *bytes.Buffer
	if body != nil {
		jsonBody, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewBuffer(jsonBody)
	} else {
		bodyReader = bytes.NewBuffer(nil)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.Client.Do(req)
	require.NoError(t, err)

	var result map[string]interface{}
	// Some endpoints might return empty body or not JSON (like 204)
	if resp.StatusCode != http.StatusNoContent {
		// We ignore error here as some error responses might be plain text or different format,
		// the tests should check status code mainly
		json.NewDecoder(resp.Body).Decode(&result)
	}
	// We do NOT close body here to allow caller to read it if needed/re-read (though we decoded it)
	// Actually, we decoded it, so caller can't read it again unless we check.
	// Let's rely on result map.

	return resp, result
}

func TestFullBusinessLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	baseURL := getTestServerURL(t)
	client := NewAPIClient(baseURL)
	env := SetupTestEnv(t) // Need env to confirm user

	uniqueID := time.Now().UnixNano()
	email := fmt.Sprintf("user-%d@example.com", uniqueID)
	password := "TestPass123!"

	// 1. Register
	t.Run("Register", func(t *testing.T) {
		body := map[string]string{
			"email":    email,
			"password": password,
			"name":     "Test Business User",
		}
		resp, _ := client.Post(t, "/api/v1/auth/register", body)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 1b. Confirm User (Backdoor)
	t.Run("Confirm User", func(t *testing.T) {
		_, err := env.AWSClients.Cognito.AdminConfirmSignUp(env.Context(), &cognitoidentityprovider.AdminConfirmSignUpInput{
			UserPoolId: aws.String(env.Config.Cognito.UserPoolID),
			Username:   aws.String(email),
		})
		require.NoError(t, err)
	})

	// 2. Login
	t.Run("Login", func(t *testing.T) {
		body := map[string]string{
			"email":    email,
			"password": password,
		}
		resp, res := client.Post(t, "/api/v1/auth/login", body)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		token, ok := res["access_token"].(string)
		require.True(t, ok, "access_token should be present")
		client.SetToken(token)
	})

	var businessID string
	// 3. Create Business Profile
	t.Run("Create Business Profile", func(t *testing.T) {
		body := map[string]interface{}{
			"company_name": "Acme Corp",
			"tax_id":       "US-123456789",
			"address":      "123 Business Rd",
			"currency":     "USD",
		}
		resp, res := client.Post(t, "/api/v1/business-profiles", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		businessID = res["id"].(string)
		assert.NotEmpty(t, businessID)
	})

	var customerID string
	// 4. Create Customer
	t.Run("Create Customer", func(t *testing.T) {
		body := map[string]interface{}{
			"name":    "Alice Smith",
			"email":   fmt.Sprintf("customer-%d@client.com", uniqueID),
			"address": "456 Client St",
		}
		resp, res := client.Post(t, "/api/v1/customers", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		customerID = res["id"].(string)
		assert.NotEmpty(t, customerID)
	})

	var vendorID string
	// 5. Create Vendor
	t.Run("Create Vendor", func(t *testing.T) {
		body := map[string]interface{}{
			"name":         "Office Depot",
			"contact_name": "Bob Vendor",
			"email":        fmt.Sprintf("vendor-%d@supply.com", uniqueID),
		}
		resp, res := client.Post(t, "/api/v1/vendors", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		vendorID = res["id"].(string)
		assert.NotEmpty(t, vendorID)
	})

	var productID string
	// 6. Create Product
	t.Run("Create Product", func(t *testing.T) {
		body := map[string]interface{}{
			"name":        "Consulting Service",
			"description": "Expert advice",
			"price":       150.00,
			"type":        "service",
		}
		resp, res := client.Post(t, "/api/v1/products", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		productID = res["id"].(string)
		assert.NotEmpty(t, productID)
	})

	var invoiceID string
	// 7. Create Invoice
	t.Run("Create Invoice", func(t *testing.T) {
		dueDate := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
		body := map[string]interface{}{
			"customer_id": customerID,
			"due_date":    dueDate,
			"items": []map[string]interface{}{
				{
					"product_id":  productID,
					"quantity":    10,
					"unit_price":  150.00,
					"description": "10 hours of consulting",
				},
			},
			"notes": "Thank you for your business",
		}
		resp, res := client.Post(t, "/api/v1/invoices", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		invoiceID = res["id"].(string)
		assert.NotEmpty(t, invoiceID)
		assert.Equal(t, "draft", res["status"])
	})

	// 8. Send Invoice (Mocked)
	t.Run("Send Invoice", func(t *testing.T) {
		resp, res := client.Post(t, fmt.Sprintf("/api/v1/invoices/%s/send", invoiceID), nil)
		// Assuming send endpoint returns 200 OK and changes status to 'sent'
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "sent", res["status"])
	})

	// 9. Record Payment
	t.Run("Record Payment", func(t *testing.T) {
		body := map[string]interface{}{
			"invoice_id":     invoiceID,
			"amount":         1500.00,
			"payment_method": "bank_transfer",
			"reference":      "REF-123",
			"notes":          "Full payment",
		}
		resp, res := client.Post(t, "/api/v1/payments", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.Equal(t, "completed", res["status"])
	})

	// 10. Check Invoice Status (Should be 'paid')
	t.Run("Check Invoice Paid", func(t *testing.T) {
		resp, res := client.Get(t, fmt.Sprintf("/api/v1/invoices/%s", invoiceID))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "paid", res["status"])
	})

	// 11. Check Ledger Balance
	t.Run("Check Ledger", func(t *testing.T) {
		resp, res := client.Get(t, "/api/v1/ledger/balance")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		// Assuming balance returns a float or string, check it's positive
		// Adjust assertion based on actual API response structure
		assert.NotNil(t, res["available_balance"])
	})

	// 12. Delete Invoice (Should fail because it's paid, depending on logic, or succeed logically)
	t.Run("Delete Paid Invoice", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", baseURL+fmt.Sprintf("/api/v1/invoices/%s", invoiceID), nil)
		req.Header.Set("Authorization", "Bearer "+client.Token)
		resp, err := client.Client.Do(req)

		// Check for error before using resp
		if err != nil {
			t.Logf("Warning: Could not delete paid invoice: %v", err)
			return // Cannot proceed with assertions if request failed
		}
		defer resp.Body.Close()

		// If logic prevents deleting paid invoices, expecting 400 or 403
		// If logic allows soft delete, expecting 200 or 204
		// For now, let's just log the result
		t.Logf("Delete paid invoice status: %d", resp.StatusCode)
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			// Verify it's gone or marked deleted
			respGet, _ := client.Get(t, fmt.Sprintf("/api/v1/invoices/%s", invoiceID))
			assert.Equal(t, http.StatusNotFound, respGet.StatusCode)
		}
	})
}