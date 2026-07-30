package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"
)

type fakeBargainingRoundProcessor struct {
	progress       *services.A2ASessionProgress
	claimed        bool
	claimErr       error
	runErr         error
	completeErr    error
	completed      bool
	runCalls       int
	completeCalls  int
	claimedRound   int
	completedRound int
	deadline       time.Time
}

func (f *fakeBargainingRoundProcessor) GetSessionProgress(string) *services.A2ASessionProgress {
	return f.progress
}

func (f *fakeBargainingRoundProcessor) GetSessionProgressByNegotiationID(context.Context, string) *services.A2ASessionProgress {
	return f.progress
}

func (f *fakeBargainingRoundProcessor) ClaimAutonomousNegotiationRound(
	_ context.Context,
	_ string,
	roundNumber int,
	_ string,
	_ time.Time,
	_ time.Time,
) (bool, error) {
	f.claimedRound = roundNumber
	return f.claimed, f.claimErr
}

func (f *fakeBargainingRoundProcessor) RunAutonomousNegotiationRound(ctx context.Context, _ string) error {
	f.runCalls++
	f.deadline, _ = ctx.Deadline()
	return f.runErr
}

func (f *fakeBargainingRoundProcessor) CompleteAutonomousNegotiationRound(
	_ context.Context,
	_ string,
	roundNumber int,
	_ string,
	_ time.Time,
) (bool, error) {
	f.completeCalls++
	f.completedRound = roundNumber
	return f.completed, f.completeErr
}

func (f *fakeBargainingRoundProcessor) StopNegotiation(string) {}

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
		"worker-1",
		startedAt,
	)
	if err != nil {
		t.Fatalf("process round: %v", err)
	}
	if processor.claimedRound != 3 || processor.completedRound != 3 {
		t.Fatalf("claimed/completed rounds = %d/%d, want 3/3", processor.claimedRound, processor.completedRound)
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
