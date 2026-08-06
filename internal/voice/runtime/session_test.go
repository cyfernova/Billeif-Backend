package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/voice/audio"
	"invoice-backend/internal/voice/protocol"
)

func TestAcquirePersistentActivityKeepsRuntimeBusyUntilClosed(t *testing.T) {
	server := NewServer(Config{}, nil)

	activityContext, activity, err := server.AcquirePersistentActivity()
	if err != nil {
		t.Fatalf("acquire persistent activity: %v", err)
	}
	assertRuntimeHealth(t, server, StatusHealthyBusy)
	select {
	case <-activityContext.Done():
		t.Fatal("persistent activity context was canceled before close")
	default:
	}

	if err := activity.Close(); err != nil {
		t.Fatalf("close persistent activity: %v", err)
	}
	if err := activity.Close(); err != nil {
		t.Fatalf("close persistent activity twice: %v", err)
	}
	select {
	case <-activityContext.Done():
	case <-time.After(time.Second):
		t.Fatal("persistent activity context was not canceled by close")
	}
	assertRuntimeHealth(t, server, StatusHealthy)
}

func TestPersistentActivitySurvivesInvocationCancellation(t *testing.T) {
	contexts := make(chan context.Context, 1)
	activities := make(chan io.Closer, 1)
	var server *Server
	server = NewServer(Config{}, InvocationHandlerFunc(func(_ context.Context, _ InvocationRequest) (InvocationResponse, error) {
		activityContext, activity, err := server.AcquirePersistentActivity()
		if err != nil {
			return InvocationResponse{}, err
		}
		contexts <- activityContext
		activities <- activity
		return InvocationResponse{StatusCode: http.StatusOK, Body: json.RawMessage(`{}`)}, nil
	}))
	request := httptest.NewRequest(http.MethodPost, "/invocations", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer test-only-token")
	request.Header.Set(HeaderRuntimeSessionID, "voice-session-123456789012345678901234567890")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("invocation status = %d, want %d", response.Code, http.StatusOK)
	}
	activityContext := <-contexts
	activity := <-activities
	defer activity.Close()

	select {
	case <-activityContext.Done():
		t.Fatal("persistent activity inherited invocation cancellation")
	case <-time.After(25 * time.Millisecond):
	}
	assertRuntimeHealth(t, server, StatusHealthyBusy)
}

func TestShutdownCancelsPersistentActivityAndRejectsNewOnes(t *testing.T) {
	server := NewServer(Config{}, nil)
	activityContext, activity, err := server.AcquirePersistentActivity()
	if err != nil {
		t.Fatalf("acquire persistent activity: %v", err)
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- server.Shutdown(shutdownContext)
	}()

	select {
	case <-activityContext.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel persistent activity context")
	}
	if _, _, err := server.AcquirePersistentActivity(); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("new activity error = %v, want ErrShuttingDown", err)
	}

	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown completed before activity released: %v", err)
	default:
	}
	if err := activity.Close(); err != nil {
		t.Fatalf("close persistent activity: %v", err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestAcquirePersistentActivityOnNilServerFailsClosed(t *testing.T) {
	var server *Server
	activityContext, activity, err := server.AcquirePersistentActivity()
	if !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("error = %v, want ErrShuttingDown", err)
	}
	if activityContext != nil || activity != nil {
		t.Fatalf("nil server returned context=%v activity=%v", activityContext, activity)
	}
}

func TestSTTSessionBindingRoutesDecodedOpusAndSpeechControls(t *testing.T) {
	server := NewServer(Config{}, nil)
	activityContext, activity, err := server.AcquirePersistentActivity()
	if err != nil {
		t.Fatalf("AcquirePersistentActivity() error = %v", err)
	}
	stream := newFakeSTTStream()
	decoder := &bindingTestDecoder{}
	finals := make(chan STTFinalTranscript, 1)
	binding, err := NewSTTSessionBinding(STTSessionBindingConfig{
		STT: STTSessionConfig{
			Context:          activityContext,
			Opener:           &fakeSTTOpener{streams: []sarvam.STTStream{stream}},
			FallbackLanguage: "en-IN",
			Clock:            newFakeSTTClock(),
			FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(_ context.Context, final STTFinalTranscript) {
				finals <- final
			}),
			RepeatRequestHandler: RepeatRequestHandlerFunc(func(context.Context) {}),
		},
		Decoder:  decoder,
		Activity: activity,
	})
	if err != nil {
		t.Fatalf("NewSTTSessionBinding() error = %v", err)
	}
	t.Cleanup(func() { _ = binding.Close() })
	assertRuntimeHealth(t, server, StatusHealthyBusy)

	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventHeartbeat}); err != nil {
		t.Fatalf("HandleControl(heartbeat) error = %v", err)
	}
	if err := binding.HandleOpus([]byte{0x11}); err != nil {
		t.Fatalf("HandleOpus(pre-roll) error = %v", err)
	}
	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechStarted}); err != nil {
		t.Fatalf("HandleControl(speech.started) error = %v", err)
	}
	if err := binding.HandleOpus([]byte{0x22}); err != nil {
		t.Fatalf("HandleOpus(live) error = %v", err)
	}
	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechEnded}); err != nil {
		t.Fatalf("HandleControl(speech.ended) error = %v", err)
	}
	if err := binding.HandleOpus([]byte{0x33}); err != nil {
		t.Fatalf("HandleOpus(cross-channel tail after speech.ended) error = %v", err)
	}
	stream.SendFinal(sarvam.FinalTranscript{Text: "bound", DetectedLanguage: "en-IN"})
	if got := receiveSTTFinal(t, finals); got.Text != "bound" {
		t.Fatalf("binding final = %#v", got)
	}
	writes := stream.Writes()
	if len(writes) != 2 || writes[0][0] != 0x11 || writes[0][1] != 0x11 || writes[1][0] != 0x22 || writes[1][1] != 0x22 {
		t.Fatalf("binding PCM writes have wrong order or little-endian samples: %v", writes)
	}

	if err := binding.Close(); err != nil {
		t.Fatalf("binding Close() error = %v", err)
	}
	if err := binding.Close(); err != nil {
		t.Fatalf("second binding Close() error = %v", err)
	}
	if got := decoder.closeCalls.Load(); got != 1 {
		t.Fatalf("decoder Close() calls = %d, want 1", got)
	}
	assertRuntimeHealth(t, server, StatusHealthy)
}

func TestSTTSessionBindingFailsClosedOnDecodeAndAfterClose(t *testing.T) {
	activity := &countingCloser{}
	decodeFailure := errors.New("offline malformed Opus")
	decoder := &bindingTestDecoder{decodeError: decodeFailure}
	binding, err := NewSTTSessionBinding(STTSessionBindingConfig{
		STT: STTSessionConfig{
			Context:                context.Background(),
			Opener:                 &fakeSTTOpener{},
			FallbackLanguage:       "en-IN",
			Clock:                  newFakeSTTClock(),
			FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(context.Context, STTFinalTranscript) {}),
			RepeatRequestHandler:   RepeatRequestHandlerFunc(func(context.Context) {}),
		},
		Decoder:  decoder,
		Activity: activity,
	})
	if err != nil {
		t.Fatalf("NewSTTSessionBinding() error = %v", err)
	}
	if err := binding.HandleOpus([]byte{0xff}); !errors.Is(err, decodeFailure) {
		t.Fatalf("HandleOpus(malformed) error = %v, want decode failure", err)
	}
	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventType("Speech.Started")}); err != nil {
		t.Fatalf("HandleControl(case variant) error = %v", err)
	}
	if err := binding.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := binding.HandleOpus([]byte{0x01}); !errors.Is(err, ErrSTTSessionClosed) {
		t.Fatalf("post-close HandleOpus() error = %v, want ErrSTTSessionClosed", err)
	}
	if err := binding.HandlePCM16(make([]byte, STTPCMFrameBytes)); !errors.Is(err, ErrSTTSessionClosed) {
		t.Fatalf("post-close HandlePCM16() error = %v, want ErrSTTSessionClosed", err)
	}
	if got := activity.calls.Load(); got != 1 {
		t.Fatalf("activity Close() calls = %d, want 1", got)
	}
}

func TestNewSTTSessionBindingRejectsNilOwnedDependencies(t *testing.T) {
	valid := STTSessionBindingConfig{
		STT: STTSessionConfig{
			Context:                context.Background(),
			Opener:                 &fakeSTTOpener{},
			FallbackLanguage:       "en-IN",
			Clock:                  newFakeSTTClock(),
			FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(context.Context, STTFinalTranscript) {}),
			RepeatRequestHandler:   RepeatRequestHandlerFunc(func(context.Context) {}),
		},
		Decoder:  &bindingTestDecoder{},
		Activity: &countingCloser{},
	}
	var nilDecoder *bindingTestDecoder
	var nilActivity *countingCloser
	tests := []struct {
		name    string
		mutate  func(*STTSessionBindingConfig)
		wantErr error
	}{
		{name: "nil decoder", mutate: func(config *STTSessionBindingConfig) { config.Decoder = nil }, wantErr: ErrSTTDecoderRequired},
		{name: "typed nil decoder", mutate: func(config *STTSessionBindingConfig) { config.Decoder = nilDecoder }, wantErr: ErrSTTDecoderRequired},
		{name: "nil activity", mutate: func(config *STTSessionBindingConfig) { config.Activity = nil }, wantErr: ErrSTTActivityRequired},
		{name: "typed nil activity", mutate: func(config *STTSessionBindingConfig) { config.Activity = nilActivity }, wantErr: ErrSTTActivityRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			binding, err := NewSTTSessionBinding(config)
			if !errors.Is(err, test.wantErr) || binding != nil {
				t.Fatalf("NewSTTSessionBinding() = (%v, %v), want (nil, %v)", binding, err, test.wantErr)
			}
		})
	}
}

func TestNewSTTSessionBindingCleansOwnedDependenciesWhenSTTConstructionFails(t *testing.T) {
	decoder := &bindingTestDecoder{}
	activity := &countingCloser{}
	binding, err := NewSTTSessionBinding(STTSessionBindingConfig{
		STT: STTSessionConfig{
			Context:                context.Background(),
			FallbackLanguage:       "en-IN",
			Clock:                  newFakeSTTClock(),
			FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(context.Context, STTFinalTranscript) {}),
			RepeatRequestHandler:   RepeatRequestHandlerFunc(func(context.Context) {}),
		},
		Decoder:  decoder,
		Activity: activity,
	})
	if !errors.Is(err, ErrSTTOpenerRequired) || binding != nil {
		t.Fatalf("NewSTTSessionBinding(invalid STT) = (%v, %v), want (nil, ErrSTTOpenerRequired)", binding, err)
	}
	if got := decoder.closeCalls.Load(); got != 1 {
		t.Fatalf("decoder Close() calls after constructor failure = %d, want 1", got)
	}
	if got := activity.calls.Load(); got != 1 {
		t.Fatalf("activity Close() calls after constructor failure = %d, want 1", got)
	}
}

type bindingTestDecoder struct {
	mu          sync.Mutex
	decodeError error
	closeCalls  atomic.Int32
}

func (decoder *bindingTestDecoder) Decode(payload []byte, pcm []int16) (int, error) {
	decoder.mu.Lock()
	defer decoder.mu.Unlock()
	if decoder.decodeError != nil {
		return 0, decoder.decodeError
	}
	if len(payload) != 1 || len(pcm) != audio.SamplesPerFrame {
		return 0, errors.New("binding test decoder received invalid buffers")
	}
	sample := int16(payload[0])<<8 | int16(payload[0])
	for index := range pcm {
		pcm[index] = sample
	}
	return len(pcm), nil
}

func (decoder *bindingTestDecoder) Close() error {
	decoder.closeCalls.Add(1)
	return nil
}

func assertRuntimeHealth(t *testing.T, server http.Handler, want string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/ping", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ping status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode ping: %v", err)
	}
	if body.Status != want {
		t.Fatalf("ping status = %q, want %q", body.Status, want)
	}
}

var _ io.Closer = (*PersistentActivity)(nil)
