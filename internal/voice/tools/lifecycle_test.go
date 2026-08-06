package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExecuteHonorsConfiguredDeadlineWhileWaitingForResponseHeaders(t *testing.T) {
	server, origin, roots, resolver, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	binding, err := NewSessionBinding(staticAuthorizationSource{value: testBearer}, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: origin, Timeout: 30 * time.Millisecond, Resolver: resolver, Dialer: dialer, RootCAs: roots,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	defer registry.Close()

	started := time.Now()
	result, err := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
	if result != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() = (%s, %v), want deadline exceeded", result, err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("configured deadline took %v", elapsed)
	}
	if stringsContainAny(err.Error(), testBearer, origin) {
		t.Fatalf("response-header deadline exposed credential or origin: %v", err)
	}
}

func TestExecuteHonorsDeadlineDuringResponseBodyWithoutLeakingCredential(t *testing.T) {
	server, origin, roots, resolver, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"items":[`))
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	}))
	defer server.Close()
	const token = "Bearer stalled-body-secret-canary"
	binding, err := NewSessionBinding(staticAuthorizationSource{value: token}, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: origin, Timeout: 30 * time.Millisecond, Resolver: resolver, Dialer: dialer, RootCAs: roots,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	defer registry.Close()

	result, err := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
	if result != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() = (%s, %v), want body-read deadline", result, err)
	}
	if stringsContainAny(err.Error(), token, origin) {
		t.Fatalf("deadline error exposed credential or origin: %v", err)
	}
}

func TestCloseCancelsInFlightExecutionAndFutureCallsFailClosed(t *testing.T) {
	requestStarted := make(chan struct{})
	server, origin, roots, resolver, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()
	source := &countingAuthorizationSource{value: testBearer}
	binding, err := NewSessionBinding(source, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: origin, Timeout: time.Second, Resolver: resolver, Dialer: dialer, RootCAs: roots,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	resultChannel := make(chan json.RawMessage, 1)
	errorChannel := make(chan error, 1)
	go func() {
		result, executeErr := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
		resultChannel <- result
		errorChannel <- executeErr
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not reach fake server")
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	select {
	case result := <-resultChannel:
		if result != nil {
			t.Fatalf("in-flight Execute() result = %s, want nil", result)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight Execute() was not canceled by Close")
	}
	select {
	case executeErr := <-errorChannel:
		if !errors.Is(executeErr, context.Canceled) {
			t.Fatalf("in-flight Execute() error = %v, want canceled", executeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight Execute() did not return an error")
	}

	callsBefore := source.calls.Load()
	result, err := registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
	if result != nil || !errors.Is(err, ErrRegistryClosed) {
		t.Fatalf("post-close Execute() = (%s, %v), want closed", result, err)
	}
	if got := source.calls.Load(); got != callsBefore {
		t.Fatalf("post-close Execute() called AuthorizationSource: before=%d after=%d", callsBefore, got)
	}
}

func TestRegistryDiagnosticsNeverExposeOriginScopeOrAuthorization(t *testing.T) {
	const token = "Bearer diagnostics-secret-canary"
	binding, err := NewSessionBinding(staticAuthorizationSource{value: token}, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: "https://private-origin-canary.example/staging", Timeout: time.Second, Resolver: panicResolver{}, Dialer: panicDialer{},
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	defer registry.Close()

	formatted := fmt.Sprintf("%v|%+v|%#v", registry, registry, registry)
	document, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, canary := range []string{token, testBusinessID, "private-origin-canary"} {
		if strings.Contains(formatted, canary) || strings.Contains(string(document), canary) {
			t.Fatalf("registry diagnostics exposed %q: %s / %s", canary, formatted, document)
		}
	}
	if string(document) != "{}" {
		t.Fatalf("json.Marshal(registry) = %s, want {}", document)
	}
}

func TestConcurrentExecuteAndCloseIsRaceSafe(t *testing.T) {
	server, origin, roots, resolver, dialer := newInjectedTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[],"next_cursor":null}`))
	}))
	defer server.Close()
	source := &countingAuthorizationSource{value: testBearer}
	binding, err := NewSessionBinding(source, testBusinessID, "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	registry, err := NewRegistry(Config{
		Origin: origin, Timeout: time.Second, Resolver: resolver, Dialer: dialer, RootCAs: roots,
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := 0; index < 48; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, _ = registry.Execute(context.Background(), "list_invoices", json.RawMessage(`{}`))
		}()
	}
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_ = registry.Close()
		}()
	}
	close(start)
	wait.Wait()
	if !registry.closed.Load() {
		t.Fatal("registry was not closed")
	}
}
