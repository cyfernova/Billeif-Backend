package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// MarketplaceProvider defines the interface for marketplace operations
type MarketplaceProvider interface {
	SearchProducts(ctx context.Context, query string, page, limit int) ([]*models.MarketplaceProduct, int64, error)
}

// A2AMessageHandler handles agent-to-agent communication
type A2AMessageHandler struct {
	shopping    *services.ShoppingAgentService
	merchant    *services.MerchantAgentService
	credential  *services.CredentialProviderService
	payment     *services.PaymentProcessorService
	marketplace MarketplaceProvider
	a2aClient   *a2a.A2AClient
	signature   *ap2.SignatureService
	log         *logger.Logger
}

// NewA2AMessageHandler creates a new A2A message handler
func NewA2AMessageHandler(
	shopping *services.ShoppingAgentService,
	merchant *services.MerchantAgentService,
	credential *services.CredentialProviderService,
	payment *services.PaymentProcessorService,
	marketplace MarketplaceProvider,
	a2aClient *a2a.A2AClient,
	signature *ap2.SignatureService,
	log *logger.Logger,
) *A2AMessageHandler {
	return &A2AMessageHandler{
		shopping:    shopping,
		merchant:    merchant,
		credential:  credential,
		payment:     payment,
		marketplace: marketplace,
		a2aClient:   a2aClient,
		signature:   signature,
		log:         log,
	}
}

// HandleMessage handles incoming A2A messages
// POST /a2a/message
func (h *A2AMessageHandler) HandleMessage(c *gin.Context) {
	// Parse incoming message
	var msg a2a.A2AMessage
	if err := c.ShouldBindJSON(&msg); err != nil {
		h.log.Error("failed to parse A2A message", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid A2A message format",
		})
		return
	}

	if msg.Signature == "" || msg.PublicKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "signed messages are required"})
		return
	}

	// Validate message
	validator := a2a.NewMessageValidator(h.signature)
	validation := validator.ValidateMessage(&msg)
	if !validation.IsValid {
		h.log.Warn("A2A message validation failed", "message_id", msg.MessageID, "errors", validation.Errors)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "message validation failed",
			"errors": validation.Errors,
		})
		return
	}

	// Route message based on message type
	response, err := h.routeMessage(c, &msg)
	if err != nil {
		h.log.Error("failed to process A2A message", "error", err, "message_id", msg.MessageID, "message_type", msg.MessageType)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to process message: %v", err),
		})
		return
	}

	h.log.Info("A2A message processed successfully", "message_id", msg.MessageID, "response_id", response.MessageID)

	c.JSON(http.StatusOK, response)
}

// routeMessage routes A2A messages to appropriate handlers
func (h *A2AMessageHandler) routeMessage(c *gin.Context, msg *a2a.A2AMessage) (*a2a.A2AMessage, error) {
	switch msg.MessageType {
	case a2a.MessageTypeTaskStart:
		return h.handleTaskStart(c, msg)
	case a2a.MessageTypeTaskData:
		return h.handleTaskData(c, msg)
	case a2a.MessageTypeTaskStatus:
		return h.handleTaskStatus(c, msg)
	case a2a.MessageTypeTaskResult:
		return h.handleTaskResult(c, msg)
	case a2a.MessageTypeTaskError:
		return h.handleTaskError(c, msg)
	case a2a.MessageTypeTaskCancel:
		return h.handleTaskCancel(c, msg)
	case a2a.MessageTypeQueryCapabilities:
		return h.handleQueryCapabilities(c, msg)
	case a2a.MessageTypeNegotiateStart:
		return h.handleNegotiateStart(c, msg)
	default:
		return nil, fmt.Errorf("unsupported message type: %s", msg.MessageType)
	}
}

// handleTaskStart processes task.start messages
func (h *A2AMessageHandler) handleTaskStart(c *gin.Context, msg *a2a.A2AMessage) (*a2a.A2AMessage, error) {
	h.log.Info("processing task.start message", "message_id", msg.MessageID, "task_id", msg.TaskID, "sender", msg.SenderID)

	// Parse task start payload
	var payload a2a.TaskStartPayload
	if err := a2a.ExtractPayload(msg, &payload); err != nil {
		return nil, fmt.Errorf("failed to extract task start payload: %w", err)
	}

	// Validate payload
	validator := a2a.NewMessageValidator(h.signature)
	if err := validator.ValidateTaskStartPayload(&payload); err != nil {
		return nil, fmt.Errorf("invalid task start payload: %w", err)
	}

	// Route to appropriate service based on task name
	switch payload.TaskName {
	case "shopping.search_products":
		return h.handleSearchProducts(c, msg, &payload)
	case "shopping.create_cart":
		return h.handleCreateCart(c, msg, &payload)
	case "merchant.process_cart":
		return h.handleMerchantProcessCart(c, msg, &payload)
	case "payment.process":
		return h.handlePaymentProcess(c, msg, &payload)
	default:
		return nil, fmt.Errorf("unknown task: %s", payload.TaskName)
	}
}

// handleSearchProducts handles product search tasks
func (h *A2AMessageHandler) handleSearchProducts(c *gin.Context, msg *a2a.A2AMessage, payload *a2a.TaskStartPayload) (*a2a.A2AMessage, error) {
	// Extract search parameters from payload
	searchTerm, ok := payload.Parameters["search_term"].(string)
	if !ok {
		return nil, fmt.Errorf("missing search_term parameter")
	}

	// Search products (use context from gin.Context for HTTP operations)
	products, _, err := h.marketplace.SearchProducts(c.Request.Context(), searchTerm, 0, 20)
	if err != nil {
		return nil, fmt.Errorf("failed to search products: %w", err)
	}

	// Create response
	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeTaskResult)
	response.TaskID = msg.TaskID
	response.CorrelationID = msg.CorrelationID
	response.ReferenceID = &msg.MessageID

	// Marshal products to JSON
	productsJSON, err := json.Marshal(products)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal products: %w", err)
	}

	// Create result payload
	resultPayload := &a2a.TaskResultPayload{
		Status:          "success",
		ResultData:      json.RawMessage(productsJSON),
		ExecutionTimeMs: 0,
	}

	if _, err := response.WithPayload(resultPayload); err != nil {
		return nil, fmt.Errorf("failed to set response payload: %w", err)
	}

	h.log.Info("product search completed", "message_id", msg.MessageID, "products_found", len(products))

	return response, nil
}

// handleCreateCart handles cart creation tasks
func (h *A2AMessageHandler) handleCreateCart(c *gin.Context, msg *a2a.A2AMessage, payload *a2a.TaskStartPayload) (*a2a.A2AMessage, error) {
	userID, ok := payload.Parameters["user_id"].(string)
	if !ok {
		return nil, fmt.Errorf("missing user_id parameter")
	}

	// Create response
	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeTaskResult)
	response.TaskID = msg.TaskID
	response.CorrelationID = msg.CorrelationID
	response.ReferenceID = &msg.MessageID

	// In production, create actual cart
	// For now, return success
	resultPayload := &a2a.TaskResultPayload{
		Status: "success",
		Metrics: map[string]interface{}{
			"cart_created": true,
			"user_id":      userID,
		},
		ExecutionTimeMs: 0,
	}

	if _, err := response.WithPayload(resultPayload); err != nil {
		return nil, fmt.Errorf("failed to set response payload: %w", err)
	}

	h.log.Info("cart created via A2A", "message_id", msg.MessageID, "user_id", userID)

	return response, nil
}

// handleMerchantProcessCart handles merchant cart processing
func (h *A2AMessageHandler) handleMerchantProcessCart(c *gin.Context, msg *a2a.A2AMessage, payload *a2a.TaskStartPayload) (*a2a.A2AMessage, error) {
	cartID, ok := payload.Parameters["cart_id"].(string)
	if !ok {
		return nil, fmt.Errorf("missing cart_id parameter")
	}

	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeTaskResult)
	response.TaskID = msg.TaskID
	response.CorrelationID = msg.CorrelationID
	response.ReferenceID = &msg.MessageID

	resultPayload := &a2a.TaskResultPayload{
		Status: "success",
		Metrics: map[string]interface{}{
			"cart_processed": true,
			"cart_id":        cartID,
		},
		ExecutionTimeMs: 0,
	}

	if _, err := response.WithPayload(resultPayload); err != nil {
		return nil, fmt.Errorf("failed to set response payload: %w", err)
	}

	h.log.Info("merchant processed cart via A2A", "message_id", msg.MessageID, "cart_id", cartID)

	return response, nil
}

// handlePaymentProcess handles payment processing tasks
func (h *A2AMessageHandler) handlePaymentProcess(c *gin.Context, msg *a2a.A2AMessage, payload *a2a.TaskStartPayload) (*a2a.A2AMessage, error) {
	amount, ok := payload.Parameters["amount"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing amount parameter")
	}

	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeTaskResult)
	response.TaskID = msg.TaskID
	response.CorrelationID = msg.CorrelationID
	response.ReferenceID = &msg.MessageID

	resultPayload := &a2a.TaskResultPayload{
		Status: "success",
		Metrics: map[string]interface{}{
			"payment_processed": true,
			"amount":            amount,
		},
		ExecutionTimeMs: 0,
	}

	if _, err := response.WithPayload(resultPayload); err != nil {
		return nil, fmt.Errorf("failed to set response payload: %w", err)
	}

	h.log.Info("payment processed via A2A", "message_id", msg.MessageID, "amount", amount)

	return response, nil
}

// handleTaskData processes task.data messages
func (h *A2AMessageHandler) handleTaskData(c *gin.Context, msg *a2a.A2AMessage) (*a2a.A2AMessage, error) {
	var payload a2a.TaskDataPayload
	if err := a2a.ExtractPayload(msg, &payload); err != nil {
		return nil, fmt.Errorf("failed to extract task data payload: %w", err)
	}

	validator := a2a.NewMessageValidator(h.signature)
	if err := validator.ValidateTaskDataPayload(&payload); err != nil {
		return nil, fmt.Errorf("invalid task data payload: %w", err)
	}

	h.log.Info("received task data", "message_id", msg.MessageID, "task_id", msg.TaskID, "sequence", payload.SequenceNumber)

	// Create acknowledgment response
	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeTaskStatus)
	response.TaskID = msg.TaskID
	response.ReferenceID = &msg.MessageID

	statusPayload := &a2a.TaskStatusPayload{
		Status:      "in_progress",
		ProgressPct: int(float64(payload.SequenceNumber) * 100.0 / 10.0),
		Message:     fmt.Sprintf("Received chunk %d", payload.SequenceNumber),
	}

	if _, err := response.WithPayload(statusPayload); err != nil {
		return nil, fmt.Errorf("failed to set response payload: %w", err)
	}

	return response, nil
}

// handleTaskStatus processes task.status messages
func (h *A2AMessageHandler) handleTaskStatus(c *gin.Context, msg *a2a.A2AMessage) (*a2a.A2AMessage, error) {
	var payload a2a.TaskStatusPayload
	if err := a2a.ExtractPayload(msg, &payload); err != nil {
		return nil, fmt.Errorf("failed to extract task status payload: %w", err)
	}

	validator := a2a.NewMessageValidator(h.signature)
	if err := validator.ValidateTaskStatusPayload(&payload); err != nil {
		return nil, fmt.Errorf("invalid task status payload: %w", err)
	}

	h.log.Info("received task status update", "message_id", msg.MessageID, "task_id", msg.TaskID, "status", payload.Status, "progress", payload.ProgressPct)

	// Create acknowledgment
	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeTaskStatus)
	response.TaskID = msg.TaskID
	response.ReferenceID = &msg.MessageID
	response.Status = "delivered"

	return response, nil
}

// handleTaskResult processes task.result messages
func (h *A2AMessageHandler) handleTaskResult(c *gin.Context, msg *a2a.A2AMessage) (*a2a.A2AMessage, error) {
	var payload a2a.TaskResultPayload
	if err := a2a.ExtractPayload(msg, &payload); err != nil {
		return nil, fmt.Errorf("failed to extract task result payload: %w", err)
	}

	validator := a2a.NewMessageValidator(h.signature)
	if err := validator.ValidateTaskResultPayload(&payload); err != nil {
		return nil, fmt.Errorf("invalid task result payload: %w", err)
	}

	h.log.Info("received task result", "message_id", msg.MessageID, "task_id", msg.TaskID, "status", payload.Status, "execution_time_ms", payload.ExecutionTimeMs)

	// Create acknowledgment
	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeTaskResult)
	response.TaskID = msg.TaskID
	response.ReferenceID = &msg.MessageID
	response.Status = "delivered"

	return response, nil
}

// handleTaskError processes task.error messages
func (h *A2AMessageHandler) handleTaskError(c *gin.Context, msg *a2a.A2AMessage) (*a2a.A2AMessage, error) {
	var payload a2a.TaskErrorPayload
	if err := a2a.ExtractPayload(msg, &payload); err != nil {
		return nil, fmt.Errorf("failed to extract task error payload: %w", err)
	}

	validator := a2a.NewMessageValidator(h.signature)
	if err := validator.ValidateTaskErrorPayload(&payload); err != nil {
		return nil, fmt.Errorf("invalid task error payload: %w", err)
	}

	h.log.Error("received task error", "message_id", msg.MessageID, "task_id", msg.TaskID, "error_code", payload.ErrorCode, "error_message", payload.ErrorMessage)

	// Create acknowledgment
	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeTaskError)
	response.TaskID = msg.TaskID
	response.ReferenceID = &msg.MessageID
	response.Status = "delivered"

	return response, nil
}

// handleTaskCancel processes task.cancel messages
func (h *A2AMessageHandler) handleTaskCancel(c *gin.Context, msg *a2a.A2AMessage) (*a2a.A2AMessage, error) {
	h.log.Info("received task cancel request", "message_id", msg.MessageID, "task_id", msg.TaskID)

	// Create acknowledgment
	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeTaskResult)
	response.TaskID = msg.TaskID
	response.ReferenceID = &msg.MessageID
	response.Status = "delivered"

	statusPayload := &a2a.TaskStatusPayload{
		Status:  "completed",
		Message: "Task cancelled",
	}

	if _, err := response.WithPayload(statusPayload); err != nil {
		return nil, fmt.Errorf("failed to set response payload: %w", err)
	}

	return response, nil
}

// handleQueryCapabilities processes query.capabilities messages
func (h *A2AMessageHandler) handleQueryCapabilities(c *gin.Context, msg *a2a.A2AMessage) (*a2a.A2AMessage, error) {
	h.log.Info("received capabilities query", "message_id", msg.MessageID, "receiver", msg.ReceiverID)

	// Create response with this agent's capabilities
	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeResponseCapabilities)
	response.CorrelationID = msg.CorrelationID
	response.ReferenceID = &msg.MessageID

	capPayload := &a2a.CapabilitiesPayload{
		Capabilities: []string{
			"shopping.search_products",
			"shopping.create_cart",
			"shopping.checkout",
			"merchant.process_cart",
			"payment.process",
		},
		Version:            "1.0",
		MaxConcurrentTasks: 10,
		SupportedDataTypes: []string{"json", "binary", "text"},
		RateLimits: map[string]interface{}{
			"requests_per_minute": 100,
			"tasks_per_hour":      500,
		},
	}

	if _, err := response.WithPayload(capPayload); err != nil {
		return nil, fmt.Errorf("failed to set capabilities payload: %w", err)
	}

	return response, nil
}

// handleNegotiateStart processes negotiate.start messages
func (h *A2AMessageHandler) handleNegotiateStart(c *gin.Context, msg *a2a.A2AMessage) (*a2a.A2AMessage, error) {
	h.log.Info("received negotiation start", "message_id", msg.MessageID, "sender", msg.SenderID)

	// Accept negotiation
	response := a2a.NewA2AMessage(msg.ReceiverID, msg.SenderID, a2a.MessageTypeNegotiateAccept)
	response.CorrelationID = msg.CorrelationID
	response.ReferenceID = &msg.MessageID

	if _, err := response.WithPayload(map[string]interface{}{
		"negotiation_accepted": true,
		"protocol_version":     "1.0",
		"supported_features": []string{
			"message_signing",
			"retry_logic",
			"task_correlation",
		},
	}); err != nil {
		return nil, fmt.Errorf("failed to set negotiation payload: %w", err)
	}

	return response, nil
}

// GetMessageStats returns statistics about A2A messaging
// GET /a2a/stats
func (h *A2AMessageHandler) GetMessageStats(c *gin.Context) {
	// Placeholder for message statistics
	c.JSON(http.StatusOK, gin.H{
		"messages_received":  0,
		"messages_sent":      0,
		"average_latency_ms": 0,
		"error_rate":         0.0,
	})
}
