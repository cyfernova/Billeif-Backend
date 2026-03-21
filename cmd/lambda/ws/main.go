package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

var (
	wsInitOnce sync.Once
	wsCfg      *config.Config
	wsSvc      *services.WebSocketConnectionService
	wsLog      *logger.Logger
	wsInitErr  error
)

func initWSRuntime() {
	cfg := loadWSConfig()

	log := logger.NewWithConfig(logger.Config{
		Environment: cfg.Environment,
		Level:       cfg.Logging.Level,
		Format:      cfg.Logging.Format,
	}).Named("ws_lambda")

	awsCfg, err := awsclients.New(context.Background(), cfg.AWS, log)
	if err != nil {
		wsInitErr = fmt.Errorf("init aws clients: %w", err)
		return
	}

	svc := services.NewWebSocketConnectionService(cfg, awsCfg, log)
	if svc == nil {
		wsInitErr = fmt.Errorf("initialize websocket connection service")
		return
	}

	wsCfg = cfg
	wsSvc = svc
	wsLog = log
}

func handleWebSocket(ctx context.Context, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	wsInitOnce.Do(initWSRuntime)
	if wsInitErr != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: wsInitErr.Error()}, nil
	}

	connectionID := req.RequestContext.ConnectionID
	switch req.RequestContext.RouteKey {
	case "$connect":
		authToken := extractAuthToken(req)
		if authToken == "" {
			return events.APIGatewayProxyResponse{StatusCode: 401, Body: "missing auth token"}, nil
		}

		claims, err := parseClaims(authToken)
		if err != nil {
			wsLog.Warn("websocket connect auth failed", "error", err)
			return events.APIGatewayProxyResponse{StatusCode: 401, Body: "invalid auth token"}, nil
		}

		if err := wsSvc.RegisterConnection(ctx, connectionID, claims.Subject); err != nil {
			wsLog.Error("failed to register websocket connection", "connection_id", connectionID, "error", err)
			return events.APIGatewayProxyResponse{StatusCode: 500, Body: "failed to register connection"}, nil
		}

		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "connected"}, nil

	case "$disconnect":
		if err := wsSvc.UnregisterConnection(ctx, connectionID); err != nil {
			wsLog.Warn("failed to unregister websocket connection", "connection_id", connectionID, "error", err)
		}
		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "disconnected"}, nil

	case "$default":
		// Incoming messages can be routed here for custom handling if needed.
		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "ok"}, nil

	default:
		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "ok"}, nil
	}
}

func parseClaims(token string) (*middleware.CognitoClaims, error) {
	if strings.Contains(strings.ToLower(token), "bearer ") {
		return middleware.ValidateCognitoAuthorization(wsCfg.Cognito, token)
	}
	return middleware.ValidateCognitoToken(wsCfg.Cognito, token)
}

func extractAuthToken(req events.APIGatewayWebsocketProxyRequest) string {
	if h, ok := req.Headers["Authorization"]; ok && h != "" {
		return h
	}
	if h, ok := req.Headers["authorization"]; ok && h != "" {
		return h
	}
	if t, ok := req.QueryStringParameters["access_token"]; ok && t != "" {
		return t
	}
	if t, ok := req.QueryStringParameters["token"]; ok && t != "" {
		return t
	}
	return ""
}

func loadWSConfig() *config.Config {
	c := &config.Config{
		Environment: getEnvOrDefault("ENVIRONMENT", "dev"),
		Logging: config.LoggingConfig{
			Level:  getEnvOrDefault("LOG_LEVEL", "info"),
			Format: getEnvOrDefault("LOG_FORMAT", "json"),
		},
		AWS: config.AWSConfig{
			Region:    getEnvOrDefault("AWS_REGION", "us-east-1"),
			AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			Endpoint:  os.Getenv("AWS_ENDPOINT"),
		},
		Cognito: config.CognitoConfig{
			UserPoolID:      os.Getenv("COGNITO_USER_POOL_ID"),
			ClientID:        os.Getenv("COGNITO_CLIENT_ID"),
			Region:          getEnvOrDefault("COGNITO_REGION", getEnvOrDefault("AWS_REGION", "us-east-1")),
			JWKSRefreshRate: 10 * time.Minute,
			Phone: config.CognitoPhoneConfig{
				UserPoolID: getEnvOrDefault("COGNITO_PHONE_USER_POOL_ID", ""),
				ClientID:   getEnvOrDefault("COGNITO_PHONE_CLIENT_ID", ""),
				Region:     getEnvOrDefault("COGNITO_PHONE_REGION", ""),
			},
		},
		WebSocket: config.WebSocketConfig{
			APIEndpoint:      os.Getenv("WEBSOCKET_API_ENDPOINT"),
			ConnectionsTable: getEnvOrDefault("WEBSOCKET_CONNECTIONS_TABLE", "invoice-backend-ws-connections"),
		},
	}

	return c
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	lambda.Start(handleWebSocket)
}
