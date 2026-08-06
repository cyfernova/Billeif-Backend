package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	"invoice-backend/internal/middleware"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

var (
	wsInitOnce sync.Once
	wsCfg      *config.Config
	wsSvc      *services.WebSocketConnectionService
	wsAuthSvc  businessAccessChecker
	wsLog      *logger.Logger
	wsInitErr  error
)

type businessAccessChecker interface {
	UserHasBusinessAccess(ctx context.Context, userID, businessID string) bool
}

func initWSRuntime() {
	cfg, err := config.LoadForProfile(config.ProfileWebSocket)
	if err != nil {
		wsInitErr = fmt.Errorf("load app config for websocket: %w", err)
		return
	}

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
	resolver, err := config.NewRuntimeResolver(config.RuntimeResolverOptions{
		Clients: config.RuntimeResolvers{
			Secrets: secretsmanager.NewFromConfig(awsCfg.SDKConfig),
			SSM:     awsCfg.SSM,
		},
		SecretIdentifiers: []string{cfg.Secrets.Database},
		ParameterNames:    []string{cfg.SSM.DatabaseHostParam},
	})
	if err != nil {
		wsInitErr = fmt.Errorf("initialize websocket credential resolver: %w", err)
		return
	}
	authSvc, err := initWebSocketBusinessAuth(cfg, resolver, log)
	if err != nil {
		wsInitErr = fmt.Errorf("initialize websocket business auth: %w", err)
		return
	}

	svc := services.NewWebSocketConnectionService(cfg, awsCfg, log)
	if svc == nil {
		wsInitErr = fmt.Errorf("initialize websocket connection service")
		return
	}

	wsCfg = cfg
	wsSvc = svc
	wsAuthSvc = authSvc
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

		businessID, response, ok := authorizeWebSocketBusinessScope(ctx, req, claims, connectionID)
		if !ok {
			return response, nil
		}

		if err := wsSvc.RegisterConnectionWithBusiness(ctx, connectionID, claims.Subject, businessID); err != nil {
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

func initWebSocketBusinessAuth(cfg *config.Config, resolver *config.RuntimeResolver, log *logger.Logger) (*services.BusinessAuthService, error) {
	db, err := app.OpenDatabase(cfg, resolver, log)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}

	return services.NewBusinessAuthService(
		db,
		postgresrepo.NewBusinessRepository(db),
		postgresrepo.NewTeamMemberRepository(db),
		log,
	), nil
}

func authorizeWebSocketBusinessScope(ctx context.Context, req events.APIGatewayWebsocketProxyRequest, claims *middleware.CognitoClaims, connectionID string) (string, events.APIGatewayProxyResponse, bool) {
	if claims == nil {
		return "", events.APIGatewayProxyResponse{StatusCode: 401, Body: "invalid auth token"}, false
	}

	requestedBusinessID := firstNonEmpty(req.QueryStringParameters["business_id"], req.Headers["business_id"], req.Headers["x-business-id"], claims.BusinessID)
	if requestedBusinessID == "" {
		wsLog.Warn("websocket connect missing business scope", "connection_id", connectionID, "user_id", claims.Subject)
		return "", events.APIGatewayProxyResponse{StatusCode: 403, Body: "business scope required"}, false
	}
	if claims.BusinessID != "" && requestedBusinessID != claims.BusinessID {
		wsLog.Warn("websocket connect business mismatch", "connection_id", connectionID, "user_id", claims.Subject)
		return "", events.APIGatewayProxyResponse{StatusCode: 403, Body: "business_id does not match authenticated scope"}, false
	}
	if wsAuthSvc == nil {
		wsLog.Error("websocket business auth is not configured", "connection_id", connectionID, "user_id", claims.Subject)
		return "", events.APIGatewayProxyResponse{StatusCode: 500, Body: "business authorization is not configured"}, false
	}
	if !wsAuthSvc.UserHasBusinessAccess(ctx, claims.Subject, requestedBusinessID) {
		wsLog.Warn("websocket connect business access denied", "connection_id", connectionID, "user_id", claims.Subject, "business_id", requestedBusinessID)
		return "", events.APIGatewayProxyResponse{StatusCode: 403, Body: "access denied to this business"}, false
	}
	return requestedBusinessID, events.APIGatewayProxyResponse{}, true
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
	if t, ok := req.QueryStringParameters["authorization"]; ok && t != "" {
		return t
	}
	if t, ok := req.QueryStringParameters["token"]; ok && t != "" {
		return t
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func main() {
	lambda.Start(handleWebSocket)
}
