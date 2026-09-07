package operationsmetrics

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Namespace = "Billeif/Operations"

var ErrInvalidSample = errors.New("invalid operational metric sample")

type Category string

const (
	CategoryRecovery       Category = "recovery"
	CategoryReconciliation Category = "reconciliation"
	CategoryProvider       Category = "provider"
	CategoryWebhook        Category = "webhook"
	CategorySchedule       Category = "schedule"
	CategoryRender         Category = "render"
	CategoryDelivery       Category = "delivery"
)

type Metric string

const (
	MetricQueueAgeSeconds             Metric = "QueueAgeSeconds"
	MetricRepeatedFailures            Metric = "RepeatedFailures"
	MetricReconciliationBacklog       Metric = "ReconciliationBacklog"
	MetricProviderLatencyMilliseconds Metric = "ProviderLatencyMilliseconds"
	MetricWebhookFailures             Metric = "WebhookFailures"
	MetricRecurringScheduleFailures   Metric = "RecurringScheduleFailures"
	MetricRenderFailureRate           Metric = "RenderFailureRate"
	MetricDeliveryFailureRate         Metric = "DeliveryFailureRate"
)

// Queue depth and dead-letter depth are observable only by AWS/SQS native
// metrics; consumers cannot truthfully emit them. They stay covered by the
// existing worker_queue_age and worker_dlq_messages alarms.
var validCategories = map[Category]struct{}{
	CategoryRecovery: {}, CategoryReconciliation: {}, CategoryProvider: {},
	CategoryWebhook: {}, CategorySchedule: {}, CategoryRender: {}, CategoryDelivery: {},
}

var metricUnits = map[Metric]string{
	MetricQueueAgeSeconds: "Seconds", MetricRepeatedFailures: "Count",
	MetricReconciliationBacklog: "Count", MetricProviderLatencyMilliseconds: "Milliseconds",
	MetricWebhookFailures: "Count", MetricRecurringScheduleFailures: "Count",
	MetricRenderFailureRate: "Percent", MetricDeliveryFailureRate: "Percent",
}

type Sample struct {
	Category Category
	Values   map[Metric]float64
}

// RecordOutcome is the bounded per-record outcome of one queued work item.
// Attributes are SQS message attributes, never payloads or message bodies.
type RecordOutcome struct {
	Category   Category
	RateMetric Metric
	Failed     bool
	Attributes map[string]string
}

type Emitter struct {
	writer      io.Writer
	environment string
	now         func() time.Time
	mu          sync.Mutex
}

func NewEmitter(writer io.Writer, environment string, now func() time.Time) (*Emitter, error) {
	environment = strings.TrimSpace(environment)
	if writer == nil || now == nil || environment == "" || len(environment) > 32 || !safeDimension(environment) {
		return nil, ErrInvalidSample
	}
	return &Emitter{writer: writer, environment: environment, now: now}, nil
}

// NewRuntimeEmitter constructs the process-wide operational emitter writing
// EMF to stdout. It returns nil, disabling emission, when the environment is
// not a safe bounded dimension: telemetry must never fail a runtime.
func NewRuntimeEmitter(environment string) *Emitter {
	emitter, err := NewEmitter(os.Stdout, environment, time.Now)
	if err != nil {
		return nil
	}
	return emitter
}

func (e *Emitter) Emit(sample Sample) error {
	if e == nil || e.writer == nil || e.now == nil || len(sample.Values) == 0 {
		return ErrInvalidSample
	}
	if _, ok := validCategories[sample.Category]; !ok {
		return ErrInvalidSample
	}
	metrics := make([]any, 0, len(sample.Values))
	payload := map[string]any{
		"Environment": e.environment,
		"Category":    string(sample.Category),
	}
	for metric, value := range sample.Values {
		unit, ok := metricUnits[metric]
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return ErrInvalidSample
		}
		if (metric == MetricRenderFailureRate || metric == MetricDeliveryFailureRate) && value > 100 {
			return ErrInvalidSample
		}
		metrics = append(metrics, map[string]any{"Name": string(metric), "Unit": unit})
		payload[string(metric)] = value
	}
	payload["_aws"] = map[string]any{
		"Timestamp": e.now().UTC().UnixMilli(),
		"CloudWatchMetrics": []any{map[string]any{
			"Namespace": Namespace, "Dimensions": [][]string{{"Environment", "Category"}}, "Metrics": metrics,
		}},
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return json.NewEncoder(e.writer).Encode(payload)
}

// EmitRecordOutcome emits one truthful per-record sample for a queued work
// record: the pipeline failure rate and, when the SQS attributes carry a
// SentTimestamp, the observed queue age. A failed record that SQS has already
// redelivered at least once additionally emits one RepeatedFailures sample.
// Records never fabricate queue age or dead-letter values.
func (e *Emitter) EmitRecordOutcome(outcome RecordOutcome) error {
	if e == nil || e.writer == nil || e.now == nil {
		return ErrInvalidSample
	}
	switch {
	case outcome.Category == CategoryRender && outcome.RateMetric == MetricRenderFailureRate:
	case outcome.Category == CategoryDelivery && outcome.RateMetric == MetricDeliveryFailureRate:
	default:
		return ErrInvalidSample
	}
	values := map[Metric]float64{outcome.RateMetric: 0}
	if outcome.Failed {
		values[outcome.RateMetric] = 100
	}
	if age, ok := SQSQueueAgeSeconds(outcome.Attributes, e.now()); ok {
		values[MetricQueueAgeSeconds] = age
	}
	if err := e.Emit(Sample{Category: outcome.Category, Values: values}); err != nil {
		return err
	}
	if !outcome.Failed {
		return nil
	}
	if count, ok := SQSReceiveCount(outcome.Attributes); !ok || count < 2 {
		return nil
	}
	return e.EmitRepeatedFailure()
}

// EmitRepeatedFailure emits one RepeatedFailures recovery sample for work that
// failed after the transport already redelivered it at least once. It is the
// bounded, pipeline-agnostic recovery signal behind the repeated-failure alarm.
func (e *Emitter) EmitRepeatedFailure() error {
	if e == nil || e.writer == nil || e.now == nil {
		return ErrInvalidSample
	}
	return e.Emit(Sample{Category: CategoryRecovery, Values: map[Metric]float64{MetricRepeatedFailures: 1}})
}

// SQS attribute keys used to derive bounded operational metric values.
const (
	SQSSentTimestampAttribute = "SentTimestamp"
	SQSReceiveCountAttribute  = "ApproximateReceiveCount"
)

// SQSQueueAgeSeconds derives the observed age of one SQS message in seconds
// from its SentTimestamp attribute. It reports ok=false for missing, invalid,
// zero, or future timestamps instead of inventing a value.
func SQSQueueAgeSeconds(attributes map[string]string, now time.Time) (float64, bool) {
	if len(attributes) == 0 || now.IsZero() {
		return 0, false
	}
	sentMilliseconds, err := strconv.ParseInt(strings.TrimSpace(attributes[SQSSentTimestampAttribute]), 10, 64)
	if err != nil || sentMilliseconds <= 0 {
		return 0, false
	}
	age := now.Sub(time.UnixMilli(sentMilliseconds)).Seconds()
	if age < 0 || math.IsNaN(age) || math.IsInf(age, 0) {
		return 0, false
	}
	return age, true
}

// SQSReceiveCount returns the approximate receive count of one SQS message,
// or ok=false when the attribute is missing or invalid.
func SQSReceiveCount(attributes map[string]string) (int, bool) {
	if len(attributes) == 0 {
		return 0, false
	}
	count, err := strconv.Atoi(strings.TrimSpace(attributes[SQSReceiveCountAttribute]))
	if err != nil || count < 1 {
		return 0, false
	}
	return count, true
}

// QueueAgeSeconds derives the observed age of one SQS message using this
// emitter's clock. It reports ok=false for missing or invalid attributes.
func (e *Emitter) QueueAgeSeconds(attributes map[string]string) (float64, bool) {
	if e == nil || e.now == nil {
		return 0, false
	}
	return SQSQueueAgeSeconds(attributes, e.now())
}

func safeDimension(value string) bool {
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}
