package audio

import (
	"errors"
	"testing"
)

func TestPCM16LEToSamplesPreservesSignedLittleEndianValues(t *testing.T) {
	t.Parallel()

	source := []byte{0x00, 0x00, 0x01, 0x00, 0xff, 0xff, 0x00, 0x80, 0xff, 0x7f}
	destination := make([]int16, 5)

	samples, err := PCM16LEToSamples(source, destination)
	if err != nil {
		t.Fatalf("PCM16LEToSamples() error = %v", err)
	}
	if samples != 5 {
		t.Fatalf("PCM16LEToSamples() samples = %d, want 5", samples)
	}
	want := []int16{0, 1, -1, -32768, 32767}
	for index := range want {
		if destination[index] != want[index] {
			t.Fatalf("destination[%d] = %d, want %d", index, destination[index], want[index])
		}
	}
}

func TestSamplesToPCM16LEPreservesSignedLittleEndianValues(t *testing.T) {
	t.Parallel()

	source := []int16{0, 1, -1, -32768, 32767}
	destination := make([]byte, 10)

	bytesWritten, err := SamplesToPCM16LE(source, destination)
	if err != nil {
		t.Fatalf("SamplesToPCM16LE() error = %v", err)
	}
	if bytesWritten != 10 {
		t.Fatalf("SamplesToPCM16LE() bytes = %d, want 10", bytesWritten)
	}
	want := []byte{0x00, 0x00, 0x01, 0x00, 0xff, 0xff, 0x00, 0x80, 0xff, 0x7f}
	for index := range want {
		if destination[index] != want[index] {
			t.Fatalf("destination[%d] = %#02x, want %#02x", index, destination[index], want[index])
		}
	}
}

func TestPCM16LEToSamplesRejectsOddLengthAndShortDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		source      []byte
		destination []int16
		wantErr     error
	}{
		{name: "odd length", source: []byte{0x01}, destination: make([]int16, 1), wantErr: ErrInvalidPCMBytes},
		{name: "short destination", source: []byte{0x01, 0x00, 0x02, 0x00}, destination: make([]int16, 1), wantErr: ErrDestinationTooSmall},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := PCM16LEToSamples(test.source, test.destination); !errors.Is(err, test.wantErr) {
				t.Fatalf("PCM16LEToSamples() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestSamplesToPCM16LERejectsShortDestination(t *testing.T) {
	t.Parallel()

	if _, err := SamplesToPCM16LE([]int16{1, 2}, make([]byte, 3)); !errors.Is(err, ErrDestinationTooSmall) {
		t.Fatalf("SamplesToPCM16LE() error = %v, want ErrDestinationTooSmall", err)
	}
}
