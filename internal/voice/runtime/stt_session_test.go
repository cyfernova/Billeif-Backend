package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/providers/sarvam"
)

func TestSTTSessionOpensOnlyOnSpeechStartedAndReplaysNewestPreRoll(t *testing.T) {
	stream := newFakeSTTStream()
	opener := &fakeSTTOpener{streams: []sarvam.STTStream{stream}}
	finals := make(chan STTFinalTranscript, 1)
	session := newTestSTTSession(t, opener, newFakeSTTClock(), finals, nil)

	for index := 0; index < 30; index++ {
		if err := session.PushPCM16(testPCMFrame(byte(index))); err != nil {
			t.Fatalf("PushPCM16(pre-roll %d) error = %v", index, err)
		}
	}
	if got := opener.OpenCalls(); got != 0 {
		t.Fatalf("OpenSTT() calls before speech.started = %d, want 0", got)
	}

	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}
	wantProbability := 0.91
	stream.SendFinal(sarvam.FinalTranscript{
		Text:                "नमस्ते",
		DetectedLanguage:    "hi-IN",
		LanguageProbability: &wantProbability,
	})

	final := receiveSTTFinal(t, finals)
	if final.Text != "नमस्ते" || final.ProviderLanguage != "hi-IN" || final.DetectedLanguage != "hi-IN" || final.ResponseLanguage != "hi-IN" {
		t.Fatalf("final transcript = %#v, want canonical Hindi result", final)
	}
	if final.LanguageProbability == nil || *final.LanguageProbability != wantProbability {
		t.Fatalf("language probability = %v, want %.2f", final.LanguageProbability, wantProbability)
	}
	writes := stream.Writes()
	if len(writes) != 25 {
		t.Fatalf("pre-roll writes = %d, want 25", len(writes))
	}
	for index, frame := range writes {
		want := byte(index + 5)
		if frame[0] != want || len(frame) != STTPCMFrameBytes {
			t.Fatalf("pre-roll frame %d = (%d, %d bytes), want (%d, %d bytes)", index, frame[0], len(frame), want, STTPCMFrameBytes)
		}
	}
	if got := stream.FlushCalls(); got != 1 {
		t.Fatalf("Flush() calls = %d, want 1", got)
	}
	if got := opener.OpenCalls(); got != 1 {
		t.Fatalf("OpenSTT() calls = %d, want 1", got)
	}
}

func TestSTTSessionSpeechStartedOpensBeforeAudioAndZeroAudioDoesNotFlush(t *testing.T) {
	stream := newFakeSTTStream()
	opener := &fakeSTTOpener{streams: []sarvam.STTStream{stream}}
	repeats := make(chan struct{}, 1)
	session := newTestSTTSession(t, opener, newFakeSTTClock(), nil, repeats)
	if got := opener.OpenCalls(); got != 0 {
		t.Fatalf("OpenSTT() calls before speech.started = %d, want 0", got)
	}
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	waitForSTTCondition(t, func() bool { return opener.OpenCalls() == 1 }, "speech.started did not open STT")
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}
	receiveRepeat(t, repeats)
	if got := stream.FlushCalls(); got != 0 {
		t.Fatalf("zero-audio Flush() calls = %d, want 0", got)
	}
}

func TestSTTSessionKeepsOneStreamWarmForTwoSequentialTurns(t *testing.T) {
	clock := newFakeSTTClock()
	stream := newFakeSTTStream()
	opener := &fakeSTTOpener{streams: []sarvam.STTStream{stream}}
	finals := make(chan STTFinalTranscript, 2)
	session := newTestSTTSession(t, opener, clock, finals, nil)

	completeSTTTurn(t, session, stream, 0x11, sarvam.FinalTranscript{Text: "first", DetectedLanguage: "en-IN"})
	first := receiveSTTFinal(t, finals)
	if first.Text != "first" {
		t.Fatalf("first transcript = %q, want first", first.Text)
	}
	if got := clock.LastDuration(); got != DefaultSTTWarmTimeout {
		t.Fatalf("warm timer = %v, want %v", got, DefaultSTTWarmTimeout)
	}

	completeSTTTurn(t, session, stream, 0x22, sarvam.FinalTranscript{Text: "second", DetectedLanguage: "mr-IN"})
	second := receiveSTTFinal(t, finals)
	if second.Text != "second" || second.ResponseLanguage != "mr-IN" {
		t.Fatalf("second transcript = %#v, want Marathi second turn", second)
	}
	if got := opener.OpenCalls(); got != 1 {
		t.Fatalf("OpenSTT() calls across warm turns = %d, want 1", got)
	}
	if got := stream.FlushCalls(); got != 2 {
		t.Fatalf("Flush() calls across warm turns = %d, want 2", got)
	}
	if stream.IsClosed() {
		t.Fatal("warm stream closed before the 30-second timer")
	}

	clock.FireLatest()
	waitForSTTCondition(t, stream.IsClosed, "warm stream did not close when its 30-second timer fired")
}

func TestSTTSessionFinalBeforeSpeechEndedDoesNotTriggerOrDoubleFlush(t *testing.T) {
	stream := newFakeSTTStream()
	stream.SendFinal(sarvam.FinalTranscript{Text: "early", DetectedLanguage: "en-IN"})
	finals := make(chan STTFinalTranscript, 1)
	session := newTestSTTSession(t, &fakeSTTOpener{streams: []sarvam.STTStream{stream}}, newFakeSTTClock(), finals, nil)

	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := session.PushPCM16(testPCMFrame(0x09)); err != nil {
		t.Fatalf("PushPCM16() error = %v", err)
	}
	select {
	case got := <-finals:
		t.Fatalf("final callback before speech.ended: %#v", got)
	case <-time.After(20 * time.Millisecond):
	}
	if got := stream.FlushCalls(); got != 0 {
		t.Fatalf("Flush() calls before speech.ended = %d, want 0", got)
	}
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}
	if err := session.SpeechEnded(); !errors.Is(err, ErrSTTSpeechAlreadyEnded) {
		t.Fatalf("duplicate SpeechEnded() error = %v, want ErrSTTSpeechAlreadyEnded", err)
	}
	if got := receiveSTTFinal(t, finals); got.Text != "early" {
		t.Fatalf("final text = %q, want early", got.Text)
	}
	waitForSTTCondition(t, func() bool { return stream.FlushCalls() == 1 }, "Flush() was not called exactly once")
}

func TestSTTSessionUsesFallbackWithoutLosingSafeProviderDetection(t *testing.T) {
	stream := newFakeSTTStream()
	finals := make(chan STTFinalTranscript, 1)
	session := newTestSTTSessionWithConfig(t, STTSessionConfig{
		Context:          context.Background(),
		Opener:           &fakeSTTOpener{streams: []sarvam.STTStream{stream}},
		FallbackLanguage: "ta-IN",
		Clock:            newFakeSTTClock(),
		FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(_ context.Context, final STTFinalTranscript) {
			finals <- final
		}),
		RepeatRequestHandler: RepeatRequestHandlerFunc(func(context.Context) {}),
	})

	completeSTTTurn(t, session, stream, 0x33, sarvam.FinalTranscript{Text: "bonjour", DetectedLanguage: "fr-FR"})
	got := receiveSTTFinal(t, finals)
	if got.ProviderLanguage != "fr-FR" || got.DetectedLanguage != "" || got.ResponseLanguage != "ta-IN" || !got.UsedFallback {
		t.Fatalf("unsupported language result = %#v, want provider recorded and Tamil fallback", got)
	}
}

func TestSTTSessionRejectsInvalidFramesAndBoundsLiveQueueAtTwentyFive(t *testing.T) {
	writeGate := make(chan struct{})
	stream := newFakeSTTStream()
	stream.writeGate = writeGate
	session := newTestSTTSession(t, &fakeSTTOpener{streams: []sarvam.STTStream{stream}}, newFakeSTTClock(), nil, nil)

	for _, size := range []int{0, STTPCMFrameBytes - 1, STTPCMFrameBytes + 1} {
		if err := session.PushPCM16(make([]byte, size)); !errors.Is(err, ErrSTTInvalidPCMFrame) {
			t.Fatalf("PushPCM16(%d bytes) error = %v, want ErrSTTInvalidPCMFrame", size, err)
		}
	}
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := session.PushPCM16(testPCMFrame(0)); err != nil {
		t.Fatalf("first live PushPCM16() error = %v", err)
	}
	stream.WaitWriteStarted(t)
	for index := 1; index <= STTWriteQueueFrames; index++ {
		if err := session.PushPCM16(testPCMFrame(byte(index))); err != nil {
			t.Fatalf("queued PushPCM16(%d) error = %v", index, err)
		}
	}
	if err := session.PushPCM16(testPCMFrame(0xff)); !errors.Is(err, ErrSTTWriteQueueFull) {
		t.Fatalf("overflow PushPCM16() error = %v, want ErrSTTWriteQueueFull", err)
	}
	close(writeGate)
}

func TestSTTSessionReconnectsOnceOnlyBeforeAnyAudioWasAccepted(t *testing.T) {
	first := newFakeSTTStream()
	first.writeErrors = []error{errors.New("offline pre-audio failure")}
	second := newFakeSTTStream()
	opener := &fakeSTTOpener{streams: []sarvam.STTStream{first, second}}
	finals := make(chan STTFinalTranscript, 1)
	repeats := make(chan struct{}, 1)
	session := newTestSTTSession(t, opener, newFakeSTTClock(), finals, repeats)

	if err := session.PushPCM16(testPCMFrame(0x51)); err != nil {
		t.Fatalf("PushPCM16(pre-roll) error = %v", err)
	}
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}
	second.SendFinal(sarvam.FinalTranscript{Text: "recovered", DetectedLanguage: "en-IN"})
	if got := receiveSTTFinal(t, finals); got.Text != "recovered" {
		t.Fatalf("recovered final = %#v", got)
	}
	if got := opener.OpenCalls(); got != 2 {
		t.Fatalf("OpenSTT() calls = %d, want exactly two", got)
	}
	if len(first.Writes()) != 0 || len(second.Writes()) != 1 || second.Writes()[0][0] != 0x51 {
		t.Fatalf("retry writes first=%v second=%v, want one full pre-roll replay only on second stream", first.Writes(), second.Writes())
	}
	assertNoRepeat(t, repeats)
}

func TestSTTSessionPostAudioFailureRequestsRepeatWithoutPartialReplay(t *testing.T) {
	first := newFakeSTTStream()
	first.writeErrors = []error{nil, errors.New("post-audio failure")}
	second := newFakeSTTStream()
	opener := &fakeSTTOpener{streams: []sarvam.STTStream{first, second}}
	finals := make(chan STTFinalTranscript, 1)
	repeats := make(chan struct{}, 1)
	session := newTestSTTSession(t, opener, newFakeSTTClock(), finals, repeats)

	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := session.PushPCM16(testPCMFrame(0x61)); err != nil {
		t.Fatalf("PushPCM16(first) error = %v", err)
	}
	if err := session.PushPCM16(testPCMFrame(0x62)); err != nil {
		t.Fatalf("PushPCM16(second) error = %v", err)
	}
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}
	receiveRepeat(t, repeats)
	if got := opener.OpenCalls(); got != 1 {
		t.Fatalf("OpenSTT() calls after post-audio failure = %d, want 1", got)
	}
	if got := len(second.Writes()); got != 0 {
		t.Fatalf("partial replay writes on second stream = %d, want 0", got)
	}
	assertNoFinal(t, finals)
}

func TestSTTSessionFailureBeforeSpeechEndedAcknowledgesThatEventOnce(t *testing.T) {
	stream := newFakeSTTStream()
	stream.writeErrors = []error{errors.New("offline write failure")}
	repeats := make(chan struct{}, 1)
	session := newTestSTTSession(t, &fakeSTTOpener{streams: []sarvam.STTStream{stream}}, newFakeSTTClock(), nil, repeats)
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := session.PushPCM16(testPCMFrame(0x63)); err != nil {
		t.Fatalf("PushPCM16() error = %v", err)
	}
	receiveRepeat(t, repeats)
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("first SpeechEnded() after provider failure error = %v, want nil", err)
	}
	if err := session.SpeechEnded(); !errors.Is(err, ErrSTTSpeechAlreadyEnded) {
		t.Fatalf("duplicate SpeechEnded() error = %v, want ErrSTTSpeechAlreadyEnded", err)
	}
}

func TestSTTSessionDiscardsFailedUtteranceTailBeforeRepeat(t *testing.T) {
	first := newFakeSTTStream()
	first.writeErrors = []error{errors.New("offline write failure")}
	second := newFakeSTTStream()
	opener := &fakeSTTOpener{
		streams: []sarvam.STTStream{first, nil, second},
		errors:  []error{nil, errors.New("offline reconnect failure"), nil},
	}
	finals := make(chan STTFinalTranscript, 1)
	repeats := make(chan struct{}, 1)
	session := newTestSTTSession(t, opener, newFakeSTTClock(), finals, repeats)
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("first SpeechStarted() error = %v", err)
	}
	if err := session.PushPCM16(testPCMFrame(0x64)); err != nil {
		t.Fatalf("PushPCM16(failing frame) error = %v", err)
	}
	receiveRepeat(t, repeats)
	if err := session.PushPCM16(testPCMFrame(0x65)); err != nil {
		t.Fatalf("PushPCM16(failed utterance tail) error = %v", err)
	}
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}

	completeSTTTurn(t, session, second, 0x66, sarvam.FinalTranscript{Text: "repeat", DetectedLanguage: "en-IN"})
	if got := receiveSTTFinal(t, finals); got.Text != "repeat" {
		t.Fatalf("repeat final = %#v", got)
	}
	writes := second.Writes()
	if len(writes) != 1 || writes[0][0] != 0x66 {
		t.Fatalf("repeat stream writes = %v, want only new-utterance marker 0x66", writes)
	}
}

func TestSTTSessionFinalTimeoutAndEmptyFinalRequestRepeat(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		clock := newFakeSTTClock()
		stream := newFakeSTTStream()
		finals := make(chan STTFinalTranscript, 1)
		repeats := make(chan struct{}, 1)
		session := newTestSTTSession(t, &fakeSTTOpener{streams: []sarvam.STTStream{stream}}, clock, finals, repeats)

		if err := session.SpeechStarted(); err != nil {
			t.Fatalf("SpeechStarted() error = %v", err)
		}
		if err := session.PushPCM16(testPCMFrame(0x71)); err != nil {
			t.Fatalf("PushPCM16() error = %v", err)
		}
		if err := session.SpeechEnded(); err != nil {
			t.Fatalf("SpeechEnded() error = %v", err)
		}
		stream.WaitAwaitStarted(t)
		if got := clock.LastDuration(); got != DefaultSTTFinalTimeout {
			t.Fatalf("final timer = %v, want %v", got, DefaultSTTFinalTimeout)
		}
		clock.FireLatest()
		receiveRepeat(t, repeats)
		assertNoFinal(t, finals)
	})

	t.Run("empty final", func(t *testing.T) {
		stream := newFakeSTTStream()
		finals := make(chan STTFinalTranscript, 1)
		repeats := make(chan struct{}, 1)
		session := newTestSTTSession(t, &fakeSTTOpener{streams: []sarvam.STTStream{stream}}, newFakeSTTClock(), finals, repeats)
		completeSTTTurn(t, session, stream, 0x72, sarvam.FinalTranscript{DetectedLanguage: "en-IN"})
		receiveRepeat(t, repeats)
		assertNoFinal(t, finals)
	})
}

func TestSTTSessionCloseCancelsWorkersScrubsAudioAndFencesLateFinals(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	stream := newFakeSTTStream()
	stream.ignoreAwaitContext = true
	finals := make(chan STTFinalTranscript, 1)
	repeats := make(chan struct{}, 1)
	session := newTestSTTSessionWithConfig(t, STTSessionConfig{
		Context:          parent,
		Opener:           &fakeSTTOpener{streams: []sarvam.STTStream{stream}},
		FallbackLanguage: "en-IN",
		Clock:            newFakeSTTClock(),
		FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(_ context.Context, final STTFinalTranscript) {
			finals <- final
		}),
		RepeatRequestHandler: RepeatRequestHandlerFunc(func(context.Context) {
			repeats <- struct{}{}
		}),
	})
	if err := session.PushPCM16(testPCMFrame(0x81)); err != nil {
		t.Fatalf("PushPCM16(pre-roll) error = %v", err)
	}
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}
	stream.WaitAwaitStarted(t)
	session.mu.Lock()
	turnState := session.current
	session.mu.Unlock()
	if turnState == nil {
		t.Fatal("active STT turn missing before cancellation")
	}

	cancel()
	waitForSTTCondition(t, stream.IsClosed, "stream was not closed after parent cancellation")
	stream.SendFinal(sarvam.FinalTranscript{Text: "late", DetectedLanguage: "en-IN"})
	if err := session.Close(); err != nil {
		t.Fatalf("Close() after cancellation error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := session.PushPCM16(testPCMFrame(0x82)); !errors.Is(err, ErrSTTSessionClosed) {
		t.Fatalf("post-close PushPCM16() error = %v, want ErrSTTSessionClosed", err)
	}
	if !allSTTFramesZero(session.preRoll[:]) || !allSTTFramesZero(turnState.preRoll[:]) {
		t.Fatal("Close() retained PCM bytes in pre-roll storage")
	}
	select {
	case frame := <-turnState.frames:
		for _, value := range frame {
			if value != 0 {
				t.Fatal("Close() retained PCM bytes in the live write queue")
			}
		}
	default:
	}
	assertNoFinal(t, finals)
	assertNoRepeat(t, repeats)
}

func TestSTTSessionCallbacksAreOutsideControllerLockAndEpochFenced(t *testing.T) {
	stream := newFakeSTTStream()
	done := make(chan struct{})
	var session *STTSession
	session = newTestSTTSessionWithConfig(t, STTSessionConfig{
		Context:          context.Background(),
		Opener:           &fakeSTTOpener{streams: []sarvam.STTStream{stream}},
		FallbackLanguage: "en-IN",
		Clock:            newFakeSTTClock(),
		FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(context.Context, STTFinalTranscript) {
			if err := session.SpeechStarted(); err != nil {
				t.Errorf("SpeechStarted() from callback error = %v", err)
			}
			close(done)
		}),
		RepeatRequestHandler: RepeatRequestHandlerFunc(func(context.Context) {}),
	})
	completeSTTTurn(t, session, stream, 0x91, sarvam.FinalTranscript{Text: "first", DetectedLanguage: "en-IN"})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("final callback deadlocked under controller lock")
	}
	stream.SendFinal(sarvam.FinalTranscript{Text: "duplicate old final", DetectedLanguage: "en-IN"})
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSTTSessionCloseRetiresResourcesWithoutWaitingForAdmittedCallback(t *testing.T) {
	stream := newFakeSTTStream()
	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	callbackDone := make(chan struct{})
	callbackContext := make(chan context.Context, 1)
	session := newTestSTTSessionWithConfig(t, STTSessionConfig{
		Context:          context.Background(),
		Opener:           &fakeSTTOpener{streams: []sarvam.STTStream{stream}},
		FallbackLanguage: "en-IN",
		Clock:            newFakeSTTClock(),
		FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(ctx context.Context, _ STTFinalTranscript) {
			callbackContext <- ctx
			close(callbackStarted)
			<-releaseCallback
			close(callbackDone)
		}),
		RepeatRequestHandler: RepeatRequestHandlerFunc(func(context.Context) {}),
	})
	completeSTTTurn(t, session, stream, 0xa1, sarvam.FinalTranscript{Text: "accepted", DetectedLanguage: "en-IN"})
	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("admitted final callback did not start")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- session.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() waited for an already-admitted user callback")
	}
	ctx := <-callbackContext
	select {
	case <-ctx.Done():
	default:
		t.Fatal("admitted callback did not observe the canceled session context")
	}
	if !stream.IsClosed() {
		t.Fatal("Close() returned before retiring the provider stream")
	}
	select {
	case <-callbackDone:
		t.Fatal("callback unexpectedly finished before its test release")
	default:
	}
	close(releaseCallback)
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("admitted callback did not unwind after release")
	}
}

func TestSTTSessionFinalCallbackCanCloseItsOwnSession(t *testing.T) {
	stream := newFakeSTTStream()
	closeResult := make(chan error, 1)
	var session *STTSession
	var err error
	session, err = NewSTTSession(STTSessionConfig{
		Context:          context.Background(),
		Opener:           &fakeSTTOpener{streams: []sarvam.STTStream{stream}},
		FallbackLanguage: "en-IN",
		Clock:            newFakeSTTClock(),
		FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(context.Context, STTFinalTranscript) {
			closeResult <- session.Close()
		}),
		RepeatRequestHandler: RepeatRequestHandlerFunc(func(context.Context) {}),
	})
	if err != nil {
		t.Fatalf("NewSTTSession() error = %v", err)
	}
	completeSTTTurn(t, session, stream, 0xa2, sarvam.FinalTranscript{Text: "close", DetectedLanguage: "en-IN"})
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("callback Close() error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("callback Close() deadlocked on its own provider worker")
	}
}

func TestSTTSessionRepeatCallbackCanCloseItsOwnSession(t *testing.T) {
	openFailure := errors.New("offline open failure")
	closeResult := make(chan error, 1)
	var session *STTSession
	var err error
	session, err = NewSTTSession(STTSessionConfig{
		Context:                context.Background(),
		Opener:                 &fakeSTTOpener{errors: []error{openFailure, openFailure}},
		FallbackLanguage:       "en-IN",
		Clock:                  newFakeSTTClock(),
		FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(context.Context, STTFinalTranscript) {}),
		RepeatRequestHandler: RepeatRequestHandlerFunc(func(context.Context) {
			closeResult <- session.Close()
		}),
	})
	if err != nil {
		t.Fatalf("NewSTTSession() error = %v", err)
	}
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("repeat callback Close() error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("repeat callback Close() deadlocked on its own provider worker")
	}
}

func TestSTTSessionClearsPreRollSnapshotAndNeverReplaysActiveTurnTail(t *testing.T) {
	stream := newFakeSTTStream()
	finals := make(chan STTFinalTranscript, 2)
	session := newTestSTTSession(t, &fakeSTTOpener{streams: []sarvam.STTStream{stream}}, newFakeSTTClock(), finals, nil)

	preRoll := testPCMFrame(0x10)
	if err := session.PushPCM16(preRoll); err != nil {
		t.Fatalf("PushPCM16(pre-roll) error = %v", err)
	}
	preRoll[0] = 0xff
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("first SpeechStarted() error = %v", err)
	}
	active := testPCMFrame(0x20)
	if err := session.PushPCM16(active); err != nil {
		t.Fatalf("PushPCM16(active) error = %v", err)
	}
	active[0] = 0xff
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("first SpeechEnded() error = %v", err)
	}
	stream.SendFinal(sarvam.FinalTranscript{Text: "first", DetectedLanguage: "en-IN"})
	receiveSTTFinal(t, finals)
	completeSTTTurn(t, session, stream, 0x30, sarvam.FinalTranscript{Text: "second", DetectedLanguage: "en-IN"})
	receiveSTTFinal(t, finals)

	writes := stream.Writes()
	if len(writes) != 3 {
		t.Fatalf("writes across two turns = %d, want pre-roll plus two live frames", len(writes))
	}
	for index, want := range []byte{0x10, 0x20, 0x30} {
		if writes[index][0] != want {
			t.Fatalf("write %d marker = 0x%x, want 0x%x", index, writes[index][0], want)
		}
	}
}

func TestSTTSessionWarmAndFinalTimersAreEpochFenced(t *testing.T) {
	clock := newFakeSTTClock()
	stream := newFakeSTTStream()
	finals := make(chan STTFinalTranscript, 2)
	repeats := make(chan struct{}, 1)
	session := newTestSTTSession(t, &fakeSTTOpener{streams: []sarvam.STTStream{stream}}, clock, finals, repeats)

	completeSTTTurn(t, session, stream, 0x41, sarvam.FinalTranscript{Text: "first", DetectedLanguage: "en-IN"})
	receiveSTTFinal(t, finals)
	firstFinalTimer := clock.Timer(0)
	firstWarmTimer := clock.Timer(1)
	if firstFinalTimer == nil || firstWarmTimer == nil {
		t.Fatal("first final/warm timers were not scheduled")
	}
	firstFinalTimer.ForceFire()
	assertNoFinal(t, finals)
	assertNoRepeat(t, repeats)

	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("second SpeechStarted() error = %v", err)
	}
	firstWarmTimer.ForceFire()
	if stream.IsClosed() {
		t.Fatal("stale stopped warm timer closed stream used by a newer epoch")
	}
	if err := session.PushPCM16(testPCMFrame(0x42)); err != nil {
		t.Fatalf("PushPCM16(second) error = %v", err)
	}
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("second SpeechEnded() error = %v", err)
	}
	stream.SendFinal(sarvam.FinalTranscript{Text: "second", DetectedLanguage: "en-IN"})
	receiveSTTFinal(t, finals)
	secondWarmTimer := clock.Timer(3)
	if secondWarmTimer == nil {
		t.Fatal("second warm timer was not scheduled")
	}
	firstWarmTimer.ForceFire()
	if stream.IsClosed() {
		t.Fatal("old warm epoch closed the current warm stream")
	}
	secondWarmTimer.ForceFire()
	waitForSTTCondition(t, stream.IsClosed, "current warm timer did not close stream")
}

func TestSTTSessionDoesNotReuseWarmStreamWhoseDoneChannelClosed(t *testing.T) {
	first := newFakeSTTStream()
	second := newFakeSTTStream()
	opener := &fakeSTTOpener{streams: []sarvam.STTStream{first, second}}
	finals := make(chan STTFinalTranscript, 2)
	session := newTestSTTSession(t, opener, newFakeSTTClock(), finals, nil)

	completeSTTTurn(t, session, first, 0x51, sarvam.FinalTranscript{Text: "first", DetectedLanguage: "en-IN"})
	receiveSTTFinal(t, finals)
	if err := first.Close(); err != nil {
		t.Fatalf("close warm fake stream: %v", err)
	}
	completeSTTTurn(t, session, second, 0x52, sarvam.FinalTranscript{Text: "second", DetectedLanguage: "en-IN"})
	receiveSTTFinal(t, finals)
	if got := opener.OpenCalls(); got != 2 {
		t.Fatalf("OpenSTT() calls after warm Done = %d, want 2", got)
	}
}

func TestSTTSessionOpenFailureRetriesOnceThenRequestsOneRepeat(t *testing.T) {
	openFailure := errors.New("offline dial failure")
	opener := &fakeSTTOpener{errors: []error{openFailure, openFailure}}
	finals := make(chan STTFinalTranscript, 1)
	repeats := make(chan struct{}, 2)
	session := newTestSTTSession(t, opener, newFakeSTTClock(), finals, repeats)
	if err := session.PushPCM16(testPCMFrame(0x61)); err != nil {
		t.Fatalf("PushPCM16(pre-roll) error = %v", err)
	}
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}
	receiveRepeat(t, repeats)
	if got := opener.OpenCalls(); got != 2 {
		t.Fatalf("OpenSTT() calls = %d, want exactly 2", got)
	}
	assertNoRepeat(t, repeats)
	assertNoFinal(t, finals)
}

func TestSTTSessionQueueOverflowCancelsEpochAndRequestsOneRepeat(t *testing.T) {
	writeGate := make(chan struct{})
	var releaseGate sync.Once
	releaseWrite := func() { releaseGate.Do(func() { close(writeGate) }) }
	defer releaseWrite()
	stream := newFakeSTTStream()
	stream.writeGate = writeGate
	stream.ignoreWriteContext = true
	repeats := make(chan struct{}, 2)
	session := newTestSTTSession(t, &fakeSTTOpener{streams: []sarvam.STTStream{stream}}, newFakeSTTClock(), nil, repeats)
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := session.PushPCM16(testPCMFrame(0)); err != nil {
		t.Fatalf("first PushPCM16() error = %v", err)
	}
	stream.WaitWriteStarted(t)
	for index := 0; index < STTWriteQueueFrames; index++ {
		if err := session.PushPCM16(testPCMFrame(byte(index + 1))); err != nil {
			t.Fatalf("queued PushPCM16(%d) error = %v", index, err)
		}
	}
	if err := session.PushPCM16(testPCMFrame(0xff)); !errors.Is(err, ErrSTTWriteQueueFull) {
		t.Fatalf("overflow PushPCM16() error = %v, want ErrSTTWriteQueueFull", err)
	}
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("first SpeechEnded() after overflow error = %v, want nil", err)
	}
	if err := session.SpeechEnded(); !errors.Is(err, ErrSTTSpeechAlreadyEnded) {
		t.Fatalf("duplicate SpeechEnded() after overflow error = %v, want ErrSTTSpeechAlreadyEnded", err)
	}
	releaseWrite()
	receiveRepeat(t, repeats)
	assertNoRepeat(t, repeats)
}

func TestNewSTTSessionValidatesRequiredDependenciesAndBounds(t *testing.T) {
	valid := STTSessionConfig{
		Context:                context.Background(),
		Opener:                 &fakeSTTOpener{},
		FallbackLanguage:       "en-IN",
		Clock:                  newFakeSTTClock(),
		FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(context.Context, STTFinalTranscript) {}),
		RepeatRequestHandler:   RepeatRequestHandlerFunc(func(context.Context) {}),
	}
	tests := []struct {
		name    string
		mutate  func(*STTSessionConfig)
		wantErr error
	}{
		{name: "nil context", mutate: func(config *STTSessionConfig) { config.Context = nil }, wantErr: ErrSTTSessionContextRequired},
		{name: "nil opener", mutate: func(config *STTSessionConfig) { config.Opener = nil }, wantErr: ErrSTTOpenerRequired},
		{name: "nil final handler", mutate: func(config *STTSessionConfig) { config.FinalTranscriptHandler = nil }, wantErr: ErrSTTFinalHandlerRequired},
		{name: "nil repeat handler", mutate: func(config *STTSessionConfig) { config.RepeatRequestHandler = nil }, wantErr: ErrSTTRepeatHandlerRequired},
		{name: "final timeout too large", mutate: func(config *STTSessionConfig) { config.FinalTimeout = MaxSTTFinalTimeout + time.Nanosecond }, wantErr: ErrSTTInvalidTimeout},
		{name: "warm timeout too large", mutate: func(config *STTSessionConfig) { config.WarmTimeout = MaxSTTWarmTimeout + time.Nanosecond }, wantErr: ErrSTTInvalidTimeout},
		{name: "negative final timeout", mutate: func(config *STTSessionConfig) { config.FinalTimeout = -time.Nanosecond }, wantErr: ErrSTTInvalidTimeout},
		{name: "negative warm timeout", mutate: func(config *STTSessionConfig) { config.WarmTimeout = -time.Nanosecond }, wantErr: ErrSTTInvalidTimeout},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			session, err := NewSTTSession(config)
			if !errors.Is(err, test.wantErr) || session != nil {
				t.Fatalf("NewSTTSession() = (%v, %v), want (nil, %v)", session, err, test.wantErr)
			}
		})
	}

	t.Run("canceled parent", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		config := valid
		config.Context = ctx
		if session, err := NewSTTSession(config); !errors.Is(err, ErrSTTSessionClosed) || session != nil {
			t.Fatalf("NewSTTSession(canceled context) = (%v, %v), want (nil, ErrSTTSessionClosed)", session, err)
		}
	})

	t.Run("typed nil dependencies", func(t *testing.T) {
		var opener *fakeSTTOpener
		var clock *fakeSTTClock
		cases := []STTSessionConfig{valid, valid, valid, valid}
		cases[0].Opener = opener
		cases[1].Clock = clock
		cases[2].FinalTranscriptHandler = FinalTranscriptHandlerFunc(nil)
		cases[3].RepeatRequestHandler = RepeatRequestHandlerFunc(nil)
		for index, config := range cases {
			if session, err := NewSTTSession(config); err == nil || session != nil {
				t.Fatalf("typed-nil case %d = (%v, %v), want validation error", index, session, err)
			}
		}
	})
}

func newTestSTTSession(t *testing.T, opener sarvam.STTOpener, clock *fakeSTTClock, finals chan<- STTFinalTranscript, repeats chan<- struct{}) *STTSession {
	t.Helper()
	if finals == nil {
		finals = make(chan STTFinalTranscript, 1)
	}
	if repeats == nil {
		repeats = make(chan struct{}, 1)
	}
	return newTestSTTSessionWithConfig(t, STTSessionConfig{
		Context:          context.Background(),
		Opener:           opener,
		FallbackLanguage: "en-IN",
		Clock:            clock,
		FinalTranscriptHandler: FinalTranscriptHandlerFunc(func(_ context.Context, final STTFinalTranscript) {
			finals <- final
		}),
		RepeatRequestHandler: RepeatRequestHandlerFunc(func(context.Context) {
			repeats <- struct{}{}
		}),
	})
}

func newTestSTTSessionWithConfig(t *testing.T, config STTSessionConfig) *STTSession {
	t.Helper()
	session, err := NewSTTSession(config)
	if err != nil {
		t.Fatalf("NewSTTSession() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func completeSTTTurn(t *testing.T, session *STTSession, stream *fakeSTTStream, marker byte, final sarvam.FinalTranscript) {
	t.Helper()
	if err := session.SpeechStarted(); err != nil {
		t.Fatalf("SpeechStarted() error = %v", err)
	}
	if err := session.PushPCM16(testPCMFrame(marker)); err != nil {
		t.Fatalf("PushPCM16() error = %v", err)
	}
	if err := session.SpeechEnded(); err != nil {
		t.Fatalf("SpeechEnded() error = %v", err)
	}
	stream.SendFinal(final)
}

func testPCMFrame(marker byte) []byte {
	frame := make([]byte, STTPCMFrameBytes)
	for index := range frame {
		frame[index] = marker
	}
	return frame
}

func receiveSTTFinal(t *testing.T, finals <-chan STTFinalTranscript) STTFinalTranscript {
	t.Helper()
	select {
	case final := <-finals:
		return final
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for final transcript")
		return STTFinalTranscript{}
	}
}

func receiveRepeat(t *testing.T, repeats <-chan struct{}) {
	t.Helper()
	select {
	case <-repeats:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for repeat request")
	}
}

func assertNoFinal(t *testing.T, finals <-chan STTFinalTranscript) {
	t.Helper()
	select {
	case final := <-finals:
		t.Fatalf("unexpected final callback: %#v", final)
	case <-time.After(20 * time.Millisecond):
	}
}

func assertNoRepeat(t *testing.T, repeats <-chan struct{}) {
	t.Helper()
	select {
	case <-repeats:
		t.Fatal("unexpected repeat request")
	case <-time.After(20 * time.Millisecond):
	}
}

func waitForSTTCondition(t *testing.T, condition func() bool, failure string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(failure)
		}
		time.Sleep(time.Millisecond)
	}
}

func allSTTFramesZero(frames []pcmSTTFrame) bool {
	for index := range frames {
		for _, value := range frames[index] {
			if value != 0 {
				return false
			}
		}
	}
	return true
}

type fakeSTTOpener struct {
	mu      sync.Mutex
	streams []sarvam.STTStream
	errors  []error
	calls   int
}

func (opener *fakeSTTOpener) OpenSTT(ctx context.Context) (sarvam.STTStream, error) {
	opener.mu.Lock()
	defer opener.mu.Unlock()
	opener.calls++
	index := opener.calls - 1
	if index < len(opener.errors) && opener.errors[index] != nil {
		return nil, opener.errors[index]
	}
	if index >= len(opener.streams) {
		return nil, errors.New("fake opener has no stream")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return opener.streams[index], nil
}

func (opener *fakeSTTOpener) OpenCalls() int {
	opener.mu.Lock()
	defer opener.mu.Unlock()
	return opener.calls
}

type fakeFinalResult struct {
	final sarvam.FinalTranscript
	err   error
}

type fakeSTTStream struct {
	mu sync.Mutex

	writes      [][]byte
	writeErrors []error
	writeCalls  int
	flushCalls  int
	flushErr    error
	closed      bool

	writeGate          <-chan struct{}
	ignoreWriteContext bool
	writeStarted       chan struct{}
	writeStartedOnce   sync.Once
	awaitStarted       chan struct{}
	awaitStartedOnce   sync.Once
	ignoreAwaitContext bool
	finals             chan fakeFinalResult
	done               chan struct{}
	closeOnce          sync.Once
}

func newFakeSTTStream() *fakeSTTStream {
	return &fakeSTTStream{
		writeStarted: make(chan struct{}),
		awaitStarted: make(chan struct{}),
		finals:       make(chan fakeFinalResult, 8),
		done:         make(chan struct{}),
	}
}

func (stream *fakeSTTStream) WritePCM16(ctx context.Context, pcm []byte) error {
	stream.writeStartedOnce.Do(func() { close(stream.writeStarted) })
	if stream.writeGate != nil {
		if stream.ignoreWriteContext {
			<-stream.writeGate
			if err := ctx.Err(); err != nil {
				return err
			}
		} else {
			select {
			case <-stream.writeGate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.writeCalls++
	index := stream.writeCalls - 1
	if index < len(stream.writeErrors) && stream.writeErrors[index] != nil {
		return stream.writeErrors[index]
	}
	stream.writes = append(stream.writes, append([]byte(nil), pcm...))
	return nil
}

func (stream *fakeSTTStream) Flush(context.Context) error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.flushCalls++
	return stream.flushErr
}

func (stream *fakeSTTStream) AwaitFinal(ctx context.Context) (sarvam.FinalTranscript, error) {
	stream.awaitStartedOnce.Do(func() { close(stream.awaitStarted) })
	if stream.ignoreAwaitContext {
		result := <-stream.finals
		return result.final, result.err
	}
	select {
	case result := <-stream.finals:
		return result.final, result.err
	case <-ctx.Done():
		return sarvam.FinalTranscript{}, ctx.Err()
	}
}

func (stream *fakeSTTStream) Done() <-chan struct{} { return stream.done }

func (stream *fakeSTTStream) Close() error {
	stream.closeOnce.Do(func() {
		stream.mu.Lock()
		stream.closed = true
		stream.mu.Unlock()
		close(stream.done)
	})
	return nil
}

func (stream *fakeSTTStream) SendFinal(final sarvam.FinalTranscript) {
	stream.finals <- fakeFinalResult{final: final}
}

func (stream *fakeSTTStream) Writes() [][]byte {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	writes := make([][]byte, len(stream.writes))
	for index := range stream.writes {
		writes[index] = append([]byte(nil), stream.writes[index]...)
	}
	return writes
}

func (stream *fakeSTTStream) FlushCalls() int {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.flushCalls
}

func (stream *fakeSTTStream) IsClosed() bool {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.closed
}

func (stream *fakeSTTStream) WaitWriteStarted(t *testing.T) {
	t.Helper()
	select {
	case <-stream.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for STT write")
	}
}

func (stream *fakeSTTStream) WaitAwaitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-stream.awaitStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for AwaitFinal")
	}
}

type fakeSTTClock struct {
	mu     sync.Mutex
	timers []*fakeSTTTimer
}

func newFakeSTTClock() *fakeSTTClock { return &fakeSTTClock{} }

func (clock *fakeSTTClock) NewTimer(duration time.Duration) STTTimer {
	timer := &fakeSTTTimer{duration: duration, channel: make(chan time.Time, 8)}
	clock.mu.Lock()
	clock.timers = append(clock.timers, timer)
	clock.mu.Unlock()
	return timer
}

func (clock *fakeSTTClock) LastDuration() time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if len(clock.timers) == 0 {
		return 0
	}
	return clock.timers[len(clock.timers)-1].duration
}

func (clock *fakeSTTClock) FireLatest() {
	clock.mu.Lock()
	if len(clock.timers) == 0 {
		clock.mu.Unlock()
		return
	}
	timer := clock.timers[len(clock.timers)-1]
	clock.mu.Unlock()
	timer.Fire()
}

func (clock *fakeSTTClock) Timer(index int) *fakeSTTTimer {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if index < 0 || index >= len(clock.timers) {
		return nil
	}
	return clock.timers[index]
}

type fakeSTTTimer struct {
	mu       sync.Mutex
	duration time.Duration
	channel  chan time.Time
	stopped  bool
}

func (timer *fakeSTTTimer) C() <-chan time.Time { return timer.channel }

func (timer *fakeSTTTimer) Stop() bool {
	timer.mu.Lock()
	defer timer.mu.Unlock()
	wasActive := !timer.stopped
	timer.stopped = true
	return wasActive
}

func (timer *fakeSTTTimer) Fire() {
	timer.mu.Lock()
	defer timer.mu.Unlock()
	if timer.stopped {
		return
	}
	timer.stopped = true
	timer.channel <- time.Unix(1, 0)
}

func (timer *fakeSTTTimer) ForceFire() {
	timer.channel <- time.Unix(2, 0)
}

var (
	_ sarvam.STTOpener = (*fakeSTTOpener)(nil)
	_ sarvam.STTStream = (*fakeSTTStream)(nil)
	_ STTClock         = (*fakeSTTClock)(nil)
	_ STTTimer         = (*fakeSTTTimer)(nil)
)
