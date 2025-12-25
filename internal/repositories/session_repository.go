package repositories

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/pkg/awsclients"
)

// SessionRepository defines the interface for session data operations
type SessionRepository interface {
	Create(ctx context.Context, session *models.Session) error
	GetByID(ctx context.Context, sessionID string) (*models.Session, error)
	GetByUserID(ctx context.Context, userID string) ([]*models.Session, error)
	Delete(ctx context.Context, sessionID string) error
	DeleteByUserID(ctx context.Context, userID string) error
}

type sessionRepository struct {
	client    *awsclients.DynamoDBClient
	tableName string
}

// NewSessionRepository creates a new session repository
func NewSessionRepository(client *awsclients.DynamoDBClient, tableName string) SessionRepository {
	return &sessionRepository{
		client:    client,
		tableName: tableName,
	}
}

func (r *sessionRepository) Create(ctx context.Context, session *models.Session) error {
	item, err := attributevalue.MarshalMap(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(r.tableName),
		Item:      item,
	}

	_, err = r.client.PutItem(ctx, input)
	return err
}

func (r *sessionRepository) GetByID(ctx context.Context, sessionID string) (*models.Session, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"session_id": &types.AttributeValueMemberS{Value: sessionID},
		},
	}

	output, err := r.client.GetItem(ctx, input)
	if err != nil {
		return nil, err
	}

	if output.Item == nil {
		return nil, nil
	}

	var session models.Session
	err = attributevalue.UnmarshalMap(output.Item, &session)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	return &session, nil
}

func (r *sessionRepository) GetByUserID(ctx context.Context, userID string) ([]*models.Session, error) {
	input := &dynamodb.QueryInput{
		TableName: aws.String(r.tableName),
		IndexName: aws.String("user-sessions-index"),
		KeyConditionExpression: aws.String("user_id = :user_id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":user_id": &types.AttributeValueMemberS{Value: userID},
		},
	}

	output, err := r.client.Query(ctx, input)
	if err != nil {
		return nil, err
	}

	var sessions []*models.Session
	err = attributevalue.UnmarshalListOfMaps(output.Items, &sessions)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal sessions: %w", err)
	}

	return sessions, nil
}

func (r *sessionRepository) Delete(ctx context.Context, sessionID string) error {
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"session_id": &types.AttributeValueMemberS{Value: sessionID},
		},
	}

	_, err := r.client.DeleteItem(ctx, input)
	return err
}

func (r *sessionRepository) DeleteByUserID(ctx context.Context, userID string) error {
	// Get all sessions for the user
	sessions, err := r.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}

	// Delete each session
	for _, session := range sessions {
		if err := r.Delete(ctx, session.SessionID); err != nil {
			return err
		}
	}

	return nil
}
