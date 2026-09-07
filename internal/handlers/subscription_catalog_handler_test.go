package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionCatalogHandlerReturnsAuthoritativeCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewCommerceHandler(nil, logger.New())
	router := gin.New()
	router.GET("/subscriptions/catalog", handler.ListSubscriptionCatalog)

	request := httptest.NewRequest(http.MethodGet, "/subscriptions/catalog", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	var catalog services.SubscriptionCatalogResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &catalog))
	require.Equal(t, services.CurrentSubscriptionCatalogVersion, catalog.Version)
	require.Len(t, catalog.Plans, 4)
	require.Equal(t, "pro_monthly", catalog.Plans[1].ID)
}
