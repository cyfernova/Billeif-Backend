//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const TestDynamoDBTable = "invoice-audit-log"

func TestDynamoDBPutItem(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	itemID := fmt.Sprintf("test-item-%d", time.Now().UnixNano())

	// Cleanup after test
	defer func() {
		env.AWSClients.DynamoDB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: aws.String(TestDynamoDBTable),
			Key: map[string]types.AttributeValue{
				"id": &types.AttributeValueMemberS{Value: itemID},
			},
		})
	}()

	// Put item
	_, err := env.AWSClients.DynamoDB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TestDynamoDBTable),
		Item: map[string]types.AttributeValue{
			"id":        &types.AttributeValueMemberS{Value: itemID},
			"action":    &types.AttributeValueMemberS{Value: "test_action"},
			"timestamp": &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
			"user_id":   &types.AttributeValueMemberS{Value: "test-user"},
			"metadata":  &types.AttributeValueMemberS{Value: `{"test": true}`},
		},
	})
	require.NoError(t, err, "Failed to put item in DynamoDB")

	// Verify item exists
	getResp, err := env.AWSClients.DynamoDB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(TestDynamoDBTable),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: itemID},
		},
	})
	require.NoError(t, err, "Failed to get item from DynamoDB")
	require.NotNil(t, getResp.Item, "Item should exist")

	// Verify item attributes
	actionAttr, ok := getResp.Item["action"].(*types.AttributeValueMemberS)
	require.True(t, ok, "action should be a string attribute")
	assert.Equal(t, "test_action", actionAttr.Value)
}

func TestDynamoDBGetItem(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	itemID := fmt.Sprintf("test-get-%d", time.Now().UnixNano())
	expectedAction := "get_test_action"

	// Cleanup after test
	defer func() {
		env.AWSClients.DynamoDB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: aws.String(TestDynamoDBTable),
			Key: map[string]types.AttributeValue{
				"id": &types.AttributeValueMemberS{Value: itemID},
			},
		})
	}()

	// Put item first
	_, err := env.AWSClients.DynamoDB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TestDynamoDBTable),
		Item: map[string]types.AttributeValue{
			"id":        &types.AttributeValueMemberS{Value: itemID},
			"action":    &types.AttributeValueMemberS{Value: expectedAction},
			"timestamp": &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
		},
	})
	require.NoError(t, err, "Failed to put item")

	// Get item
	getResp, err := env.AWSClients.DynamoDB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(TestDynamoDBTable),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: itemID},
		},
	})
	require.NoError(t, err, "Failed to get item")
	require.NotNil(t, getResp.Item, "Item should exist")

	actionAttr := getResp.Item["action"].(*types.AttributeValueMemberS)
	assert.Equal(t, expectedAction, actionAttr.Value)
}

func TestDynamoDBDeleteItem(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	itemID := fmt.Sprintf("test-delete-%d", time.Now().UnixNano())

	// Put item first
	_, err := env.AWSClients.DynamoDB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TestDynamoDBTable),
		Item: map[string]types.AttributeValue{
			"id":        &types.AttributeValueMemberS{Value: itemID},
			"action":    &types.AttributeValueMemberS{Value: "to_be_deleted"},
			"timestamp": &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
		},
	})
	require.NoError(t, err, "Failed to put item")

	// Delete item
	_, err = env.AWSClients.DynamoDB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(TestDynamoDBTable),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: itemID},
		},
	})
	require.NoError(t, err, "Failed to delete item")

	// Verify item no longer exists
	getResp, err := env.AWSClients.DynamoDB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(TestDynamoDBTable),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: itemID},
		},
	})
	require.NoError(t, err, "GetItem should not error for missing item")
	assert.Nil(t, getResp.Item, "Item should not exist after deletion")
}

func TestDynamoDBScan(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	// Create multiple items
	prefix := fmt.Sprintf("scan-test-%d", time.Now().UnixNano())
	var itemIDs []string

	for i := 0; i < 3; i++ {
		itemID := fmt.Sprintf("%s-%d", prefix, i)
		itemIDs = append(itemIDs, itemID)

		_, err := env.AWSClients.DynamoDB.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(TestDynamoDBTable),
			Item: map[string]types.AttributeValue{
				"id":        &types.AttributeValueMemberS{Value: itemID},
				"action":    &types.AttributeValueMemberS{Value: "scan_action"},
				"timestamp": &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
			},
		})
		require.NoError(t, err, "Failed to put item %d", i)
	}

	// Cleanup after test
	defer func() {
		for _, itemID := range itemIDs {
			env.AWSClients.DynamoDB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
				TableName: aws.String(TestDynamoDBTable),
				Key: map[string]types.AttributeValue{
					"id": &types.AttributeValueMemberS{Value: itemID},
				},
			})
		}
	}()

	// Scan table
	scanResp, err := env.AWSClients.DynamoDB.Scan(ctx, &dynamodb.ScanInput{
		TableName:        aws.String(TestDynamoDBTable),
		FilterExpression: aws.String("begins_with(id, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":prefix": &types.AttributeValueMemberS{Value: prefix},
		},
	})
	require.NoError(t, err, "Failed to scan table")
	assert.GreaterOrEqual(t, len(scanResp.Items), 3, "Should find at least 3 items")
}

// TestDynamoDBTableExists verifies the test table is available in LocalStack
func TestDynamoDBTableExists(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := context.Background()

	// List tables to verify our test table exists
	resp, err := env.AWSClients.DynamoDB.ListTables(ctx, &dynamodb.ListTablesInput{})
	if err != nil {
		t.Skipf("Could not list DynamoDB tables: %v", err)
	}

	found := false
	for _, table := range resp.TableNames {
		if table == TestDynamoDBTable {
			found = true
			break
		}
	}

	if !found {
		t.Skipf("Test table %s not found in LocalStack. Run 'make infra-apply' first.", TestDynamoDBTable)
	}
}
