package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/services"

	"github.com/aws/aws-lambda-go/lambdacontext"
)

type recordingRecurringInvoiceRunner struct {
	limit  int
	actor  services.ActorContext
	result services.InvoiceSubscriptionDispatchResult
	err    error
}

func (r *recordingRecurringInvoiceRunner) DispatchDueInvoiceSubscriptions(
	ctx context.Context,
	limit int,
) (services.InvoiceSubscriptionDispatchResult, error) {
	r.limit = limit
	r.actor = services.ActorFromContext(ctx)
	return r.result, r.err
}

func TestHandlerDispatchesWithRequestIdentityAndEmitsCounts(t *testing.T) {
	runner := &recordingRecurringInvoiceRunner{result: services.InvoiceSubscriptionDispatchResult{
		Due: 2, Completed: 2,
	}}
	var metricOutput bytes.Buffer
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	handler := lambdaHandler{
		runner: runner, limit: 50, metricWriter: &metricOutput, environment: "test",
		now: func() time.Time { return now },
	}
	ctx := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{
		AwsRequestID: "recurring-request-123",
	})

	result, err := handler.Handle(ctx)

	if err != nil {
		t.Fatalf("handle recurring invoice schedule: %v", err)
	}
	if result.Completed != 2 || runner.limit != 50 || runner.actor.RequestID != "recurring-request-123" {
		t.Fatalf("result/runner = %#v/%#v", result, runner)
	}
	var metric map[string]any
	if err := json.Unmarshal(metricOutput.Bytes(), &metric); err != nil {
		t.Fatalf("decode recurring invoice metric: %v\n%s", err, metricOutput.String())
	}
	if metric["Environment"] != "test" || metric["Due"] != float64(2) ||
		metric["Completed"] != float64(2) || metric["Failed"] != float64(0) {
		t.Fatalf("metric = %#v", metric)
	}
	awsMetadata, ok := metric["_aws"].(map[string]any)
	if !ok || awsMetadata["Timestamp"] != float64(now.UnixMilli()) {
		t.Fatalf("metric metadata = %#v", awsMetadata)
	}
}

func TestHandlerReturnsDispatchAndMetricFailures(t *testing.T) {
	runner := &recordingRecurringInvoiceRunner{
		result: services.InvoiceSubscriptionDispatchResult{Due: 1, Failed: 1},
		err:    errors.New("scheduled draft failed"),
	}
	handler := lambdaHandler{runner: runner, limit: 50, metricWriter: failingWriter{}, environment: "test"}
	ctx := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{
		AwsRequestID: "recurring-request-failure",
	})

	_, err := handler.Handle(ctx)

	if err == nil || !strings.Contains(err.Error(), "scheduled draft failed") || !strings.Contains(err.Error(), "emit recurring invoice metrics") {
		t.Fatalf("joined handler error = %v", err)
	}
}

func TestHandlerRejectsMissingLambdaRequestID(t *testing.T) {
	handler := lambdaHandler{runner: &recordingRecurringInvoiceRunner{}, limit: 50}

	_, err := handler.Handle(context.Background())

	if err == nil || !strings.Contains(err.Error(), "request ID") {
		t.Fatalf("missing request ID error = %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("metric output unavailable")
}

type recordingDatabasePool struct {
	maxOpen     int
	maxIdle     int
	maxIdleTime time.Duration
	maxLifetime time.Duration
}

func (p *recordingDatabasePool) SetMaxOpenConns(value int) {
	p.maxOpen = value
}

func (p *recordingDatabasePool) SetMaxIdleConns(value int) {
	p.maxIdle = value
}

func (p *recordingDatabasePool) SetConnMaxIdleTime(value time.Duration) {
	p.maxIdleTime = value
}

func (p *recordingDatabasePool) SetConnMaxLifetime(value time.Duration) {
	p.maxLifetime = value
}

func TestConfigureDatabasePoolBoundsRecurringInvoiceConnections(t *testing.T) {
	pool := &recordingDatabasePool{}

	configureDatabasePool(pool)

	if pool.maxOpen != 1 || pool.maxIdle != 0 ||
		pool.maxIdleTime != 2*time.Minute || pool.maxLifetime != 10*time.Minute {
		t.Fatalf("database pool settings = %#v", pool)
	}
}
