package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"invoice-backend/internal/providers/sarvam"
	voicetelemetry "invoice-backend/internal/voice/telemetry"
)

type sarvamProviderSet interface {
	sarvam.STTOpener
	sarvam.ChatStreamer
	sarvam.TTSOpener
	io.Closer
}

// observedSarvamProviders preserves the provider interfaces while turning
// terminal 429/503-class failures into coarse counters. Telemetry is always
// fail-open and receives no request, transcript, answer, credential, or body.
type observedSarvamProviders struct {
	providers sarvamProviderSet
	signals   voicetelemetry.SignalRecorder
}

func newObservedSarvamProviders(providers sarvamProviderSet, signals voicetelemetry.SignalRecorder) (*observedSarvamProviders, error) {
	if nilRuntimeInterface(providers) || nilRuntimeInterface(signals) {
		return nil, ErrInvalidRuntimeEnvironment
	}
	return &observedSarvamProviders{providers: providers, signals: signals}, nil
}

func (providers *observedSarvamProviders) OpenSTT(ctx context.Context) (sarvam.STTStream, error) {
	if providers == nil || nilRuntimeInterface(providers.providers) {
		return nil, ErrInvalidRuntimeEnvironment
	}
	stream, err := providers.providers.OpenSTT(ctx)
	providers.record(err)
	if err != nil {
		return nil, err
	}
	if nilRuntimeInterface(stream) {
		return nil, ErrInvalidRuntimeEnvironment
	}
	return &observedSTTStream{stream: stream, signals: providers.signals}, nil
}

func (providers *observedSarvamProviders) StreamChat(
	ctx context.Context,
	request sarvam.ChatRequest,
	handler sarvam.ChatEventHandler,
) (sarvam.ChatResult, error) {
	if providers == nil || nilRuntimeInterface(providers.providers) {
		return sarvam.ChatResult{}, ErrInvalidRuntimeEnvironment
	}
	result, err := providers.providers.StreamChat(ctx, request, handler)
	providers.record(err)
	return result, err
}

func (providers *observedSarvamProviders) OpenTTS(ctx context.Context, language string) (sarvam.TTSStream, error) {
	if providers == nil || nilRuntimeInterface(providers.providers) {
		return nil, ErrInvalidRuntimeEnvironment
	}
	stream, err := providers.providers.OpenTTS(ctx, language)
	providers.record(err)
	if err != nil {
		return nil, err
	}
	if nilRuntimeInterface(stream) {
		return nil, ErrInvalidRuntimeEnvironment
	}
	return &observedTTSStream{stream: stream, signals: providers.signals}, nil
}

func (providers *observedSarvamProviders) Close() error {
	if providers == nil || nilRuntimeInterface(providers.providers) {
		return nil
	}
	return providers.providers.Close()
}

func (providers *observedSarvamProviders) record(err error) {
	if providers == nil {
		return
	}
	recordSarvamSignal(providers.signals, err)
}

func (*observedSarvamProviders) String() string   { return "observed Sarvam providers{redacted}" }
func (*observedSarvamProviders) GoString() string { return "observed Sarvam providers{redacted}" }
func (*observedSarvamProviders) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{}{})
}

type observedSTTStream struct {
	stream  sarvam.STTStream
	signals voicetelemetry.SignalRecorder
}

func (stream *observedSTTStream) WritePCM16(ctx context.Context, audio []byte) error {
	err := stream.stream.WritePCM16(ctx, audio)
	recordSarvamSignal(stream.signals, err)
	return err
}

func (stream *observedSTTStream) Flush(ctx context.Context) error {
	err := stream.stream.Flush(ctx)
	recordSarvamSignal(stream.signals, err)
	return err
}

func (stream *observedSTTStream) AwaitFinal(ctx context.Context) (sarvam.FinalTranscript, error) {
	result, err := stream.stream.AwaitFinal(ctx)
	recordSarvamSignal(stream.signals, err)
	return result, err
}

func (stream *observedSTTStream) Done() <-chan struct{} { return stream.stream.Done() }
func (stream *observedSTTStream) Close() error          { return stream.stream.Close() }

type observedTTSStream struct {
	stream  sarvam.TTSStream
	signals voicetelemetry.SignalRecorder
}

func (stream *observedTTSStream) WriteText(ctx context.Context, text string) error {
	err := stream.stream.WriteText(ctx, text)
	recordSarvamSignal(stream.signals, err)
	return err
}

func (stream *observedTTSStream) Flush(ctx context.Context) error {
	err := stream.stream.Flush(ctx)
	recordSarvamSignal(stream.signals, err)
	return err
}

func (stream *observedTTSStream) Ping(ctx context.Context) error {
	err := stream.stream.Ping(ctx)
	recordSarvamSignal(stream.signals, err)
	return err
}

func (stream *observedTTSStream) Next(ctx context.Context) (sarvam.TTSEvent, error) {
	event, err := stream.stream.Next(ctx)
	recordSarvamSignal(stream.signals, err)
	return event, err
}

func (stream *observedTTSStream) Done() <-chan struct{} { return stream.stream.Done() }
func (stream *observedTTSStream) Close() error          { return stream.stream.Close() }

func recordSarvamSignal(recorder voicetelemetry.SignalRecorder, err error) {
	if err == nil || nilRuntimeInterface(recorder) {
		return
	}
	signal := voicetelemetry.Signal("")
	switch {
	case errors.Is(err, sarvam.ErrSTTRateLimited),
		errors.Is(err, sarvam.ErrChatRateLimited),
		errors.Is(err, sarvam.ErrChatQuotaExceeded),
		errors.Is(err, sarvam.ErrTTSRateLimited):
		signal = voicetelemetry.SignalSarvam429
	case errors.Is(err, sarvam.ErrSTTUnavailable), errors.Is(err, sarvam.ErrChatUnavailable), errors.Is(err, sarvam.ErrTTSUnavailable):
		signal = voicetelemetry.SignalSarvam503
	default:
		return
	}
	defer func() { _ = recover() }()
	_ = recorder.RecordSignal(signal, 1)
}

type runtimeSignalingMetrics struct {
	recorder voicetelemetry.SignalRecorder
}

func newRuntimeSignalingMetrics(recorder voicetelemetry.SignalRecorder) (*runtimeSignalingMetrics, error) {
	if nilRuntimeInterface(recorder) {
		return nil, ErrInvalidRuntimeEnvironment
	}
	return &runtimeSignalingMetrics{recorder: recorder}, nil
}

func (metrics *runtimeSignalingMetrics) KVSAllocationError() {
	metrics.record(voicetelemetry.SignalKVSAllocationErrors)
}

func (metrics *runtimeSignalingMetrics) ICERestart() {
	metrics.record(voicetelemetry.SignalICERestarts)
}

func (metrics *runtimeSignalingMetrics) record(signal voicetelemetry.Signal) {
	if metrics == nil || nilRuntimeInterface(metrics.recorder) {
		return
	}
	defer func() { _ = recover() }()
	_ = metrics.recorder.RecordSignal(signal, 1)
}

var _ sarvamProviderSet = (*observedSarvamProviders)(nil)
