package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteSubscriptionControlErrorReturnsMachineReadableQuotaPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)

	handled := writeSubscriptionControlError(context, &services.QuotaExceededError{
		Code: "quota_exceeded", Feature: services.FeatureEInvoice, Limit: 100, Used: 100, PlanID: "pro_monthly",
	})

	require.True(t, handled)
	require.Equal(t, http.StatusTooManyRequests, response.Code)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Equal(t, "quota_exceeded", payload["code"])
	require.Equal(t, "einvoice", payload["feature"])
	require.Equal(t, float64(100), payload["limit"])
	require.Equal(t, float64(100), payload["used"])
	require.Equal(t, "pro_monthly", payload["plan_id"])
}

func TestWriteSubscriptionControlErrorReturnsMachineReadableCapabilityPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		state      services.CapabilityState
		wantStatus int
	}{
		{state: services.CapabilityStatePermissionDenied, wantStatus: http.StatusForbidden},
		{state: services.CapabilityStateUpgradeRequired, wantStatus: http.StatusForbidden},
		{state: services.CapabilityStateSetupRequired, wantStatus: http.StatusForbidden},
		{state: services.CapabilityStateUnsupported, wantStatus: http.StatusUnprocessableEntity},
		{state: services.CapabilityStateUnsupportedPlatform, wantStatus: http.StatusUnprocessableEntity},
		{state: services.CapabilityStateQuotaExhausted, wantStatus: http.StatusTooManyRequests},
		{state: services.CapabilityStateTemporarilyUnavailable, wantStatus: http.StatusServiceUnavailable},
		{state: services.CapabilityStateUnknown, wantStatus: http.StatusServiceUnavailable},
	}

	for _, test := range tests {
		t.Run(string(test.state), func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			retryAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)

			handled := writeSubscriptionControlError(context, &services.CapabilityUnavailableError{
				Code: "capability_unavailable", Capability: services.CapabilityEmail,
				State: test.state, ReasonCode: "stable_reason", SetupAction: "contact_support", RetryAt: &retryAt,
			})

			require.True(t, handled)
			require.Equal(t, test.wantStatus, response.Code)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
			require.Equal(t, "capability_unavailable", payload["code"])
			require.Equal(t, "email_delivery", payload["capability"])
			require.Equal(t, string(test.state), payload["state"])
			require.Equal(t, "stable_reason", payload["reason_code"])
			require.Equal(t, "contact_support", payload["setup_action"])
			require.Equal(t, retryAt.Format(time.RFC3339), payload["retry_at"])
		})
	}
}
