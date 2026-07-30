package sesfeedback

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

type EventType string

const (
	EventTypeDelivery  EventType = "delivery"
	EventTypeBounce    EventType = "bounce"
	EventTypeComplaint EventType = "complaint"

	MaxEventBytes        = 256 * 1024
	maxProviderMessageID = 255
)

var ErrInvalidEvent = errors.New("invalid SES feedback event")
var ErrFeedbackUncorrelated = errors.New("SES feedback does not correlate to a delivery")

type ValidationOptions struct {
	SendingAccountID string
	ConfigurationSet string
}

type FeedbackEvent struct {
	Type              EventType
	BusinessID        string
	DeliveryID        string
	ProviderMessageID string
	OccurredAt        time.Time
}

type ApplyResult struct {
	Status  string
	Changed bool
}

type wireEvent struct {
	EventType string          `json:"eventType"`
	Mail      wireMail        `json:"mail"`
	Delivery  json.RawMessage `json:"delivery"`
	Bounce    json.RawMessage `json:"bounce"`
	Complaint json.RawMessage `json:"complaint"`
}

type wireMail struct {
	SendingAccountID string              `json:"sendingAccountId"`
	MessageID        string              `json:"messageId"`
	Tags             map[string][]string `json:"tags"`
}

type eventPayload struct {
	Timestamp time.Time `json:"timestamp"`
}

func ParseEvent(body []byte, options ValidationOptions) (FeedbackEvent, error) {
	if len(body) == 0 || len(body) > MaxEventBytes {
		return FeedbackEvent{}, fmt.Errorf("%w: event size is outside the accepted range", ErrInvalidEvent)
	}
	if strings.TrimSpace(options.SendingAccountID) == "" ||
		strings.TrimSpace(options.ConfigurationSet) == "" {
		return FeedbackEvent{}, fmt.Errorf("%w: validation configuration is required", ErrInvalidEvent)
	}

	var wire wireEvent
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&wire); err != nil {
		return FeedbackEvent{}, fmt.Errorf("%w: decode: %v", ErrInvalidEvent, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return FeedbackEvent{}, fmt.Errorf("%w: trailing JSON", ErrInvalidEvent)
	}

	eventType, payload, err := selectPayload(wire)
	if err != nil {
		return FeedbackEvent{}, err
	}
	if wire.Mail.SendingAccountID != options.SendingAccountID {
		return FeedbackEvent{}, fmt.Errorf("%w: sending account does not match", ErrInvalidEvent)
	}
	providerMessageID := strings.TrimSpace(wire.Mail.MessageID)
	if providerMessageID == "" || len(providerMessageID) > maxProviderMessageID {
		return FeedbackEvent{}, fmt.Errorf("%w: provider message id is required", ErrInvalidEvent)
	}
	if !singleTagEquals(wire.Mail.Tags["ses:configuration-set"], options.ConfigurationSet) {
		return FeedbackEvent{}, fmt.Errorf("%w: configuration set does not match", ErrInvalidEvent)
	}

	businessID, err := requiredUUIDTag(wire.Mail.Tags, "business_id")
	if err != nil {
		return FeedbackEvent{}, err
	}
	deliveryID, err := requiredUUIDTag(wire.Mail.Tags, "delivery_id")
	if err != nil {
		return FeedbackEvent{}, err
	}

	var details eventPayload
	if len(payload) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		return FeedbackEvent{}, fmt.Errorf("%w: matching event payload is required", ErrInvalidEvent)
	}
	if err := json.Unmarshal(payload, &details); err != nil || details.Timestamp.IsZero() {
		return FeedbackEvent{}, fmt.Errorf("%w: matching event timestamp is required", ErrInvalidEvent)
	}

	return FeedbackEvent{
		Type:              eventType,
		BusinessID:        businessID,
		DeliveryID:        deliveryID,
		ProviderMessageID: providerMessageID,
		OccurredAt:        details.Timestamp.UTC(),
	}, nil
}

func selectPayload(wire wireEvent) (EventType, json.RawMessage, error) {
	switch wire.EventType {
	case "Delivery":
		if len(wire.Delivery) == 0 || len(wire.Bounce) != 0 || len(wire.Complaint) != 0 {
			break
		}
		return EventTypeDelivery, wire.Delivery, nil
	case "Bounce":
		if len(wire.Bounce) == 0 || len(wire.Delivery) != 0 || len(wire.Complaint) != 0 {
			break
		}
		return EventTypeBounce, wire.Bounce, nil
	case "Complaint":
		if len(wire.Complaint) == 0 || len(wire.Delivery) != 0 || len(wire.Bounce) != 0 {
			break
		}
		return EventTypeComplaint, wire.Complaint, nil
	default:
		return "", nil, fmt.Errorf("%w: unsupported event type %q", ErrInvalidEvent, wire.EventType)
	}
	return "", nil, fmt.Errorf("%w: event payload does not uniquely match type %q", ErrInvalidEvent, wire.EventType)
}

func singleTagEquals(values []string, expected string) bool {
	return len(values) == 1 && values[0] == expected
}

func requiredUUIDTag(tags map[string][]string, name string) (string, error) {
	values := tags[name]
	if len(values) != 1 {
		return "", fmt.Errorf("%w: %s must contain exactly one value", ErrInvalidEvent, name)
	}
	value := strings.TrimSpace(values[0])
	parsed, err := uuid.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%w: %s must be a UUID", ErrInvalidEvent, name)
	}
	return parsed.String(), nil
}
