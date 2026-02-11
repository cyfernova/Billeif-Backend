package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"
)

// A2AClient handles agent-to-agent communication
type A2AClient struct {
	httpClient *http.Client
	signer     *ap2.SignatureService
	validator  *MessageValidator
	log        *logger.Logger
}

// NewA2AClient creates a new A2A client
func NewA2AClient(signer *ap2.SignatureService, log *logger.Logger) *A2AClient {
	return &A2AClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		signer:    signer,
		validator: NewMessageValidator(signer),
		log:       log,
	}
}

// SendMessage sends a message to another agent
func (c *A2AClient) SendMessage(ctx context.Context, receiverEndpoint string, msg *A2AMessage) (*A2AMessage, error) {
	// Validate message
	validationResult := c.validator.ValidateMessage(msg)
	if !validationResult.IsValid {
		return nil, fmt.Errorf("message validation failed: %v", validationResult.Errors)
	}

	// Sign the message
	if err := c.validator.SignMessage(msg, c.signer); err != nil {
		return nil, fmt.Errorf("failed to sign message: %w", err)
	}

	// Serialize message
	messageJSON, err := msg.ToJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize message: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", receiverEndpoint, bytes.NewReader(messageJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-A2A-Message-ID", msg.MessageID)
	req.Header.Set("X-A2A-Task-ID", msg.TaskID)
	req.Header.Set("X-A2A-Sender-ID", msg.SenderID)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Error("failed to send A2A message", "error", err, "receiver", receiverEndpoint, "message_id", msg.MessageID)
		return nil, fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("receiver returned error status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response message
	responseMsg := &A2AMessage{}
	if err := json.Unmarshal(respBody, responseMsg); err != nil {
		return nil, fmt.Errorf("failed to parse response message: %w", err)
	}

	// Validate response message
	respValidation := c.validator.ValidateMessage(responseMsg)
	if !respValidation.IsValid {
		c.log.Warn("response message validation has errors", "errors", respValidation.Errors, "message_id", responseMsg.MessageID)
	}

	c.log.Info("A2A message sent successfully", "message_id", msg.MessageID, "response_id", responseMsg.MessageID)

	return responseMsg, nil
}

// SendMessageWithRetry sends a message with automatic retry logic
func (c *A2AClient) SendMessageWithRetry(ctx context.Context, receiverEndpoint string, msg *A2AMessage, maxRetries int) (*A2AMessage, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if msg.ShouldRetry(maxRetries) {
			if attempt > 0 {
				c.log.Info("retrying A2A message", "message_id", msg.MessageID, "attempt", attempt+1)
			}
		}

		// Try to send message
		response, err := c.SendMessage(ctx, receiverEndpoint, msg)
		if err == nil {
			return response, nil
		}

		lastErr = err
		msg.IncrementRetry()

		// Don't sleep after last attempt
		if attempt < maxRetries {
			// Exponential backoff: 1s, 2s, 4s, 8s...
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			select {
			case <-time.After(backoff):
				// Continue to next attempt
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled: %w", ctx.Err())
			}
		}
	}

	c.log.Error("failed to send A2A message after retries", "error", lastErr, "message_id", msg.MessageID, "max_retries", maxRetries)
	return nil, fmt.Errorf("failed to send message after %d retries: %w", maxRetries, lastErr)
}

// StartTask sends a task.start message to begin a task
func (c *A2AClient) StartTask(ctx context.Context, receiverEndpoint, senderID, receiverID string, taskPayload *TaskStartPayload) (*A2AMessage, error) {
	msg := NewA2AMessage(senderID, receiverID, MessageTypeTaskStart)
	msg.ReceiverEndpoint = receiverEndpoint

	_, err := msg.WithPayload(taskPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return c.SendMessage(ctx, receiverEndpoint, msg)
}

// SendTaskData sends task data to the receiving agent
func (c *A2AClient) SendTaskData(ctx context.Context, receiverEndpoint, taskID string, senderID, receiverID string, dataPayload *TaskDataPayload) (*A2AMessage, error) {
	msg := NewA2AMessage(senderID, receiverID, MessageTypeTaskData).WithTaskID(taskID)
	msg.ReceiverEndpoint = receiverEndpoint

	_, err := msg.WithPayload(dataPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to set payload: %w", err)
	}

	return c.SendMessage(ctx, receiverEndpoint, msg)
}

// QueryCapabilities queries an agent's capabilities
func (c *A2AClient) QueryCapabilities(ctx context.Context, receiverEndpoint, senderID, receiverID string) (*CapabilitiesPayload, error) {
	msg := NewA2AMessage(senderID, receiverID, MessageTypeQueryCapabilities)
	msg.ReceiverEndpoint = receiverEndpoint

	response, err := c.SendMessage(ctx, receiverEndpoint, msg)
	if err != nil {
		return nil, err
	}

	var capabilities CapabilitiesPayload
	if err := ExtractPayload(response, &capabilities); err != nil {
		return nil, err
	}

	return &capabilities, nil
}

// BroadcastMessage sends a message to multiple agents
func (c *A2AClient) BroadcastMessage(ctx context.Context, endpoints []string, msg *A2AMessage) map[string]error {
	errs := make(map[string]error)

	for _, endpoint := range endpoints {
		if _, err := c.SendMessage(ctx, endpoint, msg); err != nil {
			errs[endpoint] = err
		}
	}

	return errs
}

// ClientConfig holds configuration for the A2A client
type ClientConfig struct {
	Timeout     time.Duration
	MaxRetries  int
	Idempotent  bool // If true, messages have idempotency guarantees
	BufferSize  int  // Size of message buffer
	Compression bool // Enable message compression
}

// DefaultClientConfig returns default client configuration
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Timeout:     30 * time.Second,
		MaxRetries:  3,
		Idempotent:  true,
		BufferSize:  1000,
		Compression: false,
	}
}

// MessageQueue provides a queue for pending messages
type MessageQueue struct {
	messages chan *A2AMessage
	client   *A2AClient
	log      *logger.Logger
}

// NewMessageQueue creates a new message queue
func NewMessageQueue(client *A2AClient, bufferSize int, log *logger.Logger) *MessageQueue {
	return &MessageQueue{
		messages: make(chan *A2AMessage, bufferSize),
		client:   client,
		log:      log,
	}
}

// Enqueue adds a message to the queue
func (mq *MessageQueue) Enqueue(msg *A2AMessage) error {
	select {
	case mq.messages <- msg:
		return nil
	default:
		return fmt.Errorf("message queue is full")
	}
}

// ProcessQueue processes all messages in the queue
func (mq *MessageQueue) ProcessQueue(ctx context.Context) {
	for {
		select {
		case msg := <-mq.messages:
			if msg.ReceiverEndpoint != "" {
				if _, err := mq.client.SendMessage(ctx, msg.ReceiverEndpoint, msg); err != nil {
					mq.log.Error("failed to process queued message", "error", err, "message_id", msg.MessageID)
					// Re-queue message if send failed
					select {
					case mq.messages <- msg:
					default:
						mq.log.Error("failed to re-queue message", "message_id", msg.MessageID)
					}
				}
			}
		case <-ctx.Done():
			mq.log.Info("message queue processing stopped")
			return
		}
	}
}

// Close closes the message queue
func (mq *MessageQueue) Close() {
	close(mq.messages)
}
