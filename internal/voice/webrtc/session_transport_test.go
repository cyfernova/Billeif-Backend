package webrtc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"invoice-backend/internal/voice/protocol"

	pion "github.com/pion/webrtc/v4"
)

func TestAttachPeerTransportLateBindsSessionOutput(t *testing.T) {
	binding := &transportAwareBinding{}
	peer := newFakeSignalingPeer()
	if err := attachPeerTransport(binding, peer); err != nil {
		t.Fatalf("attachPeerTransport() error = %v", err)
	}
	if binding.transport != peer {
		t.Fatalf("attached transport = %T, want exact peer", binding.transport)
	}
	if err := binding.transport.SendOpus([]byte{0x11}); err != nil {
		t.Fatalf("SendOpus() error = %v", err)
	}
	if got := peer.opusFrames(); len(got) != 1 || len(got[0]) != 1 || got[0][0] != 0x11 {
		t.Fatalf("peer Opus frames = %#v", got)
	}
}

func TestAttachPeerTransportFailsClosedOnMissingTransportAndPanic(t *testing.T) {
	plainPeer := &nonTransportSignalingPeer{done: make(chan struct{})}
	if err := attachPeerTransport(&transportAwareBinding{}, plainPeer); !errors.Is(err, ErrSignalingClose) {
		t.Fatalf("missing transport error = %v, want ErrSignalingClose", err)
	}
	if err := attachPeerTransport(&transportAwareBinding{attachPanic: true}, newFakeSignalingPeer()); !errors.Is(err, ErrSignalingClose) {
		t.Fatalf("attachment panic error = %v, want ErrSignalingClose", err)
	}
	if err := attachPeerTransport(&fakeSignalingSTTBinding{}, plainPeer); err != nil {
		t.Fatalf("legacy binding without attachment error = %v", err)
	}
}

func TestComposePeerControlHandlerRoutesPerSessionBeforeFallback(t *testing.T) {
	var order []string
	binding := &transportAwareBinding{handle: func(_ context.Context, message protocol.ControlMessage) error {
		order = append(order, "session:"+string(message.Type))
		return nil
	}}
	fallback := func(_ context.Context, message protocol.ControlMessage) error {
		order = append(order, "fallback:"+string(message.Type))
		return nil
	}
	handler := composePeerControlHandler(binding, fallback)
	if err := handler(context.Background(), protocol.ControlMessage{Type: protocol.EventInterrupt}); err != nil {
		t.Fatalf("composed handler error = %v", err)
	}
	want := []string{"session:interrupt", "fallback:interrupt"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("control order = %#v, want %#v", order, want)
	}
}

func TestComposePeerControlHandlerStopsAfterSessionFailure(t *testing.T) {
	fallbackCalls := 0
	binding := &transportAwareBinding{handle: func(context.Context, protocol.ControlMessage) error {
		return errors.New("session control failed")
	}}
	handler := composePeerControlHandler(binding, func(context.Context, protocol.ControlMessage) error {
		fallbackCalls++
		return nil
	})
	if err := handler(context.Background(), protocol.ControlMessage{Type: protocol.EventInterrupt}); !errors.Is(err, errControlHandlerPanic) {
		t.Fatalf("composed handler error = %v, want fail-closed sentinel", err)
	}
	if fallbackCalls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fallbackCalls)
	}
}

func TestComposePeerControlHandlerContainsSessionPanic(t *testing.T) {
	binding := &transportAwareBinding{handlePanic: true}
	handler := composePeerControlHandler(binding, nil)
	if err := handler(context.Background(), protocol.ControlMessage{Type: protocol.EventInterrupt}); !errors.Is(err, errControlHandlerPanic) {
		t.Fatalf("panicking session handler error = %v, want fail-closed sentinel", err)
	}
}

func TestPeerClosesWhenComposedSessionControlPanics(t *testing.T) {
	binding := &transportAwareBinding{handlePanic: true}
	config := testPeerConfig(context.Background())
	config.HandleControl = composePeerControlHandler(binding, nil)
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	peer.enqueueControlMessage(pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"interrupt","protocol_version":1,"session_id":"voice-session-1","sequence":1,"turn_id":1,"generation_id":1,"client_monotonic_ms":1}`)})
	awaitSignal(t, peer.Done(), time.Second, "composed per-session panic closes peer")
}

func TestPeerSessionCloseRetiresPeerAndBindingExactlyOnce(t *testing.T) {
	binding := &sessionClosingBinding{}
	config := testPeerConfig(context.Background())
	config.STTBinding = binding
	config.HandleControl = composePeerControlHandler(binding, nil)
	peer, err := NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer() error = %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	peer.enqueueControlMessage(pion.DataChannelMessage{IsString: true, Data: []byte(`{"type":"session.close","protocol_version":1,"session_id":"voice-session-1","sequence":1}`)})
	awaitSignal(t, peer.Done(), time.Second, "session.close retires peer")
	if got := binding.closeCalls.Load(); got != 1 {
		t.Fatalf("binding Close() calls = %d, want 1", got)
	}
}

type transportAwareBinding struct {
	transport   PeerTransport
	attachPanic bool
	handlePanic bool
	handle      func(context.Context, protocol.ControlMessage) error
}

func (*transportAwareBinding) HandleOpus([]byte) error { return nil }
func (*transportAwareBinding) HandleControl(context.Context, protocol.ControlMessage) error {
	return nil
}
func (*transportAwareBinding) Close() error { return nil }
func (binding *transportAwareBinding) AttachPeer(transport PeerTransport) error {
	if binding.attachPanic {
		panic("attachment-sensitive-canary")
	}
	binding.transport = transport
	return nil
}
func (binding *transportAwareBinding) HandlePeerControl(ctx context.Context, message protocol.ControlMessage) error {
	if binding.handlePanic {
		panic("session-control-sensitive-canary")
	}
	if binding.handle == nil {
		return nil
	}
	return binding.handle(ctx, message)
}

type nonTransportSignalingPeer struct{ done chan struct{} }

func (*nonTransportSignalingPeer) Answer(context.Context, string, bool) (string, error) {
	return "", nil
}
func (*nonTransportSignalingPeer) AddCandidate(context.Context, ICECandidate) error  { return nil }
func (*nonTransportSignalingPeer) RefreshICE(context.Context, TURNCredentials) error { return nil }
func (*nonTransportSignalingPeer) Close() error                                      { return nil }
func (peer *nonTransportSignalingPeer) Done() <-chan struct{}                        { return peer.done }

type sessionClosingBinding struct {
	closeOnce  sync.Once
	closeCalls atomic.Int32
}

func (*sessionClosingBinding) HandleOpus([]byte) error { return nil }
func (*sessionClosingBinding) HandleControl(context.Context, protocol.ControlMessage) error {
	return nil
}
func (binding *sessionClosingBinding) HandlePeerControl(_ context.Context, message protocol.ControlMessage) error {
	if message.Type == protocol.EventSessionClose {
		_ = binding.Close()
		return ErrSignalingClose
	}
	return nil
}
func (binding *sessionClosingBinding) Close() error {
	binding.closeOnce.Do(func() { binding.closeCalls.Add(1) })
	return nil
}
