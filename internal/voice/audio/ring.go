package audio

import (
	"errors"
	"sync"
)

const PreRollSamples = PCMRateHz / 2

var ErrInvalidRingCapacity = errors.New("audio: ring capacity must be positive")

// SampleRing is a fixed-capacity PCM ring. When full, WriteLatest drops the
// oldest samples so a VAD pre-roll always represents the newest audio.
type SampleRing struct {
	mu      sync.Mutex
	samples []int16
	head    int
	size    int
}

func NewSampleRing(capacity int) (*SampleRing, error) {
	if capacity <= 0 {
		return nil, ErrInvalidRingCapacity
	}
	return &SampleRing{samples: make([]int16, capacity)}, nil
}

func NewPreRollRing() *SampleRing {
	return &SampleRing{samples: make([]int16, PreRollSamples)}
}

func (ring *SampleRing) WriteLatest(source []int16) int {
	ring.mu.Lock()
	defer ring.mu.Unlock()

	available := len(ring.samples) - ring.size
	dropped := len(source) - available
	if dropped < 0 {
		dropped = 0
	}

	if len(source) >= len(ring.samples) {
		tail := source[len(source)-len(ring.samples):]
		copy(ring.samples, tail)
		ring.head = 0
		ring.size = len(ring.samples)
		return dropped
	}

	for _, sample := range source {
		if ring.size < len(ring.samples) {
			index := (ring.head + ring.size) % len(ring.samples)
			ring.samples[index] = sample
			ring.size++
			continue
		}
		ring.samples[ring.head] = sample
		ring.head = (ring.head + 1) % len(ring.samples)
	}
	return dropped
}

func (ring *SampleRing) CopyLatest(destination []int16) int {
	ring.mu.Lock()
	defer ring.mu.Unlock()

	count := ring.size
	if len(destination) < count {
		count = len(destination)
	}
	if count == 0 {
		return 0
	}
	start := (ring.head + ring.size - count) % len(ring.samples)
	for index := 0; index < count; index++ {
		destination[index] = ring.samples[(start+index)%len(ring.samples)]
	}
	return count
}

func (ring *SampleRing) Len() int {
	ring.mu.Lock()
	defer ring.mu.Unlock()
	return ring.size
}

func (ring *SampleRing) Cap() int {
	return len(ring.samples)
}

func (ring *SampleRing) Reset() {
	ring.mu.Lock()
	defer ring.mu.Unlock()
	clear(ring.samples)
	ring.head = 0
	ring.size = 0
}
