package telemetry

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEmitterWritesOneBoundedAggregatePerTurn(t *testing.T) {
	var output bytes.Buffer
	emitter, err := NewEmitter(&output, EmitterConfig{Environment: "dev", Service: "voice-runtime"})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	snapshot := completedSnapshot(t, "turn-aggregate-01")
	event := TurnEvent{
		Snapshot: snapshot,
		Outcome:  OutcomeSuccess,
		Usage: Usage{
			InputTokens:   120,
			OutputTokens:  45,
			TTSCharacters: 250,
		},
		Signals: Signals{DurabilityFailures: 1},
		Details: map[string]any{"audio_frame": "must-not-be-logged"},
	}
	if err := emitter.EmitTurn(event); err != nil {
		t.Fatalf("EmitTurn() error = %v", err)
	}

	lines := nonEmptyLines(output.String())
	if len(lines) != 1 {
		t.Fatalf("log events = %d, want exactly one: %q", len(lines), output.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope["correlation_id"] != snapshot.CorrelationID {
		t.Fatalf("correlation_id = %#v", envelope["correlation_id"])
	}
	if _, exists := envelope["trace"]; exists {
		t.Fatal("unsampled successful aggregate unexpectedly contains a detailed trace")
	}
	for _, forbiddenKey := range []string{"audio", "frame", "audio_frame", "transcript", "answer"} {
		if _, exists := envelope[forbiddenKey]; exists {
			t.Errorf("aggregate contains forbidden per-frame/content key %q", forbiddenKey)
		}
	}

	awsMetadata, ok := envelope["_aws"].(map[string]any)
	if !ok {
		t.Fatalf("_aws = %#v", envelope["_aws"])
	}
	directives, ok := awsMetadata["CloudWatchMetrics"].([]any)
	if !ok || len(directives) != 1 {
		t.Fatalf("CloudWatchMetrics = %#v", awsMetadata["CloudWatchMetrics"])
	}
	directive := directives[0].(map[string]any)
	if directive["Namespace"] != MetricNamespace {
		t.Fatalf("Namespace = %#v, want %q", directive["Namespace"], MetricNamespace)
	}
	dimensions := directive["Dimensions"].([]any)
	if len(dimensions) != 1 {
		t.Fatalf("Dimensions = %#v", dimensions)
	}
	dimensionNames := stringsFromAny(dimensions[0].([]any))
	if !slices.Equal(dimensionNames, []string{"Environment", "Service", "Outcome"}) {
		t.Fatalf("dimension names = %v", dimensionNames)
	}
	if slices.Contains(dimensionNames, "correlation_id") || slices.Contains(dimensionNames, "CorrelationID") {
		t.Fatal("correlation ID must remain metadata, never a metric dimension")
	}

	metricDefinitions := directives[0].(map[string]any)["Metrics"].([]any)
	if len(metricDefinitions) == 0 || len(metricDefinitions) > 100 {
		t.Fatalf("metric definitions = %d, want 1..100", len(metricDefinitions))
	}
	units := make(map[string]string, len(metricDefinitions))
	for _, definitionValue := range metricDefinitions {
		definition := definitionValue.(map[string]any)
		name, _ := definition["Name"].(string)
		unit, _ := definition["Unit"].(string)
		if name == "" || unit == "" || unit == "None" {
			t.Fatalf("metric has no explicit meaningful unit: %#v", definition)
		}
		units[name] = unit
	}
	for _, required := range []string{
		"EndOfSpeechToFirstAudioMilliseconds", "STTFinalLatencyMilliseconds",
		"LLMFirstTokenLatencyMilliseconds", "TTSFirstAudioLatencyMilliseconds",
		"InterruptToPlaybackStopMilliseconds", "DurabilityFailures", "InputTokens",
		"OutputTokens", "TTSCharacters", "STTSeconds",
	} {
		if units[required] == "" {
			t.Errorf("missing metric definition %q", required)
		}
		if _, ok := envelope[required]; !ok {
			t.Errorf("missing aggregate metric value %q", required)
		}
	}
}

func TestEmitterWritesOnlyRealProductionHealthSignals(t *testing.T) {
	var output bytes.Buffer
	emitter, err := NewEmitter(&output, EmitterConfig{Environment: "prod", Service: RuntimeService})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	runtimeSignals := []Signal{
		SignalKVSAllocationErrors, SignalICERestarts, SignalSarvam429, SignalSarvam503, SignalDurabilityFailures,
	}
	for _, signal := range runtimeSignals {
		if err := emitter.RecordSignal(signal, 1); err != nil {
			t.Fatalf("RecordSignal(%s) error = %v", signal, err)
		}
	}
	if err := emitter.RecordSignal(SignalSessionLeaks, 2); !errors.Is(err, ErrInvalidSignal) {
		t.Fatalf("runtime RecordSignal(SessionLeaks) error = %v, want ErrInvalidSignal", err)
	}
	if err := emitter.RecordSignal(SignalSarvam429, 0); !errors.Is(err, ErrInvalidMeasurement) {
		t.Fatalf("RecordSignal(zero) error = %v, want ErrInvalidMeasurement", err)
	}

	lines := nonEmptyLines(output.String())
	if len(lines) != len(runtimeSignals) {
		t.Fatalf("signal events = %d, want %d: %q", len(lines), len(runtimeSignals), output.String())
	}
	for index, signal := range runtimeSignals {
		var envelope map[string]any
		if err := json.Unmarshal([]byte(lines[index]), &envelope); err != nil {
			t.Fatalf("decode signal %s: %v", signal, err)
		}
		if envelope[string(signal)] != float64(1) || envelope["Service"] != RuntimeService {
			t.Fatalf("signal envelope %s = %#v", signal, envelope)
		}
		wantOutcome := string(OutcomeError)
		if signal == SignalICERestarts {
			wantOutcome = string(OutcomeSuccess)
		}
		if envelope["Outcome"] != wantOutcome {
			t.Fatalf("signal %s outcome = %#v, want %q", signal, envelope["Outcome"], wantOutcome)
		}
		if _, exists := envelope["correlation_id"]; exists {
			t.Fatalf("process signal %s contains a correlation ID", signal)
		}
	}
}

func TestReconcilerEmitterAcceptsOnlySessionLeakSignals(t *testing.T) {
	var output bytes.Buffer
	emitter, err := NewServiceEmitter(&output, "test", ReconcilerService)
	if err != nil {
		t.Fatalf("NewServiceEmitter() error = %v", err)
	}
	if err := emitter.RecordSignal(SignalSessionLeaks, 3); err != nil {
		t.Fatalf("RecordSignal(SessionLeaks) error = %v", err)
	}
	if err := emitter.RecordSignal(SignalSarvam503, 1); !errors.Is(err, ErrInvalidSignal) {
		t.Fatalf("reconciler RecordSignal(Sarvam503) error = %v, want ErrInvalidSignal", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(nonEmptyLines(output.String())[0]), &envelope); err != nil {
		t.Fatalf("decode reconciler signal: %v", err)
	}
	if envelope["Service"] != ReconcilerService || envelope[string(SignalSessionLeaks)] != float64(3) || envelope["Outcome"] != string(OutcomeError) {
		t.Fatalf("reconciler signal envelope = %#v", envelope)
	}
}

func TestNewRuntimeEmitterRequiresEnvironmentAndFixesTheServiceDimension(t *testing.T) {
	var output bytes.Buffer
	requested := make([]string, 0, 1)
	emitter, err := NewRuntimeEmitter(&output, func(name string) (string, bool) {
		requested = append(requested, name)
		if name == RuntimeEnvironmentVariable {
			return "prod", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("NewRuntimeEmitter() error = %v", err)
	}
	if !slices.Equal(requested, []string{RuntimeEnvironmentVariable}) {
		t.Fatalf("environment keys requested = %v", requested)
	}
	if err := emitter.EmitTurn(TurnEvent{Snapshot: completedSnapshot(t, "turn-runtime-factory"), Outcome: OutcomeSuccess}); err != nil {
		t.Fatalf("EmitTurn() error = %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(nonEmptyLines(output.String())[0]), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope["Environment"] != "prod" || envelope["Service"] != RuntimeService {
		t.Fatalf("runtime dimensions = Environment:%#v Service:%#v", envelope["Environment"], envelope["Service"])
	}

	for name, lookup := range map[string]func(string) (string, bool){
		"nil lookup": nil,
		"missing environment": func(string) (string, bool) {
			return "", false
		},
		"invalid environment": func(string) (string, bool) {
			return "PROD/tenant", true
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewRuntimeEmitter(io.Discard, lookup); !errors.Is(err, ErrInvalidDimension) {
				t.Fatalf("NewRuntimeEmitter() error = %v, want ErrInvalidDimension", err)
			}
		})
	}
}

func TestEmitterSamplesExactlyByDeterministicOnePercentRule(t *testing.T) {
	var sampledID, unsampledID string
	for index := 0; sampledID == "" || unsampledID == ""; index++ {
		candidate := fmt.Sprintf("turn-sample-%05d", index)
		if ShouldSampleSuccess(candidate) {
			sampledID = candidate
		} else {
			unsampledID = candidate
		}
	}
	if !ShouldSampleSuccess(sampledID) || ShouldSampleSuccess(unsampledID) {
		t.Fatal("success sampling is not deterministic")
	}

	var output bytes.Buffer
	emitter, err := NewEmitter(&output, EmitterConfig{Environment: "prod", Service: "voice-runtime"})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	for _, correlationID := range []string{sampledID, unsampledID} {
		if err := emitter.EmitTurn(TurnEvent{
			Snapshot: completedSnapshot(t, correlationID),
			Outcome:  OutcomeSuccess,
			Details:  map[string]any{"safe": "sample detail"},
		}); err != nil {
			t.Fatalf("EmitTurn(%s) error = %v", correlationID, err)
		}
	}
	lines := nonEmptyLines(output.String())
	if len(lines) != 2 {
		t.Fatalf("events = %d, want 2", len(lines))
	}
	var first, second map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &first)
	_ = json.Unmarshal([]byte(lines[1]), &second)
	if _, ok := first["trace"]; !ok {
		t.Fatal("sampled success has no detailed trace")
	}
	if _, ok := second["trace"]; ok {
		t.Fatal("unsampled success has a detailed trace")
	}
}

func TestEmitterRetainsAllSafeErrorTracesWithoutContent(t *testing.T) {
	var output bytes.Buffer
	emitter, err := NewEmitter(&output, EmitterConfig{Environment: "prod", Service: "voice-runtime"})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	correlationID := firstUnsampledID()
	snapshot := cancelledSnapshot(t, correlationID)
	if err := emitter.EmitTurn(TurnEvent{
		Snapshot:   snapshot,
		Outcome:    OutcomeError,
		ErrorClass: "sarvam_unavailable",
		Details: map[string]any{
			"transcript":        "private customer utterance",
			"answer":            "private agent response",
			"provider_response": "secret provider body",
			"safe_stage":        "tts_connect",
		},
	}); err != nil {
		t.Fatalf("EmitTurn() error = %v", err)
	}
	line := nonEmptyLines(output.String())[0]
	for _, forbidden := range []string{"private customer utterance", "private agent response", "secret provider body"} {
		if strings.Contains(line, forbidden) {
			t.Errorf("error trace leaked %q: %s", forbidden, line)
		}
	}
	var envelope map[string]any
	_ = json.Unmarshal([]byte(line), &envelope)
	trace, ok := envelope["trace"].(map[string]any)
	if !ok {
		t.Fatalf("unsampled error trace missing: %#v", envelope["trace"])
	}
	if trace["error_class"] != "sarvam_unavailable" {
		t.Fatalf("error_class = %#v", trace["error_class"])
	}
	details := trace["details"].(map[string]any)
	if details["safe_stage"] != "tts_connect" || details["transcript"] != RedactedValue {
		t.Fatalf("redacted error details = %#v", details)
	}
}

func TestEmitterFailsClosedForSensitiveErrorClass(t *testing.T) {
	var output bytes.Buffer
	emitter, err := NewEmitter(&output, EmitterConfig{Environment: "prod", Service: "voice-runtime"})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	if err := emitter.EmitTurn(TurnEvent{
		Snapshot:   cancelledSnapshot(t, "turn-sensitive-error-class"),
		Outcome:    OutcomeError,
		ErrorClass: "9876543210",
	}); err != nil {
		t.Fatalf("EmitTurn() error = %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(nonEmptyLines(output.String())[0]), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	trace := envelope["trace"].(map[string]any)
	if trace["error_class"] != "unknown_error" {
		t.Fatalf("sensitive error_class = %#v, want unknown_error", trace["error_class"])
	}
}

func TestEmitterRejectsTypedNilWriterAndInvalidMeasurements(t *testing.T) {
	var typedNil *bytes.Buffer
	if _, err := NewEmitter(typedNil, EmitterConfig{Environment: "dev", Service: "voice-runtime"}); !errors.Is(err, ErrInvalidWriter) {
		t.Fatalf("NewEmitter(typed nil) error = %v, want ErrInvalidWriter", err)
	}
	emitter, err := NewEmitter(io.Discard, EmitterConfig{Environment: "dev", Service: "voice-runtime"})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	event := TurnEvent{Snapshot: completedSnapshot(t, "turn-invalid-usage"), Outcome: OutcomeSuccess}
	event.Usage.InputTokens = -1
	if err := emitter.EmitTurn(event); !errors.Is(err, ErrInvalidMeasurement) {
		t.Fatalf("EmitTurn(negative usage) error = %v, want ErrInvalidMeasurement", err)
	}
}

func TestEmitterSerializesConcurrentTurnsWithoutInterleaving(t *testing.T) {
	var output lockedBuffer
	emitter, err := NewEmitter(&output, EmitterConfig{Environment: "test", Service: "voice-runtime"})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	const turns = 128
	var wait sync.WaitGroup
	wait.Add(turns)
	for index := range turns {
		go func() {
			defer wait.Done()
			correlationID := fmt.Sprintf("turn-concurrent-%03d", index)
			if emitErr := emitter.EmitTurn(TurnEvent{Snapshot: completedSnapshot(t, correlationID), Outcome: OutcomeSuccess}); emitErr != nil {
				t.Errorf("EmitTurn() error = %v", emitErr)
			}
		}()
	}
	wait.Wait()

	scanner := bufio.NewScanner(strings.NewReader(output.String()))
	seen := make(map[string]struct{}, turns)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("concurrent JSON is interleaved: %v: %q", err, scanner.Text())
		}
		seen[event["correlation_id"].(string)] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan error = %v", err)
	}
	if len(seen) != turns {
		t.Fatalf("unique emitted turns = %d, want %d", len(seen), turns)
	}
}

func TestEmitterBoundsTheCompleteEncodedEvent(t *testing.T) {
	var output bytes.Buffer
	emitter, err := NewEmitter(&output, EmitterConfig{Environment: "prod", Service: "voice-runtime"})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	wide := make(map[string]any, 256)
	for index := 0; index < 256; index++ {
		wide[fmt.Sprintf("safe_detail_%04d", index)] = strings.Repeat("x", 4096)
	}
	if err := emitter.EmitTurn(TurnEvent{
		Snapshot:   cancelledSnapshot(t, "turn-bounded-error"),
		Outcome:    OutcomeError,
		ErrorClass: "bounded_test",
		Details:    map[string]any{"wide": wide},
	}); err != nil {
		t.Fatalf("EmitTurn() error = %v", err)
	}
	if output.Len() > maxTelemetryEventBytes {
		t.Fatalf("encoded event bytes = %d, want <= %d", output.Len(), maxTelemetryEventBytes)
	}
}

func TestEmitterRejectsShortWrites(t *testing.T) {
	emitter, err := NewEmitter(shortWriter{}, EmitterConfig{Environment: "dev", Service: "voice-runtime"})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	err = emitter.EmitTurn(TurnEvent{Snapshot: completedSnapshot(t, "turn-short-write"), Outcome: OutcomeSuccess})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("EmitTurn(short write) error = %v, want io.ErrShortWrite", err)
	}
}

func TestInstrumentationIsANarrowRuntimeLifecycleSeam(t *testing.T) {
	var output bytes.Buffer
	emitter, err := NewEmitter(&output, EmitterConfig{Environment: "test", Service: "voice-runtime"})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	recorder, err := NewInstrumentation("turn-runtime-seam", emitter)
	if err != nil {
		t.Fatalf("NewInstrumentation() error = %v", err)
	}
	base := time.Date(2026, time.August, 7, 1, 2, 3, 0, time.UTC)
	if err := recorder.SpeechStarted(base); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := recorder.SpeechEnded(base.Add(time.Second)); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}
	if err := recorder.STTFinalReceived(base.Add(2 * time.Second)); err != nil {
		t.Fatalf("STTFinalReceived() error = %v", err)
	}
	if err := recorder.LLMRequestStarted(base.Add(3 * time.Second)); err != nil {
		t.Fatalf("LLMRequestStarted() error = %v", err)
	}
	if err := recorder.TTSRequestStarted(base.Add(4 * time.Second)); !errors.Is(err, ErrUnexpectedBoundary) {
		t.Fatalf("TTSRequestStarted(before first LLM delta) error = %v, want ErrUnexpectedBoundary", err)
	}
	if err := recorder.LLMFirstToken(base.Add(4 * time.Second)); err != nil {
		t.Fatalf("LLMFirstToken() error = %v", err)
	}
	// TTSRequestStarted marks first text submission, never WebSocket/config warmup.
	if err := recorder.TTSRequestStarted(base.Add(5 * time.Second)); err != nil {
		t.Fatalf("TTSRequestStarted() error = %v", err)
	}
	if err := recorder.TTSFirstAudio(base.Add(6 * time.Second)); err != nil {
		t.Fatalf("TTSFirstAudio() error = %v", err)
	}
	if err := recorder.ClientFirstAudioPlayed(base.Add(7 * time.Second)); err != nil {
		t.Fatalf("ClientFirstAudioPlayed() error = %v", err)
	}
	if err := recorder.Complete(base.Add(8*time.Second), TurnMeasurements{Usage: Usage{InputTokens: 1}}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if lines := nonEmptyLines(output.String()); len(lines) != 1 {
		t.Fatalf("runtime seam emitted %d events, want 1", len(lines))
	}
}

func TestInstrumentationCountsDurabilityFailureWithoutFailingTheTurn(t *testing.T) {
	var output bytes.Buffer
	emitter, err := NewEmitter(&output, EmitterConfig{Environment: "test", Service: "voice-runtime"})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	recorder, err := NewInstrumentation("turn-durability-degraded", emitter)
	if err != nil {
		t.Fatalf("NewInstrumentation() error = %v", err)
	}
	recorder.DurabilityFailure()
	recorder.DurabilityFailure()
	completeInstrumentationTurn(t, recorder)

	var envelope map[string]any
	if err := json.Unmarshal([]byte(nonEmptyLines(output.String())[0]), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope["Outcome"] != string(OutcomeSuccess) || envelope["DurabilityFailures"] != float64(2) {
		t.Fatalf("durability-degraded successful aggregate = %#v", envelope)
	}
}

func completedSnapshot(t *testing.T, correlationID string) TurnSnapshot {
	t.Helper()
	tracker, err := NewTracker(correlationID)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	base := time.Date(2026, time.August, 7, 1, 2, 3, 0, time.UTC)
	sequence := []Boundary{
		SpeechStarted, SpeechEnded, STTFinalReceived, LLMRequestStarted,
		LLMFirstToken, TTSRequestStarted, TTSFirstAudio,
	}
	for index, boundary := range sequence {
		if err := tracker.Record(boundary, base.Add(time.Duration(index)*100*time.Millisecond)); err != nil {
			t.Fatalf("Record(%s) error = %v", boundary, err)
		}
	}
	if err := tracker.RecordInterrupt(base.Add(650*time.Millisecond), base.Add(675*time.Millisecond)); err != nil {
		t.Fatalf("RecordInterrupt() error = %v", err)
	}
	if err := tracker.Record(ClientFirstAudioPlayed, base.Add(700*time.Millisecond)); err != nil {
		t.Fatalf("Record(client_first_audio_played) error = %v", err)
	}
	if err := tracker.Record(TurnCompleted, base.Add(800*time.Millisecond)); err != nil {
		t.Fatalf("Record(turn_completed) error = %v", err)
	}
	snapshot, err := tracker.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	return snapshot
}

func cancelledSnapshot(t *testing.T, correlationID string) TurnSnapshot {
	t.Helper()
	tracker, err := NewTracker(correlationID)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	base := time.Date(2026, time.August, 7, 1, 2, 3, 0, time.UTC)
	if err := tracker.Record(SpeechStarted, base); err != nil {
		t.Fatalf("Record(speech_started) error = %v", err)
	}
	if err := tracker.Record(TurnCancelled, base.Add(time.Millisecond)); err != nil {
		t.Fatalf("Record(turn_cancelled) error = %v", err)
	}
	snapshot, err := tracker.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	return snapshot
}

func completeInstrumentationTurn(t *testing.T, recorder *Instrumentation) {
	t.Helper()
	base := time.Date(2026, time.August, 7, 2, 3, 4, 0, time.UTC)
	steps := []func(time.Time) error{
		recorder.SpeechStarted,
		recorder.SpeechEnded,
		recorder.STTFinalReceived,
		recorder.LLMRequestStarted,
		recorder.LLMFirstToken,
		recorder.TTSRequestStarted,
		recorder.TTSFirstAudio,
		recorder.ClientFirstAudioPlayed,
	}
	for index, step := range steps {
		if err := step(base.Add(time.Duration(index) * time.Millisecond)); err != nil {
			t.Fatalf("lifecycle step %d error = %v", index, err)
		}
	}
	if err := recorder.Complete(base.Add(time.Duration(len(steps))*time.Millisecond), TurnMeasurements{}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func firstUnsampledID() string {
	for index := 0; ; index++ {
		candidate := fmt.Sprintf("turn-unsampled-%05d", index)
		if !ShouldSampleSuccess(candidate) {
			return candidate
		}
	}
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func stringsFromAny(values []any) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.(string))
	}
	return result
}

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

type shortWriter struct{}

func (shortWriter) Write(value []byte) (int, error) {
	return len(value) / 2, nil
}

func (buffer *lockedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(value)
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}
