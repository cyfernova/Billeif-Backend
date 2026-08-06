package main

import (
	"context"
	"errors"
	"sync"
	"testing"

	"invoice-backend/internal/providers/sarvam"
	voicetelemetry "invoice-backend/internal/voice/telemetry"
)

func TestObservedSarvamProvidersEmitEveryTerminalProviderStatus(t *testing.T) {
	recorder := &runtimeSignalRecorder{}
	provider := &telemetryProviderFake{
		openSTTErr: sarvam.ErrSTTRateLimited,
		chatErr:    sarvam.ErrChatUnavailable,
		openTTSErr: sarvam.ErrTTSRateLimited,
	}
	observed, err := newObservedSarvamProviders(provider, recorder)
	if err != nil {
		t.Fatalf("newObservedSarvamProviders() error = %v", err)
	}
	if _, err := observed.OpenSTT(t.Context()); !errors.Is(err, sarvam.ErrSTTRateLimited) {
		t.Fatalf("OpenSTT() error = %v", err)
	}
	if _, err := observed.StreamChat(t.Context(), sarvam.ChatRequest{}, nil); !errors.Is(err, sarvam.ErrChatUnavailable) {
		t.Fatalf("StreamChat() error = %v", err)
	}
	if _, err := observed.OpenTTS(t.Context(), "en-IN"); !errors.Is(err, sarvam.ErrTTSRateLimited) {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	if got := recorder.snapshot(); !equalSignals(got, []voicetelemetry.Signal{
		voicetelemetry.SignalSarvam429,
		voicetelemetry.SignalSarvam503,
		voicetelemetry.SignalSarvam429,
	}) {
		t.Fatalf("provider signals = %v", got)
	}
}

func TestObservedSarvamStreamsEmitErrorsAfterSuccessfulOpen(t *testing.T) {
	recorder := &runtimeSignalRecorder{}
	provider := &telemetryProviderFake{
		stt: &telemetrySTTStream{awaitErr: sarvam.ErrSTTUnavailable},
		tts: &telemetryTTSStream{nextErr: sarvam.ErrTTSRateLimited},
	}
	observed, err := newObservedSarvamProviders(provider, recorder)
	if err != nil {
		t.Fatalf("newObservedSarvamProviders() error = %v", err)
	}
	stt, err := observed.OpenSTT(t.Context())
	if err != nil {
		t.Fatalf("OpenSTT() error = %v", err)
	}
	if _, err := stt.AwaitFinal(t.Context()); !errors.Is(err, sarvam.ErrSTTUnavailable) {
		t.Fatalf("AwaitFinal() error = %v", err)
	}
	tts, err := observed.OpenTTS(t.Context(), "en-IN")
	if err != nil {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	if _, err := tts.Next(t.Context()); !errors.Is(err, sarvam.ErrTTSRateLimited) {
		t.Fatalf("Next() error = %v", err)
	}
	if got := recorder.snapshot(); !equalSignals(got, []voicetelemetry.Signal{
		voicetelemetry.SignalSarvam503,
		voicetelemetry.SignalSarvam429,
	}) {
		t.Fatalf("stream signals = %v", got)
	}
}

func TestObservedSarvamTelemetryFailsOpenAndRejectsNilDependencies(t *testing.T) {
	provider := &telemetryProviderFake{chatErr: sarvam.ErrChatUnavailable}
	if _, err := newObservedSarvamProviders(nil, &runtimeSignalRecorder{}); !errors.Is(err, ErrInvalidRuntimeEnvironment) {
		t.Fatalf("nil provider error = %v", err)
	}
	if _, err := newObservedSarvamProviders(provider, nil); !errors.Is(err, ErrInvalidRuntimeEnvironment) {
		t.Fatalf("nil recorder error = %v", err)
	}
	observed, err := newObservedSarvamProviders(provider, panicSignalRecorder{})
	if err != nil {
		t.Fatalf("newObservedSarvamProviders() error = %v", err)
	}
	if _, err := observed.StreamChat(t.Context(), sarvam.ChatRequest{}, nil); !errors.Is(err, sarvam.ErrChatUnavailable) {
		t.Fatalf("telemetry panic changed provider error: %v", err)
	}
}

func TestObservedSarvamProvidersClassifyChatQuotaExhaustionAsHTTP429Health(t *testing.T) {
	recorder := &runtimeSignalRecorder{}
	observed, err := newObservedSarvamProviders(
		&telemetryProviderFake{chatErr: sarvam.ErrChatQuotaExceeded},
		recorder,
	)
	if err != nil {
		t.Fatalf("newObservedSarvamProviders() error = %v", err)
	}
	if _, err := observed.StreamChat(t.Context(), sarvam.ChatRequest{}, nil); !errors.Is(err, sarvam.ErrChatQuotaExceeded) {
		t.Fatalf("StreamChat() error = %v, want ErrChatQuotaExceeded", err)
	}
	if got := recorder.snapshot(); !equalSignals(got, []voicetelemetry.Signal{voicetelemetry.SignalSarvam429}) {
		t.Fatalf("quota signals = %v, want Sarvam429", got)
	}
}

func TestRuntimeSignalingMetricsMapsOnlyCoarseSignalsAndFailsOpen(t *testing.T) {
	recorder := &runtimeSignalRecorder{}
	metrics, err := newRuntimeSignalingMetrics(recorder)
	if err != nil {
		t.Fatalf("newRuntimeSignalingMetrics() error = %v", err)
	}
	metrics.KVSAllocationError()
	metrics.ICERestart()
	if got := recorder.snapshot(); !equalSignals(got, []voicetelemetry.Signal{
		voicetelemetry.SignalKVSAllocationErrors,
		voicetelemetry.SignalICERestarts,
	}) {
		t.Fatalf("signaling signals = %v", got)
	}
	if _, err := newRuntimeSignalingMetrics(nil); !errors.Is(err, ErrInvalidRuntimeEnvironment) {
		t.Fatalf("nil recorder error = %v", err)
	}
	panicMetrics, err := newRuntimeSignalingMetrics(panicSignalRecorder{})
	if err != nil {
		t.Fatalf("newRuntimeSignalingMetrics(panic) error = %v", err)
	}
	panicMetrics.KVSAllocationError()
	panicMetrics.ICERestart()
}

type telemetryProviderFake struct {
	stt        sarvam.STTStream
	openSTTErr error
	chatErr    error
	tts        sarvam.TTSStream
	openTTSErr error
}

func (provider *telemetryProviderFake) OpenSTT(context.Context) (sarvam.STTStream, error) {
	return provider.stt, provider.openSTTErr
}

func (provider *telemetryProviderFake) StreamChat(context.Context, sarvam.ChatRequest, sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
	return sarvam.ChatResult{}, provider.chatErr
}

func (provider *telemetryProviderFake) OpenTTS(context.Context, string) (sarvam.TTSStream, error) {
	return provider.tts, provider.openTTSErr
}

func (*telemetryProviderFake) Close() error { return nil }

type telemetrySTTStream struct {
	awaitErr error
	done     chan struct{}
}

func (*telemetrySTTStream) WritePCM16(context.Context, []byte) error { return nil }
func (*telemetrySTTStream) Flush(context.Context) error              { return nil }
func (stream *telemetrySTTStream) AwaitFinal(context.Context) (sarvam.FinalTranscript, error) {
	return sarvam.FinalTranscript{}, stream.awaitErr
}
func (stream *telemetrySTTStream) Done() <-chan struct{} {
	if stream.done == nil {
		stream.done = make(chan struct{})
	}
	return stream.done
}
func (*telemetrySTTStream) Close() error { return nil }

type telemetryTTSStream struct {
	nextErr error
	done    chan struct{}
}

func (*telemetryTTSStream) WriteText(context.Context, string) error { return nil }
func (*telemetryTTSStream) Flush(context.Context) error             { return nil }
func (*telemetryTTSStream) Ping(context.Context) error              { return nil }
func (stream *telemetryTTSStream) Next(context.Context) (sarvam.TTSEvent, error) {
	return sarvam.TTSEvent{}, stream.nextErr
}
func (stream *telemetryTTSStream) Done() <-chan struct{} {
	if stream.done == nil {
		stream.done = make(chan struct{})
	}
	return stream.done
}
func (*telemetryTTSStream) Close() error { return nil }

type runtimeSignalRecorder struct {
	mu      sync.Mutex
	signals []voicetelemetry.Signal
}

func (recorder *runtimeSignalRecorder) RecordSignal(signal voicetelemetry.Signal, value int64) error {
	if value != 1 {
		return errors.New("unexpected signal value")
	}
	recorder.mu.Lock()
	recorder.signals = append(recorder.signals, signal)
	recorder.mu.Unlock()
	return nil
}

func (recorder *runtimeSignalRecorder) snapshot() []voicetelemetry.Signal {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]voicetelemetry.Signal(nil), recorder.signals...)
}

type panicSignalRecorder struct{}

func (panicSignalRecorder) RecordSignal(voicetelemetry.Signal, int64) error {
	panic("telemetry must fail open")
}

func equalSignals(left, right []voicetelemetry.Signal) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
