package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/operationsmetrics"

	"github.com/aws/aws-lambda-go/events"
)

func TestProcessSQSEventEmitsBoundedRenderOutcomeMetrics(t *testing.T) {
	log := logger.NewWithEnv("test")
	sent := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	now := sent.Add(45 * time.Second)
	var output bytes.Buffer
	emitter, err := operationsmetrics.NewEmitter(&output, "test", func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	attributes := map[string]string{
		operationsmetrics.SQSSentTimestampAttribute: strconv.FormatInt(sent.UnixMilli(), 10),
		operationsmetrics.SQSReceiveCountAttribute:  "3",
	}
	calls := 0
	process := func(ctx context.Context, body, owner string) error {
		calls++
		if calls == 2 {
			return context.DeadlineExceeded
		}
		return nil
	}

	response := processSQSEvent(context.Background(), events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "ok", Body: `{"type":"generate_document_pdf"}`, Attributes: attributes},
		{MessageId: "failed", Body: `{"type":"generate_document_pdf"}`, Attributes: attributes},
	}}, process, emitter, log)

	if len(response.BatchItemFailures) != 1 || response.BatchItemFailures[0].ItemIdentifier != "failed" {
		t.Fatalf("batch failures = %#v", response.BatchItemFailures)
	}
	lines := splitEMFLines(t, output.String())
	if len(lines) != 3 {
		t.Fatalf("expected two render samples and one repeated-failure sample, got %d: %v", len(lines), lines)
	}
	renderSamples := 0
	repeatedFailures := 0
	for _, payload := range lines {
		switch payload["Category"] {
		case string(operationsmetrics.CategoryRender):
			renderSamples++
			if _, present := payload["RenderFailureRate"]; !present {
				t.Fatalf("render sample missing failure rate: %#v", payload)
			}
			if _, present := payload["QueueAgeSeconds"]; !present {
				t.Fatalf("render sample missing queue age: %#v", payload)
			}
		case string(operationsmetrics.CategoryRecovery):
			if payload["RepeatedFailures"] == float64(1) {
				repeatedFailures++
			} else {
				t.Fatalf("unexpected recovery sample: %#v", payload)
			}
		default:
			t.Fatalf("unexpected category: %#v", payload["Category"])
		}
	}
	if renderSamples != 2 {
		t.Fatalf("expected exactly one render sample per record, got %d", renderSamples)
	}
	if repeatedFailures != 1 {
		t.Fatalf("expected exactly one repeated-failure sample for the redelivered failure")
	}
}

func TestProcessSQSEventNilEmitterKeepsProcessing(t *testing.T) {
	log := logger.NewWithEnv("test")
	process := func(ctx context.Context, body, owner string) error { return nil }
	response := processSQSEvent(context.Background(), events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "ok", Body: `{"type":"generate_document_pdf"}`},
	}}, process, nil, log)
	if len(response.BatchItemFailures) != 0 {
		t.Fatalf("batch failures = %#v", response.BatchItemFailures)
	}
}

func splitEMFLines(t *testing.T, output string) []map[string]any {
	t.Helper()
	lines := make([]map[string]any, 0)
	for _, line := range splitLines(output) {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("decode EMF: %v", err)
		}
		lines = append(lines, payload)
	}
	return lines
}

func splitLines(output string) []string {
	result := make([]string, 0)
	for _, line := range bytes.Split([]byte(output), []byte("\n")) {
		if len(line) > 0 {
			result = append(result, string(line))
		}
	}
	return result
}
