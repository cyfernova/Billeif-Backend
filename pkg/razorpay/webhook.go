package razorpay

import (
	"encoding/json"
	"fmt"
)

type WebhookEvent struct {
	Event          string         `json:"event"`
	CreatedAtEpoch int64          `json:"created_at"`
	Payload        WebhookPayload `json:"payload"`
}

type WebhookPayload struct {
	Payment      *WebhookPaymentEntity      `json:"payment,omitempty"`
	Order        *WebhookOrderEntity        `json:"order,omitempty"`
	Subscription *WebhookSubscriptionEntity `json:"subscription,omitempty"`
}

type WebhookSubscriptionEntity struct {
	Entity Subscription `json:"entity"`
}

type WebhookPaymentEntity struct {
	Entity Payment `json:"entity"`
}

type WebhookOrderEntity struct {
	Entity Order `json:"entity"`
}

func ParseWebhookEvent(rawBody []byte) (*WebhookEvent, error) {
	var event WebhookEvent
	if err := json.Unmarshal(rawBody, &event); err != nil {
		return nil, fmt.Errorf("parse Razorpay webhook: %w", err)
	}
	if event.Event == "" {
		return nil, fmt.Errorf("razorpay webhook event is missing event type")
	}
	return &event, nil
}
