package razorpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type WebhookHandler struct {
	secret string
}

func NewWebhookHandler(secret string) *WebhookHandler {
	return &WebhookHandler{
		secret: secret,
	}
}

type WebhookEvent struct {
	Event string                 `json:"event"`
	Data  map[string]interface{} `json:"payload"`
}

type PaymentCapturedEvent struct {
	PaymentID string `json:"payment_id"`
	OrderID   string `json:"order_id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
	Method    string `json:"method"`
	Email     string `json:"email"`
	Contact   string `json:"contact"`
}

type PaymentFailedEvent struct {
	PaymentID   string `json:"payment_id"`
	OrderID     string `json:"order_id"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	Error       string `json:"error_code"`
	Description string `json:"error_description"`
}

type RefundProcessedEvent struct {
	RefundID  string `json:"refund_id"`
	PaymentID string `json:"payment_id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
}

func (h *WebhookHandler) VerifyWebhookSignature(payload []byte, receivedSignature string) bool {
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expectedSignature), []byte(receivedSignature))
}

func (h *WebhookHandler) ParseWebhookEvent(rawBody []byte) (*WebhookEvent, error) {
	var event WebhookEvent
	if err := json.Unmarshal(rawBody, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal webhook event: %w", err)
	}
	return &event, nil
}

func (h *WebhookHandler) ParsePaymentCapturedEvent(payload map[string]interface{}) (*PaymentCapturedEvent, error) {
	data, ok := payload["payment"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid payment captured event structure")
	}

	event := &PaymentCapturedEvent{}

	if paymentID, ok := data["id"].(string); ok {
		event.PaymentID = paymentID
	}

	if orderID, ok := data["order_id"].(string); ok {
		event.OrderID = orderID
	}

	if amount, ok := data["amount"].(float64); ok {
		event.Amount = int64(amount)
	}

	if currency, ok := data["currency"].(string); ok {
		event.Currency = currency
	}

	if status, ok := data["status"].(string); ok {
		event.Status = status
	}

	if method, ok := data["method"].(string); ok {
		event.Method = method
	}

	if email, ok := data["email"].(string); ok {
		event.Email = email
	}

	if contact, ok := data["contact"].(string); ok {
		event.Contact = contact
	}

	return event, nil
}

func (h *WebhookHandler) ParsePaymentFailedEvent(payload map[string]interface{}) (*PaymentFailedEvent, error) {
	data, ok := payload["payment"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid payment failed event structure")
	}

	event := &PaymentFailedEvent{}

	if paymentID, ok := data["id"].(string); ok {
		event.PaymentID = paymentID
	}

	if orderID, ok := data["order_id"].(string); ok {
		event.OrderID = orderID
	}

	if amount, ok := data["amount"].(float64); ok {
		event.Amount = int64(amount)
	}

	if currency, ok := data["currency"].(string); ok {
		event.Currency = currency
	}

	if status, ok := data["status"].(string); ok {
		event.Status = status
	}

	if error, ok := data["error_code"].(string); ok {
		event.Error = error
	}

	if desc, ok := data["error_description"].(string); ok {
		event.Description = desc
	}

	return event, nil
}

func (h *WebhookHandler) ParseRefundProcessedEvent(payload map[string]interface{}) (*RefundProcessedEvent, error) {
	data, ok := payload["refund"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid refund processed event structure")
	}

	event := &RefundProcessedEvent{}

	if refundID, ok := data["id"].(string); ok {
		event.RefundID = refundID
	}

	if paymentID, ok := data["payment_id"].(string); ok {
		event.PaymentID = paymentID
	}

	if amount, ok := data["amount"].(float64); ok {
		event.Amount = int64(amount)
	}

	if currency, ok := data["currency"].(string); ok {
		event.Currency = currency
	}

	if status, ok := data["status"].(string); ok {
		event.Status = status
	}

	return event, nil
}

func (h *WebhookHandler) GetEventType(event WebhookEvent) string {
	return event.Event
}

func (h *WebhookHandler) IsPaymentEvent(event WebhookEvent) bool {
	paymentEvents := map[string]bool{
		"payment.captured":   true,
		"payment.authorized": true,
		"payment.failed":     true,
		"payment.refunded":   true,
		"refund.processed":   true,
	}
	return paymentEvents[event.Event]
}

func (h *WebhookHandler) GetPaymentID(event WebhookEvent) (string, error) {
	data, ok := event.Data["payment"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("no payment data in event")
	}

	paymentID, ok := data["id"].(string)
	if !ok {
		return "", fmt.Errorf("no payment ID in payment data")
	}

	return paymentID, nil
}

func (h *WebhookHandler) GetOrderID(event WebhookEvent) (string, error) {
	data, ok := event.Data["payment"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("no payment data in event")
	}

	orderID, ok := data["order_id"].(string)
	if !ok {
		return "", fmt.Errorf("no order ID in payment data")
	}

	return orderID, nil
}

func (h *WebhookHandler) GetPaymentAmount(event WebhookEvent) (float64, error) {
	data, ok := event.Data["payment"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("no payment data in event")
	}

	amount, ok := data["amount"].(float64)
	if !ok {
		return 0, fmt.Errorf("no amount in payment data")
	}

	return amount, nil
}
