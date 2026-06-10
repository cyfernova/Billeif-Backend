package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/lambda"
)

var (
	initOnce sync.Once
	runner   *services.VoiceLambdaSessionRunner
	initErr  error
)

func initRuntime() {
	cfg, err := config.LoadLambdaVoiceConfig(false)
	if err != nil {
		initErr = fmt.Errorf("load lambda voice config: %w", err)
		return
	}

	log := logger.NewWithConfig(logger.Config{
		Environment: cfg.Environment,
		Level:       cfg.LogLevel,
		Format:      cfg.LogFormat,
	}).Named("voice_session_lambda")

	awsCfg, err := awsclients.New(context.Background(), cfg.AWS, log)
	if err != nil {
		initErr = fmt.Errorf("init aws clients: %w", err)
		return
	}

	store, err := services.NewVoiceLambdaStore(awsCfg.DynamoDB, cfg.VoiceSessionsTable)
	if err != nil {
		initErr = fmt.Errorf("initialize voice session store: %w", err)
		return
	}
	poster, err := services.NewAPIGatewayVoicePoster(awsCfg.SDKConfig, cfg.WebSocket.APIEndpoint, cfg.MaxOutboundChunkBytes)
	if err != nil {
		initErr = fmt.Errorf("initialize voice websocket poster: %w", err)
		return
	}
	mcpBridge, err := services.NewVoiceMCPBridgeFromConfig(cfg.MCP, log)
	if err != nil {
		initErr = fmt.Errorf("initialize voice MCP bridge: %w", err)
		return
	}
	runner, err = services.NewVoiceLambdaSessionRunner(
		cfg.VoiceRealtime,
		store,
		poster,
		time.Duration(cfg.EventPollIntervalMS)*time.Millisecond,
		time.Duration(cfg.ProviderReadyTimeoutSeconds)*time.Second,
		nil,
		log,
		mcpBridge,
	)
	if err != nil {
		initErr = fmt.Errorf("initialize voice session runner: %w", err)
		return
	}
}

func handle(ctx context.Context, raw json.RawMessage) error {
	initOnce.Do(initRuntime)
	if initErr != nil {
		return initErr
	}

	var req services.VoiceSessionWorkerRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("voice session request must be valid JSON: %w", err)
	}
	if req.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	return runner.Run(ctx, req.SessionID, req.AccessToken)
}

func main() {
	lambda.Start(handle)
}
