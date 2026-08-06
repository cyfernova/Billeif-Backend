package audio

import (
	"errors"
	"strconv"
	"testing"
)

type encoderFunc func(pcm []int16, dst []byte) (int, error)

func (fn encoderFunc) Encode(pcm []int16, dst []byte) (int, error) {
	return fn(pcm, dst)
}

func TestFrameEncoderEncodesTwentyMillisecondsWithoutGrowingTheDestination(t *testing.T) {
	t.Parallel()

	encoder, err := NewFrameEncoder(encoderFunc(func(pcm []int16, dst []byte) (int, error) {
		if len(pcm) != 320 {
			t.Fatalf("Encode PCM = %d samples, want 320", len(pcm))
		}
		if len(dst) != 1276 {
			t.Fatalf("Encode destination = %d bytes, want 1276", len(dst))
		}
		dst[0], dst[1], dst[2] = 0x41, 0x42, 0x43
		return 3, nil
	}))
	if err != nil {
		t.Fatalf("NewFrameEncoder() error = %v", err)
	}

	pcm := make([]int16, 320)
	packet, err := encoder.Encode(pcm)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if string(packet) != "ABC" {
		t.Fatalf("Encode() packet = %x, want 414243", packet)
	}
}

func TestFrameEncoderRejectsPartialPCMFrame(t *testing.T) {
	t.Parallel()

	called := false
	encoder, err := NewFrameEncoder(encoderFunc(func(_ []int16, _ []byte) (int, error) {
		called = true
		return 0, nil
	}))
	if err != nil {
		t.Fatalf("NewFrameEncoder() error = %v", err)
	}

	for _, samples := range []int{319, 321} {
		packet, err := encoder.Encode(make([]int16, samples))
		if !errors.Is(err, ErrInvalidPCMFrame) {
			t.Fatalf("Encode(%d samples) error = %v, want ErrInvalidPCMFrame", samples, err)
		}
		if packet != nil {
			t.Fatalf("Encode(%d samples) packet = %v, want nil", samples, packet)
		}
	}
	if called {
		t.Fatal("codec called for a partial PCM frame")
	}
}

func TestFrameEncoderRejectsInvalidCodecByteCount(t *testing.T) {
	t.Parallel()

	for _, encodedBytes := range []int{0, 1277} {
		encodedBytes := encodedBytes
		t.Run(strconv.Itoa(encodedBytes), func(t *testing.T) {
			t.Parallel()

			encoder, err := NewFrameEncoder(encoderFunc(func(_ []int16, _ []byte) (int, error) {
				return encodedBytes, nil
			}))
			if err != nil {
				t.Fatalf("NewFrameEncoder() error = %v", err)
			}

			packet, err := encoder.Encode(make([]int16, 320))
			if !errors.Is(err, ErrInvalidEncodedBytes) {
				t.Fatalf("Encode() error = %v, want ErrInvalidEncodedBytes", err)
			}
			if packet != nil {
				t.Fatalf("Encode() packet = %v, want nil", packet)
			}
		})
	}
}

func TestNewFrameEncoderRejectsNilCodec(t *testing.T) {
	t.Parallel()

	encoder, err := NewFrameEncoder(nil)
	if !errors.Is(err, ErrNilEncoder) {
		t.Fatalf("NewFrameEncoder(nil) error = %v, want ErrNilEncoder", err)
	}
	if encoder != nil {
		t.Fatalf("NewFrameEncoder(nil) = %v, want nil", encoder)
	}
}
