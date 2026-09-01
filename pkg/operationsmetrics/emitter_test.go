package operationsmetrics

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
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
	} {
		if err := emitter.Emit(sample); !errors.Is(err, ErrInvalidSample) {
			t.Fatalf("Emit(%#v) error = %v", sample, err)
		}
	}
}
