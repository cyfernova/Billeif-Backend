package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAuthorization = "Bearer header-secret-must-not-leak"
	testSessionID     = "voice-session-01K000000000000000000"
)

func TestPingReportsHealthAcrossActivityTransitions(t *testing.T) {
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		return InvocationResponse{}, nil
	}))

	assertPingStatus(t, server, StatusHealthy)

	lease, err := server.AcquireActivity()
	require.NoError(t, err)
	assertPingStatus(t, server, StatusHealthyBusy)

	require.NoError(t, lease.Close())
	require.NoError(t, lease.Close(), "activity lease close must be idempotent")
	assertPingStatus(t, server, StatusHealthy)
}

func TestInvocationDelegatesBoundedPayloadAndTrustedRequestMetadata(t *testing.T) {
	var received InvocationRequest
	server := NewServer(Config{
		RuntimeID: "runtime-metadata-only",
		AWSRegion: "ap-south-1",
	}, InvocationHandlerFunc(func(_ context.Context, request InvocationRequest) (InvocationResponse, error) {
		received = request
		return InvocationResponse{
			StatusCode: http.StatusAccepted,
			Body:       json.RawMessage(`{"accepted":true}`),
		}, nil
	}))

	response := invoke(t, server, `{"type":"session.attach"}`, testSessionID, testAuthorization, "application/json; charset=utf-8")

	require.Equal(t, http.StatusAccepted, response.Code)
	assert.JSONEq(t, `{"accepted":true}`, response.Body.String())
	assert.JSONEq(t, `{"type":"session.attach"}`, string(received.Body))
	assert.Equal(t, testSessionID, received.RuntimeSessionID)
	assert.Equal(t, testAuthorization, received.Authorization)
	assert.Equal(t, "runtime-metadata-only", received.RuntimeID)
	assert.Equal(t, "ap-south-1", received.AWSRegion)
	assertSecurityHeaders(t, response.Header())
	assert.NotContains(t, response.Body.String(), testAuthorization)
	assert.NotContains(t, response.Body.String(), testSessionID)
}

func TestInvocationMarksRuntimeBusyUntilHandlerReturns(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		close(started)
		<-release
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}))

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- invoke(t, server, `{}`, testSessionID, testAuthorization, "application/json")
	}()

	<-started
	assertPingStatus(t, server, StatusHealthyBusy)
	close(release)
	response := <-done
	require.Equal(t, http.StatusNoContent, response.Code)
	assertPingStatus(t, server, StatusHealthy)
}

func TestInvocationEnforcesRuntimeSessionIDBounds(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		calls.Add(1)
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}))

	tests := []struct {
		name       string
		sessionID  string
		wantStatus int
	}{
		{name: "missing", sessionID: "", wantStatus: http.StatusBadRequest},
		{name: "32 bytes", sessionID: strings.Repeat("a", 32), wantStatus: http.StatusBadRequest},
		{name: "33 bytes", sessionID: strings.Repeat("a", 33), wantStatus: http.StatusNoContent},
		{name: "256 bytes", sessionID: strings.Repeat("a", 256), wantStatus: http.StatusNoContent},
		{name: "257 bytes", sessionID: strings.Repeat("a", 257), wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := invoke(t, server, `{}`, tt.sessionID, testAuthorization, "application/json")
			assert.Equal(t, tt.wantStatus, response.Code)
			assertSecurityHeaders(t, response.Header())
			if tt.sessionID != "" {
				assert.NotContains(t, response.Body.String(), tt.sessionID)
			}
		})
	}

	assert.EqualValues(t, 2, calls.Load())
}

func TestInvocationRejectsDuplicateRuntimeSessionIDHeaders(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		calls.Add(1)
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}))

	for _, values := range [][]string{
		{testSessionID, testSessionID},
		{testSessionID, "voice-session-02K000000000000000000"},
	} {
		request := newInvocationRequest(`{}`)
		request.Header.Del(HeaderRuntimeSessionID)
		for _, value := range values {
			request.Header.Add(HeaderRuntimeSessionID, value)
		}
		response := httptest.NewRecorder()

		server.ServeHTTP(response, request)

		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.JSONEq(t, `{"error":"invalid request"}`, response.Body.String())
		assert.NotContains(t, response.Body.String(), "voice-session")
	}
	assert.Zero(t, calls.Load())
}

func TestInvocationRequiresForwardedBearerAuthorizationWithoutEchoingIt(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		calls.Add(1)
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}))

	for _, authorization := range []string{
		"", "Basic not-a-bearer", "Bearer", "Bearer    ",
		"Bearer token with spaces", "Bearer\ttoken", "Bearer " + strings.Repeat("x", (8<<10)+1),
	} {
		response := invoke(t, server, `{}`, testSessionID, authorization, "application/json")
		assert.Equal(t, http.StatusUnauthorized, response.Code)
		assert.JSONEq(t, `{"error":"unauthorized"}`, response.Body.String())
		if authorization != "" {
			assert.NotContains(t, response.Body.String(), authorization)
		}
	}

	assert.Zero(t, calls.Load())
}

func TestInvocationRejectsDuplicateAuthorizationHeaders(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		calls.Add(1)
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}))

	for _, values := range [][]string{
		{testAuthorization, testAuthorization},
		{testAuthorization, "Bearer second-secret-must-not-leak"},
	} {
		request := newInvocationRequest(`{}`)
		request.Header.Del("Authorization")
		for _, value := range values {
			request.Header.Add("Authorization", value)
		}
		response := httptest.NewRecorder()

		server.ServeHTTP(response, request)

		assert.Equal(t, http.StatusUnauthorized, response.Code)
		assert.JSONEq(t, `{"error":"unauthorized"}`, response.Body.String())
		assert.NotContains(t, response.Body.String(), "secret-must-not-leak")
	}
	assert.Zero(t, calls.Load())
}

func TestInvocationRejectsMalformedAndTrailingJSONWithoutDelegating(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		calls.Add(1)
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}))

	for _, body := range []string{"", `{`, `{"ok":true} {"secret":"payload-secret"}`} {
		response := invoke(t, server, body, testSessionID, testAuthorization, "application/json")
		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.JSONEq(t, `{"error":"invalid request"}`, response.Body.String())
		assert.NotContains(t, response.Body.String(), "payload-secret")
		assert.NotContains(t, response.Body.String(), testAuthorization)
	}

	assert.Zero(t, calls.Load())
}

func TestInvocationRequiresTopLevelJSONObject(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		calls.Add(1)
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}))

	for _, body := range []string{`null`, `[]`, `"value"`, `42`, `true`} {
		response := invoke(t, server, body, testSessionID, testAuthorization, "application/json")
		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.JSONEq(t, `{"error":"invalid request"}`, response.Body.String())
	}

	assert.Zero(t, calls.Load())
}

func TestInvocationEnforcesFrozenSixteenKiBRequestCap(t *testing.T) {
	var calls atomic.Int32
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		calls.Add(1)
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}))

	atLimit := invoke(t, server, invocationObjectWithSize(t, 16<<10), testSessionID, testAuthorization, "application/json")
	require.Equal(t, http.StatusNoContent, atLimit.Code)

	overLimit := invoke(t, server, invocationObjectWithSize(t, (16<<10)+1), testSessionID, testAuthorization, "application/json")
	assert.Equal(t, http.StatusRequestEntityTooLarge, overLimit.Code)
	assert.JSONEq(t, `{"error":"request too large"}`, overLimit.Body.String())
	assert.EqualValues(t, 1, calls.Load())
}

func TestInvocationEnforcesFrozenSixteenKiBResponseCap(t *testing.T) {
	responseBody := invocationObjectWithSize(t, 16<<10)
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		return InvocationResponse{StatusCode: http.StatusOK, Body: json.RawMessage(responseBody)}, nil
	}))

	atLimit := invoke(t, server, `{}`, testSessionID, testAuthorization, "application/json")
	require.Equal(t, http.StatusOK, atLimit.Code)
	assert.Len(t, atLimit.Body.Bytes(), 16<<10)

	responseBody = invocationObjectWithSize(t, (16<<10)+1)
	overLimit := invoke(t, server, `{}`, testSessionID, testAuthorization, "application/json")
	assert.Equal(t, http.StatusInternalServerError, overLimit.Code)
	assert.JSONEq(t, `{"error":"internal error"}`, overLimit.Body.String())
}

func TestRuntimeRoutesRejectUnsupportedRequests(t *testing.T) {
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		return InvocationResponse{StatusCode: http.StatusNoContent}, nil
	}))

	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		wantStatus  int
		wantAllow   string
	}{
		{name: "ping method", method: http.MethodPost, path: "/ping", wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodGet},
		{name: "invocation method", method: http.MethodGet, path: "/invocations", wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodPost},
		{name: "unknown path", method: http.MethodGet, path: "/debug", wantStatus: http.StatusNotFound},
		{name: "missing content type", method: http.MethodPost, path: "/invocations", wantStatus: http.StatusUnsupportedMediaType},
		{name: "wrong content type", method: http.MethodPost, path: "/invocations", contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
		{name: "malformed content type", method: http.MethodPost, path: "/invocations", contentType: "application/json; charset", wantStatus: http.StatusUnsupportedMediaType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", tt.contentType)
			request.Header.Set(HeaderRuntimeSessionID, testSessionID)
			request.Header.Set("Authorization", testAuthorization)
			response := httptest.NewRecorder()

			server.ServeHTTP(response, request)

			assert.Equal(t, tt.wantStatus, response.Code)
			assert.Equal(t, tt.wantAllow, response.Header().Get("Allow"))
			assertSecurityHeaders(t, response.Header())
		})
	}
}

func TestInvocationHandlerErrorsAreSanitized(t *testing.T) {
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		return InvocationResponse{}, fmt.Errorf("upstream failed with %s and payload-secret", testAuthorization)
	}))

	response := invoke(t, server, `{"secret":"payload-secret"}`, testSessionID, testAuthorization, "application/json")

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.JSONEq(t, `{"error":"internal error"}`, response.Body.String())
	assert.NotContains(t, response.Body.String(), "payload-secret")
	assert.NotContains(t, response.Body.String(), testAuthorization)
	assert.NotContains(t, response.Body.String(), testSessionID)
}

func TestInvocationHandlerPanicsAreSanitized(t *testing.T) {
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		panic("panic-payload-secret")
	}))

	response := invoke(t, server, `{"secret":"payload-secret"}`, testSessionID, testAuthorization, "application/json")

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.JSONEq(t, `{"error":"internal error"}`, response.Body.String())
	assert.NotContains(t, response.Body.String(), "panic-payload-secret")
}

func TestInvocationRejectsInvalidHandlerResponse(t *testing.T) {
	server := newTestServer(t, InvocationHandlerFunc(func(context.Context, InvocationRequest) (InvocationResponse, error) {
		return InvocationResponse{StatusCode: http.StatusOK, Body: json.RawMessage(`{"truncated":`)}, nil
	}))

	response := invoke(t, server, `{}`, testSessionID, testAuthorization, "application/json")

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.JSONEq(t, `{"error":"internal error"}`, response.Body.String())
}

func newTestServer(t *testing.T, handler InvocationHandler) *Server {
	t.Helper()
	server := NewServer(Config{}, handler)
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = server.Shutdown(ctx)
	})
	return server
}

func invoke(t *testing.T, server http.Handler, body, sessionID, authorization, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/invocations", strings.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set(HeaderRuntimeSessionID, sessionID)
	request.Header.Set("Authorization", authorization)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func newInvocationRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/invocations", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(HeaderRuntimeSessionID, testSessionID)
	request.Header.Set("Authorization", testAuthorization)
	return request
}

func invocationObjectWithSize(t *testing.T, size int) string {
	t.Helper()
	const prefix = `{"value":"`
	const suffix = `"}`
	require.GreaterOrEqual(t, size, len(prefix)+len(suffix))
	return prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
}

func assertPingStatus(t *testing.T, server http.Handler, want string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/ping", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, fmt.Sprintf(`{"status":%q}`, want), response.Body.String())
	assertSecurityHeaders(t, response.Header())
}

func assertSecurityHeaders(t *testing.T, header http.Header) {
	t.Helper()
	assert.Equal(t, "application/json", header.Get("Content-Type"))
	assert.Equal(t, "no-store", header.Get("Cache-Control"))
	assert.Equal(t, "nosniff", header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", header.Get("X-Frame-Options"))
}

var _ io.Closer = (*ActivityLease)(nil)
