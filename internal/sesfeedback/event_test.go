package sesfeedback

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseEventAcceptsStrictDeliveryBounceAndComplaintCorrelation(t *testing.T) {
	businessID, deliveryID := uuid.NewString(), uuid.NewString()
	for _, eventType := range []string{"Delivery", "Bounce", "Complaint"} {
		t.Run(eventType, func(t *testing.T) {
			body := validFeedbackJSON(eventType, businessID, deliveryID, "provider-message-1")

			event, err := ParseEvent([]byte(body), ValidationOptions{
				SendingAccountID: "123456789012",
				ConfigurationSet: "Billeif-prod-ses-config",
			})

			if err != nil {
				t.Fatalf("parse %s: %v", eventType, err)
			}
			if event.BusinessID != businessID || event.DeliveryID != deliveryID ||
				event.ProviderMessageID != "provider-message-1" ||
				event.Type != EventType(strings.ToLower(eventType)) ||
				!event.OccurredAt.Equal(time.Date(2026, time.July, 30, 10, 1, 2, 0, time.UTC)) {
				t.Fatalf("event = %#v", event)
			}
		})
	}
}

func TestParseEventAcceptsAWSAddedFields(t *testing.T) {
	businessID, deliveryID := uuid.NewString(), uuid.NewString()
	body := validFeedbackJSON("Delivery", businessID, deliveryID, "provider-message-1")
	body = strings.Replace(body, `"mail":{`, `"newTopLevel":{"future":true},"mail":{"newMailField":"future",`, 1)
	body = strings.Replace(body, `"delivery":{`, `"delivery":{"newDeliveryField":{"future":true},`, 1)

	event, err := ParseEvent([]byte(body), ValidationOptions{
		SendingAccountID: "123456789012",
		ConfigurationSet: "Billeif-prod-ses-config",
	})

	if err != nil {
		t.Fatalf("parse event with AWS-added fields: %v", err)
	}
	if event.DeliveryID != deliveryID {
		t.Fatalf("delivery id = %q, want %q", event.DeliveryID, deliveryID)
	}
}

func TestParseEventRejectsUntrustedOrAmbiguousCorrelation(t *testing.T) {
	businessID, deliveryID := uuid.NewString(), uuid.NewString()
	valid := validFeedbackJSON("Delivery", businessID, deliveryID, "provider-message-1")
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown event", body: strings.Replace(valid, `"Delivery"`, `"Open"`, 1)},
		{name: "wrong account", body: strings.Replace(valid, `"123456789012"`, `"999999999999"`, 1)},
		{name: "wrong configuration set", body: strings.Replace(valid, `"Billeif-prod-ses-config"`, `"other"`, 1)},
		{name: "duplicate business tag", body: strings.Replace(valid, fmt.Sprintf(`[%q]`, businessID), fmt.Sprintf(`[%q,%q]`, businessID, uuid.NewString()), 1)},
		{name: "invalid delivery tag", body: strings.Replace(valid, deliveryID, "not-a-uuid", 1)},
		{name: "missing provider message", body: strings.Replace(valid, `"provider-message-1"`, `""`, 1)},
		{name: "provider message too long", body: strings.Replace(valid, `"provider-message-1"`, `"`+strings.Repeat("m", 256)+`"`, 1)},
		{name: "missing type payload", body: strings.Replace(valid, `"delivery":{"timestamp":"2026-07-30T10:01:02Z"}`, `"delivery":null`, 1)},
		{name: "wrong type payload", body: strings.Replace(valid, `"delivery":`, `"bounce":`, 1)},
		{name: "multiple type payloads", body: strings.TrimSuffix(valid, "}") + `,"bounce":{"timestamp":"2026-07-30T10:01:03Z"}}`},
		{name: "trailing JSON", body: valid + `{}`},
		{name: "event too large", body: valid + strings.Repeat(" ", MaxEventBytes)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseEvent([]byte(test.body), ValidationOptions{
				SendingAccountID: "123456789012",
				ConfigurationSet: "Billeif-prod-ses-config",
			}); err == nil {
				t.Fatal("invalid feedback event returned nil")
			}
		})
	}
}

func validFeedbackJSON(
	eventType, businessID, deliveryID, providerMessageID string,
) string {
	payloadName := strings.ToLower(eventType)
	return fmt.Sprintf(
		`{"eventType":%q,"mail":{"timestamp":"2026-07-30T10:00:00Z","source":"billing@example.com","sourceArn":"arn:aws:ses:ap-south-1:123456789012:identity/example.com","sendingAccountId":"123456789012","messageId":%q,"destination":["buyer@example.com"],"headersTruncated":false,"headers":[],"commonHeaders":{},"tags":{"ses:configuration-set":["Billeif-prod-ses-config"],"business_id":[%q],"delivery_id":[%q]}},"%s":{"timestamp":"2026-07-30T10:01:02Z"}}`,
		eventType, providerMessageID, businessID, deliveryID, payloadName,
	)
}
