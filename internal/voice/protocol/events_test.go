package protocol

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDecodeControlMessageAcceptsSupportedClientEvents(t *testing.T) {
	tracker := NewSequenceTracker(4, 8)
	events := []EventType{
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

	for sequence, eventType := range events {
		message, err := DecodeControlMessage(controlJSON(eventType, "voice-client", sequence+1, generationFor(eventType)), ClientToRuntime, tracker)
		if err != nil {
			t.Fatalf("DecodeControlMessage(%q) error = %v", eventType, err)
		}
		if message.Type != eventType || message.Sequence != sequence+1 {
			t.Fatalf("decoded message = %#v, want type %q with sequence %d", message, eventType, sequence+1)
		}
	}
}

func TestDecodeControlMessageAcceptsSupportedRuntimeEvents(t *testing.T) {
	tracker := NewSequenceTracker(4, 8)
	events := []EventType{
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

	for sequence, eventType := range events {
		message, err := DecodeControlMessage(controlJSON(eventType, "voice-runtime", sequence+1, generationFor(eventType)), RuntimeToClient, tracker)
		if err != nil {
			t.Fatalf("DecodeControlMessage(%q) error = %v", eventType, err)
		}
		if message.Type != eventType || message.Sequence != sequence+1 {
			t.Fatalf("decoded message = %#v, want type %q with sequence %d", message, eventType, sequence+1)
		}
	}
}

func TestDecodeControlMessageRejectsWrongProtocolVersionAndMissingSessionOrSequence(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{
			name: "wrong protocol version",
			body: `{"type":"heartbeat","protocol_version":2,"session_id":"voice-1","sequence":1}`,
			want: ErrUnsupportedProtocolVersion,
		},
		{
			name: "missing session id",
			body: `{"type":"heartbeat","protocol_version":1,"sequence":1}`,
			want: ErrSessionIDRequired,
		},
		{
			name: "zero sequence",
			body: `{"type":"heartbeat","protocol_version":1,"session_id":"voice-1","sequence":0}`,
			want: ErrSequenceRequired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeControlMessage([]byte(tc.body), ClientToRuntime, NewSequenceTracker(2, 2))
			if !errors.Is(err, tc.want) {
				t.Fatalf("DecodeControlMessage() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDecodeControlMessageRejectsDuplicateAndNonMonotonicSequencesPerSession(t *testing.T) {
	tracker := NewSequenceTracker(2, 2)
	if _, err := DecodeControlMessage(controlJSON(EventHeartbeat, "voice-1", 5, nil), ClientToRuntime, tracker); err != nil {
		t.Fatalf("first message error = %v", err)
	}

	_, err := DecodeControlMessage(controlJSON(EventHeartbeat, "voice-1", 5, nil), ClientToRuntime, tracker)
	if !errors.Is(err, ErrDuplicateSequence) {
		t.Fatalf("duplicate message error = %v, want %v", err, ErrDuplicateSequence)
	}

	_, err = DecodeControlMessage(controlJSON(EventHeartbeat, "voice-1", 4, nil), ClientToRuntime, tracker)
	if !errors.Is(err, ErrNonMonotonicSequence) {
		t.Fatalf("out-of-order message error = %v, want %v", err, ErrNonMonotonicSequence)
	}

	if _, err := DecodeControlMessage(controlJSON(EventHeartbeat, "voice-2", 1, nil), ClientToRuntime, tracker); err != nil {
		t.Fatalf("different session error = %v, want session-scoped tracking", err)
	}
}

func TestSequenceTrackerBoundsSessions(t *testing.T) {
	tracker := NewSequenceTracker(1, 2)
	if _, err := DecodeControlMessage(controlJSON(EventHeartbeat, "voice-1", 10, nil), ClientToRuntime, tracker); err != nil {
		t.Fatalf("first session error = %v", err)
	}
	if _, err := DecodeControlMessage(controlJSON(EventHeartbeat, "voice-2", 1, nil), ClientToRuntime, tracker); err != nil {
		t.Fatalf("second session error = %v", err)
	}
	if got := tracker.SessionCount(); got != 1 {
		t.Fatalf("SessionCount() = %d, want bounded count 1", got)
	}
}

func TestDecodeControlMessageRejectsUnsupportedEventsAndWrongDirection(t *testing.T) {
	tracker := NewSequenceTracker(2, 2)
	_, err := DecodeControlMessage(controlJSON("unknown.event", "voice-1", 1, nil), ClientToRuntime, tracker)
	if !errors.Is(err, ErrUnsupportedEvent) {
		t.Fatalf("unknown event error = %v, want %v", err, ErrUnsupportedEvent)
	}

	_, err = DecodeControlMessage(controlJSON(EventAgentState, "voice-1", 1, int64Pointer(1)), ClientToRuntime, tracker)
	if !errors.Is(err, ErrUnsupportedEvent) {
		t.Fatalf("runtime event in client direction error = %v, want %v", err, ErrUnsupportedEvent)
	}
}

func TestDecodeControlMessageRejectsUnknownJSONFields(t *testing.T) {
	body := `{"type":"heartbeat","protocol_version":1,"session_id":"voice-1","sequence":1,"unexpected":true}`
	_, err := DecodeControlMessage([]byte(body), ClientToRuntime, NewSequenceTracker(2, 2))
	if !errors.Is(err, ErrInvalidControlMessage) || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("DecodeControlMessage() error = %v, want unknown-field validation failure", err)
	}
}

func TestDecodeControlMessageRejectsOversizedControlFrames(t *testing.T) {
	body := []byte(`{"type":"heartbeat","protocol_version":1,"session_id":"voice-1","sequence":1,"text":"` + strings.Repeat("a", MaxControlMessageBytes) + `"}`)
	_, err := DecodeControlMessage(body, ClientToRuntime, NewSequenceTracker(2, 2))
	if !errors.Is(err, ErrControlMessageTooLarge) {
		t.Fatalf("DecodeControlMessage() error = %v, want %v", err, ErrControlMessageTooLarge)
	}
}

func TestDecodeControlMessageRequiresGenerationForGenerationSensitiveEvents(t *testing.T) {
	tests := []struct {
		name      string
		typeValue EventType
		direction Direction
	}{
		{name: "interrupt", typeValue: EventInterrupt, direction: ClientToRuntime},
		{name: "agent state", typeValue: EventAgentState, direction: RuntimeToClient},
		{name: "turn completed", typeValue: EventTurnCompleted, direction: RuntimeToClient},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeControlMessage(controlJSON(tc.typeValue, "voice-1", 1, nil), tc.direction, NewSequenceTracker(2, 2))
			if !errors.Is(err, ErrGenerationIDRequired) {
				t.Fatalf("DecodeControlMessage() error = %v, want %v", err, ErrGenerationIDRequired)
			}
		})
	}
}

func TestInvocationEventTypesAreFrozen(t *testing.T) {
	requests := []EventType{InvocationSessionAttach, InvocationWebRTCOffer, InvocationWebRTCCandidate, InvocationWebRTCRestart}
	responses := []EventType{ResponseSessionAttached, ResponseWebRTCAnswer}

	if got := SupportedInvocationRequests(); !sameEventSet(got, requests) {
		t.Fatalf("SupportedInvocationRequests() = %v, want %v", got, requests)
	}
	if got := SupportedInvocationResponses(); !sameEventSet(got, responses) {
		t.Fatalf("SupportedInvocationResponses() = %v, want %v", got, responses)
	}
}

func controlJSON(eventType EventType, sessionID string, sequence int, generation *int64) []byte {
	body := map[string]any{
		"type":             eventType,
		"protocol_version": ProtocolVersion,
		"session_id":       sessionID,
		"sequence":         sequence,
	}
	if generation != nil {
		body["generation_id"] = *generation
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return encoded
}

func generationFor(eventType EventType) *int64 {
	if IsGenerationSensitive(eventType) {
		return int64Pointer(1)
	}
	return nil
}

func int64Pointer(value int64) *int64 {
	return &value
}

func sameEventSet(got, want []EventType) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[EventType]struct{}, len(got))
	for _, eventType := range got {
		seen[eventType] = struct{}{}
	}
	for _, eventType := range want {
		if _, ok := seen[eventType]; !ok {
			return false
		}
	}
	return true
}
