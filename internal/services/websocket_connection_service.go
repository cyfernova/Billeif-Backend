package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/websocket"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	defaultWSConnectionsTable      = "invoice-backend-ws-connections"
	maxWebSocketConnectionLifetime = 2 * time.Hour
)

type WebSocketConnection struct {
	ConnectionID string    `dynamodbav:"connection_id"`
	UserID       string    `dynamodbav:"user_id"`
	BusinessID   string    `dynamodbav:"business_id,omitempty"`
	ConnectedAt  time.Time `dynamodbav:"connected_at"`
	LastSeenAt   time.Time `dynamodbav:"last_seen_at"`
	TTL          int64     `dynamodbav:"ttl"`
}

type WebSocketConnectionService struct {
	db     *dynamodb.Client
	mgmt   *apigatewaymanagementapi.Client
	table  string
	logger *logger.Logger
}

func NewWebSocketConnectionService(cfg *config.Config, awsCfg *awsclients.Config, log *logger.Logger) *WebSocketConnectionService {
	if cfg == nil || awsCfg == nil || awsCfg.DynamoDB == nil || log == nil {
		return nil
	}
	if cfg.WebSocket.APIEndpoint == "" {
		return nil
	}

	table := cfg.WebSocket.ConnectionsTable
	if table == "" {
		table = defaultWSConnectionsTable
	}

	mgmt := apigatewaymanagementapi.NewFromConfig(awsCfg.SDKConfig, func(o *apigatewaymanagementapi.Options) {
		o.BaseEndpoint = aws.String(cfg.WebSocket.APIEndpoint)
	})

	return &WebSocketConnectionService{
		db:     awsCfg.DynamoDB,
		mgmt:   mgmt,
		table:  table,
		logger: log.Named("ws_connections"),
	}
}

func (s *WebSocketConnectionService) RegisterConnection(ctx context.Context, connectionID, userID string) error {
	return s.RegisterConnectionWithBusiness(ctx, connectionID, userID, "")
}

func (s *WebSocketConnectionService) RegisterConnectionWithBusiness(ctx context.Context, connectionID, userID, businessID string) error {
	rec, err := newWebSocketConnection(connectionID, userID, businessID, time.Now().UTC())
	if err != nil {
		return err
	}

	item, err := attributevalue.MarshalMap(rec)
	if err != nil {
		return fmt.Errorf("marshal websocket connection: %w", err)
	}

	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("put websocket connection: %w", err)
	}
	return nil
}

func newWebSocketConnection(connectionID, userID, businessID string, now time.Time) (*WebSocketConnection, error) {
	connectionID = strings.TrimSpace(connectionID)
	userID = strings.TrimSpace(userID)
	businessID = strings.TrimSpace(businessID)
	if connectionID == "" || userID == "" || businessID == "" {
		return nil, fmt.Errorf("connection ID, user ID, and business ID are required")
	}
	now = now.UTC()
	return &WebSocketConnection{
		ConnectionID: connectionID,
		UserID:       userID,
		BusinessID:   businessID,
		ConnectedAt:  now,
		LastSeenAt:   now,
		TTL:          now.Add(maxWebSocketConnectionLifetime).Unix(),
	}, nil
}

func (s *WebSocketConnectionService) GetConnection(ctx context.Context, connectionID string) (*WebSocketConnection, error) {
	if connectionID == "" {
		return nil, nil
	}

	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key: map[string]types.AttributeValue{
			"connection_id": &types.AttributeValueMemberS{Value: connectionID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get websocket connection: %w", err)
	}
	if len(out.Item) == 0 {
		return nil, nil
	}

	var rec WebSocketConnection
	if err := attributevalue.UnmarshalMap(out.Item, &rec); err != nil {
		return nil, fmt.Errorf("unmarshal websocket connection: %w", err)
	}
	return &rec, nil
}

func (s *WebSocketConnectionService) UnregisterConnection(ctx context.Context, connectionID string) error {
	if connectionID == "" {
		return nil
	}

	_, err := s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.table),
		Key: map[string]types.AttributeValue{
			"connection_id": &types.AttributeValueMemberS{Value: connectionID},
		},
	})
	if err != nil {
		return fmt.Errorf("delete websocket connection: %w", err)
	}
	return nil
}

func (s *WebSocketConnectionService) GetConnectionIDsByUser(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}

	out, err := s.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		IndexName:              aws.String("user_id-index"),
		KeyConditionExpression: aws.String("user_id = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid": &types.AttributeValueMemberS{Value: userID},
		},
		ProjectionExpression: aws.String("connection_id"),
	})
	if err != nil {
		return nil, fmt.Errorf("query websocket connections by user: %w", err)
	}

	ids := make([]string, 0, len(out.Items))
	for _, item := range out.Items {
		if connAttr, ok := item["connection_id"].(*types.AttributeValueMemberS); ok {
			ids = append(ids, connAttr.Value)
		}
	}
	return ids, nil
}

func (s *WebSocketConnectionService) GetConnectedUsers(ctx context.Context) ([]string, error) {
	paginator := dynamodb.NewScanPaginator(s.db, &dynamodb.ScanInput{
		TableName:            aws.String(s.table),
		ProjectionExpression: aws.String("user_id"),
	})

	seen := map[string]struct{}{}
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("scan websocket users: %w", err)
		}
		for _, item := range page.Items {
			if uid, ok := item["user_id"].(*types.AttributeValueMemberS); ok {
				seen[uid.Value] = struct{}{}
			}
		}
	}

	users := make([]string, 0, len(seen))
	for userID := range seen {
		users = append(users, userID)
	}
	return users, nil
}

func (s *WebSocketConnectionService) GetClientCount(ctx context.Context) (int, error) {
	count := 0
	paginator := dynamodb.NewScanPaginator(s.db, &dynamodb.ScanInput{
		TableName:            aws.String(s.table),
		ProjectionExpression: aws.String("connection_id"),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return 0, fmt.Errorf("scan websocket connection count: %w", err)
		}
		count += len(page.Items)
	}
	return count, nil
}

func (s *WebSocketConnectionService) IsUserConnected(ctx context.Context, userID string) (bool, error) {
	ids, err := s.GetConnectionIDsByUser(ctx, userID)
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

func (s *WebSocketConnectionService) SendMessageToUser(ctx context.Context, userID string, msg *websocket.Message) error {
	if msg == nil {
		return fmt.Errorf("message is nil")
	}
	ids, err := s.GetConnectionIDsByUser(ctx, userID)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal websocket message: %w", err)
	}

	for _, id := range ids {
		if err := s.postToConnection(ctx, id, payload); err != nil {
			s.logger.Warn("failed to deliver websocket message", "connection_id", id, "error", err)
		}
	}

	return nil
}

func (s *WebSocketConnectionService) BroadcastToAll(ctx context.Context, msg *websocket.Message) error {
	if msg == nil {
		return fmt.Errorf("message is nil")
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal websocket broadcast message: %w", err)
	}

	paginator := dynamodb.NewScanPaginator(s.db, &dynamodb.ScanInput{
		TableName:            aws.String(s.table),
		ProjectionExpression: aws.String("connection_id"),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("scan websocket connections for broadcast: %w", err)
		}
		for _, item := range page.Items {
			connAttr, ok := item["connection_id"].(*types.AttributeValueMemberS)
			if !ok {
				continue
			}
			if err := s.postToConnection(ctx, connAttr.Value, payload); err != nil {
				s.logger.Warn("failed to broadcast websocket message", "connection_id", connAttr.Value, "error", err)
			}
		}
	}

	return nil
}

func (s *WebSocketConnectionService) postToConnection(ctx context.Context, connectionID string, payload []byte) error {
	if s.mgmt == nil {
		return fmt.Errorf("websocket management endpoint is not configured")
	}
	_, err := s.mgmt.PostToConnection(ctx, &apigatewaymanagementapi.PostToConnectionInput{
		ConnectionId: aws.String(connectionID),
		Data:         payload,
	})
	if err != nil {
		if strings.Contains(err.Error(), "GoneException") {
			_ = s.UnregisterConnection(ctx, connectionID)
		}
		return fmt.Errorf("post to connection: %w", err)
	}
	return nil
}
