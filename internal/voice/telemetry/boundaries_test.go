package telemetry

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTrackerRequiresStrictMonotonicBoundaries(t *testing.T) {
	tracker, err := NewTracker("turn-01HZX7P0M2")
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}

	started := time.Date(2026, time.August, 7, 1, 2, 3, 0, time.UTC)
	if err := tracker.Record(SpeechStarted, started); err != nil {
		t.Fatalf("Record(speech_started) error = %v", err)
	}
	if err := tracker.Record(STTFinalReceived, started.Add(time.Second)); !errors.Is(err, ErrUnexpectedBoundary) {
		t.Fatalf("Record(out of order) error = %v, want ErrUnexpectedBoundary", err)
	}
	if err := tracker.Record(SpeechEnded, started); !errors.Is(err, ErrNonMonotonicTimestamp) {
		t.Fatalf("Record(equal timestamp) error = %v, want ErrNonMonotonicTimestamp", err)
	}
	if err := tracker.Record(SpeechEnded, started.Add(-time.Nanosecond)); !errors.Is(err, ErrNonMonotonicTimestamp) {
		t.Fatalf("Record(earlier timestamp) error = %v, want ErrNonMonotonicTimestamp", err)
	}

	recordCompletedTurn(t, tracker, started)
	if err := tracker.Record(TurnCancelled, started.Add(10*time.Second)); !errors.Is(err, ErrTurnTerminal) {
		t.Fatalf("Record(after completed) error = %v, want ErrTurnTerminal", err)
	}
}

func TestTrackerDerivesStrictLatencyIntervals(t *testing.T) {
	tracker, err := NewTracker("turn-latency-01")
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}

	base := time.Date(2026, time.August, 7, 1, 2, 3, 0, time.UTC)
	record := func(boundary Boundary, offset time.Duration) {
		t.Helper()
		if recordErr := tracker.Record(boundary, base.Add(offset)); recordErr != nil {
			t.Fatalf("Record(%s) error = %v", boundary, recordErr)
		}
	}
	record(SpeechStarted, 0)
	record(SpeechEnded, 1250*time.Millisecond)
	record(STTFinalReceived, 1650*time.Millisecond)
	record(LLMRequestStarted, 1700*time.Millisecond)
	record(LLMFirstToken, 2150*time.Millisecond)
	record(TTSRequestStarted, 2200*time.Millisecond)
	record(TTSFirstAudio, 2500*time.Millisecond)
	if err := tracker.RecordInterrupt(base.Add(2600*time.Millisecond), base.Add(2675*time.Millisecond)); err != nil {
		t.Fatalf("RecordInterrupt() error = %v", err)
	}
	record(ClientFirstAudioPlayed, 2800*time.Millisecond)
	record(TurnCompleted, 4*time.Second)

	snapshot, err := tracker.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	latencies, err := snapshot.DeriveLatencies()
	if err != nil {
		t.Fatalf("DeriveLatencies() error = %v", err)
	}
	assertLatency(t, latencies.EndOfSpeechToFirstAudio, 1550*time.Millisecond)
	assertLatency(t, latencies.STTFinal, 400*time.Millisecond)
	assertLatency(t, latencies.LLMFirstToken, 450*time.Millisecond)
	assertLatency(t, latencies.TTSFirstAudio, 300*time.Millisecond)
	assertLatency(t, latencies.InterruptToPlaybackStop, 75*time.Millisecond)
	assertLatency(t, latencies.STTAudio, 1250*time.Millisecond)
}

func TestTrackerAllowsStrictPartialCancellation(t *testing.T) {
	tracker, err := NewTracker("turn-cancelled-01")
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	base := time.Now().UTC()
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
	if got := snapshot.TerminalBoundary(); got != TurnCancelled {
		t.Fatalf("TerminalBoundary() = %q, want %q", got, TurnCancelled)
	}
	if _, err := snapshot.DeriveLatencies(); err != nil {
		t.Fatalf("DeriveLatencies(partial cancellation) error = %v", err)
	}
}

func TestTrackerRejectsInvalidInterruptAndIncompleteSnapshot(t *testing.T) {
	tracker, err := NewTracker("turn-interrupt-01")
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	base := time.Now().UTC()
	if _, err := tracker.Snapshot(); !errors.Is(err, ErrIncompleteTurn) {
		t.Fatalf("Snapshot(incomplete) error = %v, want ErrIncompleteTurn", err)
	}
	if err := tracker.Record(SpeechStarted, base); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := tracker.RecordInterrupt(base.Add(time.Second), base.Add(time.Second)); !errors.Is(err, ErrNonMonotonicTimestamp) {
		t.Fatalf("RecordInterrupt(equal) error = %v, want ErrNonMonotonicTimestamp", err)
	}
	if err := tracker.RecordInterrupt(base.Add(2*time.Second), base.Add(3*time.Second)); err != nil {
		t.Fatalf("RecordInterrupt() error = %v", err)
	}
	if err := tracker.RecordInterrupt(base.Add(4*time.Second), base.Add(5*time.Second)); !errors.Is(err, ErrDuplicateInterrupt) {
		t.Fatalf("RecordInterrupt(duplicate) error = %v, want ErrDuplicateInterrupt", err)
	}
}

func TestTrackerConcurrentDuplicateBoundaryHasOneWinner(t *testing.T) {
	tracker, err := NewTracker("turn-race-01")
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	const callers = 64
	var wait sync.WaitGroup
	wait.Add(callers)
	errorsSeen := make(chan error, callers)
	stamp := time.Now().UTC()
	for range callers {
		go func() {
			defer wait.Done()
			errorsSeen <- tracker.Record(SpeechStarted, stamp)
		}()
	}
	wait.Wait()
	close(errorsSeen)

	successes := 0
	for recordErr := range errorsSeen {
		if recordErr == nil {
			successes++
			continue
		}
		if !errors.Is(recordErr, ErrUnexpectedBoundary) {
			t.Fatalf("duplicate Record() error = %v", recordErr)
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent records = %d, want 1", successes)
	}
}

func TestNewTrackerRejectsUnsafeCorrelationIDs(t *testing.T) {
	for _, correlationID := range []string{"", " turn-1", "turn 1", "user@example.com", "+919876543210"} {
		if _, err := NewTracker(correlationID); !errors.Is(err, ErrInvalidCorrelationID) {
			t.Errorf("NewTracker(%q) error = %v, want ErrInvalidCorrelationID", correlationID, err)
		}
	}
}

func recordCompletedTurn(t *testing.T, tracker *Tracker, started time.Time) {
	t.Helper()
	boundaries := []Boundary{
		SpeechEnded,
		STTFinalReceived,
		LLMRequestStarted,
		LLMFirstToken,
		TTSRequestStarted,
		TTSFirstAudio,
		ClientFirstAudioPlayed,
		TurnCompleted,
	}
	for index, boundary := range boundaries {
		if err := tracker.Record(boundary, started.Add(time.Duration(index+1)*time.Second)); err != nil {
			t.Fatalf("Record(%s) error = %v", boundary, err)
		}
	}
}

func assertLatency(t *testing.T, got OptionalLatency, want time.Duration) {
	t.Helper()
	if !got.Present || got.Duration != want {
		t.Fatalf("latency = {%v, %s}, want {true, %s}", got.Present, got.Duration, want)
	}
}
