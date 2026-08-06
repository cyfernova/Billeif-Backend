package composition

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"unicode/utf8"

	"invoice-backend/internal/voice/protocol"
	voicesession "invoice-backend/internal/voice/session"
	"invoice-backend/internal/voice/turn"
	"invoice-backend/internal/voice/webrtc"
)

const maxControlOutputTextBytes = protocol.MaxFinalAnswerTextBytes

type ControlOutputFactory struct{}

func (ControlOutputFactory) NewOutput(value voicesession.Session) (SessionOutput, error) {
	return NewControlOutput(value)
}

// ControlOutput is the production text/control sink used before the Task 12
// speech sink is layered in. It is inert until signaling late-binds the exact
// peer and it exposes only frozen, non-sensitive failure codes.
type ControlOutput struct {
	mu sync.Mutex

	sessionID string
	transport webrtc.PeerTransport
	sequence  int
	attached  bool
	closed    bool
	closeOnce sync.Once
}

func NewControlOutput(value voicesession.Session) (*ControlOutput, error) {
	if !safeIdentifier(value.ID, 128) {
		return nil, ErrSessionOutputUnavailable
	}
	return &ControlOutput{sessionID: value.ID}, nil
}

func (output *ControlOutput) AttachPeer(transport webrtc.PeerTransport) error {
	if output == nil || nilInterface(transport) {
		return ErrPeerOutputUnavailable
	}
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.closed || output.attached {
		return ErrPeerOutputUnavailable
	}
	output.transport = transport
	output.attached = true
	return nil
}

func (output *ControlOutput) HandleTextDelta(ctx context.Context, generation uint64, delta string) error {
	if !validOutputContext(ctx) || !validControlOutputDelta(delta) {
		return ErrPeerOutputUnavailable
	}
	generationID, ok := controlGeneration(generation)
	if !ok {
		return ErrPeerOutputUnavailable
	}
	return output.send(ctx, protocol.ControlMessage{
		Type: protocol.EventAgentState, GenerationID: &generationID, State: "responding", Text: delta,
	})
}

func (output *ControlOutput) HandleTurnResult(ctx context.Context, result turn.TurnResult, failure TurnFailure) {
	if !validOutputContext(ctx) {
		return
	}
	generationID, validGeneration := controlGeneration(result.GenerationID)
	switch failure {
	case TurnFailureNone:
		if validGeneration {
			_ = output.send(ctx, protocol.ControlMessage{Type: protocol.EventTurnCompleted, GenerationID: &generationID})
		}
	case TurnFailureAuthorizationRefresh:
		_ = output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventError, ErrorCode: "authorization_refresh_required", Message: "Authorization refresh is required.",
		})
	case TurnFailureCanceled:
		if validGeneration {
			_ = output.send(ctx, protocol.ControlMessage{
				Type: protocol.EventTurnCancelled, GenerationID: &generationID, ErrorCode: "canceled",
			})
		}
	default:
		_ = output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventError, ErrorCode: "voice_unavailable", Message: "Voice response is temporarily unavailable.",
		})
	}
}

func (output *ControlOutput) RequestRepeat(ctx context.Context) {
	if validOutputContext(ctx) {
		_ = output.send(ctx, protocol.ControlMessage{
			Type: protocol.EventError, ErrorCode: "repeat_required", Message: "Please repeat that.",
		})
	}
}

// HandlePeerControl is intentionally a no-op in the text-only sink. The
// per-session route is live so the Task 12 barge-in sink can synchronously
// cancel its generation and TTS socket without changing signaling again.
func (*ControlOutput) HandlePeerControl(ctx context.Context, _ protocol.ControlMessage) error {
	if !validOutputContext(ctx) {
		return ErrBindingClosed
	}
	return nil
}

func (output *ControlOutput) send(ctx context.Context, message protocol.ControlMessage) (resultErr error) {
	if output == nil || !validOutputContext(ctx) {
		return ErrPeerOutputUnavailable
	}
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.closed || !output.attached || nilInterface(output.transport) || output.sequence == math.MaxInt {
		return ErrPeerOutputUnavailable
	}
	message.ProtocolVersion = protocol.ProtocolVersion
	message.SessionID = output.sessionID
	message.Sequence = output.sequence + 1
	defer func() {
		if recover() != nil {
			resultErr = ErrPeerOutputUnavailable
		}
	}()
	if err := output.transport.SendControl(message); err != nil {
		return ErrPeerOutputUnavailable
	}
	output.sequence++
	return nil
}

func (output *ControlOutput) Close() error {
	if output == nil {
		return nil
	}
	output.closeOnce.Do(func() {
		output.mu.Lock()
		output.closed = true
		output.transport = nil
		output.sessionID = ""
		output.mu.Unlock()
	})
	return nil
}

func (*ControlOutput) String() string   { return "voice control output{redacted}" }
func (*ControlOutput) GoString() string { return "voice control output{redacted}" }
func (*ControlOutput) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{}{})
}

func validControlOutputText(value string) bool {
	return strings.TrimSpace(value) != "" && validControlOutputDelta(value)
}

func validControlOutputDelta(value string) bool {
	if value == "" || len(value) > maxControlOutputTextBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character == 0 || (character < 0x20 && character != '\n' && character != '\r' && character != '\t') || character == 0x7f {
			return false
		}
	}
	return true
}

func validOutputContext(ctx context.Context) bool {
	return ctx != nil && ctx.Err() == nil
}

func controlGeneration(generation uint64) (int64, bool) {
	if generation == 0 || generation > math.MaxInt64 {
		return 0, false
	}
	return int64(generation), true
}

var _ SessionOutput = (*ControlOutput)(nil)
