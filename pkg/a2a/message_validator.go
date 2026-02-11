package a2a

import (
	"encoding/json"
	"fmt"
	"time"

	"invoice-backend/pkg/ap2"
)

// MessageValidator handles validation of A2A messages
type MessageValidator struct {
	signatureSvc *ap2.SignatureService
}

// NewMessageValidator creates a new message validator
func NewMessageValidator(signatureSvc *ap2.SignatureService) *MessageValidator {
	return &MessageValidator{
		signatureSvc: signatureSvc,
	}
}

// ValidationResult represents the result of message validation
type ValidationResult struct {
	IsValid  bool
	Errors   []string
	Warnings []string
}

// ValidateMessage performs comprehensive validation of an A2A message
func (v *MessageValidator) ValidateMessage(msg *A2AMessage) *ValidationResult {
	result := &ValidationResult{
		IsValid:  true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Basic validation
	if err := msg.Validate(); err != nil {
		result.IsValid = false
		result.Errors = append(result.Errors, err.Error())
	}

	// Signature validation (if signature provided)
	if msg.Signature != "" && msg.PublicKey != "" {
		if err := v.verifySignature(msg); err != nil {
			result.IsValid = false
			result.Errors = append(result.Errors, fmt.Sprintf("signature verification failed: %v", err))
		}
	} else if msg.Signature != "" && msg.PublicKey == "" {
		result.Warnings = append(result.Warnings, "message has signature but no public key for verification")
	}

	// Payload size check (warn if too large)
	if len(msg.Payload) > 10*1024*1024 { // 10MB
		result.Warnings = append(result.Warnings, fmt.Sprintf("payload size is large: %d bytes", len(msg.Payload)))
	}

	// Check timeout reasonableness
	if msg.TimeoutMs < 1000 {
		result.Warnings = append(result.Warnings, "timeout is very short (< 1 second)")
	}
	if msg.TimeoutMs > 600000 { // 10 minutes
		result.Warnings = append(result.Warnings, "timeout is very long (> 10 minutes)")
	}

	// Retry count validation
	if msg.RetryCount > 10 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("high retry count: %d", msg.RetryCount))
	}

	return result
}

// VerifySignature verifies the message signature
func (v *MessageValidator) VerifySignature(msg *A2AMessage) error {
	return v.verifySignature(msg)
}

// verifySignature verifies the signature of a message
func (v *MessageValidator) verifySignature(msg *A2AMessage) error {
	if msg.Signature == "" {
		return fmt.Errorf("message has no signature")
	}

	if msg.PublicKey == "" {
		return fmt.Errorf("message has no public key")
	}

	// Build canonicalization data (exclude signature)
	dataForVerification := map[string]interface{}{
		"version":           msg.Version,
		"message_id":        msg.MessageID,
		"task_id":           msg.TaskID,
		"message_type":      msg.MessageType,
		"sender_id":         msg.SenderID,
		"sender_endpoint":   msg.SenderEndpoint,
		"receiver_id":       msg.ReceiverID,
		"receiver_endpoint": msg.ReceiverEndpoint,
		"payload":           msg.Payload,
		"metadata":          msg.Metadata,
		"retry_count":       msg.RetryCount,
		"created_at":        msg.CreatedAt.Unix(),
		"expires_at":        msg.ExpiresAt.Unix(),
		"timeout_ms":        msg.TimeoutMs,
		"status":            msg.Status,
		"correlation_id":    msg.CorrelationID,
		"reference_id":      msg.ReferenceID,
	}

	// Verify using SignatureService with canonical JSON
	if err := v.signatureSvc.VerifySignatureCanonicalWithPublicKey(
		dataForVerification,
		msg.Signature,
		msg.PublicKey,
	); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	return nil
}

// SignMessage signs a message with the signature service
func (v *MessageValidator) SignMessage(msg *A2AMessage, sigSvc *ap2.SignatureService) error {
	// Build data for signing (exclude existing signature)
	dataForSigning := map[string]interface{}{
		"version":           msg.Version,
		"message_id":        msg.MessageID,
		"task_id":           msg.TaskID,
		"message_type":      msg.MessageType,
		"sender_id":         msg.SenderID,
		"sender_endpoint":   msg.SenderEndpoint,
		"receiver_id":       msg.ReceiverID,
		"receiver_endpoint": msg.ReceiverEndpoint,
		"payload":           msg.Payload,
		"metadata":          msg.Metadata,
		"retry_count":       msg.RetryCount,
		"created_at":        msg.CreatedAt.Unix(),
		"expires_at":        msg.ExpiresAt.Unix(),
		"timeout_ms":        msg.TimeoutMs,
		"status":            msg.Status,
		"correlation_id":    msg.CorrelationID,
		"reference_id":      msg.ReferenceID,
	}

	// Sign the message
	signature, err := sigSvc.SignDataCanonical(dataForSigning)
	if err != nil {
		return fmt.Errorf("failed to sign message: %w", err)
	}

	msg.Signature = signature
	msg.PublicKey = sigSvc.GetPublicKey()

	return nil
}

// ValidateTaskStartPayload validates a task start payload
func (v *MessageValidator) ValidateTaskStartPayload(payload *TaskStartPayload) error {
	if payload.TaskName == "" {
		return fmt.Errorf("task_name is required")
	}

	if len(payload.TaskName) > 255 {
		return fmt.Errorf("task_name is too long (max 255 characters)")
	}

	if payload.Parameters == nil {
		return fmt.Errorf("parameters is required")
	}

	if payload.Priority < 1 || payload.Priority > 10 {
		return fmt.Errorf("priority must be between 1 and 10")
	}

	if payload.Deadline != nil && time.Now().After(*payload.Deadline) {
		return fmt.Errorf("deadline is in the past")
	}

	return nil
}

// ValidateTaskDataPayload validates a task data payload
func (v *MessageValidator) ValidateTaskDataPayload(payload *TaskDataPayload) error {
	if payload.SequenceNumber < 0 {
		return fmt.Errorf("sequence_number cannot be negative")
	}

	validDataTypes := map[string]bool{
		"json":   true,
		"binary": true,
		"text":   true,
	}

	if !validDataTypes[payload.DataType] {
		return fmt.Errorf("invalid data_type: %s", payload.DataType)
	}

	if len(payload.Data) == 0 {
		return fmt.Errorf("data cannot be empty")
	}

	return nil
}

// ValidateTaskStatusPayload validates a task status payload
func (v *MessageValidator) ValidateTaskStatusPayload(payload *TaskStatusPayload) error {
	validStatuses := map[string]bool{
		"pending":     true,
		"in_progress": true,
		"paused":      true,
		"completed":   true,
		"failed":      true,
	}

	if !validStatuses[payload.Status] {
		return fmt.Errorf("invalid status: %s", payload.Status)
	}

	if payload.ProgressPct < 0 || payload.ProgressPct > 100 {
		return fmt.Errorf("progress_pct must be between 0 and 100")
	}

	return nil
}

// ValidateTaskResultPayload validates a task result payload
func (v *MessageValidator) ValidateTaskResultPayload(payload *TaskResultPayload) error {
	validStatuses := map[string]bool{
		"success":         true,
		"partial_success": true,
		"failed":          true,
	}

	if !validStatuses[payload.Status] {
		return fmt.Errorf("invalid status: %s", payload.Status)
	}

	if payload.ExecutionTimeMs < 0 {
		return fmt.Errorf("execution_time_ms cannot be negative")
	}

	return nil
}

// ValidateTaskErrorPayload validates a task error payload
func (v *MessageValidator) ValidateTaskErrorPayload(payload *TaskErrorPayload) error {
	if payload.ErrorCode == "" {
		return fmt.Errorf("error_code is required")
	}

	if payload.ErrorMessage == "" {
		return fmt.Errorf("error_message is required")
	}

	if payload.RetryAfterMs != nil && *payload.RetryAfterMs < 0 {
		return fmt.Errorf("retry_after_ms cannot be negative")
	}

	return nil
}

// ExtractPayload extracts and parses a payload of a specific type
func ExtractPayload(msg *A2AMessage, targetType interface{}) error {
	if len(msg.Payload) == 0 {
		return fmt.Errorf("message payload is empty")
	}

	if err := json.Unmarshal(msg.Payload, targetType); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	return nil
}

// MessageComparator helps compare messages
type MessageComparator struct{}

// NewMessageComparator creates a new message comparator
func NewMessageComparator() *MessageComparator {
	return &MessageComparator{}
}

// AreMessagesCorrelated checks if two messages are part of the same correlation
func (mc *MessageComparator) AreMessagesCorrelated(msg1, msg2 *A2AMessage) bool {
	if msg1.CorrelationID != nil && msg2.CorrelationID != nil {
		return *msg1.CorrelationID == *msg2.CorrelationID
	}

	return msg1.TaskID == msg2.TaskID
}

// IsResponseToMessage checks if msg2 is a response to msg1
func (mc *MessageComparator) IsResponseToMessage(msg1, msg2 *A2AMessage) bool {
	// Check if msg2 references msg1
	if msg2.ReferenceID != nil && *msg2.ReferenceID == msg1.MessageID {
		return true
	}

	// Check if they're related through task or correlation
	return mc.AreMessagesCorrelated(msg1, msg2)
}
