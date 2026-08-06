package audio

import (
	"errors"
	"testing"
)

type decoderFunc func(payload []byte, pcm []int16) (int, error)

func (fn decoderFunc) Decode(payload []byte, pcm []int16) (int, error) {
	return fn(payload, pcm)
}

func TestFrameDecoderDecodesTwentyMillisecondsAtSixteenKilohertz(t *testing.T) {
	t.Parallel()

	decoder, err := NewFrameDecoder(decoderFunc(func(payload []byte, pcm []int16) (int, error) {
		if len(payload) != 3 || payload[0] != 0x11 || payload[1] != 0x22 || payload[2] != 0x33 {
			t.Fatalf("Decode payload = %x, want 112233", payload)
		}
		if len(pcm) != 320 {
			t.Fatalf("Decode PCM capacity = %d samples, want 320", len(pcm))
		}
		for index := range pcm {
			pcm[index] = int16(index - 160)
		}
		return 320, nil
	}))
	if err != nil {
		t.Fatalf("NewFrameDecoder() error = %v", err)
	}

	pcm, err := decoder.Decode([]byte{0x11, 0x22, 0x33})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(pcm) != 320 {
		t.Fatalf("Decode() samples = %d, want 320", len(pcm))
	}
	if pcm[0] != -160 || pcm[319] != 159 {
		t.Fatalf("Decode() boundary samples = (%d, %d), want (-160, 159)", pcm[0], pcm[319])
	}
}

func TestFrameDecoderRejectsNonTwentyMillisecondOutput(t *testing.T) {
	t.Parallel()

	decoder, err := NewFrameDecoder(decoderFunc(func(_ []byte, pcm []int16) (int, error) {
		pcm[0] = 42
		return 319, nil
	}))
	if err != nil {
		t.Fatalf("NewFrameDecoder() error = %v", err)
	}

	pcm, err := decoder.Decode([]byte{0x01})
	if !errors.Is(err, ErrUnexpectedDecodedSamples) {
		t.Fatalf("Decode() error = %v, want ErrUnexpectedDecodedSamples", err)
	}
	if pcm != nil {
		t.Fatalf("Decode() PCM = %v, want nil", pcm)
	}
}

func TestFrameDecoderScrubsPCMWhenCodecFails(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("malformed Opus")
	calls := 0
	decoder, err := NewFrameDecoder(decoderFunc(func(_ []byte, pcm []int16) (int, error) {
		calls++
		if calls == 1 {
			for index := range pcm {
				pcm[index] = 7
			}
			return 320, nil
		}
		for _, sample := range pcm {
			if sample != 0 {
				t.Fatalf("second Decode PCM contains stale sample %d", sample)
			}
		}
		pcm[0] = 99
		return 0, wantErr
	}))
	if err != nil {
		t.Fatalf("NewFrameDecoder() error = %v", err)
	}
	if _, err := decoder.Decode([]byte{0x01}); err != nil {
		t.Fatalf("first Decode() error = %v", err)
	}

	pcm, err := decoder.Decode([]byte{0xff})
	if !errors.Is(err, wantErr) {
		t.Fatalf("second Decode() error = %v, want %v", err, wantErr)
	}
	if pcm != nil {
		t.Fatalf("second Decode() PCM = %v, want nil", pcm)
	}

	decoder.Reset()
}

func TestNewFrameDecoderRejectsNilCodec(t *testing.T) {
	t.Parallel()

	decoder, err := NewFrameDecoder(nil)
	if !errors.Is(err, ErrNilDecoder) {
		t.Fatalf("NewFrameDecoder(nil) error = %v, want ErrNilDecoder", err)
	}
	if decoder != nil {
		t.Fatalf("NewFrameDecoder(nil) = %v, want nil", decoder)
	}
}
