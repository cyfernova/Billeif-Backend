package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVoiceAgentRouteIsGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewVoiceHandler(logger.New())

	router := gin.New()
	router.POST("/voice/agent", handler.Agent)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/voice/agent", nil)
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusGone, recorder.Code)
	require.Contains(t, recorder.Body.String(), "voice_realtime_required")
}
