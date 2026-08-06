package audio

import (
	"errors"
	"testing"
)

func TestSampleRingWrapKeepsNewestSamplesInChronologicalOrder(t *testing.T) {
	t.Parallel()

	ring, err := NewSampleRing(5)
	if err != nil {
		t.Fatalf("NewSampleRing() error = %v", err)
	}
	if dropped := ring.WriteLatest([]int16{1, 2, 3}); dropped != 0 {
		t.Fatalf("first WriteLatest() dropped = %d, want 0", dropped)
	}
	if dropped := ring.WriteLatest([]int16{4, 5, 6}); dropped != 1 {
		t.Fatalf("second WriteLatest() dropped = %d, want 1", dropped)
	}

	destination := make([]int16, 5)
	samples := ring.CopyLatest(destination)
	if samples != 5 {
		t.Fatalf("CopyLatest() samples = %d, want 5", samples)
	}
	want := []int16{2, 3, 4, 5, 6}
	for index := range want {
		if destination[index] != want[index] {
			t.Fatalf("destination[%d] = %d, want %d", index, destination[index], want[index])
		}
	}
}

func TestSampleRingOversizedWriteKeepsOnlyInputTail(t *testing.T) {
	t.Parallel()

	ring, err := NewSampleRing(4)
	if err != nil {
		t.Fatalf("NewSampleRing() error = %v", err)
	}
	ring.WriteLatest([]int16{1, 2})

	if dropped := ring.WriteLatest([]int16{3, 4, 5, 6, 7, 8}); dropped != 4 {
		t.Fatalf("WriteLatest() dropped = %d, want 4", dropped)
	}
	destination := make([]int16, 4)
	if samples := ring.CopyLatest(destination); samples != 4 {
		t.Fatalf("CopyLatest() samples = %d, want 4", samples)
	}
	want := []int16{5, 6, 7, 8}
	for index := range want {
		if destination[index] != want[index] {
			t.Fatalf("destination[%d] = %d, want %d", index, destination[index], want[index])
		}
	}
}

func TestSampleRingShortCopyReturnsNewestSubset(t *testing.T) {
	t.Parallel()

	ring, err := NewSampleRing(5)
	if err != nil {
		t.Fatalf("NewSampleRing() error = %v", err)
	}
	ring.WriteLatest([]int16{1, 2, 3, 4, 5})

	destination := make([]int16, 3)
	if samples := ring.CopyLatest(destination); samples != 3 {
		t.Fatalf("CopyLatest() samples = %d, want 3", samples)
	}
	want := []int16{3, 4, 5}
	for index := range want {
		if destination[index] != want[index] {
			t.Fatalf("destination[%d] = %d, want %d", index, destination[index], want[index])
		}
	}
}

func TestSampleRingResetDiscardsPriorAudio(t *testing.T) {
	t.Parallel()

	ring, err := NewSampleRing(3)
	if err != nil {
		t.Fatalf("NewSampleRing() error = %v", err)
	}
	ring.WriteLatest([]int16{10, 20, 30})
	ring.Reset()
	if got := ring.Len(); got != 0 {
		t.Fatalf("Len() after Reset = %d, want 0", got)
	}

	ring.WriteLatest([]int16{40})
	destination := []int16{-1, -1, -1}
	if samples := ring.CopyLatest(destination); samples != 1 {
		t.Fatalf("CopyLatest() samples = %d, want 1", samples)
	}
	if destination[0] != 40 {
		t.Fatalf("destination[0] = %d, want 40", destination[0])
	}
}

func TestNewSampleRingRejectsNonPositiveCapacity(t *testing.T) {
	t.Parallel()

	for _, capacity := range []int{-1, 0} {
		if ring, err := NewSampleRing(capacity); !errors.Is(err, ErrInvalidRingCapacity) || ring != nil {
			t.Fatalf("NewSampleRing(%d) = (%v, %v), want (nil, ErrInvalidRingCapacity)", capacity, ring, err)
		}
	}
}

func TestNewPreRollRingIsBoundedToFiveHundredMilliseconds(t *testing.T) {
	t.Parallel()

	ring := NewPreRollRing()
	ring.WriteLatest(make([]int16, 8_001))
	if got := ring.Len(); got != 8_000 {
		t.Fatalf("pre-roll Len() = %d, want 8000", got)
	}
}
