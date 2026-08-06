package telemetry

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sync"
	"time"
)

const (
	MetricNamespace            = "Billeif/Voice"
	RuntimeEnvironmentVariable = "ENVIRONMENT"
	RuntimeService             = "voice-runtime"
	ReconcilerService          = "voice-reconciler"
	maxTelemetryEventBytes     = 64 * 1024
)

var (
	ErrInvalidWriter          = errors.New("invalid telemetry writer")
	ErrInvalidDimension       = errors.New("invalid telemetry dimension")
	ErrInvalidOutcome         = errors.New("invalid telemetry outcome")
	ErrInvalidMeasurement     = errors.New("invalid telemetry measurement")
	ErrInvalidSignal          = errors.New("invalid telemetry signal")
	ErrTelemetryEventTooLarge = errors.New("telemetry event exceeds encoded size limit")
	lowCardinalityPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	errorClassPattern         = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,63}$`)
)

// Outcome is the only turn-result metric dimension.
type Outcome string

const (
	OutcomeSuccess   Outcome = "success"
	OutcomeError     Outcome = "error"
	OutcomeCancelled Outcome = "cancelled"
)

// EmitterConfig contains only bounded, low-cardinality dimensions.
type EmitterConfig struct {
	Environment string
	Service     string
	Now         func() time.Time
}

// NewRuntimeEmitter constructs the process-wide AgentCore EMF emitter from
// the required ENVIRONMENT setting. Production bootstrap should call this
// once with os.Stdout and os.LookupEnv, then share the returned emitter across
// turn instrumentation. Construction and emission perform no provider calls.
func NewRuntimeEmitter(writer io.Writer, lookup func(string) (string, bool)) (*Emitter, error) {
	if lookup == nil {
		return nil, ErrInvalidDimension
	}
	environment, ok := lookup(RuntimeEnvironmentVariable)
	if !ok {
		return nil, ErrInvalidDimension
	}
	return NewServiceEmitter(writer, environment, RuntimeService)
}

// NewServiceEmitter constructs an emitter for one fixed, low-cardinality
// Billeif voice component. It is used by non-runtime workers that cannot use
// the runtime-only environment factory.
func NewServiceEmitter(writer io.Writer, environment, service string) (*Emitter, error) {
	return NewEmitter(writer, EmitterConfig{Environment: environment, Service: service})
}

// Signal is one coarse production health counter. Signal names are also the
// exact CloudWatch metric names and are deliberately closed to arbitrary
// caller input.
type Signal string

const (
	SignalSessionLeaks        Signal = "SessionLeaks"
	SignalKVSAllocationErrors Signal = "KVSAllocationErrors"
	SignalICERestarts         Signal = "ICERestarts"
	SignalSarvam429           Signal = "Sarvam429"
	SignalSarvam503           Signal = "Sarvam503"
	SignalDurabilityFailures  Signal = "DurabilityFailures"
)

// SignalRecorder is the narrow fail-open seam used by provider, signaling,
// and reconciler production boundaries.
type SignalRecorder interface {
	RecordSignal(Signal, int64) error
}

// Usage captures metered units for one turn.
type Usage struct {
	InputTokens   int64
	OutputTokens  int64
	TTSCharacters int64
}

// Signals captures counters that are owned by the lifecycle of one turn.
// Process and worker health signals use RecordSignal at their actual
// production boundaries so a quiet turn cannot manufacture zero samples.
type Signals struct {
	DurabilityFailures int64
}

// TurnEvent is one bounded aggregate. It intentionally has no audio or frame
// fields.
type TurnEvent struct {
	Snapshot   TurnSnapshot
	Outcome    Outcome
	ErrorClass string
	Usage      Usage
	Signals    Signals
	Details    map[string]any
}

// Emitter writes CloudWatch Embedded Metric Format JSON to an injected local
// stream. It never calls a provider API.
type Emitter struct {
	mu          sync.Mutex
	writer      io.Writer
	environment string
	service     string
	now         func() time.Time
}

// NewEmitter validates the output stream and fixed metric dimensions.
func NewEmitter(writer io.Writer, config EmitterConfig) (*Emitter, error) {
	if isNilWriter(writer) {
		return nil, ErrInvalidWriter
	}
	if !lowCardinalityPattern.MatchString(config.Environment) || !lowCardinalityPattern.MatchString(config.Service) {
		return nil, ErrInvalidDimension
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Emitter{writer: writer, environment: config.Environment, service: config.Service, now: now}, nil
}

func isNilWriter(writer io.Writer) bool {
	if writer == nil {
		return true
	}
	value := reflect.ValueOf(writer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type metricDefinition struct {
	Name string `json:"Name"`
	Unit string `json:"Unit"`
}

type emfDirective struct {
	Namespace  string             `json:"Namespace"`
	Dimensions [][]string         `json:"Dimensions"`
	Metrics    []metricDefinition `json:"Metrics"`
}

type emfMetadata struct {
	Timestamp         int64          `json:"Timestamp"`
	CloudWatchMetrics []emfDirective `json:"CloudWatchMetrics"`
}

// EmitTurn writes exactly one newline-delimited EMF event.
func (emitter *Emitter) EmitTurn(event TurnEvent) error {
	if emitter == nil || isNilWriter(emitter.writer) {
		return ErrInvalidWriter
	}
	if event.Outcome != OutcomeSuccess && event.Outcome != OutcomeError && event.Outcome != OutcomeCancelled {
		return ErrInvalidOutcome
	}
	if event.Outcome == OutcomeSuccess && event.Snapshot.TerminalBoundary() != TurnCompleted {
		return ErrInvalidOutcome
	}
	if event.Outcome != OutcomeSuccess && event.Snapshot.TerminalBoundary() != TurnCancelled {
		return ErrInvalidOutcome
	}
	if err := validateMeasurements(event.Usage, event.Signals); err != nil {
		return err
	}
	latencies, err := event.Snapshot.DeriveLatencies()
	if err != nil {
		return err
	}

	metrics, definitions := buildMetrics(latencies, event.Usage, event.Signals)
	if len(definitions) > 100 {
		return fmt.Errorf("%w: EMF supports at most 100 metrics", ErrInvalidMeasurement)
	}
	envelope := make(map[string]any, len(metrics)+8)
	envelope["_aws"] = emfMetadata{
		Timestamp: emitter.now().UTC().UnixMilli(),
		CloudWatchMetrics: []emfDirective{{
			Namespace:  MetricNamespace,
			Dimensions: [][]string{{"Environment", "Service", "Outcome"}},
			Metrics:    definitions,
		}},
	}
	envelope["Environment"] = emitter.environment
	envelope["Service"] = emitter.service
	envelope["Outcome"] = string(event.Outcome)
	envelope["correlation_id"] = event.Snapshot.CorrelationID
	for name, value := range metrics {
		envelope[name] = value
	}
	if event.Outcome != OutcomeSuccess || ShouldSampleSuccess(event.Snapshot.CorrelationID) {
		envelope["trace"] = buildTrace(event)
	}

	return emitter.writeEnvelope(envelope)
}

// RecordSignal emits one event-backed health counter. Runtime and reconciler
// services have disjoint allowlists so a producer cannot silently publish a
// metric under the wrong Service dimension.
func (emitter *Emitter) RecordSignal(signal Signal, value int64) error {
	if emitter == nil || isNilWriter(emitter.writer) {
		return ErrInvalidWriter
	}
	if value <= 0 {
		return ErrInvalidMeasurement
	}
	outcome := OutcomeError
	switch emitter.service {
	case RuntimeService:
		switch signal {
		case SignalKVSAllocationErrors, SignalSarvam429, SignalSarvam503, SignalDurabilityFailures:
		case SignalICERestarts:
			outcome = OutcomeSuccess
		default:
			return ErrInvalidSignal
		}
	case ReconcilerService:
		if signal != SignalSessionLeaks {
			return ErrInvalidSignal
		}
	default:
		return ErrInvalidSignal
	}
	envelope := map[string]any{
		"_aws": emfMetadata{
			Timestamp: emitter.now().UTC().UnixMilli(),
			CloudWatchMetrics: []emfDirective{{
				Namespace:  MetricNamespace,
				Dimensions: [][]string{{"Environment", "Service", "Outcome"}},
				Metrics:    []metricDefinition{{Name: string(signal), Unit: "Count"}},
			}},
		},
		"Environment":  emitter.environment,
		"Service":      emitter.service,
		"Outcome":      string(outcome),
		string(signal): value,
	}
	return emitter.writeEnvelope(envelope)
}

func (emitter *Emitter) writeEnvelope(envelope map[string]any) error {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode telemetry event: %w", err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maxTelemetryEventBytes {
		return ErrTelemetryEventTooLarge
	}
	emitter.mu.Lock()
	defer emitter.mu.Unlock()
	written, err := emitter.writer.Write(encoded)
	if err != nil {
		return fmt.Errorf("write telemetry aggregate: %w", err)
	}
	if written != len(encoded) {
		return io.ErrShortWrite
	}
	return nil
}

func validateMeasurements(usage Usage, signals Signals) error {
	values := []int64{
		usage.InputTokens, usage.OutputTokens, usage.TTSCharacters,
		signals.DurabilityFailures,
	}
	for _, value := range values {
		if value < 0 {
			return ErrInvalidMeasurement
		}
	}
	return nil
}

func buildMetrics(latencies Latencies, usage Usage, signals Signals) (map[string]any, []metricDefinition) {
	metrics := map[string]any{
		"DurabilityFailures": signals.DurabilityFailures,
		"InputTokens":        usage.InputTokens,
		"OutputTokens":       usage.OutputTokens,
		"TTSCharacters":      usage.TTSCharacters,
	}
	definitions := []metricDefinition{
		{Name: "DurabilityFailures", Unit: "Count"},
		{Name: "InputTokens", Unit: "Count"},
		{Name: "OutputTokens", Unit: "Count"},
		{Name: "TTSCharacters", Unit: "Count"},
	}
	addLatencyMetric := func(name, unit string, latency OptionalLatency, divisor time.Duration) {
		if !latency.Present {
			return
		}
		metrics[name] = float64(latency.Duration) / float64(divisor)
		definitions = append(definitions, metricDefinition{Name: name, Unit: unit})
	}
	addLatencyMetric("EndOfSpeechToFirstAudioMilliseconds", "Milliseconds", latencies.EndOfSpeechToFirstAudio, time.Millisecond)
	addLatencyMetric("STTFinalLatencyMilliseconds", "Milliseconds", latencies.STTFinal, time.Millisecond)
	addLatencyMetric("LLMFirstTokenLatencyMilliseconds", "Milliseconds", latencies.LLMFirstToken, time.Millisecond)
	addLatencyMetric("TTSFirstAudioLatencyMilliseconds", "Milliseconds", latencies.TTSFirstAudio, time.Millisecond)
	addLatencyMetric("InterruptToPlaybackStopMilliseconds", "Milliseconds", latencies.InterruptToPlaybackStop, time.Millisecond)
	addLatencyMetric("STTSeconds", "Seconds", latencies.STTAudio, time.Second)
	return metrics, definitions
}

func buildTrace(event TurnEvent) map[string]any {
	boundaries := make(map[string]string, len(event.Snapshot.timestamps))
	for boundary, at := range event.Snapshot.timestamps {
		boundaries[string(boundary)] = at.UTC().Format(time.RFC3339Nano)
	}
	trace := map[string]any{
		"boundaries": boundaries,
		"details":    RedactDetails(event.Details),
	}
	if event.Snapshot.interruptedAt != nil {
		trace["interrupt_started"] = event.Snapshot.interruptedAt.UTC().Format(time.RFC3339Nano)
		trace["playback_stopped"] = event.Snapshot.playbackStop.UTC().Format(time.RFC3339Nano)
	}
	if event.Outcome == OutcomeError {
		if errorClassPattern.MatchString(event.ErrorClass) && !containsSensitiveValue(event.ErrorClass) {
			trace["error_class"] = event.ErrorClass
		} else {
			trace["error_class"] = "unknown_error"
		}
	}
	return trace
}

// ShouldSampleSuccess deterministically retains exactly one of 100 hash
// buckets. The same opaque correlation ID always produces the same decision.
func ShouldSampleSuccess(correlationID string) bool {
	digest := sha256.Sum256([]byte(correlationID))
	return binary.BigEndian.Uint64(digest[:8])%100 == 0
}
