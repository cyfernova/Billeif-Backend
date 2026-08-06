package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeApplicationUsesOnlyNonSecretMetadataAndAnswersPing(t *testing.T) {
	t.Setenv("RUNTIME_ID", "runtime-id-from-environment")
	t.Setenv("AWS_REGION", "ap-south-1")
	t.Setenv("SARVAM_API_KEY", "")

	application, httpServer := newRuntimeApplication()
	t.Cleanup(func() { _ = application.Shutdown(t.Context()) })

	assert.Equal(t, "0.0.0.0:8080", httpServer.Addr)
	request := httptest.NewRequest(http.MethodGet, "/ping", nil)
	response := httptest.NewRecorder()
	httpServer.Handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"status":"Healthy"}`, response.Body.String())
}

func TestProviderFreeEntrypointRejectsInvocationsWithoutEchoingRequest(t *testing.T) {
	application, httpServer := newRuntimeApplication()
	t.Cleanup(func() { _ = application.Shutdown(t.Context()) })
	request := httptest.NewRequest(http.MethodPost, "/invocations", strings.NewReader(`{"offer":"payload-secret"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer entrypoint-secret")
	request.Header.Set("X-Amzn-Bedrock-AgentCore-Runtime-Session-Id", "voice-session-01K000000000000000000")
	response := httptest.NewRecorder()

	httpServer.Handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.NotContains(t, response.Body.String(), "entrypoint-secret")
	assert.NotContains(t, response.Body.String(), "payload-secret")
}
