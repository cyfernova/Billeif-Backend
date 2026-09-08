package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type unavailablePOSService struct{ posService }

func (s *unavailablePOSService) ListSessions(context.Context, string, string, int, int, string) ([]models.POSSession, int64, error) {
	return nil, 0, &services.FeatureUnavailableError{Code: "feature_disabled", Feature: "pos", PlanID: "free"}
}

func (s *unavailablePOSService) CreateSession(context.Context, string, string, services.CreatePOSSessionInput) (*models.POSSession, error) {
	return nil, &services.FeatureUnavailableError{Code: "feature_disabled", Feature: "pos", PlanID: "free"}
}

func TestPOSSessionEntitlementResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewPOSHandler(&unavailablePOSService{}, nil)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Set("business_id", "test-business")
			c.Set("user_id", "test-user")
			c.Request = httptest.NewRequest(method, "/pos/sessions", strings.NewReader(`{}`))
			c.Request.Header.Set("Content-Type", "application/json")
			if method == http.MethodGet {
				handler.ListSessions(c)
			} else {
				handler.CreateSession(c)
			}
			require.Equal(t, http.StatusForbidden, response.Code)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
			require.Equal(t, "feature_disabled", payload["code"])
			require.Equal(t, "pos", payload["feature"])
			require.Equal(t, "free", payload["plan_id"])
		})
	}
}
