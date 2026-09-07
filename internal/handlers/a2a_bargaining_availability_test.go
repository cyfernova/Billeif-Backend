package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestA2AGovernanceUnavailableReturnsActionableServiceError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	h := &A2ABargainingHandler{}
	h.writeServiceError(c, fmt.Errorf("start: %w", services.ErrA2AGovernanceRequired), "failed to start negotiation")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.JSONEq(t, `{"error":"Agent chat is unavailable until agent governance is configured","code":"AGENT_GOVERNANCE_UNAVAILABLE"}`, w.Body.String())
}
