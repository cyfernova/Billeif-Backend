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
	}, logger.NewWithEnv("test"))

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
	emitGSTRecordMetrics(emitter, events.SQSMessage{MessageId: "gst-1", Body: `{}`}, log)
	emitGSTRecordMetrics(nil, events.SQSMessage{MessageId: "gst-2"}, log)
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
