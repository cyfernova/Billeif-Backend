package audio

import (
	"errors"
	"math"
	"testing"
)

func TestRTPClockAdvancesByTwentyMillisecondsOnlyAfterCommit(t *testing.T) {
	t.Parallel()

	clock := NewRTPClock(10_000)
	timestamp, err := clock.Prepare()
	if err != nil {
		t.Fatalf("first Prepare() error = %v", err)
	}
	if timestamp != 10_000 {
		t.Fatalf("first Prepare() timestamp = %d, want 10000", timestamp)
	}
	if err := clock.Abort(timestamp); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}

	timestamp, err = clock.Prepare()
	if err != nil {
		t.Fatalf("second Prepare() error = %v", err)
	}
	if timestamp != 10_000 {
		t.Fatalf("timestamp after aborted send = %d, want 10000", timestamp)
	}
	if err := clock.Commit(timestamp); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	next, err := clock.Prepare()
	if err != nil {
		t.Fatalf("third Prepare() error = %v", err)
	}
	if next != 10_960 {
		t.Fatalf("timestamp after committed send = %d, want 10960", next)
	}
}

func TestRTPClockRejectsOverlappingAndMismatchedReservations(t *testing.T) {
	t.Parallel()

	clock := NewRTPClock(77)
	timestamp, err := clock.Prepare()
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if _, err := clock.Prepare(); !errors.Is(err, ErrRTPClockReserved) {
		t.Fatalf("overlapping Prepare() error = %v, want ErrRTPClockReserved", err)
	}
	if err := clock.Commit(timestamp + 1); !errors.Is(err, ErrRTPTimestampMismatch) {
		t.Fatalf("mismatched Commit() error = %v, want ErrRTPTimestampMismatch", err)
	}
	if err := clock.Abort(timestamp); err != nil {
		t.Fatalf("Abort(correct timestamp) error = %v", err)
	}
	if err := clock.Commit(timestamp); !errors.Is(err, ErrNoRTPTimestampReservation) {
		t.Fatalf("Commit() without reservation error = %v, want ErrNoRTPTimestampReservation", err)
	}
}

func TestRTPClockWrapsTheUnsignedFortyEightKilohertzClock(t *testing.T) {
	t.Parallel()

	clock := NewRTPClock(math.MaxUint32 - 479)
	timestamp, err := clock.Prepare()
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if err := clock.Commit(timestamp); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	next, err := clock.Prepare()
	if err != nil {
		t.Fatalf("second Prepare() error = %v", err)
	}
	if next != 480 {
		t.Fatalf("wrapped timestamp = %d, want 480", next)
	}
}
