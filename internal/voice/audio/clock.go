package audio

import (
	"errors"
	"sync"
)

var (
	ErrRTPClockReserved          = errors.New("audio: RTP timestamp is already reserved")
	ErrNoRTPTimestampReservation = errors.New("audio: no RTP timestamp is reserved")
	ErrRTPTimestampMismatch      = errors.New("audio: RTP timestamp does not match the reservation")
)

// RTPClock advances on successful 20 millisecond sends. Prepare reserves the
// current timestamp; callers Commit only after the packet write succeeds and
// Abort after a failed write.
type RTPClock struct {
	mu       sync.Mutex
	next     uint32
	reserved bool
}

func NewRTPClock(initial uint32) *RTPClock {
	return &RTPClock{next: initial}
}

func (clock *RTPClock) Prepare() (uint32, error) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if clock.reserved {
		return 0, ErrRTPClockReserved
	}
	clock.reserved = true
	return clock.next, nil
}

func (clock *RTPClock) Commit(timestamp uint32) error {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if !clock.reserved {
		return ErrNoRTPTimestampReservation
	}
	if timestamp != clock.next {
		return ErrRTPTimestampMismatch
	}
	clock.next += RTPTimePerFrame
	clock.reserved = false
	return nil
}

func (clock *RTPClock) Abort(timestamp uint32) error {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if !clock.reserved {
		return ErrNoRTPTimestampReservation
	}
	if timestamp != clock.next {
		return ErrRTPTimestampMismatch
	}
	clock.reserved = false
	return nil
}
