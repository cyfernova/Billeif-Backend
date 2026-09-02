package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/operationsmetrics"

	"github.com/aws/aws-lambda-go/events"
)

func TestEmitGSTRecordMetricsEmitsBoundedQueueAge(t *testing.T) {
	var output bytes.Buffer
	sent := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	now := sent.Add(2 * time.Minute)
	emitter, err := operationsmetrics.NewEmitter(&output, "test", func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	attributes := map[string]string{
		operationsmetrics.SQSSentTimestampAttribute: fmt.Sprintf("%d", sent.UnixMilli()),
	}

	emitGSTRecordMetrics(emitter, events.SQSMessage{
		MessageId: "gst-1", Body: `{"type":"generate_einvoice"}`, Attributes: attributes,
	}, false, logger.NewWithEnv("test"))

	samples := decodeGSTEMFPayloads(t, output.String())
	if len(samples) != 1 {
		t.Fatalf("expected exactly one queue age sample, got %d: %s", len(samples), output.String())
	}
	if samples[0]["Category"] != string(operationsmetrics.CategoryProvider) {
		t.Fatalf("category = %#v", samples[0]["Category"])
	}
	if samples[0]["QueueAgeSeconds"] != float64(120) {
		t.Fatalf("queue age = %#v", samples[0]["QueueAgeSeconds"])
	}
	for _, forbidden := range []string{"payload", "message_id", "body"} {
		if _, present := samples[0][forbidden]; present {
			t.Fatalf("EMF leaked field %q: %#v", forbidden, samples[0])
		}
	}
}

func TestEmitGSTRecordMetricsSkipsUnobservableRecords(t *testing.T) {
	var output bytes.Buffer
	emitter, err := operationsmetrics.NewEmitter(&output, "test", time.Now)
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	log := logger.NewWithEnv("test")
	emitGSTRecordMetrics(emitter, events.SQSMessage{MessageId: "gst-1", Body: `{}`}, false, log)
	emitGSTRecordMetrics(nil, events.SQSMessage{MessageId: "gst-2"}, false, log)
	if output.Len() != 0 {
		t.Fatalf("records without a SentTimestamp or a nil emitter must not emit: %s", output.String())
	}
}

func decodeGSTEMFPayloads(t *testing.T, output string) []map[string]any {
	t.Helper()
	payloads := make([]map[string]any, 0)
	for _, line := range splitGSTLines(output) {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("decode EMF: %v", err)
		}
		payloads = append(payloads, payload)
	}
	return payloads
}

func splitGSTLines(output string) []string {
	lines := make([]string, 0)
	current := ""
	for _, character := range output {
		if character == '\n' {
			if current != "" {
				lines = append(lines, current)
			}
			current = ""
			continue
		}
		current += string(character)
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func TestEmitGSTRecordMetricsCoversFailureOutcomesWithoutInventingRates(t *testing.T) {
	var output bytes.Buffer
	sent := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	now := sent.Add(30 * time.Second)
	emitter, err := operationsmetrics.NewEmitter(&output, "test", func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	log := logger.NewWithEnv("test")
	attributes := func(receiveCount string) map[string]string {
		return map[string]string{
			operationsmetrics.SQSSentTimestampAttribute: fmt.Sprintf("%d", sent.UnixMilli()),
			operationsmetrics.SQSReceiveCountAttribute:  receiveCount,
		}
	}

	// Success: one queue-age sample, no failure or recovery samples.
	emitGSTRecordMetrics(emitter, events.SQSMessage{MessageId: "ok", Attributes: attributes("1")}, false, log)
	// First failure: queue age only, no repeated-failure sample.
	emitGSTRecordMetrics(emitter, events.SQSMessage{MessageId: "first-fail", Attributes: attributes("1")}, true, log)
	// Redelivered failure: queue age plus exactly one recovery sample.
	emitGSTRecordMetrics(emitter, events.SQSMessage{MessageId: "redelivered-fail", Attributes: attributes("3")}, true, log)

	samples := decodeGSTEMFPayloads(t, output.String())
	if len(samples) != 4 {
		t.Fatalf("expected three queue-age samples and one repeated-failure sample, got %d: %s", len(samples), output.String())
	}
	repeatedFailures := 0
	providerSamples := 0
	for _, sample := range samples {
		switch sample["Category"] {
		case string(operationsmetrics.CategoryProvider):
			providerSamples++
			if _, present := sample["QueueAgeSeconds"]; !present {
				t.Fatalf("provider sample missing queue age: %#v", sample)
			}
			for _, forbidden := range []string{"RenderFailureRate", "DeliveryFailureRate"} {
				if _, present := sample[forbidden]; present {
					t.Fatalf("GST records must not invent a %s", forbidden)
				}
			}
		case string(operationsmetrics.CategoryRecovery):
			repeatedFailures++
			if sample["RepeatedFailures"] != float64(1) {
				t.Fatalf("repeated failure sample = %#v", sample)
			}
		default:
			t.Fatalf("unexpected category: %#v", sample["Category"])
		}
	}
	if providerSamples != 3 || repeatedFailures != 1 {
		t.Fatalf("provider samples = %d, repeated failures = %d", providerSamples, repeatedFailures)
	}
}
