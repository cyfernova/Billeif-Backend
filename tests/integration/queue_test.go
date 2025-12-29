//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const TestSQSQueueName = "invoice-queue"
const TestSNSTopicName = "invoice-notifications"

func TestSQSSendMessage(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	// Get queue URL
	queueURL, err := getTestQueueURL(ctx, env.AWSClients.SQS, TestSQSQueueName)
	if err != nil {
		t.Skipf("Queue %s not found. Run 'make infra-apply' first: %v", TestSQSQueueName, err)
	}

	// Create a test message
	messageBody := map[string]interface{}{
		"type":      "test_message",
		"timestamp": time.Now().Format(time.RFC3339),
		"data": map[string]string{
			"invoice_id": fmt.Sprintf("inv-%d", time.Now().UnixNano()),
			"action":     "created",
		},
	}

	bodyJSON, err := json.Marshal(messageBody)
	require.NoError(t, err)

	// Send message
	sendResp, err := env.AWSClients.SQS.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(bodyJSON)),
	})
	require.NoError(t, err, "Failed to send message to SQS")
	assert.NotEmpty(t, sendResp.MessageId)

	// Receive and delete the message to clean up
	receiveResp, err := env.AWSClients.SQS.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueURL),
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     5,
	})
	require.NoError(t, err)

	if len(receiveResp.Messages) > 0 {
		_, err = env.AWSClients.SQS.DeleteMessage(ctx, &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(queueURL),
			ReceiptHandle: receiveResp.Messages[0].ReceiptHandle,
		})
		require.NoError(t, err)
	}
}

func TestSQSReceiveMessage(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	// Get queue URL
	queueURL, err := getTestQueueURL(ctx, env.AWSClients.SQS, TestSQSQueueName)
	if err != nil {
		t.Skipf("Queue %s not found. Run 'make infra-apply' first: %v", TestSQSQueueName, err)
	}

	// Create unique message
	uniqueID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	messageBody := map[string]string{
		"id":      uniqueID,
		"message": "Test receive message",
	}

	bodyJSON, err := json.Marshal(messageBody)
	require.NoError(t, err)

	// Send message
	_, err = env.AWSClients.SQS.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(bodyJSON)),
	})
	require.NoError(t, err, "Failed to send message")

	// Receive message
	receiveResp, err := env.AWSClients.SQS.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueURL),
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     5,
		VisibilityTimeout:   30,
	})
	require.NoError(t, err, "Failed to receive message")
	require.NotEmpty(t, receiveResp.Messages, "Should receive at least one message")

	// Find our message and verify content
	var foundMessage bool
	for _, msg := range receiveResp.Messages {
		var parsed map[string]string
		if err := json.Unmarshal([]byte(*msg.Body), &parsed); err != nil {
			continue
		}
		if parsed["id"] == uniqueID {
			foundMessage = true
			assert.Equal(t, "Test receive message", parsed["message"])

			// Delete the message
			_, err = env.AWSClients.SQS.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueURL),
				ReceiptHandle: msg.ReceiptHandle,
			})
			require.NoError(t, err)
			break
		}
	}
	assert.True(t, foundMessage, "Should find the message we sent")
}

func TestSQSMessageAttributes(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	// Get queue URL
	queueURL, err := getTestQueueURL(ctx, env.AWSClients.SQS, TestSQSQueueName)
	if err != nil {
		t.Skipf("Queue %s not found. Run 'make infra-apply' first: %v", TestSQSQueueName, err)
	}

	// Send message with attributes
	_, err = env.AWSClients.SQS.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(`{"test": "attributes"}`),
		MessageAttributes: map[string]sqs.MessageAttributeValue{
			"Type": {
				DataType:    aws.String("String"),
				StringValue: aws.String("invoice.created"),
			},
			"Priority": {
				DataType:    aws.String("Number"),
				StringValue: aws.String("1"),
			},
		},
	})
	require.NoError(t, err, "Failed to send message with attributes")

	// Receive and verify attributes
	receiveResp, err := env.AWSClients.SQS.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:              aws.String(queueURL),
		MaxNumberOfMessages:   1,
		WaitTimeSeconds:       5,
		MessageAttributeNames: []string{"All"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, receiveResp.Messages)

	msg := receiveResp.Messages[0]
	if typeAttr, ok := msg.MessageAttributes["Type"]; ok {
		assert.Equal(t, "invoice.created", *typeAttr.StringValue)
	}

	// Cleanup
	_, _ = env.AWSClients.SQS.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueURL),
		ReceiptHandle: msg.ReceiptHandle,
	})
}

func TestSNSPublish(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	// Get topic ARN
	topicARN, err := getTestTopicARN(ctx, env.AWSClients.SNS, TestSNSTopicName)
	if err != nil {
		t.Skipf("Topic %s not found. Run 'make infra-apply' first: %v", TestSNSTopicName, err)
	}

	// Publish message
	message := map[string]interface{}{
		"event":     "invoice.created",
		"timestamp": time.Now().Format(time.RFC3339),
		"payload": map[string]string{
			"invoice_id": fmt.Sprintf("inv-%d", time.Now().UnixNano()),
		},
	}

	messageJSON, err := json.Marshal(message)
	require.NoError(t, err)

	publishResp, err := env.AWSClients.SNS.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(topicARN),
		Message:  aws.String(string(messageJSON)),
		Subject:  aws.String("Invoice Created"),
	})
	require.NoError(t, err, "Failed to publish to SNS")
	assert.NotEmpty(t, publishResp.MessageId)
}

func TestSNSPublishWithAttributes(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	// Get topic ARN
	topicARN, err := getTestTopicARN(ctx, env.AWSClients.SNS, TestSNSTopicName)
	if err != nil {
		t.Skipf("Topic %s not found. Run 'make infra-apply' first: %v", TestSNSTopicName, err)
	}

	// Publish with message attributes
	publishResp, err := env.AWSClients.SNS.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(topicARN),
		Message:  aws.String(`{"event": "payment.received"}`),
		MessageAttributes: map[string]sns.MessageAttributeValue{
			"event_type": {
				DataType:    aws.String("String"),
				StringValue: aws.String("payment"),
			},
		},
	})
	require.NoError(t, err, "Failed to publish with attributes")
	assert.NotEmpty(t, publishResp.MessageId)
}

// Helper functions

func getTestQueueURL(ctx context.Context, client *sqs.Client, queueName string) (string, error) {
	resp, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		return "", err
	}
	return *resp.QueueUrl, nil
}

func getTestTopicARN(ctx context.Context, client *sns.Client, topicName string) (string, error) {
	resp, err := client.ListTopics(ctx, &sns.ListTopicsInput{})
	if err != nil {
		return "", err
	}

	for _, topic := range resp.Topics {
		// Topic ARN format: arn:aws:sns:region:account:topic-name
		if topic.TopicArn != nil && len(*topic.TopicArn) > len(topicName) {
			// Check if the ARN ends with the topic name
			arn := *topic.TopicArn
			if len(arn) > len(topicName) && arn[len(arn)-len(topicName):] == topicName {
				return arn, nil
			}
		}
	}
	return "", fmt.Errorf("topic %s not found", topicName)
}
