package operationsmetrics

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEmitterWritesOnlyBoundedOperationalEMF(t *testing.T) {
	var output bytes.Buffer
	emitter, err := NewEmitter(&output, "production", func() time.Time {
		return time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	if err := emitter.Emit(Sample{Category: CategoryWebhook, Values: map[Metric]float64{
		MetricWebhookFailures: 1, MetricProviderLatencyMilliseconds: 125,
	}}); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("decode EMF: %v", err)
	}
	if payload["Environment"] != "production" || payload["Category"] != string(CategoryWebhook) {
		t.Fatalf("bounded dimensions = %#v", payload)
	}
	metadata := payload["_aws"].(map[string]any)
	directive := metadata["CloudWatchMetrics"].([]any)[0].(map[string]any)
	if directive["Namespace"] != Namespace {
		t.Fatalf("namespace = %#v", directive["Namespace"])
	}
	dimensions := directive["Dimensions"].([]any)
	if len(dimensions) != 1 || len(dimensions[0].([]any)) != 2 {
		t.Fatalf("dimensions = %#v", dimensions)
	}
	for _, forbidden := range []string{"business_id", "operation_id", "provider_id", "queue_url", "message_id", "error"} {
		if strings.Contains(strings.ToLower(output.String()), forbidden) {
			t.Fatalf("EMF leaked high-cardinality field %q: %s", forbidden, output.String())
		}
	}
}

func TestEmitterRejectsUnknownDimensionsMetricsAndInvalidValues(t *testing.T) {
	emitter, err := NewEmitter(&bytes.Buffer{}, "test", time.Now)
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	for _, sample := range []Sample{
		{Category: Category("tenant-123"), Values: map[Metric]float64{MetricRepeatedFailures: 1}},
		{Category: CategoryRender, Values: map[Metric]float64{Metric("TenantFailures"): 1}},
		{Category: CategoryRender, Values: map[Metric]float64{MetricRenderFailureRate: math.NaN()}},
		{Category: CategoryRender, Values: map[Metric]float64{}},
		// Queue depth and dead-letter depth are observable only by AWS/SQS; the
		// application must never fabricate queue or DLQ metric values.
		{Category: Category("queue"), Values: map[Metric]float64{MetricQueueAgeSeconds: 12}},
		{Category: CategoryRender, Values: map[Metric]float64{Metric("DLQGrowth"): 1}},
	} {
		if err := emitter.Emit(sample); !errors.Is(err, ErrInvalidSample) {
			t.Fatalf("Emit(%#v) error = %v", sample, err)
		}
	}
}

func TestSQSAttributeDerivationIsBoundedAndOptional(t *testing.T) {
	sent := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	now := sent.Add(90 * time.Second)
	age, ok := SQSQueueAgeSeconds(map[string]string{SQSSentTimestampAttribute: strconv.FormatInt(sent.UnixMilli(), 10)}, now)
	if !ok || age != 90 {
		t.Fatalf("SQSQueueAgeSeconds() = %v, %v; want 90, true", age, ok)
	}
	for _, attributes := range []map[string]string{
		nil,
		{SQSSentTimestampAttribute: ""},
		{SQSSentTimestampAttribute: "not-a-number"},
		{SQSSentTimestampAttribute: "0"},
		{SQSSentTimestampAttribute: strconv.FormatInt(now.Add(time.Minute).UnixMilli(), 10)},
	} {
		if _, ok := SQSQueueAgeSeconds(attributes, now); ok {
			t.Fatalf("SQSQueueAgeSeconds(%#v) accepted an invalid timestamp", attributes)
		}
	}
	count, ok := SQSReceiveCount(map[string]string{SQSReceiveCountAttribute: "3"})
	if !ok || count != 3 {
		t.Fatalf("SQSReceiveCount() = %d, %v; want 3, true", count, ok)
	}
	if _, ok := SQSReceiveCount(map[string]string{SQSReceiveCountAttribute: "x"}); ok {
		t.Fatalf("SQSReceiveCount accepted an invalid count")
	}
	if _, ok := SQSReceiveCount(nil); ok {
		t.Fatalf("SQSReceiveCount accepted missing attributes")
	}
}

func TestEmitRecordOutcomeEmitsPipelineOutcomeAndRepeatedFailures(t *testing.T) {
	var output bytes.Buffer
	sent := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	emitter, err := NewEmitter(&output, "test", func() time.Time { return sent.Add(30 * time.Second) })
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	attributes := map[string]string{
		SQSSentTimestampAttribute: strconv.FormatInt(sent.UnixMilli(), 10),
		SQSReceiveCountAttribute:  "3",
	}
	if err := emitter.EmitRecordOutcome(RecordOutcome{
		Category: CategoryRender, RateMetric: MetricRenderFailureRate, Failed: true, Attributes: attributes,
	}); err != nil {
		t.Fatalf("EmitRecordOutcome() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected one pipeline sample and one repeated-failure sample, got %d: %s", len(lines), output.String())
	}
	var pipeline, recovery map[string]any
	for _, line := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("decode EMF: %v", err)
		}
		switch payload["Category"] {
		case string(CategoryRender):
			pipeline = payload
		case string(CategoryRecovery):
			recovery = payload
		default:
			t.Fatalf("unexpected category %v", payload["Category"])
		}
	}
	if pipeline == nil || pipeline["RenderFailureRate"] != float64(100) || pipeline["QueueAgeSeconds"] != float64(30) {
		t.Fatalf("pipeline sample = %#v", pipeline)
	}
	if recovery == nil || recovery["RepeatedFailures"] != float64(1) {
		t.Fatalf("recovery sample = %#v", recovery)
	}
}

func TestEmitRecordOutcomeSuccessEmitsOneSampleWithoutRepeatedFailures(t *testing.T) {
	var output bytes.Buffer
	emitter, err := NewEmitter(&output, "test", time.Now)
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	if err := emitter.EmitRecordOutcome(RecordOutcome{
		Category: CategoryDelivery, RateMetric: MetricDeliveryFailureRate, Failed: false,
		Attributes: map[string]string{SQSReceiveCountAttribute: "4"},
	}); err != nil {
		t.Fatalf("EmitRecordOutcome() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("decode EMF: %v", err)
	}
	if payload["Category"] != string(CategoryDelivery) || payload["DeliveryFailureRate"] != float64(0) {
		t.Fatalf("pipeline sample = %#v", payload)
	}
	if _, present := payload["QueueAgeSeconds"]; present {
		t.Fatalf("success without a SentTimestamp must not fabricate queue age")
	}
	if strings.Contains(output.String(), "RepeatedFailures") {
		t.Fatalf("successful records must not emit repeated failures: %s", output.String())
	}
}

func TestEmitRecordOutcomeRejectsMismatchedPipelineAndNilEmitter(t *testing.T) {
	emitter, err := NewEmitter(&bytes.Buffer{}, "test", time.Now)
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	for _, outcome := range []RecordOutcome{
		{Category: CategoryRender, RateMetric: MetricDeliveryFailureRate, Failed: true},
		{Category: CategoryDelivery, RateMetric: MetricRenderFailureRate, Failed: true},
		{Category: CategoryWebhook, RateMetric: MetricRenderFailureRate, Failed: true},
		{Category: CategoryRender, RateMetric: MetricRepeatedFailures, Failed: true},
	} {
		if err := emitter.EmitRecordOutcome(outcome); !errors.Is(err, ErrInvalidSample) {
			t.Fatalf("EmitRecordOutcome(%#v) error = %v", outcome, err)
		}
	}
	var nilEmitter *Emitter
	if err := nilEmitter.EmitRecordOutcome(RecordOutcome{
		Category: CategoryRender, RateMetric: MetricRenderFailureRate, Failed: true,
	}); !errors.Is(err, ErrInvalidSample) {
		t.Fatalf("nil emitter error = %v", err)
	}
}

func TestNewRuntimeEmitterFailsOpenWithoutSafeEnvironment(t *testing.T) {
	if emitter := NewRuntimeEmitter(""); emitter != nil {
		t.Fatalf("empty environment must disable emission, got %#v", emitter)
	}
	if emitter := NewRuntimeEmitter("has space"); emitter != nil {
		t.Fatalf("unsafe environment must disable emission, got %#v", emitter)
	}
}

func TestEmitterQueueAgeSecondsUsesEmitterClock(t *testing.T) {
	sent := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	emitter, err := NewEmitter(&bytes.Buffer{}, "test", func() time.Time { return sent.Add(time.Minute) })
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	age, ok := emitter.QueueAgeSeconds(map[string]string{SQSSentTimestampAttribute: strconv.FormatInt(sent.UnixMilli(), 10)})
	if !ok || age != 60 {
		t.Fatalf("QueueAgeSeconds() = %v, %v; want 60, true", age, ok)
	}
	var nilEmitter *Emitter
	if _, ok := nilEmitter.QueueAgeSeconds(nil); ok {
		t.Fatalf("nil emitter must report no age")
	}
}
