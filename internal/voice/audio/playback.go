package audio

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"
)

var (
	ErrInvalidPlayback  = errors.New("audio: invalid generation playback")
	ErrPlaybackFinished = errors.New("audio: generation playback input is finished")
)

type OpusSender interface {
	SendOpus([]byte) error
}

type FramePacer interface {
	Wait(context.Context) error
}

type GenerationGuard func(uint64, func() error) error

type GenerationPlaybackConfig struct {
	Context    context.Context
	Generation uint64
	Encoder    Encoder
	Sender     OpusSender
	Pacer      FramePacer
	Guard      GenerationGuard
}

// GenerationPlayback incrementally converts raw 16 kHz mono PCM16LE into
// paced 20 ms Opus frames. PCM and encoded queues remain independently bounded
// at 750 ms and 12 frames (240 ms); a large provider event is backpressured
// and consumed frame by frame instead of being inserted wholesale.
type GenerationPlayback struct {
	ctx        context.Context
	cancel     context.CancelFunc
	generation uint64
	queues     *GenerationQueues
	encoder    *FrameEncoder
	sender     OpusSender
	pacer      FramePacer
	guard      GenerationGuard

	pushMu sync.Mutex
	mu     sync.Mutex
	final  bool
	err    error

	wake  chan struct{}
	space chan struct{}
	done  chan struct{}

	closeOnce sync.Once
}

func NewGenerationPlayback(config GenerationPlaybackConfig) (*GenerationPlayback, error) {
	if config.Context == nil || config.Context.Err() != nil || config.Generation == 0 ||
		isNilAudioInterface(config.Encoder) || isNilAudioInterface(config.Sender) ||
		(config.Pacer != nil && isNilAudioInterface(config.Pacer)) {
		return nil, ErrInvalidPlayback
	}
	frameEncoder, err := NewFrameEncoder(config.Encoder)
	if err != nil {
		return nil, ErrInvalidPlayback
	}
	pacer := config.Pacer
	if pacer == nil {
		pacer = realtimeFramePacer{}
	}
	guard := config.Guard
	if guard == nil {
		guard = func(_ uint64, callback func() error) error { return callback() }
	}
	ctx, cancel := context.WithCancel(config.Context)
	queues := NewGenerationQueues(nil)
	if err := queues.Activate(config.Generation); err != nil {
		cancel()
		return nil, ErrInvalidPlayback
	}
	playback := &GenerationPlayback{
		ctx: ctx, cancel: cancel, generation: config.Generation,
		queues: queues, encoder: frameEncoder, sender: config.Sender, pacer: pacer, guard: guard,
		wake: make(chan struct{}, 1), space: make(chan struct{}, 1), done: make(chan struct{}),
	}
	go playback.run()
	return playback, nil
}

func (playback *GenerationPlayback) PushPCM16LE(ctx context.Context, source []byte) error {
	if playback == nil || ctx == nil {
		return ErrInvalidPlayback
	}
	if len(source) == 0 || len(source)%2 != 0 {
		return ErrInvalidPCMBytes
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	playback.pushMu.Lock()
	defer playback.pushMu.Unlock()
	if playback.inputFinished() {
		return ErrPlaybackFinished
	}
	return playback.pushPCMBytesLocked(ctx, source)
}

func (playback *GenerationPlayback) Finish(ctx context.Context) error {
	if playback == nil || ctx == nil {
		return ErrInvalidPlayback
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	playback.pushMu.Lock()
	if !playback.inputFinished() {
		state := playback.queues.State()
		if state.Cancelled {
			playback.pushMu.Unlock()
			return ErrGenerationCancelled
		}
		if remainder := state.PCMSamples % SamplesPerFrame; remainder != 0 {
			padding := make([]byte, (SamplesPerFrame-remainder)*2)
			if err := playback.pushPCMBytesLocked(ctx, padding); err != nil {
				playback.pushMu.Unlock()
				return err
			}
		}
		playback.mu.Lock()
		playback.final = true
		playback.mu.Unlock()
		playback.signal(playback.wake)
	}
	playback.pushMu.Unlock()

	select {
	case <-playback.done:
		return playback.Err()
	case <-ctx.Done():
		_ = playback.Cancel(playback.generation, ctx.Err())
		return ctx.Err()
	}
}

func (playback *GenerationPlayback) Cancel(generation uint64, cause error) error {
	if playback == nil || generation == 0 {
		return ErrInvalidPlayback
	}
	if generation < playback.generation {
		return ErrStaleGeneration
	}
	if generation > playback.generation {
		return ErrFutureGeneration
	}
	playback.setError(ErrGenerationCancelled)
	err := playback.queues.Cancel(generation, cause)
	if err != nil && !errors.Is(err, ErrGenerationCancelled) {
		return err
	}
	playback.cancel()
	playback.signal(playback.wake)
	playback.signal(playback.space)
	return nil
}

func (playback *GenerationPlayback) State() QueueState {
	if playback == nil || playback.queues == nil {
		return QueueState{Closed: true}
	}
	return playback.queues.State()
}

func (playback *GenerationPlayback) Done() <-chan struct{} {
	if playback == nil || playback.done == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return playback.done
}

func (playback *GenerationPlayback) Err() error {
	if playback == nil {
		return ErrInvalidPlayback
	}
	playback.mu.Lock()
	defer playback.mu.Unlock()
	return playback.err
}

func (playback *GenerationPlayback) Close() error {
	if playback == nil {
		return nil
	}
	playback.closeOnce.Do(func() {
		_ = playback.Cancel(playback.generation, ErrQueueClosed)
		<-playback.done
		_ = playback.queues.Close()
		playback.encoder.Reset()
	})
	return nil
}

func (playback *GenerationPlayback) pushPCMBytesLocked(ctx context.Context, source []byte) error {
	var samples [SamplesPerFrame]int16
	for offset := 0; offset < len(source); {
		state := playback.queues.State()
		if state.Cancelled {
			return ErrGenerationCancelled
		}
		if state.Closed {
			return ErrQueueClosed
		}
		available := TTSQueueSamples - state.PCMSamples
		if available == 0 {
			select {
			case <-playback.space:
				continue
			case <-playback.ctx.Done():
				return playback.cancellationError()
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		remainingSamples := (len(source) - offset) / 2
		count := min(remainingSamples, SamplesPerFrame, available)
		clear(samples[:])
		bytesRead := count * 2
		if _, err := PCM16LEToSamples(source[offset:offset+bytesRead], samples[:count]); err != nil {
			return err
		}
		if err := playback.queues.PushPCM(playback.generation, samples[:count]); err != nil {
			return err
		}
		offset += bytesRead
		playback.signal(playback.wake)
	}
	return nil
}

func (playback *GenerationPlayback) run() {
	defer close(playback.done)
	sent := false
	for {
		if err := playback.fillOpus(); err != nil {
			playback.fail(err)
			return
		}
		state := playback.queues.State()
		if playback.inputFinished() && state.PCMSamples == 0 && state.OpusFrames == 0 {
			return
		}
		if state.OpusFrames == 0 {
			select {
			case <-playback.wake:
				continue
			case <-playback.ctx.Done():
				return
			}
		}
		if sent {
			if err := playback.pacer.Wait(playback.ctx); err != nil {
				if playback.ctx.Err() == nil {
					playback.fail(err)
				}
				return
			}
		}
		var packet [MaxOpusPacketBytes]byte
		bytesRead, err := playback.queues.PopOpus(playback.generation, packet[:])
		if err != nil {
			if errors.Is(err, ErrGenerationCancelled) || errors.Is(err, ErrQueueClosed) {
				return
			}
			playback.fail(err)
			return
		}
		err = playback.guard(playback.generation, func() error {
			return playback.sender.SendOpus(packet[:bytesRead])
		})
		clear(packet[:])
		if err != nil {
			playback.fail(err)
			return
		}
		sent = true
		playback.signal(playback.space)
	}
}

func (playback *GenerationPlayback) fillOpus() error {
	var pcm [SamplesPerFrame]int16
	for {
		state := playback.queues.State()
		if state.Cancelled || state.Closed {
			return ErrGenerationCancelled
		}
		if state.PCMSamples < SamplesPerFrame || state.OpusFrames >= OpusQueueFrames {
			return nil
		}
		clear(pcm[:])
		if err := playback.queues.PopPCMFrame(playback.generation, pcm[:]); err != nil {
			return err
		}
		playback.signal(playback.space)
		packet, err := playback.encoder.Encode(pcm[:])
		clear(pcm[:])
		if err != nil {
			return err
		}
		if err := playback.queues.PushOpus(playback.generation, packet); err != nil {
			return err
		}
	}
}

func (playback *GenerationPlayback) fail(err error) {
	if err == nil {
		err = ErrInvalidPlayback
	}
	playback.setError(err)
	_ = playback.queues.Cancel(playback.generation, err)
	playback.cancel()
	playback.signal(playback.space)
}

func (playback *GenerationPlayback) setError(err error) {
	playback.mu.Lock()
	if playback.err == nil {
		playback.err = err
	}
	playback.mu.Unlock()
}

func (playback *GenerationPlayback) inputFinished() bool {
	playback.mu.Lock()
	defer playback.mu.Unlock()
	return playback.final
}

func (playback *GenerationPlayback) cancellationError() error {
	state := playback.queues.State()
	if state.Cancelled {
		return ErrGenerationCancelled
	}
	if err := playback.Err(); err != nil {
		return err
	}
	return ErrQueueClosed
}

func (*GenerationPlayback) signal(channel chan struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}

type realtimeFramePacer struct{}

func (realtimeFramePacer) Wait(ctx context.Context) error {
	timer := time.NewTimer(FrameDurationMS * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isNilAudioInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

var _ interface{ Close() error } = (*GenerationPlayback)(nil)
