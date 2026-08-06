package audio

import (
	"errors"
	"sync"
)

const (
	TTSQueueSamples = PCMRateHz * 750 / 1_000
	OpusQueueFrames = 12
)

var (
	ErrInvalidGeneration   = errors.New("audio: generation must be positive")
	ErrNoActiveGeneration  = errors.New("audio: no active generation")
	ErrStaleGeneration     = errors.New("audio: stale generation")
	ErrFutureGeneration    = errors.New("audio: generation has not been activated")
	ErrGenerationCancelled = errors.New("audio: generation is cancelled")
	ErrQueueOverflow       = errors.New("audio: bounded queue overflow")
	ErrQueueEmpty          = errors.New("audio: insufficient queued audio")
	ErrQueueClosed         = errors.New("audio: queues are closed")
	ErrInvalidOpusPacket   = errors.New("audio: Opus packet is empty")
	ErrOpusPacketTooLarge  = errors.New("audio: Opus packet exceeds the bounded frame size")
)

type CancelGenerationFunc func(generation uint64, cause error)

type QueueState struct {
	Generation uint64
	PCMSamples int
	OpusFrames int
	Cancelled  bool
	Closed     bool
}

type opusSlot struct {
	size    int
	payload [MaxOpusPacketBytes]byte
}

// GenerationQueues binds both bounded audio queues to one active generation.
// Advancing or cancelling a generation clears PCM and Opus together, so stale
// audio cannot cross a barge-in boundary.
type GenerationQueues struct {
	mu sync.Mutex

	generation uint64
	cancelled  bool
	closed     bool
	onCancel   CancelGenerationFunc

	pcm     [TTSQueueSamples]int16
	pcmHead int
	pcmSize int

	opus     [OpusQueueFrames]opusSlot
	opusHead int
	opusSize int
}

func NewGenerationQueues(onCancel CancelGenerationFunc) *GenerationQueues {
	return &GenerationQueues{onCancel: onCancel}
}

func (queues *GenerationQueues) Activate(generation uint64) error {
	queues.mu.Lock()
	defer queues.mu.Unlock()

	if queues.closed {
		return ErrQueueClosed
	}
	if generation == 0 {
		return ErrInvalidGeneration
	}
	if queues.generation == 0 {
		queues.generation = generation
		return nil
	}
	if generation < queues.generation {
		return ErrStaleGeneration
	}
	if generation == queues.generation {
		if queues.cancelled {
			return ErrGenerationCancelled
		}
		return nil
	}

	queues.clearAudioLocked()
	queues.generation = generation
	queues.cancelled = false
	return nil
}

func (queues *GenerationQueues) PushPCM(generation uint64, source []int16) error {
	queues.mu.Lock()
	if err := queues.validateGenerationLocked(generation); err != nil {
		queues.mu.Unlock()
		return err
	}
	if len(source) > len(queues.pcm)-queues.pcmSize {
		callback := queues.cancelLocked()
		queues.mu.Unlock()
		if callback != nil {
			callback(generation, ErrQueueOverflow)
		}
		return ErrQueueOverflow
	}

	queues.pushPCMLocked(source)
	queues.mu.Unlock()
	return nil
}

// PopPCMFrame consumes exactly one 20 millisecond frame. Partial consumption is
// intentionally unsupported so an encoder cannot drift from the 48 kHz RTP
// clock cadence.
func (queues *GenerationQueues) PopPCMFrame(generation uint64, destination []int16) error {
	queues.mu.Lock()
	defer queues.mu.Unlock()

	if err := queues.validateGenerationLocked(generation); err != nil {
		return err
	}
	if len(destination) != SamplesPerFrame {
		return ErrInvalidPCMFrame
	}
	if queues.pcmSize < len(destination) {
		return ErrQueueEmpty
	}

	queues.popPCMLocked(destination)
	return nil
}

// PushOpus queues only encoded payload. RTP timestamps are reserved and
// committed by RTPClock at the actual network-send boundary, never here.
func (queues *GenerationQueues) PushOpus(generation uint64, payload []byte) error {
	queues.mu.Lock()
	if err := queues.validateGenerationLocked(generation); err != nil {
		queues.mu.Unlock()
		return err
	}
	if len(payload) == 0 {
		queues.mu.Unlock()
		return ErrInvalidOpusPacket
	}
	if len(payload) > MaxOpusPacketBytes {
		queues.mu.Unlock()
		return ErrOpusPacketTooLarge
	}
	if queues.opusSize == len(queues.opus) {
		callback := queues.cancelLocked()
		queues.mu.Unlock()
		if callback != nil {
			callback(generation, ErrQueueOverflow)
		}
		return ErrQueueOverflow
	}

	index := (queues.opusHead + queues.opusSize) % len(queues.opus)
	slot := &queues.opus[index]
	clear(slot.payload[:])
	copy(slot.payload[:], payload)
	slot.size = len(payload)
	queues.opusSize++
	queues.mu.Unlock()
	return nil
}

func (queues *GenerationQueues) PopOpus(generation uint64, destination []byte) (int, error) {
	queues.mu.Lock()
	defer queues.mu.Unlock()

	if err := queues.validateGenerationLocked(generation); err != nil {
		return 0, err
	}
	if queues.opusSize == 0 {
		return 0, ErrQueueEmpty
	}

	slot := &queues.opus[queues.opusHead]
	if len(destination) < slot.size {
		return 0, ErrDestinationTooSmall
	}
	copy(destination, slot.payload[:slot.size])
	bytesRead := slot.size
	clear(slot.payload[:])
	*slot = opusSlot{}
	queues.opusHead = (queues.opusHead + 1) % len(queues.opus)
	queues.opusSize--
	if queues.opusSize == 0 {
		queues.opusHead = 0
	}
	return bytesRead, nil
}

func (queues *GenerationQueues) Cancel(generation uint64, cause error) error {
	queues.mu.Lock()
	if err := queues.validateGenerationLocked(generation); err != nil {
		queues.mu.Unlock()
		return err
	}
	if cause == nil {
		cause = ErrGenerationCancelled
	}
	callback := queues.cancelLocked()
	queues.mu.Unlock()
	if callback != nil {
		callback(generation, cause)
	}
	return nil
}

func (queues *GenerationQueues) State() QueueState {
	queues.mu.Lock()
	defer queues.mu.Unlock()
	return QueueState{
		Generation: queues.generation,
		PCMSamples: queues.pcmSize,
		OpusFrames: queues.opusSize,
		Cancelled:  queues.cancelled,
		Closed:     queues.closed,
	}
}

func (queues *GenerationQueues) Close() error {
	queues.mu.Lock()
	defer queues.mu.Unlock()
	if queues.closed {
		return nil
	}
	queues.clearAudioLocked()
	queues.closed = true
	queues.cancelled = false
	queues.onCancel = nil
	return nil
}

func (queues *GenerationQueues) validateGenerationLocked(generation uint64) error {
	if queues.closed {
		return ErrQueueClosed
	}
	if generation == 0 {
		return ErrInvalidGeneration
	}
	if queues.generation == 0 {
		return ErrNoActiveGeneration
	}
	if generation < queues.generation {
		return ErrStaleGeneration
	}
	if generation > queues.generation {
		return ErrFutureGeneration
	}
	if queues.cancelled {
		return ErrGenerationCancelled
	}
	return nil
}

func (queues *GenerationQueues) cancelLocked() CancelGenerationFunc {
	if queues.cancelled {
		return nil
	}
	queues.clearAudioLocked()
	queues.cancelled = true
	return queues.onCancel
}

func (queues *GenerationQueues) pushPCMLocked(source []int16) {
	if len(source) == 0 {
		return
	}
	writeIndex := (queues.pcmHead + queues.pcmSize) % len(queues.pcm)
	first := len(source)
	if remaining := len(queues.pcm) - writeIndex; first > remaining {
		first = remaining
	}
	copy(queues.pcm[writeIndex:writeIndex+first], source[:first])
	copy(queues.pcm[:], source[first:])
	queues.pcmSize += len(source)
}

func (queues *GenerationQueues) popPCMLocked(destination []int16) {
	first := len(destination)
	if remaining := len(queues.pcm) - queues.pcmHead; first > remaining {
		first = remaining
	}
	copy(destination[:first], queues.pcm[queues.pcmHead:queues.pcmHead+first])
	clear(queues.pcm[queues.pcmHead : queues.pcmHead+first])
	copy(destination[first:], queues.pcm[:len(destination)-first])
	clear(queues.pcm[:len(destination)-first])
	queues.pcmHead = (queues.pcmHead + len(destination)) % len(queues.pcm)
	queues.pcmSize -= len(destination)
	if queues.pcmSize == 0 {
		queues.pcmHead = 0
	}
}

func (queues *GenerationQueues) clearAudioLocked() {
	clear(queues.pcm[:])
	queues.pcmHead = 0
	queues.pcmSize = 0
	for index := range queues.opus {
		clear(queues.opus[index].payload[:])
		queues.opus[index] = opusSlot{}
	}
	queues.opusHead = 0
	queues.opusSize = 0
}
