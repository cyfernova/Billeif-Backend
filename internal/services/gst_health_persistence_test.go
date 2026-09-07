package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type observingGSTHealthRecorder struct {
	record func(context.Context, string, string, int64, CapabilityProviderOutcome) (int64, error)
}

func (r *observingGSTHealthRecorder) RecordGSTOutcome(
	ctx context.Context,
	businessID, accountID string,
	credentialRevision int64,
	outcome CapabilityProviderOutcome,
) (int64, error) {
	return r.record(ctx, businessID, accountID, credentialRevision, outcome)
}

func (*observingGSTHealthRecorder) RecordGSTValidationOutcome(
	context.Context,
	string,
	string,
	int64,
	CapabilityProviderOutcome,
	interfaces.GSTIntegrationValidationState,
) (int64, error) {
	return 0, errors.New("unexpected validation observation")
}

func (*observingGSTHealthRecorder) SaveGSTIntegrationAccountAndInvalidate(
	context.Context,
	*models.GSTIntegrationAccount,
	int64,
) error {
	return errors.New("unexpected integration account save")
}

func TestGSTOperationHealthPersistenceDetachesFromCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var observedContextError error
	recorder := &observingGSTHealthRecorder{record: func(
		writeCtx context.Context,
		businessID, accountID string,
		credentialRevision int64,
		_ CapabilityProviderOutcome,
	) (int64, error) {
		observedContextError = writeCtx.Err()
		require.Equal(t, "business-a", businessID)
		require.Equal(t, "account-a", accountID)
		require.EqualValues(t, 9, credentialRevision)
		_, hasDeadline := writeCtx.Deadline()
		require.True(t, hasDeadline)
		return 1, nil
	}}
	service := (&TaxComplianceService{gstHealth: recorder, log: logger.FromZap(zap.NewNop())}).
		WithGSTHealthPersistenceTimeout(50 * time.Millisecond)

	service.recordGSTProviderOutcome(ctx, &models.GSTIntegrationAccount{
		ID: "account-a", BusinessID: "business-a", CredentialRevision: 9,
	}, errors.New("provider result"))

	require.NoError(t, observedContextError, "provider outcome write must survive request cancellation")
}

func TestGSTOperationHealthPersistenceIsBoundedAndLogsOnlySanitizedIssue(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	recorder := &observingGSTHealthRecorder{record: func(
		ctx context.Context,
		_, _ string,
		_ int64,
		_ CapabilityProviderOutcome,
	) (int64, error) {
		<-ctx.Done()
		return 0, errors.New("postgres password fixture raw detail")
	}}
	service := (&TaxComplianceService{gstHealth: recorder, log: logger.FromZap(zap.New(core))}).
		WithGSTHealthPersistenceTimeout(15 * time.Millisecond)
	started := time.Now()

	service.recordGSTProviderOutcome(context.Background(), &models.GSTIntegrationAccount{
		ID: "account-a", BusinessID: "business-a", CredentialRevision: 1,
	}, nil)

	require.Less(t, time.Since(started), 250*time.Millisecond, "bounded persistence must not leak a blocked goroutine")
	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, "GST provider health persistence issue", entries[0].Message)
	require.Equal(t, "gst_health_persistence_timeout", entries[0].ContextMap()["code"])
	serialized := entries[0].Message
	for key, value := range entries[0].ContextMap() {
		serialized += key
		serialized += strings.TrimSpace(value.(string))
	}
	require.NotContains(t, serialized, "password")
	require.NotContains(t, serialized, "raw detail")
}
