package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

var (
	bargainingInitOnce sync.Once
	bargainingRT       *app.Runtime
	bargainingInitErr  error
)

const (
	bargainingProviderTimeout = 45 * time.Second
	bargainingClaimLease      = 60 * time.Second
)

func initBargainingRuntime() {
	ctx := context.Background()
	bargainingRT, bargainingInitErr = app.Initialize(ctx, app.InitializeOptions{
		EnableWorker: false,
		Profile:      config.ProfileBargaining,
	})
	if bargainingInitErr != nil {
		bargainingInitErr = fmt.Errorf("initialize bargaining worker runtime: %w", bargainingInitErr)
	}
}

type BargainingMessage struct {
	Type          string `json:"type"`
	SessionID     string `json:"session_id,omitempty"`
	NegotiationID string `json:"negotiation_id,omitempty"`
	Round         int    `json:"round,omitempty"`
}

func handleSQSEvent(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	bargainingInitOnce.Do(initBargainingRuntime)
	if bargainingInitErr != nil {
		return events.SQSEventResponse{}, bargainingInitErr
	}
	failures := make([]events.SQSBatchItemFailure, 0)
	for _, record := range event.Records {
		if err := processBargainingMessage(ctx, bargainingRT.Config, bargainingRT.Svcs, bargainingRT.Log, record.MessageId, record.Body); err != nil {
			bargainingRT.Log.Error("failed to process bargaining queue record", "message_id", record.MessageId, "error", err)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func processBargainingMessage(ctx context.Context, cfg *config.Config, svc *services.Container, log *logger.Logger, leaseOwner, body string) error {
	if cfg == nil || svc == nil || log == nil {
		return fmt.Errorf("invalid dependencies for bargaining queue processing")
	}
	if leaseOwner == "" {
		return fmt.Errorf("bargaining queue message ID is required")
	}

	var msg BargainingMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return err
	}

	log.Info("processing bargaining message", "type", msg.Type, "session_id", msg.SessionID, "negotiation_id", msg.NegotiationID, "round", msg.Round)

	switch msg.Type {
	case "process_round":
		return processBargainingRound(ctx, cfg, svc, log, msg.SessionID, msg.NegotiationID, msg.Round, leaseOwner)
	default:
		log.Warn("unknown bargaining message type", "type", msg.Type)
		return nil
	}
}

type bargainingRoundProcessor interface {
	GetSessionProgress(sessionID string) *services.A2ASessionProgress
	GetSessionProgressByNegotiationID(ctx context.Context, negotiationID string) *services.A2ASessionProgress
	ClaimAutonomousNegotiationRound(ctx context.Context, negotiationID string, roundNumber int, leaseOwner string, now, leaseExpiresAt time.Time) (bool, error)
	RunAutonomousNegotiationRound(ctx context.Context, sessionID string, roundNumber int) error
	EnsureAutonomousNegotiationSuccessor(ctx context.Context, sessionID, negotiationID string, completedRound int) error
	CompleteAutonomousNegotiationRound(ctx context.Context, negotiationID string, roundNumber int, leaseOwner string, completedAt time.Time) (bool, error)
	StopNegotiation(sessionID string)
}

func processBargainingRound(
	ctx context.Context,
	cfg *config.Config,
	svc *services.Container,
	log *logger.Logger,
	sessionID,
	negotiationID string,
	currentRound int,
	leaseOwner string,
) error {
	a2aSvc := svc.A2ABargaining
	if a2aSvc == nil {
		return fmt.Errorf("A2A bargaining service not available")
	}
	_ = cfg
	log.Info("received bargaining round", "session_id", sessionID, "negotiation_id", negotiationID, "message_round", currentRound)
	return processBargainingRoundWithProcessor(
		ctx,
		a2aSvc,
		log,
		sessionID,
		negotiationID,
		currentRound,
		leaseOwner,
		time.Now(),
	)
}

func processBargainingRoundWithProcessor(
	ctx context.Context,
	processor bargainingRoundProcessor,
	log *logger.Logger,
	sessionID,
	negotiationID string,
	messageRound int,
	leaseOwner string,
	startedAt time.Time,
) error {
	if messageRound <= 0 {
		return fmt.Errorf("invalid bargaining message round: %d", messageRound)
	}
	progress := processor.GetSessionProgress(sessionID)
	if progress == nil {
		progress = processor.GetSessionProgressByNegotiationID(ctx, negotiationID)
	}
	if progress == nil {
		return fmt.Errorf("session not found: session_id=%s, negotiation_id=%s", sessionID, negotiationID)
	}

	log.Info("processing bargaining round",
		"session_id", sessionID,
		"negotiation_id", negotiationID,
		"db_round", progress.Round,
		"max_rounds", progress.MaxRounds,
		"status", progress.Status)

	if progress.Status == "completed" || progress.Status == "accepted" || progress.Status == "rejected" || progress.Status == "expired" {
		log.Info("negotiation already completed", "session_id", sessionID, "status", progress.Status)
		return nil
	}

	if progress.MaxRounds <= 0 {
		log.Error("invalid max_rounds on negotiation, cannot process", "session_id", sessionID, "max_rounds", progress.MaxRounds, "db_round", progress.Round)
		processor.StopNegotiation(sessionID)
		return fmt.Errorf("invalid max_rounds: %d", progress.MaxRounds)
	}

	if progress.Round >= progress.MaxRounds {
		log.Info("max rounds reached", "session_id", sessionID, "round", progress.Round, "max_rounds", progress.MaxRounds)
		processor.StopNegotiation(sessionID)
		return nil
	}

	nextRound := progress.Round + 1
	if messageRound < nextRound {
		if messageRound == progress.Round {
			if err := processor.EnsureAutonomousNegotiationSuccessor(
				ctx,
				sessionID,
				negotiationID,
				messageRound,
			); err != nil {
				return fmt.Errorf(
					"ensure bargaining round %d successor: %w",
					messageRound,
					err,
				)
			}
			completed, err := processor.CompleteAutonomousNegotiationRound(
				ctx,
				negotiationID,
				messageRound,
				leaseOwner,
				time.Now(),
			)
			if err != nil {
				return fmt.Errorf(
					"complete repaired bargaining round %d claim: %w",
					messageRound,
					err,
				)
			}
			log.Info(
				"repaired bargaining successor after committed round",
				"session_id", sessionID,
				"negotiation_id", negotiationID,
				"message_round", messageRound,
				"claim_completed", completed,
			)
			return nil
		}
		log.Info(
			"stale bargaining round message already applied",
			"session_id", sessionID,
			"negotiation_id", negotiationID,
			"message_round", messageRound,
			"db_next_round", nextRound,
		)
		return nil
	}
	if messageRound > nextRound {
		return fmt.Errorf(
			"out-of-order bargaining round message: message_round=%d db_next_round=%d",
			messageRound,
			nextRound,
		)
	}
	claimed, err := processor.ClaimAutonomousNegotiationRound(
		ctx,
		negotiationID,
		nextRound,
		leaseOwner,
		startedAt,
		startedAt.Add(bargainingClaimLease),
	)
	if err != nil {
		return fmt.Errorf("claim bargaining round %d: %w", nextRound, err)
	}
	if !claimed {
		log.Info("bargaining round already claimed", "session_id", sessionID, "negotiation_id", negotiationID, "round", nextRound)
		return nil
	}

	providerCtx, cancel := context.WithTimeout(ctx, bargainingProviderTimeout)
	defer cancel()
	if err := processor.RunAutonomousNegotiationRound(providerCtx, sessionID, nextRound); err != nil {
		log.Error("failed to run bargaining round", "error", err, "session_id", sessionID, "round", nextRound)
		return err
	}
	completed, err := processor.CompleteAutonomousNegotiationRound(
		ctx,
		negotiationID,
		nextRound,
		leaseOwner,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("complete bargaining round %d claim: %w", nextRound, err)
	}
	if !completed {
		return fmt.Errorf("bargaining round %d claim ownership lost", nextRound)
	}

	log.Info("bargaining round completed", "session_id", sessionID, "round", nextRound)
	return nil
}

func main() {
	lambda.Start(handleSQSEvent)
}
