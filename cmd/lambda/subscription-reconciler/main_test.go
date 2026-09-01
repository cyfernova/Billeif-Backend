package main

import (
	"bytes"
	"context"
	"testing"

	"invoice-backend/internal/services"

	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/stretchr/testify/require"
)

type recordingRunner struct{ limit int }

func (r *recordingRunner) RunMaintenance(_ context.Context, limit int) (services.SubscriptionMaintenanceResult, error) {
	r.limit = limit
	return services.SubscriptionMaintenanceResult{Reconciled: 2, Suspended: 1}, nil
}

func TestHandlerRunsBoundedMaintenanceAndEmitsMetrics(t *testing.T) {
	runner := &recordingRunner{}
	var metrics bytes.Buffer
	h := lambdaHandler{runner: runner, limit: 50, metricWriter: &metrics, environment: "test"}
	ctx := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{AwsRequestID: "request-fixture"})
	result, err := h.Handle(ctx)
	require.NoError(t, err)
	require.Equal(t, 50, runner.limit)
	require.EqualValues(t, 2, result.Reconciled)
	require.Contains(t, metrics.String(), "Billeif/SubscriptionLifecycle")
}
