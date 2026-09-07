package composition

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/voice/audio"
	"invoice-backend/internal/voice/protocol"
	voicesession "invoice-backend/internal/voice/session"
	"invoice-backend/internal/voice/tools"
	"invoice-backend/internal/voice/turn"
	"invoice-backend/internal/voice/webrtc"
)

func TestBindingFactoryStartsReasoningOnlyAfterAuthoritativeSTTFinal(t *testing.T) {
	stream := newCompositionSTTStream()
	opener := &compositionSTTOpener{stream: stream}
	chat := &compositionChat{}
	output := newCompositionOutput()
	activity := &countingCloser{}
	decoder := &compositionDecoder{}
	factory, err := NewBindingFactory(FactoryConfig{
		STT:          opener,
		Chat:         chat,
		LeaseRenewer: &compositionLeaseRenewer{},
		Tools:        tools.Config{Origin: "https://api.example.com/staging"},
		Decoders: DecoderFactoryFunc(func() (audio.Decoder, error) {
			return decoder, nil
		}),
		Outputs: OutputFactoryFunc(func(voicesession.Session) (SessionOutput, error) {
			return output, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewBindingFactory() error = %v", err)
	}

	binding, err := factory.Create(webrtc.STTBindingConfig{
		Context:       context.Background(),
		Session:       validCompositionSession(),
		Activity:      activity,
		Authorization: staticAuthorizationSource("Bearer validated-user-token"),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	t.Cleanup(func() { _ = binding.Close() })

	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechStarted}); err != nil {
		t.Fatalf("speech.started error = %v", err)
	}
	if err := binding.HandleOpus([]byte{0x01, 0x02}); err != nil {
		t.Fatalf("HandleOpus() error = %v", err)
	}
	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechEnded}); err != nil {
		t.Fatalf("speech.ended error = %v", err)
	}

	select {
	case <-stream.flushed:
	case <-time.After(time.Second):
		t.Fatal("STT stream did not flush")
	}
	if got := chat.calls.Load(); got != 0 {
		t.Fatalf("chat calls before authoritative final = %d, want 0", got)
	}
	stream.finals <- sarvam.FinalTranscript{Text: "मेरा नवीनतम चालान दिखाओ", DetectedLanguage: "hi-IN"}

	select {
	case completed := <-output.completed:
		if completed.Failure != TurnFailureNone {
			t.Fatalf("turn failure = %q, want none", completed.Failure)
		}
		if completed.Result.Text != "आपका नवीनतम चालान तैयार है।" {
			t.Fatalf("result text = %q", completed.Result.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completed reasoning turn")
	}
	if got := chat.calls.Load(); got != 1 {
		t.Fatalf("chat calls after authoritative final = %d, want 1", got)
	}
	if got := output.text(); got != "आपका नवीनतम चालान तैयार है।" {
		t.Fatalf("spoken text = %q", got)
	}

	if err := binding.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := decoder.closes.Load(); got != 1 {
		t.Fatalf("decoder closes = %d, want 1", got)
	}
	if got := activity.closes.Load(); got != 1 {
		t.Fatalf("activity closes = %d, want 1", got)
	}
}

func TestBindingSharesGenerationFenceAndCancelsOutputBeforeNewSpeechStarts(t *testing.T) {
	stream := newCompositionSTTStream()
	chat := &cancellationCompositionChat{started: make(chan struct{}), cancelled: make(chan error, 1)}
	output := newBargeAwareCompositionOutput()
	factory, err := NewBindingFactory(FactoryConfig{
		STT: &compositionSTTOpener{stream: stream}, Chat: chat,
		LeaseRenewer: &compositionLeaseRenewer{},
		Tools:        tools.Config{Origin: "https://api.example.com/staging"},
		Decoders:     DecoderFactoryFunc(func() (audio.Decoder, error) { return &compositionDecoder{}, nil }),
		Outputs:      OutputFactoryFunc(func(voicesession.Session) (SessionOutput, error) { return output, nil }),
	})
	if err != nil {
		t.Fatalf("NewBindingFactory() error = %v", err)
	}
	binding, err := factory.Create(webrtc.STTBindingConfig{
		Context: context.Background(), Session: validCompositionSession(), Activity: &countingCloser{},
		Authorization: staticAuthorizationSource("Bearer validated-user-token"),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	t.Cleanup(func() { _ = binding.Close() })

	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechStarted}); err != nil {
		t.Fatalf("first speech.started error = %v", err)
	}
	if err := binding.HandleOpus([]byte{0x01}); err != nil {
		t.Fatalf("HandleOpus() error = %v", err)
	}
	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechEnded}); err != nil {
		t.Fatalf("speech.ended error = %v", err)
	}
	<-stream.flushed
	stream.finals <- sarvam.FinalTranscript{Text: "hello", DetectedLanguage: "en-IN"}
	select {
	case <-chat.started:
	case <-time.After(time.Second):
		t.Fatal("reasoning generation did not start")
	}

	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechStarted}); err != nil {
		t.Fatalf("barge-in speech.started error = %v", err)
	}
	if got := output.controller.Current(); got != 2 {
		t.Fatalf("shared generation when speech.started returned = %d, want 2", got)
	}
	select {
	case cause := <-chat.cancelled:
		if !errors.Is(cause, turn.ErrTurnInterrupted) {
			t.Fatalf("chat cancellation cause = %v, want ErrTurnInterrupted", cause)
		}
	case <-time.After(time.Second):
		t.Fatal("generation cancellation did not reach chat")
	}
}

func TestBindingFactoryFailsClosedAndReleasesTransferredActivity(t *testing.T) {
	factory, err := NewBindingFactory(FactoryConfig{
		STT:          &compositionSTTOpener{stream: newCompositionSTTStream()},
		Chat:         &compositionChat{},
		LeaseRenewer: &compositionLeaseRenewer{},
		Tools:        tools.Config{Origin: "https://api.example.com/staging"},
		Decoders: DecoderFactoryFunc(func() (audio.Decoder, error) {
			return &compositionDecoder{}, nil
		}),
		Outputs: OutputFactoryFunc(func(voicesession.Session) (SessionOutput, error) {
			return nil, errors.New("output setup failed with sensitive-canary")
		}),
	})
	if err != nil {
		t.Fatalf("NewBindingFactory() error = %v", err)
	}
	activity := &countingCloser{}
	binding, err := factory.Create(webrtc.STTBindingConfig{
		Context: context.Background(), Session: validCompositionSession(), Activity: activity,
		Authorization: staticAuthorizationSource("Bearer constructor-sensitive-canary"),
	})
	if binding != nil || !errors.Is(err, ErrSessionOutputUnavailable) {
		t.Fatalf("Create() = (%v, %v), want (nil, ErrSessionOutputUnavailable)", binding, err)
	}
	if got := activity.closes.Load(); got != 1 {
		t.Fatalf("activity closes after construction failure = %d, want 1", got)
	}
	if containsAny(err.Error(), "sensitive-canary", "constructor-sensitive-canary") {
		t.Fatalf("construction error leaked sensitive value: %v", err)
	}
}

func TestBindingFactoryBranchSessionAdvertisesNoBusinessTools(t *testing.T) {
	chat := &compositionChat{}
	output := newCompositionOutput()
	factory, err := NewBindingFactory(FactoryConfig{
		STT: &compositionSTTOpener{stream: newCompositionSTTStream()}, Chat: chat,
		LeaseRenewer: &compositionLeaseRenewer{},
		Tools:        tools.Config{Origin: "https://api.example.com/staging", EnableCustomerTools: true},
		Decoders:     DecoderFactoryFunc(func() (audio.Decoder, error) { return &compositionDecoder{}, nil }),
		Outputs:      OutputFactoryFunc(func(voicesession.Session) (SessionOutput, error) { return output, nil }),
	})
	if err != nil {
		t.Fatalf("NewBindingFactory() error = %v", err)
	}
	value := validCompositionSession()
	value.BranchID = "22222222-2222-4222-8222-222222222222"
	activity := &countingCloser{}
	binding, err := factory.Create(webrtc.STTBindingConfig{
		Context: context.Background(), Session: value, Activity: activity,
		Authorization: staticAuthorizationSource("Bearer validated-user-token"),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer binding.Close()

	stream := factoryTestStream(t, factory)
	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechStarted}); err != nil {
		t.Fatalf("speech.started error = %v", err)
	}
	if err := binding.HandleOpus([]byte{0x01}); err != nil {
		t.Fatalf("HandleOpus() error = %v", err)
	}
	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechEnded}); err != nil {
		t.Fatalf("speech.ended error = %v", err)
	}
	<-stream.flushed
	stream.finals <- sarvam.FinalTranscript{Text: "hello", DetectedLanguage: "en-IN"}
	select {
	case <-output.completed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for branch-scoped turn")
	}
	request := chat.lastRequest()
	if len(request.Tools) != 0 {
		t.Fatalf("branch-scoped tool definitions = %#v, want none", request.Tools)
	}
}

func TestBindingFactoryMissingRequiredGovernanceAdvertisesNoTools(t *testing.T) {
	stream := newCompositionSTTStream()
	chat := &compositionChat{}
	output := newCompositionOutput()
	factory, err := NewBindingFactory(FactoryConfig{
		STT: &compositionSTTOpener{stream: stream}, Chat: chat,
		LeaseRenewer:          &compositionLeaseRenewer{},
		Tools:                 tools.Config{Origin: "https://api.example.com/staging", EnableCustomerTools: true},
		Decoders:              DecoderFactoryFunc(func() (audio.Decoder, error) { return &compositionDecoder{}, nil }),
		Outputs:               OutputFactoryFunc(func(voicesession.Session) (SessionOutput, error) { return output, nil }),
		RequireToolGovernance: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := factory.Create(webrtc.STTBindingConfig{
		Context: context.Background(), Session: validCompositionSession(), Activity: &countingCloser{},
		Authorization: staticAuthorizationSource("Bearer validated-user-token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechStarted}); err != nil {
		t.Fatal(err)
	}
	if err := binding.HandleOpus([]byte{0x01}); err != nil {
		t.Fatal(err)
	}
	if err := binding.HandleControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSpeechEnded}); err != nil {
		t.Fatal(err)
	}
	<-stream.flushed
	stream.finals <- sarvam.FinalTranscript{Text: "show invoices", DetectedLanguage: "en-IN"}
	select {
	case <-output.completed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for governed turn")
	}
	if got := chat.lastRequest().Tools; len(got) != 0 {
		t.Fatalf("missing-governance tool definitions = %#v, want none", got)
	}
}

func TestBindingSessionCloseControlReleasesPersistentActivity(t *testing.T) {
	activity := &countingCloser{}
	factory, err := NewBindingFactory(FactoryConfig{
		STT: &compositionSTTOpener{stream: newCompositionSTTStream()}, Chat: &compositionChat{},
		LeaseRenewer: &compositionLeaseRenewer{},
		Tools:        tools.Config{Origin: "https://api.example.com/staging"},
		Decoders:     DecoderFactoryFunc(func() (audio.Decoder, error) { return &compositionDecoder{}, nil }),
		Outputs:      OutputFactoryFunc(func(voicesession.Session) (SessionOutput, error) { return newCompositionOutput(), nil }),
	})
	if err != nil {
		t.Fatalf("NewBindingFactory() error = %v", err)
	}
	binding, err := factory.Create(webrtc.STTBindingConfig{
		Context: context.Background(), Session: validCompositionSession(), Activity: activity,
		Authorization: staticAuthorizationSource("Bearer validated-user-token"),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	peerBinding := binding.(*Binding)
	if err := peerBinding.HandlePeerControl(context.Background(), protocol.ControlMessage{Type: protocol.EventSessionClose}); !errors.Is(err, webrtc.ErrSignalingClose) {
		t.Fatalf("HandlePeerControl(session.close) error = %v, want ErrSignalingClose", err)
	}
	if got := activity.closes.Load(); got != 1 {
		t.Fatalf("activity closes = %d, want 1", got)
	}
	if err := binding.HandleOpus([]byte{0x01}); !errors.Is(err, ErrBindingClosed) {
		t.Fatalf("HandleOpus(after session.close) error = %v, want ErrBindingClosed", err)
	}
	if err := binding.Close(); err != nil {
		t.Fatalf("Close(after session.close) error = %v", err)
	}
	if got := activity.closes.Load(); got != 1 {
		t.Fatalf("activity closes after repeated Close = %d, want 1", got)
	}
}

func TestBindingRoutesDataChannelHeartbeatToDurableLeaseAndCoalescesBurst(t *testing.T) {
	now := time.Date(2026, 8, 7, 11, 0, 0, 0, time.UTC)
	renewer := &compositionLeaseRenewer{calls: make(chan voicesession.LeaseRenewal, 4)}
	factory, err := NewBindingFactory(FactoryConfig{
		STT: &compositionSTTOpener{stream: newCompositionSTTStream()}, Chat: &compositionChat{},
		LeaseRenewer: renewer, LeaseDuration: 2 * time.Minute,
		LeaseHeartbeatInterval: 30 * time.Second, LeaseWriteTimeout: time.Second,
		LeaseNow: func() time.Time { return now },
		Tools:    tools.Config{Origin: "https://api.example.com/staging"},
		Decoders: DecoderFactoryFunc(func() (audio.Decoder, error) { return &compositionDecoder{}, nil }),
		Outputs:  OutputFactoryFunc(func(voicesession.Session) (SessionOutput, error) { return newCompositionOutput(), nil }),
	})
	if err != nil {
		t.Fatalf("NewBindingFactory() error = %v", err)
	}
	value := validCompositionSessionAt(now)
	value.UpdatedAt = now.Add(-31 * time.Second)
	value.LeaseExpiresAt = now.Add(90 * time.Second)
	bindingValue, err := factory.Create(webrtc.STTBindingConfig{
		Context: t.Context(), Session: value, Activity: &countingCloser{},
		Authorization: staticAuthorizationSource("Bearer validated-user-token"),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	binding := bindingValue.(*Binding)
	t.Cleanup(func() { _ = binding.Close() })

	clientMonotonicMS := int64(9_999_999_999)
	for index := 0; index < 64; index++ {
		if err := binding.HandlePeerControl(t.Context(), protocol.ControlMessage{
			Type: protocol.EventHeartbeat, ClientMonotonicMS: &clientMonotonicMS,
		}); err != nil {
			t.Fatalf("HandlePeerControl(heartbeat %d) error = %v", index, err)
		}
	}
	select {
	case request := <-renewer.calls:
		if request.SessionID != value.ID || request.RuntimeSessionID != value.RuntimeSessionID ||
			request.Scope.UserID != value.UserID || request.Scope.BusinessID != value.BusinessID ||
			!request.RenewedAt.Equal(now) {
			t.Fatalf("durable heartbeat request = %#v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("DataChannel heartbeat did not reach durable lease renewer")
	}
	select {
	case duplicate := <-renewer.calls:
		t.Fatalf("heartbeat burst was not coalesced: %#v", duplicate)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestBindingFailsPeerClosedWhenDurableHeartbeatFailsOrPanics(t *testing.T) {
	now := time.Date(2026, 8, 7, 11, 0, 0, 0, time.UTC)
	for name, renewer := range map[string]*compositionLeaseRenewer{
		"error": {err: errors.New("dynamo failed with secret-canary")},
		"panic": {panicValue: "panic-secret-canary"},
	} {
		t.Run(name, func(t *testing.T) {
			activity := &countingCloser{}
			factory, err := NewBindingFactory(FactoryConfig{
				STT: &compositionSTTOpener{stream: newCompositionSTTStream()}, Chat: &compositionChat{},
				LeaseRenewer: renewer, LeaseDuration: 2 * time.Minute,
				LeaseHeartbeatInterval: 30 * time.Second, LeaseWriteTimeout: time.Second,
				LeaseNow: func() time.Time { return now },
				Tools:    tools.Config{Origin: "https://api.example.com/staging"},
				Decoders: DecoderFactoryFunc(func() (audio.Decoder, error) { return &compositionDecoder{}, nil }),
				Outputs:  OutputFactoryFunc(func(voicesession.Session) (SessionOutput, error) { return newCompositionOutput(), nil }),
			})
			if err != nil {
				t.Fatalf("NewBindingFactory() error = %v", err)
			}
			value := validCompositionSessionAt(now)
			value.UpdatedAt = now.Add(-31 * time.Second)
			value.LeaseExpiresAt = now.Add(90 * time.Second)
			bindingValue, err := factory.Create(webrtc.STTBindingConfig{
				Context: t.Context(), Session: value, Activity: activity,
				Authorization: staticAuthorizationSource("Bearer validated-user-token"),
			})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			binding := bindingValue.(*Binding)
			if err := binding.HandlePeerControl(t.Context(), protocol.ControlMessage{Type: protocol.EventHeartbeat}); err != nil {
				t.Fatalf("initial heartbeat error = %v", err)
			}
			deadline := time.Now().Add(time.Second)
			for activity.closes.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if got := activity.closes.Load(); got != 1 {
				t.Fatalf("activity closes after renewal failure = %d, want 1", got)
			}
			if err := binding.HandleOpus([]byte{0x01}); !errors.Is(err, ErrBindingClosed) {
				t.Fatalf("HandleOpus(after renewal failure) error = %v, want ErrBindingClosed", err)
			}
			if err := binding.Close(); err != nil && strings.Contains(err.Error(), "canary") {
				t.Fatalf("Close() leaked renewal cause: %v", err)
			}
		})
	}
}

func TestBindingFactoryRejectsTypedNilLeaseRenewer(t *testing.T) {
	var renewer *compositionLeaseRenewer
	factory, err := NewBindingFactory(FactoryConfig{
		STT: &compositionSTTOpener{stream: newCompositionSTTStream()}, Chat: &compositionChat{}, LeaseRenewer: renewer,
		Decoders: DecoderFactoryFunc(func() (audio.Decoder, error) { return &compositionDecoder{}, nil }),
		Outputs:  OutputFactoryFunc(func(voicesession.Session) (SessionOutput, error) { return newCompositionOutput(), nil }),
	})
	if factory != nil || !errors.Is(err, ErrInvalidFactoryConfig) {
		t.Fatalf("NewBindingFactory(typed nil lease renewer) = (%#v, %v), want ErrInvalidFactoryConfig", factory, err)
	}
}

func validCompositionSession() voicesession.Session {
	return validCompositionSessionAt(time.Now().UTC())
}

func validCompositionSessionAt(now time.Time) voicesession.Session {
	return voicesession.Session{
		ID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST", RuntimeSessionID: "voice-session-01K1ABCDE2FGHIJK3LMNOPQRST",
		UserID: "user-1", BusinessID: "11111111-1111-4111-8111-111111111111",
		Status: voicesession.StatusActive, RuntimeState: voicesession.RuntimeStateRunning,
		FallbackLanguage: "en-IN", PreferredLanguage: "hi-IN",
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
		LeaseExpiresAt: now.Add(2 * time.Minute), ExpiresAt: now.Add(55 * time.Minute), RotateAt: now.Add(52 * time.Minute),
	}
}

func factoryTestStream(t *testing.T, factory *BindingFactory) *compositionSTTStream {
	t.Helper()
	opener, ok := factory.stt.(*compositionSTTOpener)
	if !ok || opener.stream == nil {
		t.Fatal("test factory does not contain composition STT stream")
	}
	return opener.stream
}

type staticAuthorizationSource string

func (source staticAuthorizationSource) Authorization(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(source), nil
}

type compositionDecoder struct{ closes atomic.Int32 }

func (*compositionDecoder) Decode(_ []byte, pcm []int16) (int, error) {
	for index := range pcm {
		pcm[index] = int16(index)
	}
	return audio.SamplesPerFrame, nil
}
func (decoder *compositionDecoder) Close() error { decoder.closes.Add(1); return nil }

type countingCloser struct{ closes atomic.Int32 }

func (closer *countingCloser) Close() error { closer.closes.Add(1); return nil }

type compositionLeaseRenewer struct {
	calls      chan voicesession.LeaseRenewal
	err        error
	panicValue any
}

func (renewer *compositionLeaseRenewer) RenewLease(_ context.Context, request voicesession.LeaseRenewal) (*voicesession.Session, error) {
	if renewer.panicValue != nil {
		panic(renewer.panicValue)
	}
	if renewer.calls != nil {
		renewer.calls <- request
	}
	if renewer.err != nil {
		return nil, renewer.err
	}
	leaseExpiresAt := request.RenewedAt.Add(request.LeaseDuration)
	if leaseExpiresAt.After(request.ExpectedExpiresAt) {
		leaseExpiresAt = request.ExpectedExpiresAt
	}
	return &voicesession.Session{
		ID: request.SessionID, RuntimeSessionID: request.RuntimeSessionID,
		UserID: request.Scope.UserID, BusinessID: request.Scope.BusinessID, BranchID: request.ExpectedBranchID,
		Status: voicesession.StatusActive, RuntimeState: voicesession.RuntimeStateRunning,
		LeaseExpiresAt: leaseExpiresAt, ExpiresAt: request.ExpectedExpiresAt, UpdatedAt: request.RenewedAt,
	}, nil
}

type compositionSTTOpener struct{ stream *compositionSTTStream }

func (opener *compositionSTTOpener) OpenSTT(context.Context) (sarvam.STTStream, error) {
	return opener.stream, nil
}

type compositionSTTStream struct {
	finals   chan sarvam.FinalTranscript
	flushed  chan struct{}
	done     chan struct{}
	flushOne sync.Once
	closeOne sync.Once
}

func newCompositionSTTStream() *compositionSTTStream {
	return &compositionSTTStream{
		finals: make(chan sarvam.FinalTranscript, 1), flushed: make(chan struct{}), done: make(chan struct{}),
	}
}
func (*compositionSTTStream) WritePCM16(context.Context, []byte) error { return nil }
func (stream *compositionSTTStream) Flush(context.Context) error {
	stream.flushOne.Do(func() { close(stream.flushed) })
	return nil
}
func (stream *compositionSTTStream) AwaitFinal(ctx context.Context) (sarvam.FinalTranscript, error) {
	select {
	case final := <-stream.finals:
		return final, nil
	case <-ctx.Done():
		return sarvam.FinalTranscript{}, ctx.Err()
	}
}
func (stream *compositionSTTStream) Done() <-chan struct{} { return stream.done }
func (stream *compositionSTTStream) Close() error {
	stream.closeOne.Do(func() { close(stream.done) })
	return nil
}

type compositionChat struct {
	mu       sync.Mutex
	requests []sarvam.ChatRequest
	calls    atomic.Int32
}

type cancellationCompositionChat struct {
	started   chan struct{}
	cancelled chan error
	once      sync.Once
}

func (chat *cancellationCompositionChat) StreamChat(
	ctx context.Context,
	_ sarvam.ChatRequest,
	_ sarvam.ChatEventHandler,
) (sarvam.ChatResult, error) {
	chat.once.Do(func() { close(chat.started) })
	<-ctx.Done()
	cause := context.Cause(ctx)
	chat.cancelled <- cause
	return sarvam.ChatResult{}, cause
}

func (chat *compositionChat) StreamChat(ctx context.Context, request sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
	chat.calls.Add(1)
	chat.mu.Lock()
	chat.requests = append(chat.requests, cloneCompositionRequest(request))
	chat.mu.Unlock()
	text := "आपका नवीनतम चालान तैयार है।"
	if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: text}); err != nil {
		return sarvam.ChatResult{}, err
	}
	return sarvam.ChatResult{
		Text: text, FinishReason: "stop",
		Usage: sarvam.ChatUsage{PromptTokens: 12, CompletionTokens: 6, TotalTokens: 18},
	}, nil
}

func (chat *compositionChat) lastRequest() sarvam.ChatRequest {
	chat.mu.Lock()
	defer chat.mu.Unlock()
	if len(chat.requests) == 0 {
		return sarvam.ChatRequest{}
	}
	return cloneCompositionRequest(chat.requests[len(chat.requests)-1])
}

func cloneCompositionRequest(request sarvam.ChatRequest) sarvam.ChatRequest {
	cloned := request
	cloned.Messages = append([]sarvam.ChatMessage(nil), request.Messages...)
	cloned.Tools = append([]sarvam.ChatToolDefinition(nil), request.Tools...)
	for index := range cloned.Tools {
		cloned.Tools[index].Parameters = append(json.RawMessage(nil), cloned.Tools[index].Parameters...)
	}
	return cloned
}

type completedCompositionTurn struct {
	Result  turn.TurnResult
	Failure TurnFailure
}

type compositionOutput struct {
	mu        sync.Mutex
	spoken    string
	completed chan completedCompositionTurn
}

type bargeAwareCompositionOutput struct {
	*compositionOutput
	controller *turn.BargeIn
}

func newBargeAwareCompositionOutput() *bargeAwareCompositionOutput {
	return &bargeAwareCompositionOutput{compositionOutput: newCompositionOutput(), controller: turn.NewBargeIn()}
}

func (output *bargeAwareCompositionOutput) BargeInController() *turn.BargeIn {
	return output.controller
}

func (output *bargeAwareCompositionOutput) HandlePeerControl(_ context.Context, message protocol.ControlMessage) error {
	if message.Type != protocol.EventSpeechStarted || output.controller.Current() == 0 {
		return nil
	}
	_, err := output.controller.Interrupt(output.controller.Current())
	return err
}

func newCompositionOutput() *compositionOutput {
	return &compositionOutput{completed: make(chan completedCompositionTurn, 4)}
}
func (output *compositionOutput) HandleTextDelta(_ context.Context, _ uint64, delta string) error {
	output.mu.Lock()
	output.spoken += delta
	output.mu.Unlock()
	return nil
}
func (*compositionOutput) AttachPeer(webrtc.PeerTransport) error { return nil }
func (*compositionOutput) HandlePeerControl(context.Context, protocol.ControlMessage) error {
	return nil
}
func (*compositionOutput) RequestRepeat(context.Context) {}
func (output *compositionOutput) HandleTurnResult(_ context.Context, result turn.TurnResult, failure TurnFailure) {
	output.completed <- completedCompositionTurn{Result: result, Failure: failure}
}
func (output *compositionOutput) text() string {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.spoken
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if len(needle) != 0 && len(value) >= len(needle) {
			for index := 0; index+len(needle) <= len(value); index++ {
				if value[index:index+len(needle)] == needle {
					return true
				}
			}
		}
	}
	return false
}

var (
	_ io.Closer                = (*compositionDecoder)(nil)
	_ webrtc.STTBindingFactory = (*BindingFactory)(nil)
)
