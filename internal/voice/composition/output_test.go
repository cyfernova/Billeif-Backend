package composition

import (
	"context"
	"errors"
	"sync"
	"testing"

	"invoice-backend/internal/voice/protocol"
	"invoice-backend/internal/voice/turn"
)

func TestControlOutputLateBindsAndSendsGenerationAwareText(t *testing.T) {
	output, err := NewControlOutput(validCompositionSession())
	if err != nil {
		t.Fatalf("NewControlOutput() error = %v", err)
	}
	if err := output.HandleTextDelta(context.Background(), 7, "bounded answer"); !errors.Is(err, ErrPeerOutputUnavailable) {
		t.Fatalf("pre-attach HandleTextDelta() error = %v, want ErrPeerOutputUnavailable", err)
	}
	transport := &recordingPeerTransport{}
	if err := output.AttachPeer(transport); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}
	if err := output.AttachPeer(transport); !errors.Is(err, ErrPeerOutputUnavailable) {
		t.Fatalf("second AttachPeer() error = %v, want ErrPeerOutputUnavailable", err)
	}
	if err := output.HandleTextDelta(context.Background(), 7, "bounded answer"); err != nil {
		t.Fatalf("HandleTextDelta() error = %v", err)
	}
	output.HandleTurnResult(context.Background(), turn.TurnResult{GenerationID: 7, Text: "bounded answer"}, TurnFailureNone)

	messages := transport.controlMessages()
	if len(messages) != 2 {
		t.Fatalf("control messages = %#v, want text + completion", messages)
	}
	if messages[0].Type != protocol.EventAgentState || messages[0].State != "responding" ||
		messages[0].Text != "bounded answer" || messages[0].GenerationID == nil || *messages[0].GenerationID != 7 {
		t.Fatalf("text control = %#v", messages[0])
	}
	if messages[1].Type != protocol.EventTurnCompleted || messages[1].GenerationID == nil || *messages[1].GenerationID != 7 {
		t.Fatalf("completion control = %#v", messages[1])
	}
	if messages[0].Sequence != 1 || messages[1].Sequence != 2 {
		t.Fatalf("outbound sequences = %d, %d, want 1, 2", messages[0].Sequence, messages[1].Sequence)
	}
}

func TestControlOutputUsesOnlyFrozenSafeFailureCodes(t *testing.T) {
	output, err := NewControlOutput(validCompositionSession())
	if err != nil {
		t.Fatalf("NewControlOutput() error = %v", err)
	}
	transport := &recordingPeerTransport{}
	if err := output.AttachPeer(transport); err != nil {
		t.Fatalf("AttachPeer() error = %v", err)
	}
	output.HandleTurnResult(context.Background(), turn.TurnResult{GenerationID: 1}, TurnFailureAuthorizationRefresh)
	output.HandleTurnResult(context.Background(), turn.TurnResult{GenerationID: 2}, TurnFailureCanceled)
	output.HandleTurnResult(context.Background(), turn.TurnResult{GenerationID: 3}, TurnFailureUnavailable)
	output.RequestRepeat(context.Background())

	messages := transport.controlMessages()
	if len(messages) != 4 {
		t.Fatalf("control messages = %#v", messages)
	}
	if messages[0].Type != protocol.EventError || messages[0].ErrorCode != "authorization_refresh_required" {
		t.Fatalf("authorization message = %#v", messages[0])
	}
	if messages[1].Type != protocol.EventTurnCancelled || messages[1].ErrorCode != "canceled" {
		t.Fatalf("canceled message = %#v", messages[1])
	}
	if messages[2].Type != protocol.EventError || messages[2].ErrorCode != "voice_unavailable" {
		t.Fatalf("unavailable message = %#v", messages[2])
	}
	if messages[3].Type != protocol.EventError || messages[3].ErrorCode != "repeat_required" {
		t.Fatalf("repeat message = %#v", messages[3])
	}
	for _, message := range messages {
		if message.Message != "" && message.Message != "Please repeat that." && message.Message != "Voice response is temporarily unavailable." && message.Message != "Authorization refresh is required." {
			t.Fatalf("unexpected public failure message: %#v", message)
		}
	}
}

type recordingPeerTransport struct {
	mu       sync.Mutex
	controls []protocol.ControlMessage
	opus     [][]byte
}

func (transport *recordingPeerTransport) SendControl(message protocol.ControlMessage) error {
	transport.mu.Lock()
	transport.controls = append(transport.controls, message)
	transport.mu.Unlock()
	return nil
}
func (transport *recordingPeerTransport) SendOpus(payload []byte) error {
	transport.mu.Lock()
	transport.opus = append(transport.opus, append([]byte(nil), payload...))
	transport.mu.Unlock()
	return nil
}
func (transport *recordingPeerTransport) controlMessages() []protocol.ControlMessage {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]protocol.ControlMessage(nil), transport.controls...)
}
