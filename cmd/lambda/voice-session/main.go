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
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

var (
	initOnce sync.Once
	runtime  *voiceRuntime
	initErr  error
)

type voiceRuntime struct {
	mu      sync.Mutex
	cfg     *config.LambdaVoiceConfig
	secrets *config.SecretResolver
	store   *services.VoiceLambdaStore
	poster  services.VoicePoster
	log     *logger.Logger
	mcp     *services.VoiceMCPBridge
}

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
	resolver, err := config.NewSecretResolver(
		secretsmanager.NewFromConfig(awsCfg.SDKConfig),
		[]string{cfg.DeepgramSecretIdentifier, cfg.DeepSeekSecretIdentifier},
		5*time.Minute,
		time.Now,
	)
	if err != nil {
		initErr = fmt.Errorf("initialize voice credential resolver: %w", err)
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
	runtime = &voiceRuntime{
		cfg: cfg, secrets: resolver, store: store, poster: poster, log: log, mcp: mcpBridge,
	}
}

func (r *voiceRuntime) runner(ctx context.Context) (*services.VoiceLambdaSessionRunner, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	deepgramKey, err := r.secrets.JSONField(ctx, r.cfg.DeepgramSecretIdentifier, "api_key")
	if err != nil {
		return nil, err
	}
	deepSeekKey, err := r.secrets.JSONField(ctx, r.cfg.DeepSeekSecretIdentifier, "api_key")
	if err != nil {
		return nil, err
	}
	r.cfg.VoiceRealtime.DeepgramAPIKey = deepgramKey
	r.cfg.VoiceRealtime.DeepSeekAPIKey = deepSeekKey
	return services.NewVoiceLambdaSessionRunner(
		r.cfg.VoiceRealtime,
		r.store,
		r.poster,
		time.Duration(r.cfg.EventPollIntervalMS)*time.Millisecond,
		time.Duration(r.cfg.ProviderReadyTimeoutSeconds)*time.Second,
		nil,
		r.log,
		r.mcp,
	)
}

func handle(ctx context.Context, raw json.RawMessage) error {
	initOnce.Do(initRuntime)
	if initErr != nil {
		return initErr
	}
	activeRunner, err := runtime.runner(ctx)
	if err != nil {
		return fmt.Errorf("refresh voice provider credentials: %w", err)
	}

	var req services.VoiceSessionWorkerRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("voice session request must be valid JSON: %w", err)
	}
	if req.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	return activeRunner.Run(ctx, req.SessionID, req.AccessToken)
}

func main() {
	lambda.Start(handle)
}
