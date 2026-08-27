package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"
)

type fakeBargainingRoundProcessor struct {
	progress              *services.A2ASessionProgress
	progressErr           error
	claimed               bool
	claimErr              error
	runErr                error
	completeErr           error
	completed             bool
	runCalls              int
	completeCalls         int
	claimedRound          int
	completedRound        int
	runRound              int
	deadline              time.Time
	ensureCalls           int
	ensureErr             error
	progressSessionID     string
	progressNegotiationID string
}

func (f *fakeBargainingRoundProcessor) GetAutonomousNegotiationProgress(
	_ context.Context,
	sessionID,
	negotiationID string,
) (*services.A2ASessionProgress, error) {
	f.progressSessionID = sessionID
	f.progressNegotiationID = negotiationID
	return f.progress, f.progressErr
}

func (f *fakeBargainingRoundProcessor) ClaimAutonomousNegotiationRound(
	_ context.Context,
	_ string,
	_ string,
	roundNumber int,
	_ string,
	_ time.Time,
	_ time.Time,
) (bool, error) {
	f.claimedRound = roundNumber
	return f.claimed, f.claimErr
}

func (f *fakeBargainingRoundProcessor) RunAutonomousNegotiationRound(
	ctx context.Context,
	_ string,
	_ string,
	roundNumber int,
) error {
	f.runCalls++
	f.runRound = roundNumber
	f.deadline, _ = ctx.Deadline()
	return f.runErr
}

func (f *fakeBargainingRoundProcessor) EnsureAutonomousNegotiationSuccessor(
	context.Context,
	string,
	string,
	int,
) error {
	f.ensureCalls++
	return f.ensureErr
}

func (f *fakeBargainingRoundProcessor) CompleteAutonomousNegotiationRound(
	_ context.Context,
	_ string,
	_ string,
	roundNumber int,
	_ string,
	_ time.Time,
) (bool, error) {
	f.completeCalls++
	f.completedRound = roundNumber
	return f.completed, f.completeErr
}

func (f *fakeBargainingRoundProcessor) StopAutonomousNegotiation(context.Context, string, string) error {
	return nil
}

func TestProcessBargainingRoundRejectsMismatchedSessionNegotiationTupleBeforeClaim(t *testing.T) {
	processor := &fakeBargainingRoundProcessor{
		progress: &services.A2ASessionProgress{
			NegotiationID: "different-session", NegotiationUUID: "negotiation-1",
			Round: 0, MaxRounds: 5, Status: "running",
		},
	}

	err := processBargainingRoundWithProcessor(
		context.Background(), processor, logger.New(),
		"session-1", "negotiation-1", 1, "worker-tuple", time.Now(),
	)
	if err == nil || !strings.Contains(err.Error(), "tuple") {
		t.Fatalf("mismatched tuple error = %v", err)
	}
	if processor.claimedRound != 0 || processor.runCalls != 0 || processor.completeCalls != 0 {
		t.Fatalf("mismatched tuple claim/run/complete = %d/%d/%d", processor.claimedRound, processor.runCalls, processor.completeCalls)
	}
	if processor.progressSessionID != "session-1" || processor.progressNegotiationID != "negotiation-1" {
		t.Fatalf("progress lookup tuple = %q/%q", processor.progressSessionID, processor.progressNegotiationID)
	}
}

func TestProcessBargainingRoundRetriesStorageFailureBeforeClaim(t *testing.T) {
	repositoryFailure := errors.New("database unavailable")
	processor := &fakeBargainingRoundProcessor{progressErr: repositoryFailure}

	err := processBargainingRoundWithProcessor(
		context.Background(), processor, logger.New(),
		"session-1", "negotiation-1", 1, "worker-storage", time.Now(),
	)
	if !errors.Is(err, repositoryFailure) {
		t.Fatalf("storage failure error = %v, want wrapped repository error", err)
	}
	if strings.Contains(err.Error(), "not found") {
		t.Fatalf("storage failure was mislabeled as not found: %v", err)
	}
	if processor.claimedRound != 0 || processor.runCalls != 0 || processor.ensureCalls != 0 || processor.completeCalls != 0 {
		t.Fatalf(
			"storage failure claim/run/ensure/complete = %d/%d/%d/%d",
			processor.claimedRound,
			processor.runCalls,
			processor.ensureCalls,
			processor.completeCalls,
		)
	}
}

func TestProcessBargainingRoundNoOpsStoppedDelayedMessageBeforeClaim(t *testing.T) {
	processor := &fakeBargainingRoundProcessor{
		progress: &services.A2ASessionProgress{
			NegotiationID: "session-1", NegotiationUUID: "negotiation-1",
			Round: 1, MaxRounds: 5, Status: "stopped",
		},
	}

	if err := processBargainingRoundWithProcessor(
		context.Background(), processor, logger.New(),
		"session-1", "negotiation-1", 2, "worker-delayed", time.Now(),
	); err != nil {
		t.Fatalf("stopped delayed message: %v", err)
	}
	if processor.claimedRound != 0 || processor.runCalls != 0 || processor.ensureCalls != 0 || processor.completeCalls != 0 {
		t.Fatalf("stopped message claim/run/ensure/complete = %d/%d/%d/%d", processor.claimedRound, processor.runCalls, processor.ensureCalls, processor.completeCalls)
	}
}

func TestProcessBargainingRoundClaimsNextRoundAndBoundsProviderCall(t *testing.T) {
	processor := &fakeBargainingRoundProcessor{
		progress: &services.A2ASessionProgress{
			NegotiationID:   "session-1",
			NegotiationUUID: "negotiation-1",
			Round:           2,
			MaxRounds:       5,
			Status:          "running",
		},
		claimed:   true,
		completed: true,
	}

	startedAt := time.Now()
	err := processBargainingRoundWithProcessor(
		context.Background(),
		processor,
		logger.New(),
		"session-1",
		"negotiation-1",
		3,
		"worker-1",
		startedAt,
	)
	if err != nil {
		t.Fatalf("process round: %v", err)
	}
	if processor.claimedRound != 3 || processor.runRound != 3 || processor.completedRound != 3 {
		t.Fatalf(
			"claimed/run/completed rounds = %d/%d/%d, want 3/3/3",
			processor.claimedRound,
			processor.runRound,
			processor.completedRound,
		)
	}
	if processor.runCalls != 1 || processor.completeCalls != 1 {
		t.Fatalf("run/complete calls = %d/%d, want 1/1", processor.runCalls, processor.completeCalls)
	}
	if processor.deadline.IsZero() {
		t.Fatal("provider call has no deadline")
	}
	timeout := processor.deadline.Sub(startedAt)
	if timeout < 44*time.Second || timeout > 46*time.Second {
		t.Fatalf("provider timeout = %v, want approximately 45s", timeout)
	}
}

func TestProcessBargainingRoundNoOpsWhenAnotherWorkerOwnsClaim(t *testing.T) {
	processor := &fakeBargainingRoundProcessor{
		progress: &services.A2ASessionProgress{
			NegotiationID:   "session-1",
			NegotiationUUID: "negotiation-1",
			Round:           0,
			MaxRounds:       5,
			Status:          "running",
		},
		claimed: false,
	}

	err := processBargainingRoundWithProcessor(
		context.Background(),
		processor,
		logger.New(),
		"session-1",
		"negotiation-1",
		1,
		"worker-2",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("duplicate claim should no-op: %v", err)
	}
	if processor.runCalls != 0 || processor.completeCalls != 0 {
		t.Fatalf("duplicate invoked provider/completion: %d/%d", processor.runCalls, processor.completeCalls)
	}
}

func TestProcessBargainingRoundDoesNotCompleteFailedProviderCall(t *testing.T) {
	processor := &fakeBargainingRoundProcessor{
		progress: &services.A2ASessionProgress{
			NegotiationID:   "session-1",
			NegotiationUUID: "negotiation-1",
			Round:           1,
			MaxRounds:       5,
			Status:          "running",
		},
		claimed: true,
		runErr:  errors.New("provider timeout"),
	}

	err := processBargainingRoundWithProcessor(
		context.Background(),
		processor,
		logger.New(),
		"session-1",
		"negotiation-1",
		2,
		"worker-3",
		time.Now(),
	)
	if err == nil {
		t.Fatal("provider failure returned nil")
	}
	if processor.completeCalls != 0 {
		t.Fatalf("failed provider completed claim %d times", processor.completeCalls)
	}
}

func TestProcessBargainingRoundDoesNotLetStaleOrFutureMessagesAdvanceDatabaseRound(t *testing.T) {
	tests := []struct {
		name              string
		messageRound      int
		wantErr           bool
		wantEnsureCalls   int
		wantCompleteCalls int
	}{
		{
			name:              "immediately stale message repairs missing successor",
			messageRound:      2,
			wantEnsureCalls:   1,
			wantCompleteCalls: 1,
		},
		{name: "older stale duplicate no-ops", messageRound: 1},
		{name: "future out of order retries", messageRound: 4, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &fakeBargainingRoundProcessor{
				progress: &services.A2ASessionProgress{
					NegotiationID:   "session-1",
					NegotiationUUID: "negotiation-1",
					Round:           2,
					MaxRounds:       5,
					Status:          "running",
				},
				claimed:   true,
				completed: true,
			}

			err := processBargainingRoundWithProcessor(
				context.Background(),
				processor,
				logger.New(),
				"session-1",
				"negotiation-1",
				test.messageRound,
				"worker-replay",
				time.Now(),
			)

			if test.wantErr && err == nil {
				t.Fatal("future message returned nil")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("stale duplicate returned error: %v", err)
			}
			if processor.claimedRound != 0 || processor.runCalls != 0 ||
				processor.ensureCalls != test.wantEnsureCalls ||
				processor.completeCalls != test.wantCompleteCalls {
				t.Fatalf(
					"mismatched message claim/run/ensure/complete = %d/%d/%d/%d",
					processor.claimedRound,
					processor.runCalls,
					processor.ensureCalls,
					processor.completeCalls,
				)
			}
		})
	}
}

func TestProcessBargainingRoundRetriesWhenSuccessorRepairFails(t *testing.T) {
	processor := &fakeBargainingRoundProcessor{
		progress: &services.A2ASessionProgress{
			NegotiationID:   "session-1",
			NegotiationUUID: "negotiation-1",
			Round:           2,
			MaxRounds:       5,
			Status:          "running",
		},
		ensureErr: errors.New("SQS unavailable"),
	}

	err := processBargainingRoundWithProcessor(
		context.Background(),
		processor,
		logger.New(),
		"session-1",
		"negotiation-1",
		2,
		"worker-repair",
		time.Now(),
	)

	if err == nil || !strings.Contains(err.Error(), "successor") {
		t.Fatalf("successor repair error = %v", err)
	}
	if processor.completeCalls != 0 {
		t.Fatalf("failed successor repair completed claim %d times", processor.completeCalls)
	}
}
