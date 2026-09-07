package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/emaildelivery"
	"invoice-backend/pkg/operationsmetrics"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/google/uuid"
)

func TestHandleEmitsBoundedDeliveryOutcomeMetrics(t *testing.T) {
	var output bytes.Buffer
	now := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	emitter, err := operationsmetrics.NewEmitter(&output, "test", func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	attributes := map[string]string{
		operationsmetrics.SQSSentTimestampAttribute: fmt.Sprintf("%d", now.Add(-30*time.Second).UnixMilli()),
		operationsmetrics.SQSReceiveCountAttribute:  "2",
	}
	processor := &metricsRecordingDeliveryProcessor{failSecond: true}
	handler := emailDeliveryHandler{worker: processor, metrics: emitter}

	response, err := handler.Handle(handlerContext(), events.SQSEvent{Records: []events.SQSMessage{
		validSQSEmailDeliveryRecordWithAttributes("message-ok", attributes),
		validSQSEmailDeliveryRecordWithAttributes("message-fail", attributes),
	}})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(response.BatchItemFailures) != 1 {
		t.Fatalf("batch failures = %#v", response.BatchItemFailures)
	}
	samples := decodeEMFPayloads(t, output.String())
	if len(samples) != 3 {
		t.Fatalf("expected two delivery samples and one repeated-failure sample, got %d: %s", len(samples), output.String())
	}
	deliverySamples := 0
	repeatedFailures := 0
	for _, payload := range samples {
		switch payload["Category"] {
		case string(operationsmetrics.CategoryDelivery):
			deliverySamples++
			if _, present := payload["DeliveryFailureRate"]; !present {
				t.Fatalf("delivery sample missing failure rate: %#v", payload)
			}
		case string(operationsmetrics.CategoryRecovery):
			repeatedFailures++
		default:
			t.Fatalf("unexpected category: %#v", payload["Category"])
		}
	}
	if deliverySamples != 2 || repeatedFailures != 1 {
		t.Fatalf("delivery samples = %d, repeated failures = %d", deliverySamples, repeatedFailures)
	}
	for _, forbidden := range []string{"recipient", "delivery_id", "invoice_id"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("EMF leaked high-cardinality field %q", forbidden)
		}
	}
}

func TestHandleNilEmitterKeepsDeliveryBehavior(t *testing.T) {
	processor := &metricsRecordingDeliveryProcessor{}
	handler := emailDeliveryHandler{worker: processor}
	response, err := handler.Handle(handlerContext(), events.SQSEvent{Records: []events.SQSMessage{
		validSQSEmailDeliveryRecordWithAttributes("message-ok", nil),
	}})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(response.BatchItemFailures) != 0 {
		t.Fatalf("batch failures = %#v", response.BatchItemFailures)
	}
}

type metricsRecordingDeliveryProcessor struct {
	failSecond bool
	calls      int
}

func (p *metricsRecordingDeliveryProcessor) Process(
	_ context.Context,
	_ emaildelivery.DeliveryMessage,
	_ string,
) error {
	p.calls++
	if p.failSecond && p.calls == 2 {
		return context.DeadlineExceeded
	}
	return nil
}

func validSQSEmailDeliveryRecordWithAttributes(messageID string, attributes map[string]string) events.SQSMessage {
	record := events.SQSMessage{
		MessageId: messageID,
		Body: fmt.Sprintf(
			`{"schema_version":1,"type":"send_invoice_pdf","business_id":%q,"delivery_id":%q,"invoice_id":%q,"render_job_id":%q}`,
			uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(),
		),
	}
	record.Attributes = attributes
	return record
}

func handlerContext() context.Context {
	return lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{
		AwsRequestID: "email-delivery-request-1",
	})
}

func decodeEMFPayloads(t *testing.T, output string) []map[string]any {
	t.Helper()
	payloads := make([]map[string]any, 0)
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("decode EMF: %v", err)
		}
		payloads = append(payloads, payload)
	}
	return payloads
}
