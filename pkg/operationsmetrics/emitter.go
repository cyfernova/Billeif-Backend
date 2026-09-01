package operationsmetrics

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"time"
)

const Namespace = "Billeif/Operations"

var ErrInvalidSample = errors.New("invalid operational metric sample")

type Category string

const (
	CategoryQueue          Category = "queue"
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
	MetricDLQGrowth                   Metric = "DLQGrowth"
	MetricReconciliationBacklog       Metric = "ReconciliationBacklog"
	MetricProviderLatencyMilliseconds Metric = "ProviderLatencyMilliseconds"
	MetricWebhookFailures             Metric = "WebhookFailures"
	MetricRecurringScheduleFailures   Metric = "RecurringScheduleFailures"
	MetricRenderFailureRate           Metric = "RenderFailureRate"
	MetricDeliveryFailureRate         Metric = "DeliveryFailureRate"
)

var validCategories = map[Category]struct{}{
	CategoryQueue: {}, CategoryRecovery: {}, CategoryReconciliation: {}, CategoryProvider: {},
	CategoryWebhook: {}, CategorySchedule: {}, CategoryRender: {}, CategoryDelivery: {},
}

var metricUnits = map[Metric]string{
	MetricQueueAgeSeconds: "Seconds", MetricRepeatedFailures: "Count", MetricDLQGrowth: "Count",
	MetricReconciliationBacklog: "Count", MetricProviderLatencyMilliseconds: "Milliseconds",
	MetricWebhookFailures: "Count", MetricRecurringScheduleFailures: "Count",
	MetricRenderFailureRate: "Percent", MetricDeliveryFailureRate: "Percent",
}

type Sample struct {
	Category Category
	Values   map[Metric]float64
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

func safeDimension(value string) bool {
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}
