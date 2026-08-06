// Package protocol defines the versioned control-plane contract for voice calls.
package protocol

import (
	"bytes"
	"container/list"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const (
	ProtocolVersion        = 1
	MaxControlMessageBytes = 16 * 1024
	DataChannelLabel       = "billeif.voice.control.v1"
)

// Direction identifies which peer sent a DataChannel control message.
type Direction uint8

const (
	ClientToRuntime Direction = iota + 1
	RuntimeToClient
)

// EventType is a stable, versioned control or invocation event name.
type EventType string

const (
	EventClientReady       EventType = "client.ready"
	EventSpeechStarted     EventType = "speech.started"
	EventSpeechEnded       EventType = "speech.ended"
	EventInterrupt         EventType = "interrupt"
	EventPlaybackStarted   EventType = "playback.started"
	EventPlaybackCompleted EventType = "playback.completed"
	EventHeartbeat         EventType = "heartbeat"
	EventSessionClose      EventType = "session.close"
	EventNetworkChanged    EventType = "network.changed"

	EventSessionReady     EventType = "session.ready"
	EventAgentState       EventType = "agent.state"
	EventTranscriptFinal  EventType = "transcript.final"
	EventLanguageSelected EventType = "language.selected"
	EventTurnStarted      EventType = "turn.started"
	EventTurnCompleted    EventType = "turn.completed"
	EventTurnCancelled    EventType = "turn.cancelled"
	EventError            EventType = "error"
	EventSessionRotate    EventType = "session.rotate"
	EventICERefresh       EventType = "ice.refresh"

	InvocationSessionAttach   EventType = "session.attach"
	InvocationWebRTCOffer     EventType = "webrtc.offer"
	InvocationWebRTCCandidate EventType = "webrtc.candidate"
	InvocationWebRTCRestart   EventType = "webrtc.restart"
	ResponseSessionAttached   EventType = "session.attached"
	ResponseWebRTCAnswer      EventType = "webrtc.answer"
)

var clientEvents = []EventType{
	EventClientReady,
	EventSpeechStarted,
	EventSpeechEnded,
	EventInterrupt,
	EventPlaybackStarted,
	EventPlaybackCompleted,
	EventHeartbeat,
	EventSessionClose,
	EventNetworkChanged,
}

var runtimeEvents = []EventType{
	EventSessionReady,
	EventAgentState,
	EventTranscriptFinal,
	EventLanguageSelected,
	EventTurnStarted,
	EventTurnCompleted,
	EventTurnCancelled,
	EventError,
	EventSessionRotate,
	EventICERefresh,
}

var invocationRequests = []EventType{
	InvocationSessionAttach,
	InvocationWebRTCOffer,
	InvocationWebRTCCandidate,
	InvocationWebRTCRestart,
}

var invocationResponses = []EventType{
	ResponseSessionAttached,
	ResponseWebRTCAnswer,
}

// ControlMessage contains the common fields accepted on the ordered reliable
// DataChannel. Event-specific fields remain optional because individual events
// use only the fields relevant to their payload.
type ControlMessage struct {
	Type                EventType `json:"type"`
	ProtocolVersion     int       `json:"protocol_version"`
	SessionID           string    `json:"session_id"`
	Sequence            int       `json:"sequence"`
	TurnID              *int64    `json:"turn_id,omitempty"`
	GenerationID        *int64    `json:"generation_id,omitempty"`
	ClientMonotonicMS   *int64    `json:"client_monotonic_ms,omitempty"`
	State               string    `json:"state,omitempty"`
	Text                string    `json:"text,omitempty"`
	DetectedLanguage    string    `json:"detected_language,omitempty"`
	LanguageProbability *float64  `json:"language_probability,omitempty"`
	ErrorCode           string    `json:"error_code,omitempty"`
	Message             string    `json:"message,omitempty"`
	RotateAt            string    `json:"rotate_at,omitempty"`
	ICEServers          []any     `json:"ice_servers,omitempty"`
}

// DecodeControlMessage strictly decodes and validates a DataChannel message.
// It records accepted sequence numbers only after the message is valid.
func DecodeControlMessage(data []byte, direction Direction, tracker *SequenceTracker) (ControlMessage, error) {
	if len(data) > MaxControlMessageBytes {
		return ControlMessage{}, ErrControlMessageTooLarge
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var message ControlMessage
	if err := decoder.Decode(&message); err != nil {
		return ControlMessage{}, fmt.Errorf("%w: %v", ErrInvalidControlMessage, err)
	}
	if err := requireSingleJSONValue(decoder); err != nil {
		return ControlMessage{}, err
	}
	if err := ValidateControlMessage(message, direction); err != nil {
		return ControlMessage{}, err
	}
	if tracker != nil {
		if err := tracker.Accept(message.SessionID, direction, message.Sequence); err != nil {
			return ControlMessage{}, err
		}
	}
	return message, nil
}

// ValidateControlMessage validates a decoded DataChannel message without
// modifying duplicate-sequence state.
func ValidateControlMessage(message ControlMessage, direction Direction) error {
	if message.ProtocolVersion != ProtocolVersion {
		return ErrUnsupportedProtocolVersion
	}
	if message.SessionID == "" {
		return ErrSessionIDRequired
	}
	if message.Sequence <= 0 {
		return ErrSequenceRequired
	}
	if !isSupportedControlEvent(message.Type, direction) {
		return ErrUnsupportedEvent
	}
	if IsGenerationSensitive(message.Type) && (message.GenerationID == nil || *message.GenerationID <= 0) {
		return ErrGenerationIDRequired
	}
	return nil
}

// IsGenerationSensitive reports whether an event must include generation_id.
func IsGenerationSensitive(eventType EventType) bool {
	switch eventType {
	case EventInterrupt, EventPlaybackStarted, EventPlaybackCompleted,
		EventAgentState, EventTurnStarted, EventTurnCompleted, EventTurnCancelled:
		return true
	default:
		return false
	}
}

// SupportedInvocationRequests returns a copy of the frozen invocation request names.
func SupportedInvocationRequests() []EventType {
	return append([]EventType(nil), invocationRequests...)
}

// SupportedInvocationResponses returns a copy of the frozen invocation response names.
func SupportedInvocationResponses() []EventType {
	return append([]EventType(nil), invocationResponses...)
}

func isSupportedControlEvent(eventType EventType, direction Direction) bool {
	var events []EventType
	switch direction {
	case ClientToRuntime:
		events = clientEvents
	case RuntimeToClient:
		events = runtimeEvents
	default:
		return false
	}
	for _, supported := range events {
		if eventType == supported {
			return true
		}
	}
	return false
}

func requireSingleJSONValue(decoder *json.Decoder) error {
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%w: multiple JSON values", ErrInvalidControlMessage)
		}
		return fmt.Errorf("%w: %v", ErrInvalidControlMessage, err)
	}
	return nil
}

// SequenceTracker rejects duplicate and out-of-order messages while bounding
// memory by both active sessions and retained sequence numbers per session.
type SequenceTracker struct {
	mu             sync.Mutex
	maxSessions    int
	maxPerSession  int
	sessions       map[string]*sessionSequenceState
	sessionRecency *list.List
}

type sessionSequenceState struct {
	directions map[Direction]*directionSequenceState
	recency    *list.Element
}

type directionSequenceState struct {
	lastSequence  int
	seen          map[int]struct{}
	sequenceOrder *list.List
}

// NewSequenceTracker creates a bounded, session-scoped tracker. Non-positive
// limits are normalized to one so callers cannot accidentally create an
// unbounded tracker.
func NewSequenceTracker(maxSessions, maxSequencesPerSession int) *SequenceTracker {
	if maxSessions < 1 {
		maxSessions = 1
	}
	if maxSequencesPerSession < 1 {
		maxSequencesPerSession = 1
	}
	return &SequenceTracker{
		maxSessions:    maxSessions,
		maxPerSession:  maxSequencesPerSession,
		sessions:       make(map[string]*sessionSequenceState, maxSessions),
		sessionRecency: list.New(),
	}
}

// Accept records a sequence after validation, rejecting replayed or
// out-of-order values for the same logical session and sender direction.
func (tracker *SequenceTracker) Accept(sessionID string, direction Direction, sequence int) error {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	state := tracker.sessions[sessionID]
	if state == nil {
		state = tracker.newSession(sessionID)
	} else {
		tracker.sessionRecency.MoveToFront(state.recency)
	}

	directionState := state.directions[direction]
	if directionState == nil {
		directionState = &directionSequenceState{
			seen:          make(map[int]struct{}, tracker.maxPerSession),
			sequenceOrder: list.New(),
		}
		state.directions[direction] = directionState
	}

	if _, duplicate := directionState.seen[sequence]; duplicate {
		return ErrDuplicateSequence
	}
	if sequence <= directionState.lastSequence {
		return ErrNonMonotonicSequence
	}

	directionState.lastSequence = sequence
	directionState.seen[sequence] = struct{}{}
	directionState.sequenceOrder.PushBack(sequence)
	if directionState.sequenceOrder.Len() > tracker.maxPerSession {
		oldest := directionState.sequenceOrder.Remove(directionState.sequenceOrder.Front()).(int)
		delete(directionState.seen, oldest)
	}
	return nil
}

// SessionCount reports the current bounded number of tracked sessions.
func (tracker *SequenceTracker) SessionCount() int {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return len(tracker.sessions)
}

func (tracker *SequenceTracker) newSession(sessionID string) *sessionSequenceState {
	if len(tracker.sessions) >= tracker.maxSessions {
		oldest := tracker.sessionRecency.Back()
		oldestSessionID := oldest.Value.(string)
		delete(tracker.sessions, oldestSessionID)
		tracker.sessionRecency.Remove(oldest)
	}
	recency := tracker.sessionRecency.PushFront(sessionID)
	state := &sessionSequenceState{
		directions: make(map[Direction]*directionSequenceState, 2),
		recency:    recency,
	}
	tracker.sessions[sessionID] = state
	return state
}
