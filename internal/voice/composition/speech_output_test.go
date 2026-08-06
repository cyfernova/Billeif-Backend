package composition

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/voice/audio"
	"invoice-backend/internal/voice/protocol"
	voicesession "invoice-backend/internal/voice/session"
	voicetelemetry "invoice-backend/internal/voice/telemetry"
	"invoice-backend/internal/voice/turn"
)

func TestSpeechOutputStreamsOneTTSGenerationAndCompletesOnlyAfterClientPlayback(t *testing.T) {
	stream := newFakeSpeechTTSStream()
	opener := &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{stream}}
	encoder := &fakeSpeechEncoder{}
	sink := &recordingFinalTurnSink{}
	now := time.Now().UTC()
	clock := &scriptedSpeechClock{times: []time.Time{
		now.Add(2 * time.Second),
		now.Add(3 * time.Second),
		now.Add(4 * time.Second),
		now.Add(5 * time.Second),
	}}
	session := validCompositionSession()
	session.ConsentTranscriptStorage = true
	session.ExpiresAt = now.Add(30 * time.Minute)
	probability := 0.73
	output, err := NewSpeechOutput(SpeechOutputConfig{
		Session: session, TTS: opener,
		Encoders:   SpeechEncoderFactoryFunc(func() (audio.Encoder, error) { return encoder, nil }),
		Pacers:     SpeechPacerFactoryFunc(func() (audio.FramePacer, error) { return instantSpeechPacer{}, nil }),
		FinalTurns: sink, PlaybackTimeout: time.Second, Now: clock.Now,
	})
	if err != nil {
		t.Fatalf("NewSpeechOutput() error = %v", err)
	}
	t.Cleanup(func() { _ = output.Close() })
	transport := &recordingPeerTransport{}
	if err := output.AttachPeer(transport); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}

	turnContext, generation, err := output.BargeInController().Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin generation error = %v", err)
	}
	final := turn.FinalTranscript{
		Text: "show the latest invoice", ProviderLanguage: "fr-FR",
		ResponseLanguage: "ta-IN", SpeechEndedAt: now, STTFinalAt: now.Add(time.Second),
		LanguageProbability: &probability,
	}
	if err := output.BeginTextGeneration(turnContext, generation, final); err != nil {
		t.Fatalf("BeginTextGeneration() error = %v", err)
	}
	for _, delta := range []string{
		"Your latest invoice is ready. ",
		"It was paid yesterday.",
	} {
		if err := output.HandleTextDelta(turnContext, generation, delta); err != nil {
			t.Fatalf("HandleTextDelta(%q) error = %v", delta, err)
		}
	}
	if got := opener.OpenCalls(); got != 1 {
		t.Fatalf("OpenTTS() calls = %d, want one per active generation", got)
	}
	if got := stream.Texts(); len(got) != 2 || got[0] != "Your latest invoice is ready." || got[1] != "It was paid yesterday." {
		t.Fatalf("TTS chunks = %#v", got)
	}

	stream.events <- sarvam.TTSEvent{PCM16: make([]byte, audio.SamplesPerFrame*2), ContentType: "audio/raw"}
	waitForSpeechOutput(t, func() bool { return len(transport.opusPackets()) == 1 }, "first Opus frame was not sent")

	result := turn.TurnResult{
		GenerationID: generation, Transcript: final,
		Text:  "Your latest invoice is ready. It was paid yesterday.",
		Usage: sarvam.ChatUsage{PromptTokens: 7, CompletionTokens: 9, TotalTokens: 16},
		Tools: []turn.ToolOutcome{{Name: "get_invoice", Success: true}},
	}
	complete := make(chan error, 1)
	go func() { complete <- output.CompleteTextGeneration(turnContext, generation, result) }()
	select {
	case <-stream.flushed:
	case <-time.After(time.Second):
		t.Fatal("TTS stream was not flushed")
	}
	stream.events <- sarvam.TTSEvent{Final: true}

	select {
	case err := <-complete:
		t.Fatalf("completion returned before playback acknowledgements: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if got := sink.Turns(); len(got) != 0 {
		t.Fatalf("turns persisted before client playback = %#v", got)
	}
	generationID := int64(generation)
	wrongTurnID := int64(2)
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventPlaybackStarted, GenerationID: &generationID, TurnID: &wrongTurnID,
	}); !errors.Is(err, ErrPeerOutputUnavailable) {
		t.Fatalf("wrong-turn playback.started error = %v, want ErrPeerOutputUnavailable", err)
	}
	turnID := int64(1)
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventPlaybackStarted, GenerationID: &generationID, TurnID: &turnID,
	}); err != nil {
		t.Fatalf("playback.started error = %v", err)
	}
	select {
	case err := <-complete:
		t.Fatalf("completion returned before playback.completed: %v", err)
	default:
	}
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventPlaybackCompleted, GenerationID: &generationID, TurnID: &turnID,
	}); err != nil {
		t.Fatalf("playback.completed error = %v", err)
	}
	if err := <-complete; err != nil {
		t.Fatalf("CompleteTextGeneration() error = %v", err)
	}
	if err := output.BargeInController().CompleteWith(generation, nil); err != nil {
		t.Fatalf("generation CompleteWith() error = %v", err)
	}
	output.HandleTurnResult(context.Background(), result, TurnFailureNone)

	turns := sink.Turns()
	if len(turns) != 1 {
		t.Fatalf("persisted final turns = %#v", turns)
	}
	got := turns[0]
	if got.SessionID != session.ID || got.Sequence != 1 || got.GenerationID != int64(generation) ||
		got.Transcript != final.Text || got.ProviderLanguage != "fr-FR" || got.SelectedLanguage != "ta-IN" || got.Response != result.Text {
		t.Fatalf("persisted final turn identity/content = %#v", got)
	}
	if !got.ExpiresAt.Equal(session.ExpiresAt) || !got.Timings.ClientFirstAudioAt.Equal(now.Add(4*time.Second)) ||
		got.Timings.LLMFirstTokenAt.IsZero() || got.Timings.TTSFirstAudioAt.IsZero() {
		t.Fatalf("persisted final turn timing/expiry = %#v", got)
	}
	if got.Usage.InputTokens == nil || *got.Usage.InputTokens != 7 || got.Usage.OutputTokens == nil || *got.Usage.OutputTokens != 9 ||
		got.Usage.TotalTokens == nil || *got.Usage.TotalTokens != 16 || got.Usage.TTSCharacters == nil || *got.Usage.TTSCharacters != 51 {
		t.Fatalf("persisted usage = %#v", got.Usage)
	}
	messages := transport.controlMessages()
	for index, message := range messages {
		if err := protocol.ValidateControlMessage(message, protocol.RuntimeToClient); err != nil {
			t.Fatalf("control message %d is not valid protocol v1: %#v: %v", index, message, err)
		}
		if message.Sequence != index+1 {
			t.Fatalf("control message sequence %d = %d", index, message.Sequence)
		}
	}
	if len(messages) < 5 || messages[0].Type != protocol.EventTranscriptFinal ||
		messages[0].TurnID == nil || *messages[0].TurnID != 1 || messages[0].Text != final.Text ||
		messages[0].DetectedLanguage != final.ProviderLanguage || messages[0].LanguageProbability == nil ||
		*messages[0].LanguageProbability != probability || messages[1].Type != protocol.EventLanguageSelected ||
		messages[1].Language != final.ResponseLanguage || messages[2].Type != protocol.EventTurnStarted ||
		messages[len(messages)-1].Type != protocol.EventTurnCompleted {
		t.Fatalf("control lifecycle = %#v", messages)
	}
}

func TestSpeechOutputTTSCloseFallsBackToOneFinalAnswerAndNextTurnRemainsUsable(t *testing.T) {
	failedStream := newFakeSpeechTTSStream()
	nextStream := newFakeSpeechTTSStream()
	output, err := NewSpeechOutput(SpeechOutputConfig{
		Session: validCompositionSession(),
		TTS:     &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{failedStream, nextStream}},
		Encoders: SpeechEncoderFactoryFunc(func() (audio.Encoder, error) {
			return &fakeSpeechEncoder{}, nil
		}),
		Pacers:          SpeechPacerFactoryFunc(func() (audio.FramePacer, error) { return instantSpeechPacer{}, nil }),
		PlaybackTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewSpeechOutput() error = %v", err)
	}
	t.Cleanup(func() { _ = output.Close() })
	transport := &recordingPeerTransport{}
	if err := output.AttachPeer(transport); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}

	turnContext, generation, err := output.BargeInController().Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	final := turn.FinalTranscript{
		Text: "show invoice status", ProviderLanguage: "en-IN", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
	}
	if err := output.BeginTextGeneration(turnContext, generation, final); err != nil {
		t.Fatalf("BeginTextGeneration() error = %v", err)
	}
	if err := output.HandleTextDelta(turnContext, generation, "The invoice is paid."); err != nil {
		t.Fatalf("first HandleTextDelta() error = %v", err)
	}
	if err := output.HandleTextDelta(turnContext, generation, " "); err != nil {
		t.Fatalf("whitespace-only streaming delta error = %v", err)
	}
	for _, message := range transport.controlMessages() {
		if message.Type == protocol.EventAnswerFinal {
			t.Fatalf("partial model output escaped as a final answer: %#v", message)
		}
	}
	if err := failedStream.Close(); err != nil {
		t.Fatalf("close failed TTS stream: %v", err)
	}
	waitForSpeechOutput(t, func() bool {
		output.mu.Lock()
		state := output.active
		output.mu.Unlock()
		return state != nil && state.readerError() != nil
	}, "TTS close was not observed")
	if cause := context.Cause(turnContext); cause != nil {
		t.Fatalf("TTS failure canceled completed-text generation: %v", cause)
	}
	if err := output.HandleTextDelta(turnContext, generation, "It was settled yesterday."); err != nil {
		t.Fatalf("text generation after TTS close error = %v", err)
	}
	result := turn.TurnResult{
		GenerationID: generation, Transcript: final,
		Text: "The invoice is paid. It was settled yesterday.",
	}
	if err := output.CompleteTextGeneration(turnContext, generation, result); err != nil {
		t.Fatalf("CompleteTextGeneration() text fallback error = %v", err)
	}
	if err := output.BargeInController().CompleteWith(generation, nil); err != nil {
		t.Fatalf("CompleteWith() error = %v", err)
	}
	output.HandleTurnResult(context.Background(), result, TurnFailureNone)

	messages := transport.controlMessages()
	answerIndex, completedIndex, answers := -1, -1, 0
	for index, message := range messages {
		if err := protocol.ValidateControlMessage(message, protocol.RuntimeToClient); err != nil {
			t.Fatalf("control message %d invalid: %#v: %v", index, message, err)
		}
		switch message.Type {
		case protocol.EventAnswerFinal:
			answers++
			answerIndex = index
			if message.Text != result.Text || message.GenerationID == nil || *message.GenerationID != int64(generation) ||
				message.TurnID == nil || *message.TurnID != 1 {
				t.Fatalf("final answer control = %#v", message)
			}
		case protocol.EventTurnCompleted:
			completedIndex = index
		}
	}
	if answers != 1 || answerIndex < 0 || completedIndex <= answerIndex {
		t.Fatalf("control lifecycle = %#v, want one final answer before turn completion", messages)
	}

	nextContext, nextGeneration, err := output.BargeInController().Begin(context.Background())
	if err != nil {
		t.Fatalf("next Begin() after TTS fallback error = %v", err)
	}
	if err := output.BeginTextGeneration(nextContext, nextGeneration, final); err != nil {
		t.Fatalf("next BeginTextGeneration() after TTS fallback error = %v", err)
	}
	_ = output.BargeInController().Abort(nextGeneration, errors.New("test cleanup"))
}

func TestSpeechOutputPreInstallFailureRetiresPendingTurnTelemetryAndAllowsNextSpeech(t *testing.T) {
	tests := []struct {
		name     string
		tts      sarvam.TTSOpener
		encoders SpeechEncoderFactory
		pacers   SpeechPacerFactory
	}{
		{
			name: "TTS open", tts: &fakeSpeechTTSOpener{},
			encoders: SpeechEncoderFactoryFunc(func() (audio.Encoder, error) { return &fakeSpeechEncoder{}, nil }),
		},
		{
			name: "encoder", tts: &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{newFakeSpeechTTSStream()}},
			encoders: SpeechEncoderFactoryFunc(func() (audio.Encoder, error) {
				return nil, errors.New("encoder setup failed")
			}),
		},
		{
			name: "pacer", tts: &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{newFakeSpeechTTSStream()}},
			encoders: SpeechEncoderFactoryFunc(func() (audio.Encoder, error) { return &fakeSpeechEncoder{}, nil }),
			pacers: SpeechPacerFactoryFunc(func() (audio.FramePacer, error) {
				return nil, errors.New("pacer setup failed")
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
			clock := &incrementingSpeechClock{next: base}
			var recordersMu sync.Mutex
			var recorders []*recordingTurnTelemetry
			output, err := NewSpeechOutput(SpeechOutputConfig{
				Session: validCompositionSession(), TTS: test.tts, Encoders: test.encoders, Pacers: test.pacers,
				Telemetry: TurnTelemetryFactoryFunc(func() (voicetelemetry.RuntimeTurnTelemetry, error) {
					recorder := &recordingTurnTelemetry{}
					recordersMu.Lock()
					recorders = append(recorders, recorder)
					recordersMu.Unlock()
					return recorder, nil
				}),
				Now: clock.Now,
			})
			if err != nil {
				t.Fatalf("NewSpeechOutput() error = %v", err)
			}
			t.Cleanup(func() { _ = output.Close() })
			if err := output.AttachPeer(&recordingPeerTransport{}); err != nil {
				t.Fatalf("AttachPeer() error = %v", err)
			}

			clientTime := int64(1)
			firstTurnID := int64(1)
			for _, event := range []protocol.EventType{protocol.EventSpeechStarted, protocol.EventSpeechEnded} {
				if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
					Type: event, TurnID: &firstTurnID, ClientMonotonicMS: &clientTime,
				}); err != nil {
					t.Fatalf("first %s error = %v", event, err)
				}
				clientTime++
			}
			turnContext, generation, err := output.BargeInController().Begin(context.Background())
			if err != nil {
				t.Fatalf("Begin() error = %v", err)
			}
			final := turn.FinalTranscript{Text: "hello", ProviderLanguage: "en-IN", ResponseLanguage: "en-IN"}
			if err := output.BeginTextGeneration(turnContext, generation, final); !errors.Is(err, ErrSpeechOutputUnavailable) {
				t.Fatalf("BeginTextGeneration() error = %v, want ErrSpeechOutputUnavailable", err)
			}
			_ = output.BargeInController().Abort(generation, ErrSpeechOutputUnavailable)
			result := turn.TurnResult{GenerationID: generation, Transcript: final}
			output.HandleTurnResult(context.Background(), result, TurnFailureUnavailable)

			recordersMu.Lock()
			created := append([]*recordingTurnTelemetry(nil), recorders...)
			recordersMu.Unlock()
			if len(created) != 1 {
				t.Fatalf("telemetry recorders = %d, want one", len(created))
			}
			if got := created[0].EventNames(); !equalStrings(got, []string{"speech_started", "speech_ended", "fail"}) {
				t.Fatalf("setup-failed telemetry lifecycle = %#v", got)
			}

			secondTurnID := int64(2)
			if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
				Type: protocol.EventSpeechStarted, TurnID: &secondTurnID, ClientMonotonicMS: &clientTime,
			}); err != nil {
				t.Fatalf("next speech.started after setup failure error = %v", err)
			}
			// A duplicate/stale setup result must not retire the newly pending turn.
			output.HandleTurnResult(context.Background(), result, TurnFailureUnavailable)
			clientTime++
			if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
				Type: protocol.EventSpeechEnded, TurnID: &secondTurnID, ClientMonotonicMS: &clientTime,
			}); err != nil {
				t.Fatalf("next speech.ended after stale result error = %v", err)
			}
		})
	}
}

func TestSpeechOutputInterruptClosesTTSAndNeverCompletesOrPersists(t *testing.T) {
	stream := newFakeSpeechTTSStream()
	opener := &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{stream}}
	sink := &recordingFinalTurnSink{}
	session := validCompositionSession()
	session.ConsentTranscriptStorage = true
	session.ExpiresAt = time.Now().UTC().Add(30 * time.Minute)
	output, err := NewSpeechOutput(SpeechOutputConfig{
		Session: session, TTS: opener,
		Encoders:   SpeechEncoderFactoryFunc(func() (audio.Encoder, error) { return &fakeSpeechEncoder{}, nil }),
		Pacers:     SpeechPacerFactoryFunc(func() (audio.FramePacer, error) { return instantSpeechPacer{}, nil }),
		FinalTurns: sink, PlaybackTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewSpeechOutput() error = %v", err)
	}
	t.Cleanup(func() { _ = output.Close() })
	if err := output.AttachPeer(&recordingPeerTransport{}); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}
	turnContext, generation, err := output.BargeInController().Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	final := turn.FinalTranscript{Text: "hello", ProviderLanguage: "en-IN", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"}
	if err := output.BeginTextGeneration(turnContext, generation, final); err != nil {
		t.Fatalf("BeginTextGeneration() error = %v", err)
	}
	if err := output.HandleTextDelta(turnContext, generation, "This answer must be interrupted."); err != nil {
		t.Fatalf("HandleTextDelta() error = %v", err)
	}
	stream.events <- sarvam.TTSEvent{PCM16: make([]byte, audio.SamplesPerFrame*4*2), ContentType: "audio/raw"}
	generationID := int64(generation)
	wrongTurnID := int64(2)
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventInterrupt, GenerationID: &generationID, TurnID: &wrongTurnID,
	}); !errors.Is(err, ErrPeerOutputUnavailable) {
		t.Fatalf("wrong-turn interrupt error = %v, want ErrPeerOutputUnavailable", err)
	}
	turnID := int64(1)
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventInterrupt, GenerationID: &generationID, TurnID: &turnID,
	}); err != nil {
		t.Fatalf("interrupt error = %v", err)
	}
	if !errors.Is(context.Cause(turnContext), turn.ErrTurnInterrupted) {
		t.Fatalf("turn context cause = %v", context.Cause(turnContext))
	}
	if got := stream.closeCalls.Load(); got != 1 {
		t.Fatalf("TTS close calls = %d, want 1", got)
	}
	if output.BargeInController().Current() != generation+1 {
		t.Fatalf("current generation = %d, want %d", output.BargeInController().Current(), generation+1)
	}
	result := turn.TurnResult{GenerationID: generation, Transcript: final, Text: "This answer must be interrupted."}
	output.HandleTurnResult(context.Background(), result, TurnFailureCanceled)
	if got := sink.Turns(); len(got) != 0 {
		t.Fatalf("interrupted final turns = %#v", got)
	}
}

func TestSpeechOutputPlaybackAcknowledgementWaitIsBoundedAndAbortsGeneration(t *testing.T) {
	stream := newFakeSpeechTTSStream()
	output, err := NewSpeechOutput(SpeechOutputConfig{
		Session: validCompositionSession(), TTS: &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{stream}},
		Encoders:        SpeechEncoderFactoryFunc(func() (audio.Encoder, error) { return &fakeSpeechEncoder{}, nil }),
		Pacers:          SpeechPacerFactoryFunc(func() (audio.FramePacer, error) { return instantSpeechPacer{}, nil }),
		PlaybackTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSpeechOutput() error = %v", err)
	}
	t.Cleanup(func() { _ = output.Close() })
	if err := output.AttachPeer(&recordingPeerTransport{}); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}
	turnContext, generation, err := output.BargeInController().Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	final := turn.FinalTranscript{Text: "hello", ProviderLanguage: "en-IN", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN"}
	if err := output.BeginTextGeneration(turnContext, generation, final); err != nil {
		t.Fatalf("BeginTextGeneration() error = %v", err)
	}
	if err := output.HandleTextDelta(turnContext, generation, "A bounded spoken response."); err != nil {
		t.Fatalf("HandleTextDelta() error = %v", err)
	}
	stream.events <- sarvam.TTSEvent{PCM16: make([]byte, audio.SamplesPerFrame*2), ContentType: "audio/raw"}
	result := turn.TurnResult{GenerationID: generation, Transcript: final, Text: "A bounded spoken response."}
	completed := make(chan error, 1)
	go func() { completed <- output.CompleteTextGeneration(turnContext, generation, result) }()
	<-stream.flushed
	stream.events <- sarvam.TTSEvent{Final: true}
	select {
	case err := <-completed:
		if !errors.Is(err, ErrSpeechOutputUnavailable) {
			t.Fatalf("CompleteTextGeneration() error = %v, want ErrSpeechOutputUnavailable", err)
		}
	case <-time.After(time.Second):
		t.Fatal("playback acknowledgement wait was not bounded")
	}
	if cause := context.Cause(turnContext); !errors.Is(cause, ErrSpeechOutputUnavailable) {
		t.Fatalf("generation context cause = %v, want ErrSpeechOutputUnavailable", cause)
	}
}

func TestSpeechOutputSeparatesInterruptedWireTurnsFromDurableSequence(t *testing.T) {
	now := time.Now().UTC()
	firstStream := newFakeSpeechTTSStream()
	secondStream := newFakeSpeechTTSStream()
	sink := &recordingFinalTurnSink{}
	session := validCompositionSession()
	session.GenerationID = 40
	session.ConsentTranscriptStorage = true
	session.ExpiresAt = now.Add(30 * time.Minute)
	output, err := NewSpeechOutput(SpeechOutputConfig{
		Session: session, TTS: &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{firstStream, secondStream}},
		Encoders:   SpeechEncoderFactoryFunc(func() (audio.Encoder, error) { return &fakeSpeechEncoder{}, nil }),
		Pacers:     SpeechPacerFactoryFunc(func() (audio.FramePacer, error) { return instantSpeechPacer{}, nil }),
		FinalTurns: sink, PlaybackTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewSpeechOutput() error = %v", err)
	}
	t.Cleanup(func() { _ = output.Close() })
	transport := &recordingPeerTransport{}
	if err := output.AttachPeer(transport); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}
	clientTime := int64(10)
	firstTurnID := int64(1)
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventSpeechStarted, TurnID: &firstTurnID, ClientMonotonicMS: &clientTime,
	}); err != nil {
		t.Fatalf("first speech.started error = %v", err)
	}
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventSpeechEnded, TurnID: &firstTurnID, ClientMonotonicMS: &clientTime,
	}); err != nil {
		t.Fatalf("first speech.ended error = %v", err)
	}
	firstContext, firstGeneration, err := output.BargeInController().Begin(context.Background())
	if err != nil || firstGeneration != 41 {
		t.Fatalf("first Begin() = (%d, %v), want generation 41", firstGeneration, err)
	}
	firstFinal := turn.FinalTranscript{
		Text: "first transcript", ProviderLanguage: "en-IN", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
		SpeechEndedAt: now, STTFinalAt: now.Add(time.Second),
	}
	if err := output.BeginTextGeneration(firstContext, firstGeneration, firstFinal); err != nil {
		t.Fatalf("first BeginTextGeneration() error = %v", err)
	}

	secondTurnID := int64(2)
	clientTime++
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventSpeechStarted, TurnID: &secondTurnID, ClientMonotonicMS: &clientTime,
	}); err != nil {
		t.Fatalf("second speech.started error = %v", err)
	}
	if !errors.Is(context.Cause(firstContext), turn.ErrTurnInterrupted) {
		t.Fatalf("first generation cause = %v", context.Cause(firstContext))
	}
	output.HandleTurnResult(context.Background(), turn.TurnResult{
		GenerationID: firstGeneration, Transcript: firstFinal,
	}, TurnFailureCanceled)
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventSpeechEnded, TurnID: &secondTurnID, ClientMonotonicMS: &clientTime,
	}); err != nil {
		t.Fatalf("second speech.ended error = %v", err)
	}

	secondContext, secondGeneration, err := output.BargeInController().Begin(context.Background())
	if err != nil || secondGeneration != 42 {
		t.Fatalf("second Begin() = (%d, %v), want generation 42", secondGeneration, err)
	}
	secondFinal := turn.FinalTranscript{
		Text: "replacement transcript", ProviderLanguage: "en-IN", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
		SpeechEndedAt: now.Add(2 * time.Second), STTFinalAt: now.Add(3 * time.Second),
	}
	if err := output.BeginTextGeneration(secondContext, secondGeneration, secondFinal); err != nil {
		t.Fatalf("second BeginTextGeneration() error = %v", err)
	}
	if err := output.HandleTextDelta(secondContext, secondGeneration, "Replacement answer."); err != nil {
		t.Fatalf("HandleTextDelta() error = %v", err)
	}
	secondStream.events <- sarvam.TTSEvent{PCM16: make([]byte, audio.SamplesPerFrame*2), ContentType: "audio/raw"}
	waitForSpeechOutput(t, func() bool { return len(transport.opusPackets()) == 1 }, "replacement first audio was not sent")
	result := turn.TurnResult{GenerationID: secondGeneration, Transcript: secondFinal, Text: "Replacement answer."}
	completed := make(chan error, 1)
	go func() { completed <- output.CompleteTextGeneration(secondContext, secondGeneration, result) }()
	<-secondStream.flushed
	secondStream.events <- sarvam.TTSEvent{Final: true}
	secondGenerationID := int64(secondGeneration)
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventPlaybackStarted, TurnID: &secondTurnID, GenerationID: &secondGenerationID,
	}); err != nil {
		t.Fatalf("replacement playback.started error = %v", err)
	}
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventPlaybackCompleted, TurnID: &secondTurnID, GenerationID: &secondGenerationID,
	}); err != nil {
		t.Fatalf("replacement playback.completed error = %v", err)
	}
	if err := <-completed; err != nil {
		t.Fatalf("replacement CompleteTextGeneration() error = %v", err)
	}
	if err := output.BargeInController().CompleteWith(secondGeneration, nil); err != nil {
		t.Fatalf("replacement CompleteWith() error = %v", err)
	}
	output.HandleTurnResult(context.Background(), result, TurnFailureNone)

	turns := sink.Turns()
	if len(turns) != 1 || turns[0].Sequence != 1 || turns[0].GenerationID != 42 {
		t.Fatalf("durable turns = %#v, want first durable sequence 1 at generation 42", turns)
	}
	messages := transport.controlMessages()
	var cancelledFirst, startedSecond, completedSecond bool
	for _, message := range messages {
		if message.Type == protocol.EventTurnCancelled && message.TurnID != nil && *message.TurnID == 1 &&
			message.GenerationID != nil && *message.GenerationID == 41 {
			cancelledFirst = true
		}
		if message.Type == protocol.EventTurnStarted && message.TurnID != nil && *message.TurnID == 2 &&
			message.GenerationID != nil && *message.GenerationID == 42 {
			startedSecond = true
		}
		if message.Type == protocol.EventTurnCompleted && message.TurnID != nil && *message.TurnID == 2 &&
			message.GenerationID != nil && *message.GenerationID == 42 {
			completedSecond = true
		}
	}
	if !cancelledFirst || !startedSecond || !completedSecond {
		t.Fatalf("wire lifecycle missing canceled 1/41 or replacement 2/42: %#v", messages)
	}
}

func TestSpeechOutputDurabilityFailureDoesNotPoisonConversation(t *testing.T) {
	now := time.Now().UTC()
	sink := &failingFinalTurnSink{err: voicesession.ErrFinalTurnWorkerFailed}
	telemetryRecorder := &recordingTurnTelemetry{}
	nextStream := newFakeSpeechTTSStream()
	session := validCompositionSession()
	session.ConsentTranscriptStorage = true
	session.ExpiresAt = now.Add(30 * time.Minute)
	output, err := NewSpeechOutput(SpeechOutputConfig{
		Session: session, TTS: &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{nextStream}},
		Encoders:   SpeechEncoderFactoryFunc(func() (audio.Encoder, error) { return &fakeSpeechEncoder{}, nil }),
		Pacers:     SpeechPacerFactoryFunc(func() (audio.FramePacer, error) { return instantSpeechPacer{}, nil }),
		FinalTurns: sink, PlaybackTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewSpeechOutput() error = %v", err)
	}
	t.Cleanup(func() { _ = output.Close() })
	transport := &recordingPeerTransport{}
	if err := output.AttachPeer(transport); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}

	turnContext, generation, err := output.BargeInController().Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin(first) error = %v", err)
	}
	final := turn.FinalTranscript{
		Text: "first transcript", ProviderLanguage: "en-IN", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
		SpeechEndedAt: now, STTFinalAt: now.Add(time.Millisecond),
	}
	result := turn.TurnResult{GenerationID: generation, Transcript: final, Text: "first response"}
	state := &speechGeneration{
		id: generation, turnID: 1, sequence: 1, ctx: turnContext, final: final,
		providerFinal: true, answerSent: true, lifecycleDone: true, playbackStarted: true, playbackComplete: true,
		result: result, clientCompleted: now.Add(2 * time.Millisecond),
		telemetry: &turnTelemetryState{recorder: telemetryRecorder},
	}
	output.mu.Lock()
	output.active = state
	output.lastWireTurnID = 1
	output.mu.Unlock()
	if err := output.BargeInController().CompleteWith(generation, nil); err != nil {
		t.Fatalf("CompleteWith(first) error = %v", err)
	}
	output.HandleTurnResult(context.Background(), result, TurnFailureNone)

	output.mu.Lock()
	failed := output.failed
	degraded := output.durabilityDegraded
	sequence := output.turnSequence
	output.mu.Unlock()
	if failed || !degraded || sequence != 0 {
		t.Fatalf("post-failure state failed=%v degraded=%v sequence=%d, want false/true/0", failed, degraded, sequence)
	}
	if sink.calls.Load() != 1 {
		t.Fatalf("durable sink calls = %d, want 1", sink.calls.Load())
	}
	if got := telemetryRecorder.DurabilityFailures(); got != 1 {
		t.Fatalf("durability failure telemetry = %d, want 1", got)
	}
	for _, message := range transport.controlMessages() {
		if message.Type == protocol.EventError {
			t.Fatalf("durability-only failure emitted fatal voice error: %#v", message)
		}
	}

	secondTurnID := int64(2)
	clientTime := int64(20)
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventSpeechStarted, TurnID: &secondTurnID, ClientMonotonicMS: &clientTime,
	}); err != nil {
		t.Fatalf("speech.started after durability degradation error = %v", err)
	}
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventSpeechEnded, TurnID: &secondTurnID, ClientMonotonicMS: &clientTime,
	}); err != nil {
		t.Fatalf("speech.ended after durability degradation error = %v", err)
	}
	secondContext, secondGeneration, err := output.BargeInController().Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin(second) error = %v", err)
	}
	secondFinal := turn.FinalTranscript{
		Text: "second transcript", ProviderLanguage: "en-IN", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
		SpeechEndedAt: now.Add(time.Second), STTFinalAt: now.Add(time.Second + time.Millisecond),
	}
	if err := output.BeginTextGeneration(secondContext, secondGeneration, secondFinal); err != nil {
		t.Fatalf("BeginTextGeneration after durability degradation error = %v", err)
	}
	_ = output.BargeInController().Abort(secondGeneration, context.Canceled)
	output.HandleTurnResult(context.Background(), turn.TurnResult{
		GenerationID: secondGeneration, Transcript: secondFinal,
	}, TurnFailureCanceled)
	if sink.calls.Load() != 1 {
		t.Fatalf("disabled durable sink calls = %d, want 1", sink.calls.Load())
	}
}

func TestSpeechOutputEmitsOneBoundedTelemetryLifecyclePerCompletedTurn(t *testing.T) {
	base := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	clock := &incrementingSpeechClock{next: base}
	recorder := &recordingTurnTelemetry{}
	stream := newFakeSpeechTTSStream()
	output, err := NewSpeechOutput(SpeechOutputConfig{
		Session: validCompositionSession(),
		TTS:     &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{stream}},
		Encoders: SpeechEncoderFactoryFunc(func() (audio.Encoder, error) {
			return &fakeSpeechEncoder{}, nil
		}),
		Pacers: SpeechPacerFactoryFunc(func() (audio.FramePacer, error) { return instantSpeechPacer{}, nil }),
		Telemetry: TurnTelemetryFactoryFunc(func() (voicetelemetry.RuntimeTurnTelemetry, error) {
			return recorder, nil
		}),
		PlaybackTimeout: time.Second,
		Now:             clock.Now,
	})
	if err != nil {
		t.Fatalf("NewSpeechOutput() error = %v", err)
	}
	t.Cleanup(func() { _ = output.Close() })
	transport := &recordingPeerTransport{}
	if err := output.AttachPeer(transport); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}

	turnID := int64(1)
	clientTime := int64(10)
	for _, event := range []protocol.EventType{protocol.EventSpeechStarted, protocol.EventSpeechEnded} {
		if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
			Type: event, TurnID: &turnID, ClientMonotonicMS: &clientTime,
		}); err != nil {
			t.Fatalf("HandlePeerControl(%s) error = %v", event, err)
		}
		clientTime++
	}
	turnContext, generation, err := output.BargeInController().Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	final := turn.FinalTranscript{
		Text: "show invoices", ProviderLanguage: "en-IN", DetectedLanguage: "en-IN", ResponseLanguage: "en-IN",
		SpeechEndedAt: base.Add(time.Second), STTFinalAt: base.Add(2 * time.Second),
	}
	if err := output.BeginTextGeneration(turnContext, generation, final); err != nil {
		t.Fatalf("BeginTextGeneration() error = %v", err)
	}
	if err := output.HandleTextDelta(turnContext, generation, "Here is the result."); err != nil {
		t.Fatalf("HandleTextDelta() error = %v", err)
	}
	stream.events <- sarvam.TTSEvent{PCM16: make([]byte, audio.SamplesPerFrame*2), ContentType: "audio/raw"}
	waitForSpeechOutput(t, func() bool { return len(transport.opusPackets()) == 1 }, "first audio was not sent")

	result := turn.TurnResult{
		GenerationID: generation, Transcript: final, Text: "Here is the result.",
		Usage: sarvam.ChatUsage{PromptTokens: 11, CompletionTokens: 4, TotalTokens: 15},
	}
	completed := make(chan error, 1)
	go func() { completed <- output.CompleteTextGeneration(turnContext, generation, result) }()
	<-stream.flushed
	stream.events <- sarvam.TTSEvent{Final: true}
	generationID := int64(generation)
	for _, event := range []protocol.EventType{protocol.EventPlaybackStarted, protocol.EventPlaybackCompleted} {
		if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
			Type: event, GenerationID: &generationID, TurnID: &turnID,
		}); err != nil {
			t.Fatalf("HandlePeerControl(%s) error = %v", event, err)
		}
	}
	if err := <-completed; err != nil {
		t.Fatalf("CompleteTextGeneration() error = %v", err)
	}
	if err := output.BargeInController().CompleteWith(generation, nil); err != nil {
		t.Fatalf("CompleteWith() error = %v", err)
	}
	output.HandleTurnResult(context.Background(), result, TurnFailureNone)

	want := []string{
		"speech_started", "speech_ended", "stt_final_received", "llm_request_started",
		"llm_first_token", "tts_request_started", "tts_first_audio", "client_first_audio_played", "complete",
	}
	if got := recorder.EventNames(); !equalStrings(got, want) {
		t.Fatalf("telemetry lifecycle = %#v, want %#v", got, want)
	}
	measurements := recorder.Measurements()
	if measurements.Usage.InputTokens != 11 || measurements.Usage.OutputTokens != 4 ||
		measurements.Usage.TTSCharacters != int64(len("Here is the result.")) || measurements.Signals != (voicetelemetry.Signals{}) {
		t.Fatalf("telemetry measurements = %#v", measurements)
	}
	if !strictlyIncreasingTimes(recorder.EventTimes()) {
		t.Fatalf("telemetry timestamps are not strictly increasing: %#v", recorder.EventTimes())
	}
}

func TestSpeechOutputTelemetryFailureNeverPoisonsRealtimeVoice(t *testing.T) {
	recorder := &recordingTurnTelemetry{err: errors.New("telemetry unavailable")}
	output, err := NewSpeechOutput(SpeechOutputConfig{
		Session:  validCompositionSession(),
		TTS:      &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{newFakeSpeechTTSStream()}},
		Encoders: SpeechEncoderFactoryFunc(func() (audio.Encoder, error) { return &fakeSpeechEncoder{}, nil }),
		Telemetry: TurnTelemetryFactoryFunc(func() (voicetelemetry.RuntimeTurnTelemetry, error) {
			return recorder, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSpeechOutput() error = %v", err)
	}
	t.Cleanup(func() { _ = output.Close() })
	if err := output.AttachPeer(&recordingPeerTransport{}); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}
	turnID := int64(1)
	clientTime := int64(1)
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventSpeechStarted, TurnID: &turnID, ClientMonotonicMS: &clientTime,
	}); err != nil {
		t.Fatalf("speech.started was poisoned by telemetry: %v", err)
	}
	if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
		Type: protocol.EventSpeechEnded, TurnID: &turnID, ClientMonotonicMS: &clientTime,
	}); err != nil {
		t.Fatalf("speech.ended was poisoned by telemetry: %v", err)
	}
}

func TestSpeechOutputRepeatRetiresPendingWireTurnAndTelemetry(t *testing.T) {
	var recordersMu sync.Mutex
	var recorders []*recordingTurnTelemetry
	output, err := NewSpeechOutput(SpeechOutputConfig{
		Session:  validCompositionSession(),
		TTS:      &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{newFakeSpeechTTSStream()}},
		Encoders: SpeechEncoderFactoryFunc(func() (audio.Encoder, error) { return &fakeSpeechEncoder{}, nil }),
		Telemetry: TurnTelemetryFactoryFunc(func() (voicetelemetry.RuntimeTurnTelemetry, error) {
			recorder := &recordingTurnTelemetry{}
			recordersMu.Lock()
			recorders = append(recorders, recorder)
			recordersMu.Unlock()
			return recorder, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSpeechOutput() error = %v", err)
	}
	t.Cleanup(func() { _ = output.Close() })
	if err := output.AttachPeer(&recordingPeerTransport{}); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}
	clientTime := int64(1)
	firstTurnID := int64(1)
	for _, event := range []protocol.EventType{protocol.EventSpeechStarted, protocol.EventSpeechEnded} {
		if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
			Type: event, TurnID: &firstTurnID, ClientMonotonicMS: &clientTime,
		}); err != nil {
			t.Fatalf("first %s error = %v", event, err)
		}
		clientTime++
	}
	output.RequestRepeat(context.Background())

	secondTurnID := int64(2)
	for _, event := range []protocol.EventType{protocol.EventSpeechStarted, protocol.EventSpeechEnded} {
		if err := output.HandlePeerControl(context.Background(), protocol.ControlMessage{
			Type: event, TurnID: &secondTurnID, ClientMonotonicMS: &clientTime,
		}); err != nil {
			t.Fatalf("second %s after repeat error = %v", event, err)
		}
		clientTime++
	}
	recordersMu.Lock()
	created := append([]*recordingTurnTelemetry(nil), recorders...)
	recordersMu.Unlock()
	if len(created) != 2 {
		t.Fatalf("telemetry recorders = %d, want one per wire turn", len(created))
	}
	if got := created[0].EventNames(); len(got) != 3 || got[2] != "cancel" {
		t.Fatalf("repeated turn telemetry = %#v, want terminal cancellation", got)
	}
}

func TestSpeechOutputFactoryCreatesFinalSinkOnlyWithTranscriptConsent(t *testing.T) {
	var sinkCalls atomic.Int32
	factory := SpeechOutputFactory{
		TTS: SessionTTSFactoryFunc(func(voicesession.Session) (sarvam.TTSOpener, error) {
			return &fakeSpeechTTSOpener{streams: []*fakeSpeechTTSStream{newFakeSpeechTTSStream()}}, nil
		}),
		Encoders: SpeechEncoderFactoryFunc(func() (audio.Encoder, error) { return &fakeSpeechEncoder{}, nil }),
		FinalTurns: FinalTurnSinkFactoryFunc(func(voicesession.Session) (FinalTurnSinkLease, error) {
			sinkCalls.Add(1)
			return FinalTurnSinkLease{Sink: &recordingFinalTurnSink{}}, nil
		}),
	}
	withoutConsent := validCompositionSession()
	output, err := factory.NewOutput(withoutConsent)
	if err != nil {
		t.Fatalf("NewOutput(no consent) error = %v", err)
	}
	_ = output.(io.Closer).Close()
	if got := sinkCalls.Load(); got != 0 {
		t.Fatalf("final sink factory calls without consent = %d", got)
	}
	withConsent := validCompositionSession()
	withConsent.ConsentTranscriptStorage = true
	withConsent.ExpiresAt = time.Now().UTC().Add(30 * time.Minute)
	output, err = factory.NewOutput(withConsent)
	if err != nil {
		t.Fatalf("NewOutput(consent) error = %v", err)
	}
	_ = output.(io.Closer).Close()
	if got := sinkCalls.Load(); got != 1 {
		t.Fatalf("final sink factory calls with consent = %d, want 1", got)
	}
}

type fakeSpeechTTSOpener struct {
	mu      sync.Mutex
	streams []*fakeSpeechTTSStream
	calls   int
}

func (opener *fakeSpeechTTSOpener) OpenTTS(ctx context.Context, _ string) (sarvam.TTSStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opener.mu.Lock()
	defer opener.mu.Unlock()
	if opener.calls >= len(opener.streams) {
		return nil, errors.New("unexpected fake TTS open")
	}
	stream := opener.streams[opener.calls]
	opener.calls++
	return stream, nil
}

func (opener *fakeSpeechTTSOpener) OpenCalls() int {
	opener.mu.Lock()
	defer opener.mu.Unlock()
	return opener.calls
}

type fakeSpeechTTSStream struct {
	mu         sync.Mutex
	texts      []string
	events     chan sarvam.TTSEvent
	flushed    chan struct{}
	done       chan struct{}
	flushOnce  sync.Once
	closeOnce  sync.Once
	closeCalls atomic.Int32
}

func newFakeSpeechTTSStream() *fakeSpeechTTSStream {
	return &fakeSpeechTTSStream{
		events: make(chan sarvam.TTSEvent, 8), flushed: make(chan struct{}), done: make(chan struct{}),
	}
}

func (stream *fakeSpeechTTSStream) WriteText(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stream.mu.Lock()
	stream.texts = append(stream.texts, text)
	stream.mu.Unlock()
	return nil
}

func (stream *fakeSpeechTTSStream) Flush(context.Context) error {
	stream.flushOnce.Do(func() { close(stream.flushed) })
	return nil
}

func (*fakeSpeechTTSStream) Ping(context.Context) error { return nil }

func (stream *fakeSpeechTTSStream) Next(ctx context.Context) (sarvam.TTSEvent, error) {
	select {
	case event := <-stream.events:
		return event, nil
	case <-stream.done:
		return sarvam.TTSEvent{}, sarvam.ErrTTSStreamClosed
	case <-ctx.Done():
		return sarvam.TTSEvent{}, ctx.Err()
	}
}

func (stream *fakeSpeechTTSStream) Done() <-chan struct{} { return stream.done }

func (stream *fakeSpeechTTSStream) Close() error {
	stream.closeCalls.Add(1)
	stream.closeOnce.Do(func() { close(stream.done) })
	return nil
}

func (stream *fakeSpeechTTSStream) Texts() []string {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return append([]string(nil), stream.texts...)
}

type fakeSpeechEncoder struct{ closes atomic.Int32 }

func (*fakeSpeechEncoder) Encode(_ []int16, destination []byte) (int, error) {
	copy(destination, []byte{0xf8, 0xff, 0xfe})
	return 3, nil
}

func (encoder *fakeSpeechEncoder) Close() error { encoder.closes.Add(1); return nil }

type instantSpeechPacer struct{}

func (instantSpeechPacer) Wait(ctx context.Context) error { return ctx.Err() }

type recordingFinalTurnSink struct {
	mu    sync.Mutex
	turns []voicesession.FinalTurn
}

type failingFinalTurnSink struct {
	err   error
	calls atomic.Int32
}

func (sink *failingFinalTurnSink) EnqueueAfterPlayback(voicesession.FinalTurn) error {
	sink.calls.Add(1)
	return sink.err
}

func (sink *recordingFinalTurnSink) EnqueueAfterPlayback(value voicesession.FinalTurn) error {
	sink.mu.Lock()
	sink.turns = append(sink.turns, value)
	sink.mu.Unlock()
	return nil
}

func (sink *recordingFinalTurnSink) Turns() []voicesession.FinalTurn {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]voicesession.FinalTurn(nil), sink.turns...)
}

type scriptedSpeechClock struct {
	mu    sync.Mutex
	times []time.Time
	index int
}

func (clock *scriptedSpeechClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if clock.index >= len(clock.times) {
		return clock.times[len(clock.times)-1]
	}
	value := clock.times[clock.index]
	clock.index++
	return value
}

type incrementingSpeechClock struct {
	mu   sync.Mutex
	next time.Time
}

func (clock *incrementingSpeechClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.next = clock.next.Add(time.Millisecond)
	return clock.next
}

type telemetryEvent struct {
	name string
	at   time.Time
}

type recordingTurnTelemetry struct {
	mu           sync.Mutex
	events       []telemetryEvent
	measurements voicetelemetry.TurnMeasurements
	err          error
	durability   int
}

func (recorder *recordingTurnTelemetry) record(name string, at time.Time) error {
	recorder.mu.Lock()
	recorder.events = append(recorder.events, telemetryEvent{name: name, at: at})
	err := recorder.err
	recorder.mu.Unlock()
	return err
}

func (recorder *recordingTurnTelemetry) SpeechStarted(at time.Time) error {
	return recorder.record("speech_started", at)
}
func (recorder *recordingTurnTelemetry) SpeechEnded(at time.Time) error {
	return recorder.record("speech_ended", at)
}
func (recorder *recordingTurnTelemetry) STTFinalReceived(at time.Time) error {
	return recorder.record("stt_final_received", at)
}
func (recorder *recordingTurnTelemetry) LLMRequestStarted(at time.Time) error {
	return recorder.record("llm_request_started", at)
}
func (recorder *recordingTurnTelemetry) LLMFirstToken(at time.Time) error {
	return recorder.record("llm_first_token", at)
}
func (recorder *recordingTurnTelemetry) TTSRequestStarted(at time.Time) error {
	return recorder.record("tts_request_started", at)
}
func (recorder *recordingTurnTelemetry) TTSFirstAudio(at time.Time) error {
	return recorder.record("tts_first_audio", at)
}
func (recorder *recordingTurnTelemetry) ClientFirstAudioPlayed(at time.Time) error {
	return recorder.record("client_first_audio_played", at)
}
func (recorder *recordingTurnTelemetry) Interrupt(interruptedAt, playbackStoppedAt time.Time) error {
	if err := recorder.record("interrupt", interruptedAt); err != nil {
		return err
	}
	return recorder.record("playback_stopped", playbackStoppedAt)
}
func (recorder *recordingTurnTelemetry) DurabilityFailure() {
	recorder.mu.Lock()
	recorder.durability++
	recorder.mu.Unlock()
}
func (recorder *recordingTurnTelemetry) terminal(name string, at time.Time, value voicetelemetry.TurnMeasurements) error {
	recorder.mu.Lock()
	recorder.measurements = value
	recorder.mu.Unlock()
	return recorder.record(name, at)
}
func (recorder *recordingTurnTelemetry) Complete(at time.Time, value voicetelemetry.TurnMeasurements) error {
	return recorder.terminal("complete", at, value)
}
func (recorder *recordingTurnTelemetry) Fail(at time.Time, value voicetelemetry.TurnMeasurements) error {
	return recorder.terminal("fail", at, value)
}
func (recorder *recordingTurnTelemetry) Cancel(at time.Time, value voicetelemetry.TurnMeasurements) error {
	return recorder.terminal("cancel", at, value)
}
func (recorder *recordingTurnTelemetry) EventNames() []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	result := make([]string, len(recorder.events))
	for index, event := range recorder.events {
		result[index] = event.name
	}
	return result
}
func (recorder *recordingTurnTelemetry) EventTimes() []time.Time {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	result := make([]time.Time, len(recorder.events))
	for index, event := range recorder.events {
		result[index] = event.at
	}
	return result
}
func (recorder *recordingTurnTelemetry) Measurements() voicetelemetry.TurnMeasurements {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.measurements
}

func (recorder *recordingTurnTelemetry) DurabilityFailures() int {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.durability
}

func equalStrings(left, right []string) bool {
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

func strictlyIncreasingTimes(values []time.Time) bool {
	for index := 1; index < len(values); index++ {
		if !values[index].After(values[index-1]) {
			return false
		}
	}
	return len(values) > 0
}

func (transport *recordingPeerTransport) opusPackets() [][]byte {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	result := make([][]byte, len(transport.opus))
	for index := range transport.opus {
		result[index] = append([]byte(nil), transport.opus[index]...)
	}
	return result
}

func waitForSpeechOutput(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(message)
		}
		time.Sleep(time.Millisecond)
	}
}
