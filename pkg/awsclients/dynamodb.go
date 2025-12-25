package awsclients

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// DynamoDBClient wraps the AWS DynamoDB client
type DynamoDBClient struct {
	Client *dynamodb.Client
}

// NewDynamoDBClient creates a new DynamoDB client
func NewDynamoDBClient(ctx context.Context, region, accessKey, secretKey, endpoint string) (*DynamoDBClient, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, err
	}

	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" && endpoint != "http://localhost:4566" && endpoint != "http://localstack:4566" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	return &DynamoDBClient{Client: client}, nil
}

// HealthCheck checks if DynamoDB is accessible
func (d *DynamoDBClient) HealthCheck(ctx context.Context) error {
	if d.Client == nil {
		return ErrClientNotInitialized
	}

	// List tables to check connectivity
	_, err := d.Client.ListTables(ctx, &dynamodb.ListTablesInput{
		Limit: aws.Int32(1),
	})
	return err
}

// PutItem puts an item in DynamoDB
func (d *DynamoDBClient) PutItem(ctx context.Context, input *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
	return d.Client.PutItem(ctx, input)
}

// GetItem gets an item from DynamoDB
func (d *DynamoDBClient) GetItem(ctx context.Context, input *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
	return d.Client.GetItem(ctx, input)
}

// DeleteItem deletes an item from DynamoDB
func (d *DynamoDBClient) DeleteItem(ctx context.Context, input *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
	return d.Client.DeleteItem(ctx, input)
}

// Query queries items from DynamoDB
func (d *DynamoDBClient) Query(ctx context.Context, input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
	return d.Client.Query(ctx, input)
}

// UpdateItem updates an item in DynamoDB
func (d *DynamoDBClient) UpdateItem(ctx context.Context, input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
	return d.Client.UpdateItem(ctx, input)
}

// Scan scans items from DynamoDB
func (d *DynamoDBClient) Scan(ctx context.Context, input *dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
	return d.Client.Scan(ctx, input)
}

// CreateTable creates a table in DynamoDB
func (d *DynamoDBClient) CreateTable(ctx context.Context, input *dynamodb.CreateTableInput) (*dynamodb.CreateTableOutput, error) {
	return d.Client.CreateTable(ctx, input)
}

// DescribeTable describes a table in DynamoDB
func (d *DynamoDBClient) DescribeTable(ctx context.Context, tableName string) (*dynamodb.DescribeTableOutput, error) {
	input := &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	}
	return d.Client.DescribeTable(ctx, input)
}
