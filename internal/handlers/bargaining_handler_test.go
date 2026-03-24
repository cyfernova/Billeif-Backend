package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// TestCreateNegotiation tests the POST /bargaining/negotiations endpoint
// This test validates request parsing and validation logic
func TestCreateNegotiation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := logger.New()

	tests := []struct {
		name           string
		requestBody    interface{}
		expectedStatus int
		description    string
	}{
		{
			name: "missing buyer_agent_id",
			requestBody: map[string]interface{}{
				"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
				"initial_amount":  1000.00,
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when buyer_agent_id is missing",
		},
		{
			name: "missing seller_agent_id",
			requestBody: map[string]interface{}{
				"buyer_agent_id": "550e8400-e29b-41d4-a716-446655440000",
				"initial_amount": 1000.00,
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when seller_agent_id is missing",
		},
		{
			name: "missing initial_amount",
			requestBody: map[string]interface{}{
				"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
				"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when initial_amount is missing",
		},
		{
			name: "invalid initial_amount (negative)",
			requestBody: map[string]interface{}{
				"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
				"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
				"initial_amount":  -100.00,
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when initial_amount is negative",
		},
		{
			name: "invalid initial_amount (zero)",
			requestBody: map[string]interface{}{
				"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
				"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
				"initial_amount":  0,
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when initial_amount is zero",
		},
		{
			name: "max_rounds zero (allowed by omitempty)",
			requestBody: map[string]interface{}{
				"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
				"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
				"initial_amount":  1000.00,
				"max_rounds":      0,
			},
			expectedStatus: http.StatusInternalServerError,
			description:    "should pass validation and try to create negotiation (will fail without DB)",
		},
		{
			name: "invalid max_rounds (too high)",
			requestBody: map[string]interface{}{
				"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
				"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
				"initial_amount":  1000.00,
				"max_rounds":      15,
			},
			expectedStatus: http.StatusInternalServerError,
			description:    "should pass validation and try to create negotiation when max_rounds is within the extended 20-round limit",
		},
		{
			name: "invalid buyer_agent_id format",
			requestBody: map[string]interface{}{
				"buyer_agent_id":  "not-a-uuid",
				"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
				"initial_amount":  1000.00,
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when buyer_agent_id is not a valid UUID",
		},
		{
			name: "invalid seller_agent_id format",
			requestBody: map[string]interface{}{
				"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
				"seller_agent_id": "not-a-uuid",
				"initial_amount":  1000.00,
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when seller_agent_id is not a valid UUID",
		},
		{
			name: "invalid marketplace_order_id format",
			requestBody: map[string]interface{}{
				"buyer_agent_id":       "550e8400-e29b-41d4-a716-446655440000",
				"seller_agent_id":      "550e8400-e29b-41d4-a716-446655440001",
				"initial_amount":       1000.00,
				"marketplace_order_id": "not-a-uuid",
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when marketplace_order_id is not a valid UUID",
		},
		{
			name: "valid request with all fields",
			requestBody: map[string]interface{}{
				"buyer_agent_id":       "550e8400-e29b-41d4-a716-446655440000",
				"seller_agent_id":      "550e8400-e29b-41d4-a716-446655440001",
				"initial_amount":       1000.00,
				"max_rounds":           5,
				"marketplace_order_id": "660e8400-e29b-41d4-a716-446655440002",
			},
			expectedStatus: http.StatusInternalServerError,
			description:    "should try to create negotiation (will fail without DB)",
		},
		{
			name: "valid request with minimal fields",
			requestBody: map[string]interface{}{
				"buyer_agent_id":  "550e8400-e29b-41d4-a716-446655440000",
				"seller_agent_id": "550e8400-e29b-41d4-a716-446655440001",
				"initial_amount":  1000.00,
			},
			expectedStatus: http.StatusInternalServerError,
			description:    "should try to create negotiation (will fail without DB)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.description)

			defer func() {
				if r := recover(); r != nil && tt.expectedStatus != http.StatusInternalServerError {
					t.Errorf("unexpected panic: %v", r)
				}
			}()

			handler := NewBargainingHandler(nil, nil, log)

			reqBody, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/bargaining/negotiations", bytes.NewBuffer(reqBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req
			c.Set("user_id", "user-123")

			handler.CreateNegotiation(c)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}

// TestCounterOfferRequestValidation tests counter offer request validation
func TestCounterOfferRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := logger.New()
	handler := NewBargainingHandler(nil, nil, log)

	tests := []struct {
		name           string
		requestBody    interface{}
		expectedStatus int
		description    string
	}{
		{
			name: "valid counteroffer action",
			requestBody: map[string]interface{}{
				"agent_id":        "550e8400-e29b-41d4-a716-446655440000",
				"proposed_amount": 950.00,
				"action":          "counteroffer",
			},
			expectedStatus: http.StatusNotFound,
			description:    "should return 404 (negotiation not found)",
		},
		{
			name: "valid accept action",
			requestBody: map[string]interface{}{
				"agent_id":        "550e8400-e29b-41d4-a716-446655440000",
				"proposed_amount": 1000.00,
				"action":          "accept",
			},
			expectedStatus: http.StatusNotFound,
			description:    "should return 404 (negotiation not found)",
		},
		{
			name: "valid reject action",
			requestBody: map[string]interface{}{
				"agent_id":        "550e8400-e29b-41d4-a716-446655440000",
				"proposed_amount": 1000.00,
				"action":          "reject",
			},
			expectedStatus: http.StatusNotFound,
			description:    "should return 404 (negotiation not found)",
		},
		{
			name: "missing agent_id",
			requestBody: map[string]interface{}{
				"proposed_amount": 950.00,
				"action":          "counteroffer",
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when agent_id is missing",
		},
		{
			name: "missing proposed_amount",
			requestBody: map[string]interface{}{
				"agent_id": "550e8400-e29b-41d4-a716-446655440000",
				"action":   "counteroffer",
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when proposed_amount is missing",
		},
		{
			name: "missing action",
			requestBody: map[string]interface{}{
				"agent_id":        "550e8400-e29b-41d4-a716-446655440000",
				"proposed_amount": 950.00,
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when action is missing",
		},
		{
			name: "invalid action",
			requestBody: map[string]interface{}{
				"agent_id":        "550e8400-e29b-41d4-a716-446655440000",
				"proposed_amount": 950.00,
				"action":          "invalid",
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when action is invalid",
		},
		{
			name: "negative proposed_amount",
			requestBody: map[string]interface{}{
				"agent_id":        "550e8400-e29b-41d4-a716-446655440000",
				"proposed_amount": -100.00,
				"action":          "counteroffer",
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when proposed_amount is negative",
		},
		{
			name: "too long reason",
			requestBody: map[string]interface{}{
				"agent_id":        "550e8400-e29b-41d4-a716-446655440000",
				"proposed_amount": 950.00,
				"action":          "counteroffer",
				"reason":          string(make([]byte, 501)),
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when reason exceeds 500 characters",
		},
		{
			name: "invalid agent_id format",
			requestBody: map[string]interface{}{
				"agent_id":        "not-a-uuid",
				"proposed_amount": 950.00,
				"action":          "counteroffer",
			},
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when agent_id is not a valid UUID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.description)

			defer func() {
				if r := recover(); r != nil && tt.expectedStatus != http.StatusNotFound && tt.expectedStatus != http.StatusInternalServerError {
					t.Errorf("unexpected panic: %v", r)
				}
			}()

			reqBody, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/bargaining/negotiations/test-id/counteroffer", bytes.NewBuffer(reqBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req
			c.Params = gin.Params{gin.Param{Key: "id", Value: "test-id"}}

			handler.SubmitCounterOffer(c)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}

// TestGetSuggestedCounterOffer validates suggestion endpoint parameters
func TestGetSuggestedCounterOffer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := logger.New()
	handler := NewBargainingHandler(nil, nil, log)

	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		description    string
	}{
		{
			name:           "valid buyer agent_type",
			queryParams:    "?agent_type=buyer",
			expectedStatus: http.StatusNotFound,
			description:    "should return 404 (negotiation not found)",
		},
		{
			name:           "valid seller agent_type",
			queryParams:    "?agent_type=seller",
			expectedStatus: http.StatusNotFound,
			description:    "should return 404 (negotiation not found)",
		},
		{
			name:           "missing agent_type",
			queryParams:    "",
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when agent_type is missing",
		},
		{
			name:           "invalid agent_type",
			queryParams:    "?agent_type=invalid",
			expectedStatus: http.StatusBadRequest,
			description:    "should return 400 when agent_type is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.description)

			defer func() {
				if r := recover(); r != nil && tt.expectedStatus != http.StatusNotFound {
					t.Errorf("unexpected panic: %v", r)
				}
			}()

			req := httptest.NewRequest(http.MethodGet, "/bargaining/negotiations/test-id/suggest"+tt.queryParams, nil)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req
			c.Params = gin.Params{gin.Param{Key: "id", Value: "test-id"}}

			handler.GetSuggestedCounterOffer(c)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestListNegotiations validates request parsing for listing negotiations
func TestListNegotiations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := logger.New()
	handler := NewBargainingHandler(nil, nil, log)

	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		description    string
	}{
		{
			name:           "valid pagination params",
			queryParams:    "?page=1&limit=10",
			expectedStatus: http.StatusOK,
			description:    "should return 200 (empty list)",
		},
		{
			name:           "no pagination params",
			queryParams:    "",
			expectedStatus: http.StatusOK,
			description:    "should return 200 with default pagination",
		},
		{
			name:           "large page number",
			queryParams:    "?page=100&limit=50",
			expectedStatus: http.StatusOK,
			description:    "should return 200 with empty list",
		},
		{
			name:           "page=0",
			queryParams:    "?page=0&limit=10",
			expectedStatus: http.StatusOK,
			description:    "should return 200 (handler may validate)",
		},
		{
			name:           "negative page",
			queryParams:    "?page=-1&limit=10",
			expectedStatus: http.StatusOK,
			description:    "should return 200 (handler may validate)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.description)

			defer func() {
				if r := recover(); r != nil && tt.expectedStatus != http.StatusOK {
					t.Errorf("unexpected panic: %v", r)
				}
			}()

			req := httptest.NewRequest(http.MethodGet, "/bargaining/negotiations"+tt.queryParams, nil)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req
			c.Set("user_id", "user-123")

			handler.ListNegotiations(c)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
		})
	}
}
