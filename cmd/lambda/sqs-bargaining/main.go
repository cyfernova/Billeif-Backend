package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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

func initBargainingRuntime() {
	ctx := context.Background()
	bargainingRT, bargainingInitErr = app.Initialize(ctx, app.InitializeOptions{EnableWorker: false})
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
		if err := processBargainingMessage(ctx, bargainingRT.Config, bargainingRT.Svcs, bargainingRT.Log, record.Body); err != nil {
			bargainingRT.Log.Error("failed to process bargaining queue record", "message_id", record.MessageId, "error", err)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func processBargainingMessage(ctx context.Context, cfg *config.Config, svc *services.Container, log *logger.Logger, body string) error {
	if cfg == nil || svc == nil || log == nil {
		return fmt.Errorf("invalid dependencies for bargaining queue processing")
	}

	var msg BargainingMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return err
	}

	log.Info("processing bargaining message", "type", msg.Type, "session_id", msg.SessionID, "negotiation_id", msg.NegotiationID, "round", msg.Round)

	switch msg.Type {
	case "process_round":
		return processBargainingRound(ctx, cfg, svc, log, msg.SessionID, msg.NegotiationID, msg.Round)
	default:
		log.Warn("unknown bargaining message type", "type", msg.Type)
		return nil
	}
}

func processBargainingRound(ctx context.Context, cfg *config.Config, svc *services.Container, log *logger.Logger, sessionID, negotiationID string, currentRound int) error {
	a2aSvc := svc.A2ABargaining
	if a2aSvc == nil {
		return fmt.Errorf("A2A bargaining service not available")
	}

	// Get the session progress to determine next action
	progress := a2aSvc.GetSessionProgress(sessionID)
	if progress == nil {
		// Session not found in memory, try by negotiation ID
		progress = a2aSvc.GetSessionProgressByNegotiationID(ctx, negotiationID)
	}
	if progress == nil {
		return fmt.Errorf("session not found: session_id=%s, negotiation_id=%s", sessionID, negotiationID)
	}

	log.Info("processing bargaining round",
		"session_id", sessionID,
		"negotiation_id", negotiationID,
		"current_round", currentRound,
		"db_round", progress.Round,
		"max_rounds", progress.MaxRounds,
		"status", progress.Status)

	// Check if negotiation is complete
	if progress.Status == "completed" || progress.Status == "accepted" || progress.Status == "rejected" || progress.Status == "expired" {
		log.Info("negotiation already completed", "session_id", sessionID, "status", progress.Status)
		return nil
	}

	// Guard: if max_rounds is 0 or negative, negotiation can never progress
	if progress.MaxRounds <= 0 {
		log.Error("invalid max_rounds on negotiation, cannot process", "session_id", sessionID, "max_rounds", progress.MaxRounds, "db_round", progress.Round)
		a2aSvc.StopNegotiation(sessionID)
		return fmt.Errorf("invalid max_rounds: %d", progress.MaxRounds)
	}

	// Check if max rounds reached
	if progress.Round >= progress.MaxRounds {
		log.Info("max rounds reached", "session_id", sessionID, "round", progress.Round, "max_rounds", progress.MaxRounds)
		a2aSvc.StopNegotiation(sessionID)
		return nil
	}

	// Run one round of the autonomous negotiation
	err := a2aSvc.RunAutonomousNegotiationRound(ctx, sessionID)
	if err != nil {
		log.Error("failed to run bargaining round", "error", err, "session_id", sessionID, "round", progress.Round)
		return err
	}

	// Enqueue next round if negotiation is still active
	updatedProgress := a2aSvc.GetSessionProgress(sessionID)
	if updatedProgress == nil {
		updatedProgress = a2aSvc.GetSessionProgressByNegotiationID(ctx, negotiationID)
	}
	if updatedProgress != nil {
		isDone := updatedProgress.Status == "completed" || updatedProgress.Status == "accepted" ||
			updatedProgress.Status == "rejected" || updatedProgress.Status == "expired" || updatedProgress.Status == "stopped"
		hasMoreRounds := updatedProgress.Round < updatedProgress.MaxRounds

		log.Info("round processing result",
			"session_id", sessionID,
			"round_completed", updatedProgress.Round,
			"max_rounds", updatedProgress.MaxRounds,
			"is_done", isDone,
			"has_more_rounds", hasMoreRounds,
			"status", updatedProgress.Status)

		if !isDone && hasMoreRounds {
			if enqueueErr := a2aSvc.EnqueueNegotiationRound(sessionID, negotiationID, updatedProgress.Round); enqueueErr != nil {
				log.Warn("failed to enqueue next round", "error", enqueueErr, "session_id", sessionID, "round", updatedProgress.Round)
			}
		}
	} else {
		log.Error("could not get updated progress after round processing", "session_id", sessionID)
	}

	log.Info("bargaining round completed", "session_id", sessionID, "round", progress.Round)
	return nil
}

func main() {
	lambda.Start(handleSQSEvent)
}
