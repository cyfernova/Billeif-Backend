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
		EventAnswerFinal,
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

func TestDecodeControlMessageTracksSequencesIndependentlyByDirection(t *testing.T) {
	tracker := NewSequenceTracker(2, 2)
	clientHeartbeat := controlJSON(EventHeartbeat, "voice-1", 1, nil)
	runtimeReady := controlJSON(EventSessionReady, "voice-1", 1, nil)

	if _, err := DecodeControlMessage(clientHeartbeat, ClientToRuntime, tracker); err != nil {
		t.Fatalf("client sequence 1 error = %v", err)
	}
	if _, err := DecodeControlMessage(runtimeReady, RuntimeToClient, tracker); err != nil {
		t.Fatalf("runtime sequence 1 error = %v, want independently valid sender sequence", err)
	}

	if _, err := DecodeControlMessage(clientHeartbeat, ClientToRuntime, tracker); !errors.Is(err, ErrDuplicateSequence) {
		t.Fatalf("duplicate client sequence error = %v, want %v", err, ErrDuplicateSequence)
	}
	if _, err := DecodeControlMessage(runtimeReady, RuntimeToClient, tracker); !errors.Is(err, ErrDuplicateSequence) {
		t.Fatalf("duplicate runtime sequence error = %v, want %v", err, ErrDuplicateSequence)
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

func TestDecodeControlMessageRejectsDuplicateAndCaseVariantJSONFields(t *testing.T) {
	tests := []string{
		`{"type":"heartbeat","type":"heartbeat","protocol_version":1,"session_id":"voice-1","sequence":1}`,
		`{"type":"heartbeat","Type":"session.close","protocol_version":1,"session_id":"voice-1","sequence":1}`,
		`{"type":"heartbeat","Protocol_Version":1,"session_id":"voice-1","sequence":1}`,
		`{"type":"heartbeat","protocol_version":1,"Session_ID":"voice-1","sequence":1}`,
		`{"type":"heartbeat","protocol_version":1,"session_id":"voice-1","Sequence":1}`,
	}

	for _, body := range tests {
		_, err := DecodeControlMessage([]byte(body), ClientToRuntime, NewSequenceTracker(2, 2))
		if !errors.Is(err, ErrInvalidControlMessage) {
			t.Fatalf("DecodeControlMessage(%s) error = %v, want %v", body, err, ErrInvalidControlMessage)
		}
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
		{name: "final answer", typeValue: EventAnswerFinal, direction: RuntimeToClient},
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

func TestDecodeControlMessageRejectsIncompleteV1EventPayloads(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		direction Direction
	}{
		{
			name:      "speech start without turn",
			body:      `{"type":"speech.started","protocol_version":1,"session_id":"voice-1","sequence":1,"client_monotonic_ms":10}`,
			direction: ClientToRuntime,
		},
		{
			name:      "speech end without monotonic timestamp",
			body:      `{"type":"speech.ended","protocol_version":1,"session_id":"voice-1","sequence":1,"turn_id":1}`,
			direction: ClientToRuntime,
		},
		{
			name:      "interrupt without turn",
			body:      `{"type":"interrupt","protocol_version":1,"session_id":"voice-1","sequence":1,"generation_id":1,"client_monotonic_ms":10}`,
			direction: ClientToRuntime,
		},
		{
			name:      "playback start without monotonic timestamp",
			body:      `{"type":"playback.started","protocol_version":1,"session_id":"voice-1","sequence":1,"turn_id":1,"generation_id":1}`,
			direction: ClientToRuntime,
		},
		{
			name:      "playback complete with zero turn",
			body:      `{"type":"playback.completed","protocol_version":1,"session_id":"voice-1","sequence":1,"turn_id":0,"generation_id":1,"client_monotonic_ms":10}`,
			direction: ClientToRuntime,
		},
		{
			name:      "network change with zero monotonic timestamp",
			body:      `{"type":"network.changed","protocol_version":1,"session_id":"voice-1","sequence":1,"client_monotonic_ms":0}`,
			direction: ClientToRuntime,
		},
		{
			name:      "agent state without state",
			body:      `{"type":"agent.state","protocol_version":1,"session_id":"voice-1","sequence":1,"generation_id":1}`,
			direction: RuntimeToClient,
		},
		{
			name:      "transcript without final text",
			body:      `{"type":"transcript.final","protocol_version":1,"session_id":"voice-1","sequence":1,"turn_id":1,"detected_language":"hi-IN","language_probability":0.98}`,
			direction: RuntimeToClient,
		},
		{
			name:      "transcript probability outside unit interval",
			body:      `{"type":"transcript.final","protocol_version":1,"session_id":"voice-1","sequence":1,"turn_id":1,"text":"नमस्ते","detected_language":"hi-IN","language_probability":1.01}`,
			direction: RuntimeToClient,
		},
		{
			name:      "transcript probability without detected language",
			body:      `{"type":"transcript.final","protocol_version":1,"session_id":"voice-1","sequence":1,"turn_id":1,"text":"नमस्ते","language_probability":0.98}`,
			direction: RuntimeToClient,
		},
		{
			name:      "selected language without language",
			body:      `{"type":"language.selected","protocol_version":1,"session_id":"voice-1","sequence":1}`,
			direction: RuntimeToClient,
		},
		{
			name:      "final answer without turn",
			body:      `{"type":"answer.final","protocol_version":1,"session_id":"voice-1","sequence":1,"generation_id":1,"text":"The invoice is paid."}`,
			direction: RuntimeToClient,
		},
		{
			name:      "final answer without text",
			body:      `{"type":"answer.final","protocol_version":1,"session_id":"voice-1","sequence":1,"turn_id":1,"generation_id":1}`,
			direction: RuntimeToClient,
		},
		{
			name:      "error without stable code",
			body:      `{"type":"error","protocol_version":1,"session_id":"voice-1","sequence":1,"message":"Please retry."}`,
			direction: RuntimeToClient,
		},
		{
			name:      "rotation without deadline",
			body:      `{"type":"session.rotate","protocol_version":1,"session_id":"voice-1","sequence":1}`,
			direction: RuntimeToClient,
		},
		{
			name:      "rotation with non RFC3339 deadline",
			body:      `{"type":"session.rotate","protocol_version":1,"session_id":"voice-1","sequence":1,"rotate_at":"in 3 minutes"}`,
			direction: RuntimeToClient,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeControlMessage([]byte(tc.body), tc.direction, NewSequenceTracker(2, 2))
			if !errors.Is(err, ErrInvalidControlMessage) {
				t.Fatalf("DecodeControlMessage() error = %v, want %v", err, ErrInvalidControlMessage)
			}
		})
	}
}

func TestDecodeControlMessageAcceptsCompleteV1EventPayloads(t *testing.T) {
	tests := []string{
		`{"type":"agent.state","protocol_version":1,"session_id":"voice-1","sequence":1,"generation_id":1,"state":"speaking"}`,
		`{"type":"transcript.final","protocol_version":1,"session_id":"voice-1","sequence":2,"turn_id":1,"text":"final without provider language"}`,
		`{"type":"transcript.final","protocol_version":1,"session_id":"voice-1","sequence":3,"turn_id":1,"text":"नमस्ते","detected_language":"hi-IN"}`,
		`{"type":"transcript.final","protocol_version":1,"session_id":"voice-1","sequence":4,"turn_id":1,"text":"नमस्ते","detected_language":"hi-IN","language_probability":0.98}`,
		`{"type":"language.selected","protocol_version":1,"session_id":"voice-1","sequence":5,"language":"hi-IN"}`,
		`{"type":"answer.final","protocol_version":1,"session_id":"voice-1","sequence":6,"turn_id":1,"generation_id":1,"text":"आपका चालान भुगतान किया गया है।"}`,
		`{"type":"error","protocol_version":1,"session_id":"voice-1","sequence":7,"error_code":"voice_unavailable","message":"Please retry."}`,
		`{"type":"session.rotate","protocol_version":1,"session_id":"voice-1","sequence":8,"rotate_at":"2026-08-06T07:52:00Z"}`,
	}
	tracker := NewSequenceTracker(2, 9)
	for _, body := range tests {
		if _, err := DecodeControlMessage([]byte(body), RuntimeToClient, tracker); err != nil {
			t.Fatalf("DecodeControlMessage(%s) error = %v", body, err)
		}
	}
}

func TestValidateControlMessageRejectsUnsafeOrOversizedFinalAnswerText(t *testing.T) {
	generation, turnID := int64(1), int64(1)
	base := ControlMessage{
		Type: EventAnswerFinal, ProtocolVersion: ProtocolVersion, SessionID: "voice-1", Sequence: 1,
		GenerationID: &generation, TurnID: &turnID,
	}
	for _, text := range []string{
		strings.Repeat("a", MaxFinalAnswerTextBytes+1),
		"visible\x00hidden",
		" \t\n ",
	} {
		message := base
		message.Text = text
		if err := ValidateControlMessage(message, RuntimeToClient); !errors.Is(err, ErrInvalidControlMessage) {
			t.Fatalf("ValidateControlMessage(%q) error = %v, want ErrInvalidControlMessage", text, err)
		}
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
	switch eventType {
	case EventSpeechStarted, EventSpeechEnded:
		body["turn_id"] = 1
		body["client_monotonic_ms"] = 1
	case EventInterrupt, EventPlaybackStarted, EventPlaybackCompleted:
		body["turn_id"] = 1
		body["client_monotonic_ms"] = 1
	case EventNetworkChanged:
		body["client_monotonic_ms"] = 1
	case EventAgentState:
		body["state"] = "responding"
	case EventTranscriptFinal:
		body["turn_id"] = 1
		body["text"] = "final transcript"
		body["detected_language"] = "en-IN"
		body["language_probability"] = 0.99
	case EventLanguageSelected:
		body["language"] = "en-IN"
	case EventAnswerFinal:
		body["turn_id"] = 1
		body["text"] = "final visible answer"
	case EventError:
		body["error_code"] = "voice_unavailable"
	case EventSessionRotate:
		body["rotate_at"] = "2026-08-06T07:52:00Z"
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
