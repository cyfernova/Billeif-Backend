package a2a

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidMessageType    = errors.New("invalid message type")
	ErrMissingRequiredFields = errors.New("missing required fields in message")
	ErrInvalidMessageVersion = errors.New("unsupported message version")
)

// MessageType represents the type of A2A message
type MessageType string

const (
	// Task message types
	MessageTypeTaskStart  MessageType = "task.start"
	MessageTypeTaskData   MessageType = "task.data"
	MessageTypeTaskStatus MessageType = "task.status"
	MessageTypeTaskResult MessageType = "task.result"
	MessageTypeTaskError  MessageType = "task.error"
	MessageTypeTaskCancel MessageType = "task.cancel"

	// Negotiation message types
	MessageTypeNegotiateStart  MessageType = "negotiate.start"
	MessageTypeNegotiateAccept MessageType = "negotiate.accept"
	MessageTypeNegotiateReject MessageType = "negotiate.reject"

	// Query/Response message types
	MessageTypeQueryCapabilities    MessageType = "query.capabilities"
	MessageTypeResponseCapabilities MessageType = "response.capabilities"
)

// MessageVersion represents the A2A protocol version
const MessageVersion = "1.0"

// A2AMessage is the base message structure for agent-to-agent communication
type A2AMessage struct {
	// Protocol information
	Version     string      `json:"version"`      // Protocol version (e.g., "1.0")
	MessageID   string      `json:"message_id"`   // Unique message identifier (UUID)
	TaskID      string      `json:"task_id"`      // Task identifier linking related messages
	MessageType MessageType `json:"message_type"` // Type of message (task.start, task.result, etc.)

	// Agent information
	SenderID         string `json:"sender_id"`         // Sending agent ID
	SenderEndpoint   string `json:"sender_endpoint"`   // Sending agent's endpoint for replies
	ReceiverID       string `json:"receiver_id"`       // Receiving agent ID
	ReceiverEndpoint string `json:"receiver_endpoint"` // Receiving agent's endpoint

	// Payload and context
	Payload    json.RawMessage        `json:"payload"`     // Message-specific data
	Metadata   map[string]interface{} `json:"metadata"`    // Additional context
	RetryCount int                    `json:"retry_count"` // Number of retry attempts

	// Security and signatures
	Signature string `json:"signature"`  // ECDSA signature of message (RFC 8785 JCS)
	PublicKey string `json:"public_key"` // Sender's public key for verification

	// Timestamps and timing
	CreatedAt time.Time `json:"created_at"` // Message creation time
	ExpiresAt time.Time `json:"expires_at"` // Message expiration time
	TimeoutMs int       `json:"timeout_ms"` // Expected response timeout in milliseconds

	// Status tracking
	Status    string `json:"status"`     // Current message status (pending, delivered, processed, failed)
	StatusMsg string `json:"status_msg"` // Human-readable status message

	// Correlation and relationships
	CorrelationID *string `json:"correlation_id,omitempty"` // Correlate related messages
	ReferenceID   *string `json:"reference_id,omitempty"`   // Reference to previous message
}

// TaskStartPayload represents the payload for task.start messages
type TaskStartPayload struct {
	TaskName    string                 `json:"task_name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
	Constraints map[string]interface{} `json:"constraints,omitempty"`
	Priority    int                    `json:"priority,omitempty"` // 1-10, default 5
	Deadline    *time.Time             `json:"deadline,omitempty"`
}

// TaskDataPayload represents the payload for task.data messages
type TaskDataPayload struct {
	SequenceNumber int                    `json:"sequence_number"`
	DataType       string                 `json:"data_type"` // json, binary, text
	Data           json.RawMessage        `json:"data"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	IsLastChunk    bool                   `json:"is_last_chunk"`
}

// TaskStatusPayload represents the payload for task.status messages
type TaskStatusPayload struct {
	Status                   string `json:"status"`       // pending, in_progress, paused, completed, failed
	ProgressPct              int    `json:"progress_pct"` // 0-100
	Message                  string `json:"message,omitempty"`
	EstimatedTimeRemainingMs int    `json:"estimated_time_remaining_ms,omitempty"`
}

// TaskResultPayload represents the payload for task.result messages
type TaskResultPayload struct {
	Status          string                 `json:"status"` // success, partial_success, failed
	ResultData      json.RawMessage        `json:"result_data,omitempty"`
	Metrics         map[string]interface{} `json:"metrics,omitempty"`
	ExecutionTimeMs int                    `json:"execution_time_ms"`
}

// TaskErrorPayload represents the payload for task.error messages
type TaskErrorPayload struct {
	ErrorCode    string                 `json:"error_code"`
	ErrorMessage string                 `json:"error_message"`
	ErrorDetails map[string]interface{} `json:"error_details,omitempty"`
	Recoverable  bool                   `json:"recoverable"` // Can task be retried?
	RetryAfterMs *int                   `json:"retry_after_ms,omitempty"`
}

// CapabilitiesPayload represents the payload for capability queries/responses
type CapabilitiesPayload struct {
	Capabilities       []string               `json:"capabilities"`
	Version            string                 `json:"version"`
	MaxConcurrentTasks int                    `json:"max_concurrent_tasks"`
	SupportedDataTypes []string               `json:"supported_data_types"`
	RateLimits         map[string]interface{} `json:"rate_limits,omitempty"`
}

// NewA2AMessage creates a new A2A message with default values
func NewA2AMessage(senderID, receiverID string, messageType MessageType) *A2AMessage {
	return &A2AMessage{
		Version:     MessageVersion,
		MessageID:   uuid.New().String(),
		SenderID:    senderID,
		ReceiverID:  receiverID,
		MessageType: messageType,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		Status:      "pending",
		TimeoutMs:   30000, // 30 seconds default
		RetryCount:  0,
		Metadata:    make(map[string]interface{}),
	}
}

// WithTaskID sets the task ID
func (m *A2AMessage) WithTaskID(taskID string) *A2AMessage {
	m.TaskID = taskID
	return m
}

// WithPayload sets the message payload
func (m *A2AMessage) WithPayload(payload interface{}) (*A2AMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	m.Payload = data
	return m, nil
}

// WithMetadata sets metadata
func (m *A2AMessage) WithMetadata(key string, value interface{}) *A2AMessage {
	m.Metadata[key] = value
	return m
}

// WithTimeout sets the response timeout
func (m *A2AMessage) WithTimeout(ms int) *A2AMessage {
	m.TimeoutMs = ms
	return m
}

// WithExpiration sets when the message expires
func (m *A2AMessage) WithExpiration(duration time.Duration) *A2AMessage {
	m.ExpiresAt = time.Now().Add(duration)
	return m
}

// WithEndpoints sets sender and receiver endpoints
func (m *A2AMessage) WithEndpoints(senderEndpoint, receiverEndpoint string) *A2AMessage {
	m.SenderEndpoint = senderEndpoint
	m.ReceiverEndpoint = receiverEndpoint
	return m
}

// WithCorrelation sets correlation ID for related messages
func (m *A2AMessage) WithCorrelation(correlationID string) *A2AMessage {
	m.CorrelationID = &correlationID
	return m
}

// WithReference sets a reference to a previous message
func (m *A2AMessage) WithReference(referenceID string) *A2AMessage {
	m.ReferenceID = &referenceID
	return m
}

// Validate checks if the message has all required fields
func (m *A2AMessage) Validate() error {
	if m.Version == "" {
		return ErrInvalidMessageVersion
	}

	if m.MessageID == "" || m.SenderID == "" || m.ReceiverID == "" {
		return ErrMissingRequiredFields
	}

	if m.MessageType == "" {
		return ErrInvalidMessageType
	}

	// Validate message type
	validTypes := map[MessageType]bool{
		MessageTypeTaskStart:            true,
		MessageTypeTaskData:             true,
		MessageTypeTaskStatus:           true,
		MessageTypeTaskResult:           true,
		MessageTypeTaskError:            true,
		MessageTypeTaskCancel:           true,
		MessageTypeNegotiateStart:       true,
		MessageTypeNegotiateAccept:      true,
		MessageTypeNegotiateReject:      true,
		MessageTypeQueryCapabilities:    true,
		MessageTypeResponseCapabilities: true,
	}

	if !validTypes[m.MessageType] {
		return fmt.Errorf("%w: %s", ErrInvalidMessageType, m.MessageType)
	}

	// Task messages require TaskID
	if m.MessageType == MessageTypeTaskStart || m.MessageType == MessageTypeTaskData ||
		m.MessageType == MessageTypeTaskStatus || m.MessageType == MessageTypeTaskResult ||
		m.MessageType == MessageTypeTaskError || m.MessageType == MessageTypeTaskCancel {
		if m.TaskID == "" {
			return fmt.Errorf("task_id is required for message type %s", m.MessageType)
		}
	}

	// Check expiration
	if time.Now().After(m.ExpiresAt) {
		return fmt.Errorf("message has expired")
	}

	return nil
}

// IsExpired checks if the message has expired
func (m *A2AMessage) IsExpired() bool {
	return time.Now().After(m.ExpiresAt)
}

// IsDelivered checks if the message was successfully delivered
func (m *A2AMessage) IsDelivered() bool {
	return m.Status == "delivered" || m.Status == "processed"
}

// MarkAsDelivered marks the message as delivered
func (m *A2AMessage) MarkAsDelivered() {
	m.Status = "delivered"
	m.StatusMsg = "Message delivered to recipient"
}

// MarkAsProcessed marks the message as processed
func (m *A2AMessage) MarkAsProcessed() {
	m.Status = "processed"
	m.StatusMsg = "Message processed by recipient"
}

// MarkAsFailed marks the message as failed
func (m *A2AMessage) MarkAsFailed(reason string) {
	m.Status = "failed"
	m.StatusMsg = reason
}

// IncrementRetry increments the retry count
func (m *A2AMessage) IncrementRetry() {
	m.RetryCount++
}

// ShouldRetry determines if the message should be retried
func (m *A2AMessage) ShouldRetry(maxRetries int) bool {
	return m.RetryCount < maxRetries && !m.IsExpired()
}

// ToJSON converts the message to JSON
func (m *A2AMessage) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// FromJSON parses a JSON message
func FromJSON(data []byte) (*A2AMessage, error) {
	var msg A2AMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}
	return &msg, nil
}

// MessageBuilder provides a fluent interface for building messages
type MessageBuilder struct {
	msg *A2AMessage
}

// NewMessageBuilder creates a new message builder
func NewMessageBuilder(senderID, receiverID string, msgType MessageType) *MessageBuilder {
	return &MessageBuilder{
		msg: NewA2AMessage(senderID, receiverID, msgType),
	}
}

// WithTaskID sets the task ID
func (b *MessageBuilder) WithTaskID(taskID string) *MessageBuilder {
	b.msg.TaskID = taskID
	return b
}

// WithPayload sets the payload
func (b *MessageBuilder) WithPayload(payload interface{}) (*MessageBuilder, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	b.msg.Payload = data
	return b, nil
}

// WithTimeout sets timeout
func (b *MessageBuilder) WithTimeout(ms int) *MessageBuilder {
	b.msg.TimeoutMs = ms
	return b
}

// WithEndpoints sets endpoints
func (b *MessageBuilder) WithEndpoints(sender, receiver string) *MessageBuilder {
	b.msg.SenderEndpoint = sender
	b.msg.ReceiverEndpoint = receiver
	return b
}

// WithMetadata adds metadata
func (b *MessageBuilder) WithMetadata(key string, value interface{}) *MessageBuilder {
	b.msg.Metadata[key] = value
	return b
}

// Build creates the final message
func (b *MessageBuilder) Build() (*A2AMessage, error) {
	if err := b.msg.Validate(); err != nil {
		return nil, err
	}
	return b.msg, nil
}
