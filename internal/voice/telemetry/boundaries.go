package telemetry

import (
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
)

// Boundary is a coarse, per-turn lifecycle timestamp. Audio frames are
// intentionally not part of this model.
type Boundary string

const (
	SpeechStarted          Boundary = "speech_started"
	SpeechEnded            Boundary = "speech_ended"
	STTFinalReceived       Boundary = "stt_final_received"
	LLMRequestStarted      Boundary = "llm_request_started"
	LLMFirstToken          Boundary = "llm_first_token"
	TTSRequestStarted      Boundary = "tts_request_started"
	TTSFirstAudio          Boundary = "tts_first_audio"
	ClientFirstAudioPlayed Boundary = "client_first_audio_played"
	TurnCompleted          Boundary = "turn_completed"
	TurnCancelled          Boundary = "turn_cancelled"
)

var (
	ErrInvalidCorrelationID   = errors.New("invalid telemetry correlation ID")
	ErrUnexpectedBoundary     = errors.New("unexpected turn boundary")
	ErrNonMonotonicTimestamp  = errors.New("turn timestamp is not strictly monotonic")
	ErrTurnTerminal           = errors.New("turn is already terminal")
	ErrIncompleteTurn         = errors.New("turn has no terminal boundary")
	ErrDuplicateInterrupt     = errors.New("interrupt interval is already recorded")
	correlationIDPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	completedBoundarySequence = []Boundary{SpeechStarted, SpeechEnded, STTFinalReceived, LLMRequestStarted, LLMFirstToken, TTSRequestStarted, TTSFirstAudio, ClientFirstAudioPlayed, TurnCompleted}
)

// Tracker serializes boundary capture for one turn.
type Tracker struct {
	mu sync.RWMutex

	correlationID string
	timestamps    map[Boundary]time.Time
	nextIndex     int
	lastEventAt   time.Time
	terminal      Boundary
	interruptedAt *time.Time
	playbackStop  *time.Time
}

// NewTracker creates an empty tracker. Correlation IDs are restricted to
// opaque, bounded identifiers so user identifiers cannot accidentally become
// log metadata.
func NewTracker(correlationID string) (*Tracker, error) {
	if !correlationIDPattern.MatchString(correlationID) || containsSensitiveValue(correlationID) {
		return nil, ErrInvalidCorrelationID
	}
	return &Tracker{
		correlationID: correlationID,
		timestamps:    make(map[Boundary]time.Time, len(completedBoundarySequence)),
	}, nil
}

// Record captures the next normal boundary, or a terminal cancellation after
// speech has started. Every timestamp must be strictly later than every event
// already observed for the turn, including interrupt events.
func (tracker *Tracker) Record(boundary Boundary, at time.Time) error {
	if tracker == nil {
		return ErrIncompleteTurn
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	if tracker.terminal != "" {
		return ErrTurnTerminal
	}
	if boundary == TurnCancelled {
		if tracker.nextIndex == 0 {
			return fmt.Errorf("%w: cancellation before speech_started", ErrUnexpectedBoundary)
		}
		if err := tracker.validateNextTimestamp(at); err != nil {
			return err
		}
		tracker.timestamps[boundary] = at
		tracker.lastEventAt = at
		tracker.terminal = boundary
		return nil
	}
	if tracker.nextIndex >= len(completedBoundarySequence) || boundary != completedBoundarySequence[tracker.nextIndex] {
		return fmt.Errorf("%w: got %q", ErrUnexpectedBoundary, boundary)
	}
	if err := tracker.validateNextTimestamp(at); err != nil {
		return err
	}

	tracker.timestamps[boundary] = at
	tracker.lastEventAt = at
	tracker.nextIndex++
	if boundary == TurnCompleted {
		tracker.terminal = boundary
	}
	return nil
}

// RecordInterrupt captures one interrupt-to-playback-stop interval without
// creating per-frame telemetry.
func (tracker *Tracker) RecordInterrupt(interruptedAt, playbackStoppedAt time.Time) error {
	if tracker == nil {
		return ErrIncompleteTurn
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	if tracker.terminal != "" {
		return ErrTurnTerminal
	}
	if tracker.interruptedAt != nil {
		return ErrDuplicateInterrupt
	}
	if tracker.nextIndex == 0 {
		return fmt.Errorf("%w: interrupt before speech_started", ErrUnexpectedBoundary)
	}
	if err := tracker.validateNextTimestamp(interruptedAt); err != nil {
		return err
	}
	if playbackStoppedAt.IsZero() || !playbackStoppedAt.After(interruptedAt) {
		return ErrNonMonotonicTimestamp
	}

	interruptCopy := interruptedAt
	playbackCopy := playbackStoppedAt
	tracker.interruptedAt = &interruptCopy
	tracker.playbackStop = &playbackCopy
	tracker.lastEventAt = playbackStoppedAt
	return nil
}

func (tracker *Tracker) validateNextTimestamp(at time.Time) error {
	if at.IsZero() || (!tracker.lastEventAt.IsZero() && !at.After(tracker.lastEventAt)) {
		return ErrNonMonotonicTimestamp
	}
	return nil
}

// Snapshot returns a detached immutable-by-convention copy once the turn is
// terminal.
func (tracker *Tracker) Snapshot() (TurnSnapshot, error) {
	if tracker == nil {
		return TurnSnapshot{}, ErrIncompleteTurn
	}
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	if tracker.terminal == "" {
		return TurnSnapshot{}, ErrIncompleteTurn
	}

	timestamps := make(map[Boundary]time.Time, len(tracker.timestamps))
	for boundary, at := range tracker.timestamps {
		timestamps[boundary] = at
	}
	return TurnSnapshot{
		CorrelationID: tracker.correlationID,
		timestamps:    timestamps,
		terminal:      tracker.terminal,
		interruptedAt: cloneTime(tracker.interruptedAt),
		playbackStop:  cloneTime(tracker.playbackStop),
	}, nil
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// TurnSnapshot is a detached terminal turn record.
type TurnSnapshot struct {
	CorrelationID string

	timestamps    map[Boundary]time.Time
	terminal      Boundary
	interruptedAt *time.Time
	playbackStop  *time.Time
}

// BoundaryTime returns the captured time for one lifecycle boundary.
func (snapshot TurnSnapshot) BoundaryTime(boundary Boundary) (time.Time, bool) {
	at, ok := snapshot.timestamps[boundary]
	return at, ok
}

// TerminalBoundary reports whether the turn completed or was cancelled.
func (snapshot TurnSnapshot) TerminalBoundary() Boundary {
	return snapshot.terminal
}

// OptionalLatency distinguishes an unavailable interval from a zero duration.
// Valid tracker intervals are always strictly positive.
type OptionalLatency struct {
	Duration time.Duration
	Present  bool
}

// Latencies contains the aggregate timing intervals derived from turn
// boundaries.
type Latencies struct {
	EndOfSpeechToFirstAudio OptionalLatency
	STTFinal                OptionalLatency
	LLMFirstToken           OptionalLatency
	TTSFirstAudio           OptionalLatency
	InterruptToPlaybackStop OptionalLatency
	STTAudio                OptionalLatency
}

// DeriveLatencies validates and derives every available coarse interval. A
// cancelled turn can legitimately have only a subset of intervals.
func (snapshot TurnSnapshot) DeriveLatencies() (Latencies, error) {
	if err := snapshot.validate(); err != nil {
		return Latencies{}, err
	}
	return Latencies{
		EndOfSpeechToFirstAudio: durationBetween(snapshot.timestamps, SpeechEnded, ClientFirstAudioPlayed),
		STTFinal:                durationBetween(snapshot.timestamps, SpeechEnded, STTFinalReceived),
		LLMFirstToken:           durationBetween(snapshot.timestamps, LLMRequestStarted, LLMFirstToken),
		TTSFirstAudio:           durationBetween(snapshot.timestamps, TTSRequestStarted, TTSFirstAudio),
		InterruptToPlaybackStop: pointerDuration(snapshot.interruptedAt, snapshot.playbackStop),
		STTAudio:                durationBetween(snapshot.timestamps, SpeechStarted, SpeechEnded),
	}, nil
}

func (snapshot TurnSnapshot) validate() error {
	if !correlationIDPattern.MatchString(snapshot.CorrelationID) || containsSensitiveValue(snapshot.CorrelationID) {
		return ErrInvalidCorrelationID
	}
	if snapshot.terminal != TurnCompleted && snapshot.terminal != TurnCancelled {
		return ErrIncompleteTurn
	}
	if len(snapshot.timestamps) == 0 {
		return ErrIncompleteTurn
	}

	var previous time.Time
	terminalSeen := false
	for _, boundary := range completedBoundarySequence {
		at, ok := snapshot.timestamps[boundary]
		if !ok {
			break
		}
		if at.IsZero() || (!previous.IsZero() && !at.After(previous)) {
			return ErrNonMonotonicTimestamp
		}
		previous = at
		if boundary == TurnCompleted {
			terminalSeen = true
		}
	}
	if snapshot.terminal == TurnCompleted && !terminalSeen {
		return ErrIncompleteTurn
	}
	if snapshot.terminal == TurnCancelled {
		cancelledAt, ok := snapshot.timestamps[TurnCancelled]
		if !ok || cancelledAt.IsZero() || (!previous.IsZero() && !cancelledAt.After(previous)) {
			return ErrNonMonotonicTimestamp
		}
	}
	if (snapshot.interruptedAt == nil) != (snapshot.playbackStop == nil) {
		return ErrIncompleteTurn
	}
	if snapshot.interruptedAt != nil && !snapshot.playbackStop.After(*snapshot.interruptedAt) {
		return ErrNonMonotonicTimestamp
	}
	return nil
}

func durationBetween(timestamps map[Boundary]time.Time, start, end Boundary) OptionalLatency {
	startAt, hasStart := timestamps[start]
	endAt, hasEnd := timestamps[end]
	if !hasStart || !hasEnd {
		return OptionalLatency{}
	}
	return OptionalLatency{Duration: endAt.Sub(startAt), Present: true}
}

func pointerDuration(start, end *time.Time) OptionalLatency {
	if start == nil || end == nil {
		return OptionalLatency{}
	}
	return OptionalLatency{Duration: end.Sub(*start), Present: true}
}
