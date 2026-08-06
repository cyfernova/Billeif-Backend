package audio

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGenerationPlaybackPacesLargePCMWithoutExceedingQueues(t *testing.T) {
	pacer := newManualFramePacer()
	sender := &recordingOpusSender{sent: make(chan struct{}, 64)}
	playback, err := NewGenerationPlayback(GenerationPlaybackConfig{
		Context: context.Background(), Generation: 7,
		Encoder: &playbackTestEncoder{}, Sender: sender, Pacer: pacer,
		Guard: func(generation uint64, callback func() error) error {
			if generation != 7 {
				return ErrStaleGeneration
			}
			return callback()
		},
	})
	if err != nil {
		t.Fatalf("NewGenerationPlayback() error = %v", err)
	}
	t.Cleanup(func() { _ = playback.Close() })

	// Two seconds is larger than the combined bounded PCM and Opus queues. PushPCM16LE must
	// backpressure and feed it incrementally instead of overflowing/cancelling.
	pcm := make([]byte, PCMRateHz*2*2)
	pushDone := make(chan error, 1)
	go func() { pushDone <- playback.PushPCM16LE(context.Background(), pcm) }()

	select {
	case <-sender.sent:
	case <-time.After(time.Second):
		t.Fatal("first Opus packet was not sent immediately")
	}
	state := playback.State()
	if state.PCMSamples > TTSQueueSamples || state.OpusFrames > OpusQueueFrames || state.Cancelled {
		t.Fatalf("bounded state while producer is backpressured = %#v", state)
	}
	select {
	case err := <-pushDone:
		t.Fatalf("two-second PushPCM16LE completed before paced capacity was available: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	for range 99 {
		pacer.Tick()
	}
	if err := <-pushDone; err != nil {
		t.Fatalf("PushPCM16LE() error = %v", err)
	}
	if err := playback.Finish(context.Background()); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if got := sender.count(); got != 100 {
		t.Fatalf("sent Opus frames = %d, want 100", got)
	}
	state = playback.State()
	if state.PCMSamples != 0 || state.OpusFrames != 0 || state.Cancelled {
		t.Fatalf("final playback state = %#v", state)
	}
}

func TestGenerationPlaybackSendsAtMostOneFramePerPacerTick(t *testing.T) {
	pacer := newManualFramePacer()
	sender := &recordingOpusSender{sent: make(chan struct{}, 8)}
	playback, err := NewGenerationPlayback(GenerationPlaybackConfig{
		Context: context.Background(), Generation: 1,
		Encoder: &playbackTestEncoder{}, Sender: sender, Pacer: pacer,
	})
	if err != nil {
		t.Fatalf("NewGenerationPlayback() error = %v", err)
	}
	t.Cleanup(func() { _ = playback.Close() })
	if err := playback.PushPCM16LE(context.Background(), make([]byte, PCMBytesPerFrame*3)); err != nil {
		t.Fatalf("PushPCM16LE() error = %v", err)
	}
	select {
	case <-sender.sent:
	case <-time.After(time.Second):
		t.Fatal("first packet was not sent")
	}
	if got := sender.count(); got != 1 {
		t.Fatalf("packets before pacing tick = %d, want 1", got)
	}
	pacer.Tick()
	select {
	case <-sender.sent:
	case <-time.After(time.Second):
		t.Fatal("second packet was not sent after tick")
	}
	if got := sender.count(); got != 2 {
		t.Fatalf("packets after one tick = %d, want 2", got)
	}
	pacer.Tick()
	if err := playback.Finish(context.Background()); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if got := sender.count(); got != 3 {
		t.Fatalf("final packet count = %d, want 3", got)
	}
}

func TestGenerationPlaybackInterruptPurgesAndFencesLatePCM(t *testing.T) {
	pacer := newManualFramePacer()
	sender := &recordingOpusSender{sent: make(chan struct{}, 8)}
	playback, err := NewGenerationPlayback(GenerationPlaybackConfig{
		Context: context.Background(), Generation: 4,
		Encoder: &playbackTestEncoder{}, Sender: sender, Pacer: pacer,
	})
	if err != nil {
		t.Fatalf("NewGenerationPlayback() error = %v", err)
	}
	if err := playback.PushPCM16LE(context.Background(), make([]byte, PCMBytesPerFrame*4)); err != nil {
		t.Fatalf("PushPCM16LE() error = %v", err)
	}
	select {
	case <-sender.sent:
	case <-time.After(time.Second):
		t.Fatal("first packet was not sent")
	}
	if err := playback.Cancel(4, context.Canceled); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	state := playback.State()
	if state.PCMSamples != 0 || state.OpusFrames != 0 || !state.Cancelled {
		t.Fatalf("state after cancellation = %#v", state)
	}
	pacer.Tick()
	time.Sleep(20 * time.Millisecond)
	if got := sender.count(); got != 1 {
		t.Fatalf("stale packets sent after cancellation = %d", got-1)
	}
	if err := playback.PushPCM16LE(context.Background(), make([]byte, PCMBytesPerFrame)); !errors.Is(err, ErrGenerationCancelled) {
		t.Fatalf("late PushPCM16LE() error = %v, want ErrGenerationCancelled", err)
	}
	if err := playback.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestGenerationPlaybackRejectsMalformedPCMAndDependencies(t *testing.T) {
	if playback, err := NewGenerationPlayback(GenerationPlaybackConfig{}); playback != nil || !errors.Is(err, ErrInvalidPlayback) {
		t.Fatalf("NewGenerationPlayback(empty) = (%v, %v)", playback, err)
	}
	playback, err := NewGenerationPlayback(GenerationPlaybackConfig{
		Context: context.Background(), Generation: 1, Encoder: &playbackTestEncoder{}, Sender: &recordingOpusSender{},
	})
	if err != nil {
		t.Fatalf("NewGenerationPlayback() error = %v", err)
	}
	t.Cleanup(func() { _ = playback.Close() })
	if err := playback.PushPCM16LE(context.Background(), []byte{1}); !errors.Is(err, ErrInvalidPCMBytes) {
		t.Fatalf("odd PCM error = %v, want ErrInvalidPCMBytes", err)
	}
}

type playbackTestEncoder struct{ calls atomic.Int32 }

func (encoder *playbackTestEncoder) Encode(_ []int16, destination []byte) (int, error) {
	call := encoder.calls.Add(1)
	destination[0] = byte(call)
	destination[1] = 0x7f
	return 2, nil
}

type recordingOpusSender struct {
	mu      sync.Mutex
	packets [][]byte
	sent    chan struct{}
}

func (sender *recordingOpusSender) SendOpus(payload []byte) error {
	sender.mu.Lock()
	sender.packets = append(sender.packets, append([]byte(nil), payload...))
	sender.mu.Unlock()
	if sender.sent != nil {
		select {
		case sender.sent <- struct{}{}:
		default:
		}
	}
	return nil
}

func (sender *recordingOpusSender) count() int {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return len(sender.packets)
}

type manualFramePacer struct{ ticks chan struct{} }

func newManualFramePacer() *manualFramePacer {
	return &manualFramePacer{ticks: make(chan struct{}, 128)}
}

func (pacer *manualFramePacer) Wait(ctx context.Context) error {
	select {
	case <-pacer.ticks:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (pacer *manualFramePacer) Tick() { pacer.ticks <- struct{}{} }
