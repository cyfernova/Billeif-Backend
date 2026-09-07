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
	"github.com/stretchr/testify/require"
)

type recordingRunner struct {
	limit       int
	hasDeadline bool
}

func (r *recordingRunner) RunMaintenance(ctx context.Context, limit int) (services.SubscriptionMaintenanceResult, error) {
	r.limit = limit
	_, r.hasDeadline = ctx.Deadline()
	return services.SubscriptionMaintenanceResult{Reconciled: 2, Suspended: 1}, nil
}

func TestHandlerRunsBoundedMaintenanceAndEmitsMetrics(t *testing.T) {
	runner := &recordingRunner{}
	var metrics bytes.Buffer
	h := lambdaHandler{runner: runner, limit: 50, runTimeout: 10 * time.Second, metricWriter: &metrics, environment: "test"}
	ctx := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{AwsRequestID: "request-fixture"})
	result, err := h.Handle(ctx)
	require.NoError(t, err)
	require.Equal(t, 50, runner.limit)
	require.True(t, runner.hasDeadline)
	require.EqualValues(t, 2, result.Reconciled)
	require.Contains(t, metrics.String(), "Billeif/SubscriptionLifecycle")
}

type recordingBacklogCounter struct {
	count int64
	err   error
}

func (r *recordingBacklogCounter) CountReconciliationBacklog(context.Context) (int64, error) {
	return r.count, r.err
}

func TestHandlerEmitsReconciliationBacklogMetric(t *testing.T) {
	runner := &recordingRunner{}
	var metrics bytes.Buffer
	h := lambdaHandler{
		runner: runner, limit: 50, runTimeout: 10 * time.Second,
		metricWriter: &metrics, environment: "test",
		backlog: &recordingBacklogCounter{count: 7},
	}
	ctx := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{AwsRequestID: "request-backlog"})
	if _, err := h.Handle(ctx); err != nil {
		t.Fatalf("handle: %v", err)
	}
	var found bool
	for _, line := range strings.Split(metrics.String(), "\n") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		if payload["Category"] == "reconciliation" {
			require.EqualValues(t, 7, payload["ReconciliationBacklog"])
			require.Equal(t, "test", payload["Environment"])
			found = true
		}
	}
	require.True(t, found, "expected one reconciliation backlog sample, got %s", metrics.String())
}

// failOnSecondWriteWriter succeeds for the lifecycle metric and fails for the
// backlog sample so backlog-only encode failures are provably fail-open.
type failOnSecondWrite struct {
	writes int
}

func (w *failOnSecondWrite) Write(p []byte) (int, error) {
	w.writes++
	if w.writes >= 2 {
		return 0, errors.New("backlog encode failure")
	}
	return len(p), nil
}

type contextCapturingBacklogCounter struct {
	seenErr error
	count   int64
	err     error
}

func (c *contextCapturingBacklogCounter) CountReconciliationBacklog(ctx context.Context) (int64, error) {
	c.seenErr = ctx.Err()
	return c.count, c.err
}

func TestHandlerCancellationReachesBacklogCounter(t *testing.T) {
	runner := &recordingRunner{}
	var metrics bytes.Buffer
	counter := &contextCapturingBacklogCounter{count: 4}
	h := lambdaHandler{
		runner: runner, backlog: counter, limit: 50, runTimeout: 10 * time.Second,
		metricWriter: &metrics, environment: "test",
	}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.Handle(lambdacontext.NewContext(parent, &lambdacontext.LambdaContext{AwsRequestID: "request-cancelled"}))
	require.NoError(t, err)
	require.ErrorIs(t, counter.seenErr, context.Canceled)
}

func TestHandlerFailingBacklogCounterKeepsMaintenanceOutcome(t *testing.T) {
	runner := &recordingRunner{}
	var metrics bytes.Buffer
	counter := &contextCapturingBacklogCounter{err: errors.New("backlog query failed")}
	h := lambdaHandler{
		runner: runner, backlog: counter, limit: 50, runTimeout: 10 * time.Second,
		metricWriter: &metrics, environment: "test",
	}
	result, err := h.Handle(lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{AwsRequestID: "request-counter-fail"}))
	require.NoError(t, err)
	require.EqualValues(t, 2, result.Reconciled)
	require.Contains(t, metrics.String(), "Billeif/SubscriptionLifecycle")
}

func TestHandlerBacklogEncodeFailureKeepsMaintenanceOutcome(t *testing.T) {
	runner := &recordingRunner{}
	writer := &failOnSecondWrite{}
	counter := &contextCapturingBacklogCounter{count: 9}
	h := lambdaHandler{
		runner: runner, backlog: counter, limit: 50, runTimeout: 10 * time.Second,
		metricWriter: writer, environment: "test",
	}
	result, err := h.Handle(lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{AwsRequestID: "request-encode-fail"}))
	require.NoError(t, err)
	require.EqualValues(t, 2, result.Reconciled)
	require.GreaterOrEqual(t, writer.writes, 2)
}

func TestHandlerNilBacklogCounterKeepsMaintenanceBehavior(t *testing.T) {
	runner := &recordingRunner{}
	var metrics bytes.Buffer
	h := lambdaHandler{runner: runner, limit: 50, runTimeout: 10 * time.Second, metricWriter: &metrics, environment: "test"}
	_, err := h.Handle(lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{AwsRequestID: "request-nil"}))
	require.NoError(t, err)
	require.NotContains(t, metrics.String(), "Billeif/Operations")
}
