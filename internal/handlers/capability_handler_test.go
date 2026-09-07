package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCapabilityHandlerListsAuthenticatedBusinessCapabilities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	evaluatedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	lister := &stubCapabilityLister{result: services.CapabilityList{
		BusinessID: "biz-a", Platform: services.CapabilityPlatformWeb, EvaluatedAt: evaluatedAt,
		Capabilities: []services.Capability{{
			Key: services.CapabilitySavedPayments, State: services.CapabilityStateUnsupported,
			ReasonCode: services.ReasonSavedPaymentsUnsupported, EvaluatedAt: evaluatedAt,
		}},
	}}
	handler := NewCapabilityHandler(lister, logger.New())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/capabilities?business_id=other&platform=web", nil)
	c.Set("user_id", "user-a")
	c.Set("business_id", "biz-a")

	handler.List(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "biz-a", lister.businessID)
	require.Equal(t, "user-a", lister.userID)
	require.Equal(t, services.CapabilityPlatformWeb, lister.platform)
	var response services.CapabilityList
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "biz-a", response.BusinessID)
	require.Equal(t, services.ReasonSavedPaymentsUnsupported, response.Capabilities[0].ReasonCode)
}

func TestCapabilityHandlerRejectsMissingScopeAndInvalidPlatformWithStableCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, fixture := range []struct {
		name       string
		userID     string
		businessID string
		platform   string
		wantStatus int
		wantCode   string
	}{
		{name: "missing user", businessID: "biz-a", platform: "web", wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "missing business", userID: "user-a", platform: "web", wantStatus: http.StatusForbidden, wantCode: "business_scope_required"},
		{name: "invalid platform", userID: "user-a", businessID: "biz-a", platform: "desktop", wantStatus: http.StatusBadRequest, wantCode: "invalid_platform"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			lister := &stubCapabilityLister{}
			handler := NewCapabilityHandler(lister, logger.New())
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/capabilities?platform="+fixture.platform, nil)
			c.Set("user_id", fixture.userID)
			c.Set("business_id", fixture.businessID)

			handler.List(c)

			require.Equal(t, fixture.wantStatus, recorder.Code)
			var response CapabilityErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, fixture.wantCode, response.Error.Code)
			require.False(t, lister.called)
		})
	}
}

type stubCapabilityLister struct {
	called     bool
	businessID string
	userID     string
	platform   services.CapabilityPlatform
	result     services.CapabilityList
	err        error
}

func (s *stubCapabilityLister) List(_ context.Context, businessID, userID string, platform services.CapabilityPlatform) (services.CapabilityList, error) {
	s.called = true
	s.businessID = businessID
	s.userID = userID
	s.platform = platform
	return s.result, s.err
}
