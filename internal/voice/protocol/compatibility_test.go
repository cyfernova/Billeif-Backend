package protocol_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/voice/protocol"
)

type goldenFlow struct {
	ProtocolVersion           int          `json:"protocol_version"`
	TerminalRuntimeSessionIDs []string     `json:"terminal_runtime_session_ids"`
	Steps                     []goldenStep `json:"steps"`
}

type goldenStep struct {
	ID               string `json:"id"`
	Transport        string `json:"transport"`
	Direction        string `json:"direction"`
	Fixture          string `json:"fixture,omitempty"`
	RuntimeSessionID string `json:"runtime_session_id,omitempty"`
}

func TestMobileHeartbeatCadencePolicyIsExplicit(t *testing.T) {
	if protocol.ClientHeartbeatInterval != 20*time.Second {
		t.Fatalf("client heartbeat interval = %s, want 20s", protocol.ClientHeartbeatInterval)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "mobile", "voice-protocol-v1.md"))
	if err != nil {
		t.Fatalf("read mobile voice protocol: %v", err)
	}
	contract := string(body)
	for _, required := range []string{
		"every 20 seconds",
		"at most one lease renewal write",
		"every 30 seconds",
		"two-minute lease",
		"ignores client timestamps",
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("mobile heartbeat policy is missing %q", required)
		}
	}
}

func TestV1GoldenMobileFlowIsDeterministicAndWireCompatible(t *testing.T) {
	manifestPath := filepath.Join("testdata", "v1", "mobile-flow.json")
	encoded, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read v1 mobile flow: %v", err)
	}
	var flow goldenFlow
	if err := json.Unmarshal(encoded, &flow); err != nil {
		t.Fatalf("decode v1 mobile flow: %v", err)
	}
	if flow.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("protocol version = %d, want %d", flow.ProtocolVersion, protocol.ProtocolVersion)
	}

	wantOrder := []string{
		"token.refresh",
		"session.create",
		"session.created",
		"session.attach",
		"session.attached",
		"webrtc.offer",
		"webrtc.answer",
		"webrtc.candidate",
		"media.open",
		"client.ready",
		"speech.started",
		"speech.ended",
		"turn.started",
		"agent.speaking",
		"answer.final",
		"playback.started",
		"playback.completed",
		"turn.completed",
		"network.changed",
		"network.reattach",
		"network.reattached",
		"webrtc.restart",
		"webrtc.restarted",
		"session.rotate",
		"session.close.control",
		"session.close.http",
		"successor.create",
		"successor.created",
		"successor.attach",
		"successor.attached",
	}
	gotOrder := make([]string, len(flow.Steps))
	for index, step := range flow.Steps {
		gotOrder[index] = step.ID
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("mobile flow order = %v, want %v", gotOrder, wantOrder)
	}
	wantTerminal := []string{"voice-session-01K1ABCDE2FGHIJK3LMNOPQRST"}
	if !reflect.DeepEqual(flow.TerminalRuntimeSessionIDs, wantTerminal) {
		t.Fatalf("terminal runtime session ids = %v, want %v", flow.TerminalRuntimeSessionIDs, wantTerminal)
	}

	clientTracker := protocol.NewSequenceTracker(4, 32)
	runtimeTracker := protocol.NewSequenceTracker(4, 32)
	oldRuntimeClosed := false
	successorAttached := false
	networkRuntimeID := ""
	restartRuntimeID := ""
	for _, step := range flow.Steps {
		if oldRuntimeClosed && containsString(flow.TerminalRuntimeSessionIDs, step.RuntimeSessionID) {
			t.Fatalf("step %q reuses terminal runtime session id %q", step.ID, step.RuntimeSessionID)
		}
		if step.ID == "successor.attach" && step.RuntimeSessionID != "" {
			successorAttached = true
		}
		if step.ID == "network.reattach" {
			networkRuntimeID = step.RuntimeSessionID
		}
		if step.ID == "webrtc.restart" {
			restartRuntimeID = step.RuntimeSessionID
		}
		if step.ID == "session.close.http" {
			oldRuntimeClosed = true
		}
		if step.Fixture == "" {
			continue
		}
		if filepath.Base(step.Fixture) != step.Fixture {
			t.Fatalf("step %q has unsafe fixture path %q", step.ID, step.Fixture)
		}
		body, err := os.ReadFile(filepath.Join("testdata", "v1", step.Fixture))
		if err != nil {
			t.Fatalf("read fixture for %q: %v", step.ID, err)
		}
		if !json.Valid(body) {
			t.Fatalf("fixture for %q is not one JSON value", step.ID)
		}
		switch step.Transport {
		case "datachannel":
			direction, tracker := protocol.ClientToRuntime, clientTracker
			if step.Direction == "runtime_to_client" {
				direction, tracker = protocol.RuntimeToClient, runtimeTracker
			} else if step.Direction != "client_to_runtime" {
				t.Fatalf("step %q has invalid DataChannel direction %q", step.ID, step.Direction)
			}
			if _, err := protocol.DecodeControlMessage(body, direction, tracker); err != nil {
				t.Fatalf("decode DataChannel fixture for %q: %v", step.ID, err)
			}
		case "agentcore", "https":
			assertGoldenEnvelope(t, step, body)
		default:
			t.Fatalf("step %q has unsupported fixture transport %q", step.ID, step.Transport)
		}
	}
	if !successorAttached {
		t.Fatal("successor session never attaches with a fresh runtime session id")
	}
	if networkRuntimeID == "" || restartRuntimeID != networkRuntimeID {
		t.Fatalf("ICE restart must stay on the surviving runtime: reattach=%q restart=%q", networkRuntimeID, restartRuntimeID)
	}
}

func TestV1GoldenInterruptionFlowFencesTheCancelledGeneration(t *testing.T) {
	encoded, err := os.ReadFile(filepath.Join("testdata", "v1", "interruption-flow.json"))
	if err != nil {
		t.Fatalf("read v1 interruption flow: %v", err)
	}
	var steps []struct {
		Direction string `json:"direction"`
		Fixture   string `json:"fixture"`
	}
	if err := json.Unmarshal(encoded, &steps); err != nil {
		t.Fatalf("decode v1 interruption flow: %v", err)
	}
	wantTypes := []protocol.EventType{
		protocol.EventSpeechStarted,
		protocol.EventSpeechEnded,
		protocol.EventTurnStarted,
		protocol.EventAgentState,
		protocol.EventInterrupt,
		protocol.EventTurnCancelled,
		protocol.EventTurnStarted,
	}
	wantGenerations := []int64{0, 0, 41, 41, 41, 41, 42}
	if len(steps) != len(wantTypes) {
		t.Fatalf("interruption steps = %d, want %d", len(steps), len(wantTypes))
	}
	clientTracker := protocol.NewSequenceTracker(1, 16)
	runtimeTracker := protocol.NewSequenceTracker(1, 16)
	for index, step := range steps {
		body, err := os.ReadFile(filepath.Join("testdata", "v1", step.Fixture))
		if err != nil {
			t.Fatalf("read interruption fixture %d: %v", index, err)
		}
		direction, tracker := protocol.ClientToRuntime, clientTracker
		if step.Direction == "runtime_to_client" {
			direction, tracker = protocol.RuntimeToClient, runtimeTracker
		} else if step.Direction != "client_to_runtime" {
			t.Fatalf("interruption fixture %d has invalid direction %q", index, step.Direction)
		}
		message, err := protocol.DecodeControlMessage(body, direction, tracker)
		if err != nil {
			t.Fatalf("decode interruption fixture %d: %v", index, err)
		}
		if message.Type != wantTypes[index] {
			t.Fatalf("interruption type %d = %q, want %q", index, message.Type, wantTypes[index])
		}
		generation := int64(0)
		if message.GenerationID != nil {
			generation = *message.GenerationID
		}
		if generation != wantGenerations[index] {
			t.Fatalf("interruption generation %d = %d, want %d", index, generation, wantGenerations[index])
		}
	}
}

func assertGoldenEnvelope(t *testing.T, step goldenStep, body []byte) {
	t.Helper()
	var envelope struct {
		Type            protocol.EventType `json:"type"`
		ProtocolVersion int                `json:"protocol_version"`
		SessionID       string             `json:"session_id"`
		Sequence        int64              `json:"sequence"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode envelope for %q: %v", step.ID, err)
	}
	if step.Transport == "https" {
		return
	}
	if envelope.ProtocolVersion != protocol.ProtocolVersion || envelope.SessionID == "" || envelope.Sequence <= 0 {
		t.Fatalf("invalid AgentCore envelope for %q: %#v", step.ID, envelope)
	}
	if step.Direction == "client_to_runtime" {
		if !containsEvent(protocol.SupportedInvocationRequests(), envelope.Type) {
			t.Fatalf("request fixture %q has unsupported type %q", step.ID, envelope.Type)
		}
		return
	}
	if step.Direction != "runtime_to_client" || !containsEvent(protocol.SupportedInvocationResponses(), envelope.Type) {
		t.Fatalf("response fixture %q has unsupported direction/type %q/%q", step.ID, step.Direction, envelope.Type)
	}
}

func containsEvent(events []protocol.EventType, want protocol.EventType) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	if want == "" {
		return false
	}
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
