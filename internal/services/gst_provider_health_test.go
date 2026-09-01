package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/require"
)

func TestGSTAdapterHTTPStatusFeedsRateLimitHealthWithoutRawBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"raw GST account acct_123 secret response"}`))
	}))
	defer server.Close()
	provider := NewConfiguredGSTProvider(&config.Config{GST: config.GSTConfig{
		Provider: "test", BaseURL: server.URL, EInvoicePath: "/einvoice",
	}}, logger.New())

	_, err := provider.GenerateEInvoice(context.Background(), GSTEInvoiceRequest{Payload: map[string]interface{}{"document": "test"}})

	var statusError interface{ HTTPStatusCode() int }
	require.ErrorAs(t, err, &statusError)
	require.Equal(t, http.StatusTooManyRequests, statusError.HTTPStatusCode())
	require.False(t, strings.Contains(err.Error(), "acct_123"))
	require.False(t, strings.Contains(err.Error(), "secret response"))

	now := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	repository := newMemoryCapabilityProviderHealthRepository()
	recorder := NewGSTProviderHealthRecorder(repository, GSTProviderHealthRecorderOptions{Now: func() time.Time { return now }})
	require.NoError(t, recorder.RecordGSTOutcome(context.Background(), "biz-1", CapabilityProviderOutcome{Err: err}))
	fact, ok, readErr := NewCapabilityBusinessHealthReader(repository, func() time.Time { return now }).
		CustomerFact(context.Background(), "biz-1", CapabilityGSTProvider)
	require.NoError(t, readErr)
	require.True(t, ok)
	require.Equal(t, CapabilityProviderDegraded, fact.Status)
	require.Equal(t, "provider_rate_limited", fact.Degradation.Code)
	require.Equal(t, now.Add(time.Minute), *fact.RetryAt)
}

func TestGSTAdapterTimeoutFeedsUnavailableHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-time.After(100 * time.Millisecond)
	}))
	defer server.Close()
	provider := NewConfiguredGSTProvider(&config.Config{GST: config.GSTConfig{
		Provider: "test", BaseURL: server.URL, EInvoicePath: "/einvoice",
	}}, logger.New())
	configured := provider.(*configuredGSTProvider)
	configured.httpClient.Timeout = time.Millisecond

	_, err := provider.GenerateEInvoice(context.Background(), GSTEInvoiceRequest{Payload: map[string]interface{}{}})
	require.Error(t, err)

	now := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	repository := newMemoryCapabilityProviderHealthRepository()
	recorder := NewGSTProviderHealthRecorder(repository, GSTProviderHealthRecorderOptions{Now: func() time.Time { return now }})
	require.NoError(t, recorder.RecordGSTOutcome(context.Background(), "biz-1", CapabilityProviderOutcome{Err: err}))
	fact, ok, readErr := NewCapabilityBusinessHealthReader(repository, func() time.Time { return now }).
		CustomerFact(context.Background(), "biz-1", CapabilityGSTProvider)
	require.NoError(t, readErr)
	require.True(t, ok)
	require.Equal(t, CapabilityProviderUnavailable, fact.Status)
	require.Equal(t, now.Add(30*time.Second), *fact.RetryAt)
}
