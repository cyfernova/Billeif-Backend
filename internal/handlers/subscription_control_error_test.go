package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
