package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionHistoryRejectsMalformedAndOutOfRangeLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE subscription_billing_records (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, subscription_id TEXT NOT NULL, provider_mode TEXT NOT NULL,
		provider_event_id TEXT, provider_invoice_id TEXT, provider_payment_id TEXT, amount_minor INTEGER NOT NULL,
		currency TEXT NOT NULL, status TEXT NOT NULL, receipt_reference TEXT NOT NULL, period_start DATETIME,
		period_end DATETIME, quota_period_start DATETIME, quota_period_end DATETIME, occurred_at DATETIME NOT NULL,
		created_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE subscription_audit_records (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, subscription_id TEXT, actor_user_id TEXT, action TEXT NOT NULL,
		from_status TEXT, to_status TEXT, from_plan_id TEXT, to_plan_id TEXT, provider_mode TEXT, provider_event_id TEXT,
		sanitized_code TEXT NOT NULL, occurred_at DATETIME NOT NULL, created_at DATETIME
	)`).Error)
	lifecycle := services.NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), nil, services.SubscriptionLifecycleConfig{}, logger.New())
	handler := NewSubscriptionHandler(nil, logger.New(), lifecycle)

	for _, endpoint := range []string{"/subscriptions/billing-history", "/subscriptions/audit"} {
		for _, limit := range []string{"abc", "0", "-1", "101"} {
			t.Run(endpoint+"_"+limit, func(t *testing.T) {
				router := gin.New()
				router.Use(func(c *gin.Context) { c.Set("business_id", uuid.NewString()) })
				if endpoint == "/subscriptions/billing-history" {
					router.GET(endpoint, handler.BillingHistory)
				} else {
					router.GET(endpoint, handler.AuditHistory)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint+"?limit="+limit, nil))

				require.Equal(t, http.StatusBadRequest, response.Code)
				var body SubscriptionAPIError
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				require.Equal(t, "subscription_invalid_limit", body.Error.Code)
			})
		}
	}
}

func TestSubscriptionHistoryMapsInternalFailuresToSanitizedServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	lifecycle := services.NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), nil, services.SubscriptionLifecycleConfig{}, logger.New())
	handler := NewSubscriptionHandler(nil, logger.New(), lifecycle)

	for _, endpoint := range []string{"/subscriptions/billing-history", "/subscriptions/audit"} {
		t.Run(endpoint, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("business_id", uuid.NewString()) })
			if endpoint == "/subscriptions/billing-history" {
				router.GET(endpoint, handler.BillingHistory)
			} else {
				router.GET(endpoint, handler.AuditHistory)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint+"?limit=50", nil))

			require.Equal(t, http.StatusInternalServerError, response.Code)
			var body SubscriptionAPIError
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, "subscription_internal_error", body.Error.Code)
			require.NotContains(t, response.Body.String(), "no such table")
		})
	}
}

func TestSubscriptionProviderRejectionMapsToStableUnprocessableResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)

	writeSubscriptionLifecycleError(context, services.ErrSubscriptionProviderRejected)

	require.Equal(t, http.StatusUnprocessableEntity, response.Code)
	var body SubscriptionAPIError
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "subscription_provider_rejected", body.Error.Code)
}
