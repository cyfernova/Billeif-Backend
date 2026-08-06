package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAcquirePersistentActivityKeepsRuntimeBusyUntilClosed(t *testing.T) {
	server := NewServer(Config{}, nil)

	activityContext, activity, err := server.AcquirePersistentActivity()
	if err != nil {
		t.Fatalf("acquire persistent activity: %v", err)
	}
	assertRuntimeHealth(t, server, StatusHealthyBusy)
	select {
	case <-activityContext.Done():
		t.Fatal("persistent activity context was canceled before close")
	default:
	}

	if err := activity.Close(); err != nil {
		t.Fatalf("close persistent activity: %v", err)
	}
	if err := activity.Close(); err != nil {
		t.Fatalf("close persistent activity twice: %v", err)
	}
	select {
	case <-activityContext.Done():
	case <-time.After(time.Second):
		t.Fatal("persistent activity context was not canceled by close")
	}
	assertRuntimeHealth(t, server, StatusHealthy)
}

func TestPersistentActivitySurvivesInvocationCancellation(t *testing.T) {
	contexts := make(chan context.Context, 1)
	activities := make(chan io.Closer, 1)
	var server *Server
	server = NewServer(Config{}, InvocationHandlerFunc(func(_ context.Context, _ InvocationRequest) (InvocationResponse, error) {
		activityContext, activity, err := server.AcquirePersistentActivity()
		if err != nil {
			return InvocationResponse{}, err
		}
		contexts <- activityContext
		activities <- activity
		return InvocationResponse{StatusCode: http.StatusOK, Body: json.RawMessage(`{}`)}, nil
	}))
	request := httptest.NewRequest(http.MethodPost, "/invocations", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer test-only-token")
	request.Header.Set(HeaderRuntimeSessionID, "voice-session-123456789012345678901234567890")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("invocation status = %d, want %d", response.Code, http.StatusOK)
	}
	activityContext := <-contexts
	activity := <-activities
	defer activity.Close()

	select {
	case <-activityContext.Done():
		t.Fatal("persistent activity inherited invocation cancellation")
	case <-time.After(25 * time.Millisecond):
	}
	assertRuntimeHealth(t, server, StatusHealthyBusy)
}

func TestShutdownCancelsPersistentActivityAndRejectsNewOnes(t *testing.T) {
	server := NewServer(Config{}, nil)
	activityContext, activity, err := server.AcquirePersistentActivity()
	if err != nil {
		t.Fatalf("acquire persistent activity: %v", err)
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- server.Shutdown(shutdownContext)
	}()

	select {
	case <-activityContext.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel persistent activity context")
	}
	if _, _, err := server.AcquirePersistentActivity(); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("new activity error = %v, want ErrShuttingDown", err)
	}

	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown completed before activity released: %v", err)
	default:
	}
	if err := activity.Close(); err != nil {
		t.Fatalf("close persistent activity: %v", err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestAcquirePersistentActivityOnNilServerFailsClosed(t *testing.T) {
	var server *Server
	activityContext, activity, err := server.AcquirePersistentActivity()
	if !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("error = %v, want ErrShuttingDown", err)
	}
	if activityContext != nil || activity != nil {
		t.Fatalf("nil server returned context=%v activity=%v", activityContext, activity)
	}
}

func assertRuntimeHealth(t *testing.T, server http.Handler, want string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/ping", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ping status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode ping: %v", err)
	}
	if body.Status != want {
		t.Fatalf("ping status = %q, want %q", body.Status, want)
	}
}

var _ io.Closer = (*PersistentActivity)(nil)
