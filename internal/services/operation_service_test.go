package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/awsclients"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
)

type operationRepositoryFake struct {
	pages           map[string]interfaces.OperationRecordPage
	get             map[string]*interfaces.OperationRecord
	timeline        map[string][]interfaces.OperationTimelineRecord
	recoveryResult  *interfaces.OperationRecoveryResult
	recoveryCommand interfaces.RenderRecoveryCommand
	recoveryCalls   int
	decision        interfaces.OperationRecoveryDecision
	decisionCalls   int
	decisionErr     error
	err             error
}

func (f *operationRepositoryFake) RecordRecoveryDecision(
	_ context.Context,
	decision interfaces.OperationRecoveryDecision,
) (*interfaces.OperationRecoveryResult, error) {
	f.decisionCalls++
	f.decision = decision
	if f.decisionErr != nil {
		return nil, f.decisionErr
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.recoveryResult != nil {
		clone := *f.recoveryResult
		clone.ResultCode = decision.ResultCode
		return &clone, nil
	}
	return &interfaces.OperationRecoveryResult{
		CommandID: uuid.NewString(), ResultCode: decision.ResultCode,
		CorrelationID: decision.CorrelationID, AcceptedAt: decision.OccurredAt,
	}, nil
}

func (f *operationRepositoryFake) ListOperations(
	_ context.Context,
	businessID string,
	query interfaces.OperationRecordQuery,
) (interfaces.OperationRecordPage, error) {
	if f.err != nil {
		return interfaces.OperationRecordPage{}, f.err
	}
	return f.pages[businessID], nil
}

func (f *operationRepositoryFake) GetOperation(
	_ context.Context,
	businessID, operationType, operationID string,
) (*interfaces.OperationRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	value := f.get[businessID+"/"+operationType+"/"+operationID]
	if value == nil {
		return nil, interfaces.ErrOperationNotFound
	}
	clone := *value
	return &clone, nil
}

func (f *operationRepositoryFake) ListOperationTimeline(
	_ context.Context,
	businessID string,
	operationType string,
	operationID string,
	_ int,
) ([]interfaces.OperationTimelineRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]interfaces.OperationTimelineRecord(nil), f.timeline[businessID+"/"+operationType+"/"+operationID]...), nil
}

func TestOperationServiceOperatorDetailAndTimelineRemainSanitized(t *testing.T) {
	now := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	businessID := uuid.NewString()
	operationID := uuid.NewString()
	key := businessID + "/" + OperationTypeRazorpayWebhook + "/" + operationID
	repository := &operationRepositoryFake{
		get: map[string]*interfaces.OperationRecord{key: {
			ID: operationID, Type: OperationTypeRazorpayWebhook, ResourceType: "subscription", ResourceID: uuid.NewString(),
			InternalStatus: "reconciliation_required", ErrorCode: "provider_state_ambiguous", Attempts: 2,
			ReconciliationRequired: true, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		}},
		timeline: map[string][]interfaces.OperationTimelineRecord{key: {
			{Status: "received", Code: "webhook_received", OccurredAt: now.Add(-time.Hour)},
			{Status: "reconciliation_required", Code: "unsafe raw provider error: card 4242", OccurredAt: now},
		}},
	}
	service := NewOperationService(repository, nil, nil, OperationServiceOptions{})

	detail, err := service.GetOperatorOperation(context.Background(), businessID, OperationTypeRazorpayWebhook+":"+operationID)
	if err != nil {
		t.Fatalf("GetOperatorOperation() error = %v", err)
	}
	if detail.SourceStatus != "reconciliation_required" || len(detail.RecoveryActions) == 0 ||
		detail.RecoveryActions[0].RequirementCode != OperationCodeStepUpRequired {
		t.Fatalf("operator detail = %#v", detail)
	}
	timeline, err := service.GetOperationTimeline(context.Background(), businessID, OperationTypeRazorpayWebhook+":"+operationID, 10)
	if err != nil {
		t.Fatalf("GetOperationTimeline() error = %v", err)
	}
	if len(timeline.Events) != 2 || timeline.Events[1].Status != OperationStatusReconciliationRequired ||
		timeline.Events[1].Code != "operation_error" {
		t.Fatalf("timeline = %#v", timeline)
	}
	payload, _ := json.Marshal(struct {
		Detail   *OperationOperatorDetail `json:"detail"`
		Timeline *OperationTimeline       `json:"timeline"`
	}{detail, timeline})
	for _, forbidden := range []string{"raw_payload", "provider_reference", "queue_message_id", "card 4242"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("operator payload leaked %q: %s", forbidden, payload)
		}
	}

	_, err = service.GetOperationTimeline(context.Background(), businessID, OperationTypeRazorpayWebhook+":"+operationID, 101)
	if !errors.Is(err, ErrInvalidOperationQuery) {
		t.Fatalf("timeline limit error = %v, want ErrInvalidOperationQuery", err)
	}
}

func TestOperationServiceOperatorDetailDoesNotAdvertiseUnsafeRenderRetry(t *testing.T) {
	businessID := uuid.NewString()
	operationID := uuid.NewString()
	repository := &operationRepositoryFake{get: map[string]*interfaces.OperationRecord{
		businessID + "/" + OperationTypeInvoiceRender + "/" + operationID: {
			ID: operationID, Type: OperationTypeInvoiceRender, InternalStatus: "failed", Retryable: false,
			CreatedAt: time.Now().UTC().Add(-time.Minute), UpdatedAt: time.Now().UTC(),
		},
	}}
	detail, err := NewOperationService(repository, nil, nil, OperationServiceOptions{}).
		GetOperatorOperation(context.Background(), businessID, OperationTypeInvoiceRender+":"+operationID)
	if err != nil {
		t.Fatalf("GetOperatorOperation() error = %v", err)
	}
	if len(detail.RecoveryActions) != 1 || detail.RecoveryActions[0].Available ||
		detail.RecoveryActions[0].RequirementCode != OperationCodeUnsupportedRecovery {
		t.Fatalf("unsafe render actions = %#v", detail.RecoveryActions)
	}
}

func (f *operationRepositoryFake) RetryRender(
	_ context.Context,
	command interfaces.RenderRecoveryCommand,
) (*interfaces.OperationRecoveryResult, error) {
	f.recoveryCalls++
	f.recoveryCommand = command
	if f.err != nil {
		return nil, f.err
	}
	if f.recoveryResult == nil {
		return nil, interfaces.ErrUnsupportedRecovery
	}
	clone := *f.recoveryResult
	return &clone, nil
}

type operationPermissionFake struct {
	allow      bool
	calls      int
	userID     string
	businessID string
	permission string
}

func (f *operationPermissionFake) UserHasPermission(_ context.Context, userID, businessID, permission string) bool {
	f.calls++
	f.userID, f.businessID, f.permission = userID, businessID, permission
	return f.allow
}

type operationRecoveryCapabilityFake struct {
	err     error
	calls   int
	request OperationRecoveryCapabilityRequest
}

func (f *operationRecoveryCapabilityFake) RequireOperationRecovery(_ context.Context, request OperationRecoveryCapabilityRequest) error {
	f.calls++
	f.request = request
	return f.err
}

func TestOperationServiceBusinessProjectionPreservesUnknownAndSanitizesSources(t *testing.T) {
	now := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	businessID := uuid.NewString()
	renderID := uuid.NewString()
	deliveryID := uuid.NewString()
	repository := &operationRepositoryFake{pages: map[string]interfaces.OperationRecordPage{
		businessID: {
			Records: []interfaces.OperationRecord{
				{
					ID: renderID, Type: OperationTypeInvoiceRender, ResourceType: "invoice", ResourceID: uuid.NewString(),
					InternalStatus: "provider_made_this_up", Attempts: 2, ErrorCode: "render_status_unknown",
					CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
				},
				{
					ID: deliveryID, Type: OperationTypeInvoiceDelivery, ResourceType: "invoice", ResourceID: uuid.NewString(),
					InternalStatus: "failed", Attempts: 3, ErrorCode: "delivery_failed", Retryable: false,
					CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-time.Minute),
				},
			},
			UnavailableTypes: []string{OperationTypeVoiceReconciliation},
		},
	}}
	service := NewOperationService(repository, nil, nil, OperationServiceOptions{Now: func() time.Time { return now }})

	response, err := service.ListBusinessOperations(context.Background(), businessID, OperationListInput{Limit: 10})
	if err != nil {
		t.Fatalf("ListBusinessOperations() error = %v", err)
	}
	if len(response.Operations) != 2 {
		t.Fatalf("operations = %#v", response.Operations)
	}
	if response.Operations[0].Status != OperationStatusUnknown {
		t.Fatalf("unknown status collapsed to %q", response.Operations[0].Status)
	}
	if response.Operations[1].Status != OperationStatusFailed {
		t.Fatalf("failed status = %q", response.Operations[1].Status)
	}
	if len(response.UnavailableTypes) != 1 || response.UnavailableTypes[0] != OperationTypeVoiceReconciliation {
		t.Fatalf("unavailable types = %#v", response.UnavailableTypes)
	}

	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	for _, forbidden := range []string{
		"internal_status", "provider_reference", "queue_message_id", "lease_owner", "raw_payload", "raw_error",
		"provider_made_this_up",
	} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("business response leaked %q: %s", forbidden, payload)
		}
	}
}

func TestOperationServiceGetCannotCrossTenant(t *testing.T) {
	operationID := uuid.NewString()
	businessA := uuid.NewString()
	businessB := uuid.NewString()
	repository := &operationRepositoryFake{get: map[string]*interfaces.OperationRecord{
		businessA + "/" + OperationTypeInvoiceRender + "/" + operationID: {
			ID: operationID, Type: OperationTypeInvoiceRender, ResourceType: "invoice", ResourceID: uuid.NewString(),
			InternalStatus: "completed", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
	}}
	service := NewOperationService(repository, nil, nil, OperationServiceOptions{})

	_, err := service.GetBusinessOperation(context.Background(), businessB, OperationTypeInvoiceRender+":"+operationID)
	if !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("cross-tenant GetBusinessOperation() error = %v, want ErrOperationNotFound", err)
	}
	result, err := service.GetBusinessOperation(context.Background(), businessA, OperationTypeInvoiceRender+":"+operationID)
	if err != nil || result.OperationID != OperationTypeInvoiceRender+":"+operationID {
		t.Fatalf("tenant get = %#v, %v", result, err)
	}
}

func TestOperationServicePaginationIsBoundedAndCursorIsFilterBound(t *testing.T) {
	now := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	businessID := uuid.NewString()
	repository := &operationRepositoryFake{pages: map[string]interfaces.OperationRecordPage{
		businessID: {Records: []interfaces.OperationRecord{
			{ID: uuid.NewString(), Type: OperationTypeOutbox, InternalStatus: "pending", CreatedAt: now, UpdatedAt: now},
			{ID: uuid.NewString(), Type: OperationTypeOutbox, InternalStatus: "pending", CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)},
		}},
	}}
	service := NewOperationService(repository, nil, nil, OperationServiceOptions{Now: func() time.Time { return now }})

	first, err := service.ListBusinessOperations(context.Background(), businessID, OperationListInput{
		Types: []string{OperationTypeOutbox}, Statuses: []OperationStatus{OperationStatusQueued}, Limit: 1,
	})
	if err != nil || len(first.Operations) != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	_, err = service.ListBusinessOperations(context.Background(), businessID, OperationListInput{
		Types: []string{OperationTypeInvoiceRender}, Statuses: []OperationStatus{OperationStatusQueued}, Limit: 1,
		Cursor: first.NextCursor,
	})
	if !errors.Is(err, ErrInvalidOperationQuery) {
		t.Fatalf("cursor reused across filters error = %v, want ErrInvalidOperationQuery", err)
	}
	_, err = service.ListBusinessOperations(context.Background(), businessID, OperationListInput{Limit: 101})
	if !errors.Is(err, ErrInvalidOperationQuery) {
		t.Fatalf("limit 101 error = %v, want ErrInvalidOperationQuery", err)
	}
}

func TestOperationServiceRetriesOnlyExactFailedRenderAfterPermissionAndCapabilityChecks(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 0, 0, 123, time.UTC)
	businessID := uuid.NewString()
	actorID := uuid.NewString()
	operationID := uuid.NewString()
	correlationID := uuid.NewString()
	repository := &operationRepositoryFake{
		get: map[string]*interfaces.OperationRecord{
			businessID + "/" + OperationTypeInvoiceRender + "/" + operationID: {
				ID: operationID, Type: OperationTypeInvoiceRender, ResourceType: "invoice", ResourceID: uuid.NewString(),
				InternalStatus: "failed", Retryable: true, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute),
			},
		},
		recoveryResult: &interfaces.OperationRecoveryResult{
			CommandID: uuid.NewString(), ResultCode: OperationCodeAccepted, CorrelationID: correlationID, AcceptedAt: now,
		},
	}
	permissions := &operationPermissionFake{allow: true}
	capabilities := &operationRecoveryCapabilityFake{}
	service := NewOperationService(repository, permissions, capabilities, OperationServiceOptions{Now: func() time.Time { return now }})
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: actorID, Role: "member"})

	result, err := service.RecoverBusinessOperation(ctx, businessID, OperationTypeInvoiceRender+":"+operationID, OperationRecoveryInput{
		Action: OperationActionRetry, Reason: "retry deterministic render", IdempotencyKey: uuid.NewString(), CorrelationID: correlationID,
	})
	if err != nil {
		t.Fatalf("RecoverBusinessOperation() error = %v", err)
	}
	if result.ResultCode != OperationCodeAccepted || repository.recoveryCalls != 1 {
		t.Fatalf("recovery result/calls = %#v/%d", result, repository.recoveryCalls)
	}
	if permissions.calls != 1 || permissions.userID != actorID || permissions.businessID != businessID ||
		permissions.permission != PermissionDocumentsManage {
		t.Fatalf("permission check = %#v", permissions)
	}
	if capabilities.calls != 1 || capabilities.request.BusinessID != businessID ||
		capabilities.request.UserID != actorID || capabilities.request.OperationType != OperationTypeInvoiceRender ||
		capabilities.request.Action != OperationActionRetry {
		t.Fatalf("capability check = %#v", capabilities)
	}
	if repository.recoveryCommand.BusinessID != businessID || repository.recoveryCommand.OperationID != operationID ||
		repository.recoveryCommand.ActorSubject != actorID || repository.recoveryCommand.PrincipalKind != OperationPrincipalBusiness ||
		repository.recoveryCommand.OperationVersion != now.Add(-time.Minute).Format(time.RFC3339Nano) ||
		len(repository.recoveryCommand.RequestHash) != 64 {
		t.Fatalf("recovery command = %#v", repository.recoveryCommand)
	}
}

func TestOperationServiceReturnsAnIdempotentRenderReplayAfterTheWorkerChangesState(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 15, 0, 0, time.UTC)
	businessID := uuid.NewString()
	actorID := uuid.NewString()
	operationID := uuid.NewString()
	correlationID := uuid.NewString()
	repository := &operationRepositoryFake{
		get: map[string]*interfaces.OperationRecord{
			businessID + "/" + OperationTypeInvoiceRender + "/" + operationID: {
				ID: operationID, Type: OperationTypeInvoiceRender, InternalStatus: "completed", Retryable: false,
				CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
			},
		},
		recoveryResult: &interfaces.OperationRecoveryResult{
			CommandID: uuid.NewString(), ResultCode: OperationCodeAccepted, CorrelationID: correlationID,
			Replayed: true, AcceptedAt: now.Add(-time.Minute),
		},
	}
	service := NewOperationService(
		repository,
		&operationPermissionFake{allow: true},
		&operationRecoveryCapabilityFake{},
		OperationServiceOptions{Now: func() time.Time { return now }},
	)
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: actorID})
	result, err := service.RecoverBusinessOperation(
		ctx,
		businessID,
		OperationTypeInvoiceRender+":"+operationID,
		OperationRecoveryInput{
			Action: OperationActionRetry, Reason: "retry deterministic render",
			IdempotencyKey: uuid.NewString(), CorrelationID: correlationID,
		},
	)
	if err != nil || result == nil || !result.Replayed || repository.recoveryCalls != 1 {
		t.Fatalf("idempotent replay after state change = %#v, %v; recovery calls = %d", result, err, repository.recoveryCalls)
	}
	first := repository.recoveryCommand
	second := first
	second.OperationVersion = now.Add(time.Minute).Format(time.RFC3339Nano)
	if operationRecoveryRequestHash(first) != operationRecoveryRequestHash(second) {
		t.Fatal("client-equivalent recovery request hash changed with server-derived operation version")
	}
}

func TestOperationServiceOperatorHighRiskRecoveryFailsClosedAndIsDurablyAudited(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 30, 0, 0, time.UTC)
	businessID := uuid.NewString()
	actorID := "operator-subject"
	operationID := uuid.NewString()
	repository := &operationRepositoryFake{get: map[string]*interfaces.OperationRecord{
		businessID + "/" + OperationTypeRazorpayWebhook + "/" + operationID: {
			ID: operationID, Type: OperationTypeRazorpayWebhook, ResourceType: "subscription", ResourceID: uuid.NewString(),
			InternalStatus: "reconciliation_required", ReconciliationRequired: true,
			CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute),
		},
	}}
	service := NewOperationService(repository, nil, nil, OperationServiceOptions{Now: func() time.Time { return now }})
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: actorID, Role: "viewer"})
	input := OperationRecoveryInput{
		Action: OperationActionReprocessWebhook, Reason: "verified event requires controlled replay",
		IdempotencyKey: uuid.NewString(), CorrelationID: uuid.NewString(),
	}

	result, err := service.RecoverOperatorOperation(ctx, businessID, OperationTypeRazorpayWebhook+":"+operationID, input, "untrusted-step-up-token")
	if !errors.Is(err, ErrOperationStepUpRequired) || result != nil {
		t.Fatalf("RecoverOperatorOperation() = %#v, %v; want step-up required", result, err)
	}
	if repository.decisionCalls != 1 || repository.recoveryCalls != 0 {
		t.Fatalf("decision/recovery calls = %d/%d", repository.decisionCalls, repository.recoveryCalls)
	}
	if repository.decision.BusinessID != businessID || repository.decision.OperationID != operationID ||
		repository.decision.ActorSubject != actorID || repository.decision.PrincipalKind != OperationPrincipalOperator ||
		repository.decision.ResultCode != OperationCodeStepUpRequired || repository.decision.Reason != input.Reason ||
		repository.decision.IdempotencyKey != input.IdempotencyKey || repository.decision.CorrelationID != input.CorrelationID ||
		len(repository.decision.RequestHash) != 64 {
		t.Fatalf("audited decision = %#v", repository.decision)
	}
	updatedDecision := repository.decision
	updatedDecision.OperationVersion = now.Add(time.Minute).Format(time.RFC3339Nano)
	if operationRecoveryDecisionHash(repository.decision) != operationRecoveryDecisionHash(updatedDecision) {
		t.Fatal("operator idempotency hash changed with server-derived operation version")
	}
}

func TestOperationServiceOperatorRecoveryRejectsConflictingIdempotencyReplayWithStableCode(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 45, 0, 0, time.UTC)
	businessID := uuid.NewString()
	operationID := uuid.NewString()
	repository := &operationRepositoryFake{
		get: map[string]*interfaces.OperationRecord{
			businessID + "/" + OperationTypeRazorpayWebhook + "/" + operationID: {
				ID: operationID, Type: OperationTypeRazorpayWebhook, InternalStatus: "reconciliation_required",
				CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute),
			},
		},
		decisionErr: interfaces.ErrUnsafeOperationReplay,
	}
	service := NewOperationService(repository, nil, nil, OperationServiceOptions{Now: func() time.Time { return now }})
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: "operator-subject"})
	_, err := service.RecoverOperatorOperation(
		ctx,
		businessID,
		OperationTypeRazorpayWebhook+":"+operationID,
		OperationRecoveryInput{
			Action: OperationActionReprocessWebhook, Reason: "conflicting command",
			IdempotencyKey: uuid.NewString(), CorrelationID: uuid.NewString(),
		},
		"",
	)
	if !errors.Is(err, ErrUnsafeOperationReplay) {
		t.Fatalf("conflicting operator replay error = %v, want ErrUnsafeOperationReplay", err)
	}
}

func TestConfiguredOperationRecoveryCapabilityGuardReevaluatesRuntimeConfiguration(t *testing.T) {
	cfg := &config.Config{}
	guard := NewConfiguredOperationRecoveryCapabilityGuard(cfg, &awsclients.Config{})
	request := OperationRecoveryCapabilityRequest{
		BusinessID: uuid.NewString(), UserID: uuid.NewString(),
		OperationType: OperationTypeInvoiceRender, Action: OperationActionRetry,
	}
	if err := guard.RequireOperationRecovery(context.Background(), request); !errors.Is(err, ErrUnsupportedOperationRecovery) {
		t.Fatalf("missing runtime configuration error = %v", err)
	}
	cfg.SQS.InvoiceQueue = "https://sqs.example/invoice"
	cfg.S3.BucketInvoices = "invoice-bucket"
	if err := guard.RequireOperationRecovery(context.Background(), request); !errors.Is(err, ErrUnsupportedOperationRecovery) {
		t.Fatalf("missing runtime clients error = %v", err)
	}
	guard.aws.SQS = &sqs.Client{}
	guard.aws.S3 = &s3.Client{}
	if err := guard.RequireOperationRecovery(context.Background(), request); err != nil {
		t.Fatalf("configured recovery capability error = %v", err)
	}
	cfg.SQS.InvoiceQueue = ""
	if err := guard.RequireOperationRecovery(context.Background(), request); !errors.Is(err, ErrUnsupportedOperationRecovery) {
		t.Fatalf("runtime configuration removal error = %v", err)
	}
}
