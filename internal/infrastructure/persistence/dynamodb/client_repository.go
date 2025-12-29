package dynamodb

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/rs/zerolog/log"
	"github.com/skythrill256/invoice-backend/internal/domain/client"
	"github.com/skythrill256/invoice-backend/internal/utils"
)

type ClientRepository struct {
	client    *dynamodb.Client
	tableName string
}

func NewClientRepository(client *dynamodb.Client, tableName string) *ClientRepository {
	return &ClientRepository{
		client:    client,
		tableName: tableName,
	}
}

func (r *ClientRepository) Create(ctx context.Context, c *client.Client) error {
	item, err := attributevalue.MarshalMap(c)
	if err != nil {
		return fmt.Errorf("marshal client: %w", err)
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(r.tableName),
		Item:      item,
	}

	_, err = r.client.PutItem(ctx, input)
	if err != nil {
		return fmt.Errorf("put item: %w", err)
	}

	log.Info().Str("client_id", c.ID).Msg("Client created")
	return nil
}

func (r *ClientRepository) GetByID(ctx context.Context, id string) (*client.Client, *utils.AppError) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	}

	result, err := r.client.GetItem(ctx, input)
	if err != nil {
		return nil, utils.WrapError(err, "get client")
	}

	if result.Item == nil {
		return nil, utils.ErrNotFound
	}

	var c client.Client
	if err := attributevalue.UnmarshalMap(result.Item, &c); err != nil {
		return nil, utils.WrapError(err, "unmarshal client")
	}

	return &c, nil
}

func (r *ClientRepository) GetByEmail(ctx context.Context, email string) (*client.Client, *utils.AppError) {
	input := &dynamodb.QueryInput{
		TableName:              aws.String(r.tableName),
		IndexName:              aws.String("email-index"),
		KeyConditionExpression: aws.String("email = :email"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":email": &types.AttributeValueMemberS{Value: email},
		},
	}

	result, err := r.client.Query(ctx, input)
	if err != nil {
		return nil, utils.WrapError(err, "query client by email")
	}

	if len(result.Items) == 0 {
		return nil, utils.ErrNotFound
	}

	var c client.Client
	if err := attributevalue.UnmarshalMap(result.Items[0], &c); err != nil {
		return nil, utils.WrapError(err, "unmarshal client")
	}

	return &c, nil
}

func (r *ClientRepository) List(ctx context.Context, page, limit int) ([]*client.Client, int, *utils.AppError) {
	input := &dynamodb.ScanInput{
		TableName: aws.String(r.tableName),
		Limit:     aws.Int32(int32(limit)),
	}

	if page > 1 {
		lastEvaluatedKey, err := r.getLastEvaluatedKey(page, limit)
		if err == nil {
			input.ExclusiveStartKey = lastEvaluatedKey
		}
	}

	result, err := r.client.Scan(ctx, input)
	if err != nil {
		return nil, 0, utils.WrapError(err, "scan clients")
	}

	var clients []*client.Client
	for _, item := range result.Items {
		var c client.Client
		if err := attributevalue.UnmarshalMap(item, &c); err != nil {
			log.Error().Err(err).Str("client_id", c.ID).Msg("Failed to unmarshal client")
			continue
		}
		clients = append(clients, &c)
	}

	total := len(clients)
	if result.LastEvaluatedKey != nil {
		total = r.getEstimatedTotal(ctx)
	}

	return clients, total, nil
}

func (r *ClientRepository) Update(ctx context.Context, c *client.Client) *utils.AppError {
	item, err := attributevalue.MarshalMap(c)
	if err != nil {
		return utils.WrapError(err, "marshal client")
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(r.tableName),
		Item:      item,
	}

	_, err = r.client.PutItem(ctx, input)
	if err != nil {
		return utils.WrapError(err, "update client")
	}

	log.Info().Str("client_id", c.ID).Msg("Client updated")
	return nil
}

func (r *ClientRepository) Delete(ctx context.Context, id string) *utils.AppError {
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	}

	_, err := r.client.DeleteItem(ctx, input)
	if err != nil {
		return utils.WrapError(err, "delete client")
	}

	log.Info().Str("client_id", id).Msg("Client deleted")
	return nil
}

func (r *ClientRepository) getLastEvaluatedKey(page, limit int) (map[string]types.AttributeValue, error) {
	data := map[string]interface{}{
		"page":  page,
		"limit": limit,
	}
	return attributevalue.MarshalMap(data)
}

func (r *ClientRepository) getEstimatedTotal(ctx context.Context) int {
	input := &dynamodb.DescribeTableInput{
		TableName: aws.String(r.tableName),
	}

	result, err := r.client.DescribeTable(ctx, input)
	if err != nil {
		return 0
	}

	if result.ItemCount != nil {
		return int(*result.ItemCount)
	}

	return 0
}

func (r *ClientRepository) GetByInvoice(ctx context.Context, invoiceID string) (*client.Client, *utils.AppError) {
	return nil, utils.ErrNotFound
}

func (r *ClientRepository) SaveJSON(ctx context.Context, key string, data interface{}) error {
	item, err := attributevalue.MarshalMap(map[string]interface{}{
		"pk":   key,
		"data": data,
	})
	if err != nil {
		return fmt.Errorf("marshal item: %w", err)
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(r.tableName),
		Item:      item,
	}

	_, err = r.client.PutItem(ctx, input)
	if err != nil {
		return fmt.Errorf("put item: %w", err)
	}

	return nil
}

func (r *ClientRepository) LoadJSON(ctx context.Context, key string, target interface{}) error {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: key},
		},
	}

	result, err := r.client.GetItem(ctx, input)
	if err != nil {
		return fmt.Errorf("get item: %w", err)
	}

	if result.Item == nil {
		return utils.ErrNotFound
	}

	if dataAttr, ok := result.Item["data"]; ok {
		var dataJSON []byte
		if err := attributevalue.Unmarshal(dataAttr, &dataJSON); err != nil {
			return fmt.Errorf("unmarshal data: %w", err)
		}
		if err := json.Unmarshal(dataJSON, target); err != nil {
			return fmt.Errorf("json unmarshal: %w", err)
		}
	}

	return nil
}
