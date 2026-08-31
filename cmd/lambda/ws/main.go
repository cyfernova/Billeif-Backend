package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

var (
	wsInitOnce  sync.Once
	wsSvc       websocketConnectionRegistry
	wsTicketSvc websocketTicketConsumer
	wsLog       *logger.Logger
	wsInitErr   error
)

type websocketTicketConsumer interface {
	Consume(ctx context.Context, ticketValue string) (*models.WebSocketTicket, error)
}

type websocketConnectionRegistry interface {
	RegisterConnectionWithBusiness(ctx context.Context, connectionID, userID, businessID string) error
	UnregisterConnection(ctx context.Context, connectionID string) error
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
	ticketSvc, err := initWebSocketTicketService(cfg, resolver, log)
	if err != nil {
		wsInitErr = fmt.Errorf("initialize websocket ticket service: %w", err)
		return
	}

	svc := services.NewWebSocketConnectionService(cfg, awsCfg, log)
	if svc == nil {
		wsInitErr = fmt.Errorf("initialize websocket connection service")
		return
	}

	wsSvc = svc
	wsTicketSvc = ticketSvc
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
		return handleWebSocketConnect(ctx, req, wsTicketSvc, wsSvc, wsLog), nil

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

func handleWebSocketConnect(
	ctx context.Context,
	req events.APIGatewayWebsocketProxyRequest,
	tickets websocketTicketConsumer,
	connections websocketConnectionRegistry,
	log *logger.Logger,
) events.APIGatewayProxyResponse {
	connectionID := req.RequestContext.ConnectionID
	ticketValue := extractWebSocketTicket(req)
	if ticketValue == "" {
		return events.APIGatewayProxyResponse{StatusCode: 401, Body: "missing websocket ticket"}
	}
	if tickets == nil || connections == nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "websocket services are unavailable"}
	}
	ticket, err := tickets.Consume(ctx, ticketValue)
	if err != nil {
		if errors.Is(err, interfaces.ErrWebSocketTicketInvalid) {
			log.Warn("websocket connect ticket rejected", "connection_id", connectionID)
			return events.APIGatewayProxyResponse{StatusCode: 401, Body: "invalid websocket ticket"}
		}
		log.Error("websocket connect ticket consume failed", "connection_id", connectionID, "error", err)
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "failed to validate websocket ticket"}
	}

	if err := connections.RegisterConnectionWithBusiness(ctx, connectionID, ticket.Subject, ticket.BusinessID); err != nil {
		log.Error("failed to register websocket connection", "connection_id", connectionID, "error", err)
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "failed to register connection"}
	}
	return events.APIGatewayProxyResponse{StatusCode: 200, Body: "connected"}
}

func initWebSocketTicketService(cfg *config.Config, resolver *config.RuntimeResolver, log *logger.Logger) (*services.WebSocketTicketService, error) {
	db, err := app.OpenDatabase(cfg, resolver, log)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	return services.NewWebSocketTicketService(
		postgresrepo.NewWebSocketTicketRepository(db),
		services.WebSocketTicketServiceOptions{},
	), nil
}

func extractWebSocketTicket(req events.APIGatewayWebsocketProxyRequest) string {
	return strings.TrimSpace(req.QueryStringParameters["ticket"])
}

func main() {
	lambda.Start(handleWebSocket)
}
