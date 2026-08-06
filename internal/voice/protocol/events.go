// Package protocol defines the versioned control-plane contract for voice calls.
package protocol

import (
	"bytes"
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	ProtocolVersion         = 1
	MaxControlMessageBytes  = 16 * 1024
	MaxFinalAnswerTextBytes = 8 * 1024
	DataChannelLabel        = "billeif.voice.control.v1"
	// ClientHeartbeatInterval is the protocol-v1 mobile send cadence while a
	// peer remains connected. Durable writes are independently coalesced by the
	// runtime, so this wire cadence does not imply one write per message.
	ClientHeartbeatInterval = 20 * time.Second
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
	EventAnswerFinal      EventType = "answer.final"
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
	EventAnswerFinal,
	EventTurnCompleted,
	EventTurnCancelled,
	EventError,
	EventSessionRotate,
	EventICERefresh,
}

var controlMessageJSONFields = map[string]struct{}{
	"type": {}, "protocol_version": {}, "session_id": {}, "sequence": {},
	"turn_id": {}, "generation_id": {}, "client_monotonic_ms": {},
	"state": {}, "text": {}, "detected_language": {},
	"language": {}, "language_probability": {}, "error_code": {}, "message": {},
	"rotate_at": {}, "ice_servers": {},
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
	Language            string    `json:"language,omitempty"`
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
	if err := validateControlJSONShape(data); err != nil {
		return ControlMessage{}, fmt.Errorf("%w: %v", ErrInvalidControlMessage, err)
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

func validateControlJSONShape(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("control message must be one JSON object")
	}
	seen := make(map[string]struct{}, len(controlMessageJSONFields))
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return errors.New("invalid JSON object key")
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("invalid JSON object key")
		}
		if _, allowed := controlMessageJSONFields[key]; !allowed {
			return errors.New("unknown field")
		}
		if _, duplicate := seen[key]; duplicate {
			return errors.New("duplicate field")
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return errors.New("invalid JSON field value")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return errors.New("invalid JSON object")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
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
	if !validEventPayload(message) {
		return fmt.Errorf("%w: incomplete %s payload", ErrInvalidControlMessage, message.Type)
	}
	return nil
}

func validEventPayload(message ControlMessage) bool {
	switch message.Type {
	case EventSpeechStarted, EventSpeechEnded:
		return positiveInt64(message.TurnID) && positiveInt64(message.ClientMonotonicMS)
	case EventInterrupt, EventPlaybackStarted, EventPlaybackCompleted:
		return positiveInt64(message.TurnID) && positiveInt64(message.GenerationID) && positiveInt64(message.ClientMonotonicMS)
	case EventNetworkChanged:
		return positiveInt64(message.ClientMonotonicMS)
	case EventAgentState:
		return safeProtocolToken(message.State, 32)
	case EventTranscriptFinal:
		if !positiveInt64(message.TurnID) || strings.TrimSpace(message.Text) == "" ||
			(message.DetectedLanguage != "" && !safeProviderLanguage(message.DetectedLanguage)) {
			return false
		}
		if message.LanguageProbability == nil {
			return true
		}
		return message.DetectedLanguage != "" &&
			*message.LanguageProbability >= 0 && *message.LanguageProbability <= 1
	case EventLanguageSelected:
		return supportedSpokenLanguage(message.Language)
	case EventAnswerFinal:
		return positiveInt64(message.TurnID) && positiveInt64(message.GenerationID) &&
			validFinalAnswerText(message.Text)
	case EventError:
		return safeProtocolToken(message.ErrorCode, 64) && len(message.Message) <= 256
	case EventSessionRotate:
		deadline, err := time.Parse(time.RFC3339, message.RotateAt)
		return err == nil && deadline.UTC().Format(time.RFC3339) == message.RotateAt
	default:
		return true
	}
}

func validFinalAnswerText(value string) bool {
	if strings.TrimSpace(value) == "" || len(value) > MaxFinalAnswerTextBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character == 0 || (character < 0x20 && character != '\n' && character != '\r' && character != '\t') || character == 0x7f {
			return false
		}
	}
	return true
}

func positiveInt64(value *int64) bool {
	return value != nil && *value > 0
}

func safeProtocolToken(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.TrimSpace(value) != value {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (index > 0 && character >= '0' && character <= '9') ||
			(index > 0 && (character == '_' || character == '.' || character == '-')) {
			continue
		}
		return false
	}
	return true
}

func safeProviderLanguage(value string) bool {
	if value == "" || len(value) > 16 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' {
			continue
		}
		return false
	}
	return true
}

func supportedSpokenLanguage(value string) bool {
	switch value {
	case "bn-IN", "en-IN", "gu-IN", "hi-IN", "kn-IN", "ml-IN", "mr-IN", "od-IN", "pa-IN", "ta-IN", "te-IN":
		return true
	default:
		return false
	}
}

// IsGenerationSensitive reports whether an event must include generation_id.
func IsGenerationSensitive(eventType EventType) bool {
	switch eventType {
	case EventInterrupt, EventPlaybackStarted, EventPlaybackCompleted,
		EventAgentState, EventTurnStarted, EventAnswerFinal, EventTurnCompleted, EventTurnCancelled:
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
