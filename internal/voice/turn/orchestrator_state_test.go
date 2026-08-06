package turn_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/voice/runtime"
	"invoice-backend/internal/voice/turn"
)

func TestSTTStateMachineCannotCallChatBeforeProviderFinal(t *testing.T) {
	t.Parallel()

	chat := &stateMachineChat{}
	orchestrator, err := turn.NewOrchestrator(turn.OrchestratorConfig{
		Context: context.Background(),
		Chat:    chat,
		Window: turn.ConversationWindow{
			SystemPolicy: "voice policy", BusinessContext: "business=b-1", Summary: "none",
		},
		TextHandler: turn.TextDeltaHandlerFunc(func(context.Context, uint64, string) error { return nil }),
	})
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	t.Cleanup(func() { _ = orchestrator.Close() })

	stream := newStateMachineSTTStream()
	opener := &stateMachineSTTOpener{stream: stream}
	reasoned := make(chan error, 1)
	stt, err := runtime.NewSTTSession(runtime.STTSessionConfig{
		Context:          context.Background(),
		Opener:           opener,
		FallbackLanguage: "en-IN",
		FinalTranscriptHandler: runtime.FinalTranscriptHandlerFunc(func(ctx context.Context, final runtime.STTFinalTranscript) {
			_, handleErr := orchestrator.HandleFinal(ctx, turn.FinalTranscript{
				Text: final.Text, DetectedLanguage: final.DetectedLanguage, ResponseLanguage: final.ResponseLanguage,
			})
			reasoned <- handleErr
		}),
		RepeatRequestHandler: runtime.RepeatRequestHandlerFunc(func(context.Context) {
			reasoned <- runtime.ErrSTTNoActiveTurn
		}),
	})
	if err != nil {
		t.Fatalf("NewSTTSession() error = %v", err)
	}
	t.Cleanup(func() { _ = stt.Close() })

	if err := stt.PushPCM16(make([]byte, runtime.STTPCMFrameBytes)); err != nil {
		t.Fatalf("pre-roll PushPCM16() error = %v", err)
	}
	if got := chat.calls.Load(); got != 0 {
		t.Fatalf("chat calls during listening = %d", got)
	}
	if err := stt.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := stt.PushPCM16(make([]byte, runtime.STTPCMFrameBytes)); err != nil {
		t.Fatalf("live PushPCM16() error = %v", err)
	}
	if err := stt.SpeechEnded(); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}
	<-stream.flushed
	if got := chat.calls.Load(); got != 0 {
		t.Fatalf("chat calls while awaiting authoritative final = %d", got)
	}

	stream.finals <- sarvam.FinalTranscript{Text: "नमस्ते", DetectedLanguage: "hi-IN"}
	if err := <-reasoned; err != nil {
		t.Fatalf("final reasoning error = %v", err)
	}
	if got := chat.calls.Load(); got != 1 {
		t.Fatalf("chat calls after authoritative final = %d, want 1", got)
	}
}

type stateMachineChat struct{ calls atomic.Int64 }

func (chat *stateMachineChat) StreamChat(ctx context.Context, _ sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
	chat.calls.Add(1)
	if progress, ok := handler.(sarvam.ChatProgressHandler); ok {
		if err := progress.HandleChatProgress(ctx); err != nil {
			return sarvam.ChatResult{}, err
		}
	}
	if err := handler.HandleChatEvent(ctx, sarvam.ChatEvent{TextDelta: "नमस्ते!"}); err != nil {
		return sarvam.ChatResult{}, err
	}
	return sarvam.ChatResult{Text: "नमस्ते!", FinishReason: "stop"}, nil
}

type stateMachineSTTOpener struct{ stream *stateMachineSTTStream }

func (opener *stateMachineSTTOpener) OpenSTT(context.Context) (sarvam.STTStream, error) {
	return opener.stream, nil
}

type stateMachineSTTStream struct {
	finals  chan sarvam.FinalTranscript
	flushed chan struct{}
	done    chan struct{}

	flushOnce sync.Once
	closeOnce sync.Once
}

func newStateMachineSTTStream() *stateMachineSTTStream {
	return &stateMachineSTTStream{
		finals: make(chan sarvam.FinalTranscript, 1), flushed: make(chan struct{}), done: make(chan struct{}),
	}
}

func (*stateMachineSTTStream) WritePCM16(context.Context, []byte) error { return nil }

func (stream *stateMachineSTTStream) Flush(context.Context) error {
	stream.flushOnce.Do(func() { close(stream.flushed) })
	return nil
}

func (stream *stateMachineSTTStream) AwaitFinal(ctx context.Context) (sarvam.FinalTranscript, error) {
	select {
	case final := <-stream.finals:
		return final, nil
	case <-ctx.Done():
		return sarvam.FinalTranscript{}, ctx.Err()
	}
}

func (stream *stateMachineSTTStream) Done() <-chan struct{} { return stream.done }

func (stream *stateMachineSTTStream) Close() error {
	stream.closeOnce.Do(func() { close(stream.done) })
	return nil
}
