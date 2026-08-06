//go:build cgo && voice_libopus

package audio

import (
	"errors"
	"sync"
	"testing"
)

func TestLibopusRoundTripUsesTwentyMillisecondSixteenKilohertzMonoFrames(t *testing.T) {
	decoder, err := NewLibopusDecoder()
	if err != nil {
		t.Fatalf("NewLibopusDecoder() error = %v", err)
	}
	t.Cleanup(func() { _ = decoder.Close() })
	encoder, err := NewLibopusEncoder()
	if err != nil {
		t.Fatalf("NewLibopusEncoder() error = %v", err)
	}
	t.Cleanup(func() { _ = encoder.Close() })

	var packet [MaxOpusPacketBytes]byte
	var decoded [SamplesPerFrame]int16
	nonZero := 0
	for frameNumber := 0; frameNumber < 4; frameNumber++ {
		pcm := deterministicPCMFixture(frameNumber)
		encodedBytes, err := encoder.Encode(pcm[:], packet[:])
		if err != nil {
			t.Fatalf("Encode(frame %d) error = %v", frameNumber, err)
		}
		if encodedBytes <= 0 || encodedBytes > 1276 {
			t.Fatalf("Encode(frame %d) bytes = %d, want 1..1276", frameNumber, encodedBytes)
		}

		samples, err := decoder.Decode(packet[:encodedBytes], decoded[:])
		if err != nil {
			t.Fatalf("Decode(frame %d) error = %v", frameNumber, err)
		}
		if samples != 320 {
			t.Fatalf("Decode(frame %d) samples = %d, want 320", frameNumber, samples)
		}
		for _, sample := range decoded {
			if sample != 0 {
				nonZero++
			}
		}
	}
	if nonZero == 0 {
		t.Fatal("decoded deterministic speech fixture is entirely silent")
	}
}

func TestLibopusDecoderRejectsMalformedAndNonTwentyMillisecondPackets(t *testing.T) {
	t.Parallel()

	decoder, err := NewLibopusDecoder()
	if err != nil {
		t.Fatalf("NewLibopusDecoder() error = %v", err)
	}
	t.Cleanup(func() { _ = decoder.Close() })
	var pcm [SamplesPerFrame]int16

	if samples, err := decoder.Decode([]byte{0xff}, pcm[:]); samples != 0 || !errors.Is(err, ErrMalformedOpus) {
		t.Fatalf("Decode(malformed) = (%d, %v), want (0, ErrMalformedOpus)", samples, err)
	}
	if samples, err := decoder.Decode([]byte{0x00, 0x00}, pcm[:]); samples != 0 || !errors.Is(err, ErrUnexpectedOpusDuration) {
		t.Fatalf("Decode(10 ms) = (%d, %v), want (0, ErrUnexpectedOpusDuration)", samples, err)
	}
}

func TestLibopusCodecsRejectUnboundedOrMisalignedBuffers(t *testing.T) {
	t.Parallel()

	decoder, err := NewLibopusDecoder()
	if err != nil {
		t.Fatalf("NewLibopusDecoder() error = %v", err)
	}
	t.Cleanup(func() { _ = decoder.Close() })
	encoder, err := NewLibopusEncoder()
	if err != nil {
		t.Fatalf("NewLibopusEncoder() error = %v", err)
	}
	t.Cleanup(func() { _ = encoder.Close() })

	if _, err := decoder.Decode(make([]byte, 1277), make([]int16, 320)); !errors.Is(err, ErrOpusPacketTooLarge) {
		t.Fatalf("oversized Decode() error = %v, want ErrOpusPacketTooLarge", err)
	}
	if _, err := decoder.Decode([]byte{0xff}, make([]int16, 319)); !errors.Is(err, ErrDestinationTooSmall) {
		t.Fatalf("short Decode() destination error = %v, want ErrDestinationTooSmall", err)
	}
	for _, samples := range []int{319, 321} {
		if _, err := encoder.Encode(make([]int16, samples), make([]byte, 1276)); !errors.Is(err, ErrInvalidPCMFrame) {
			t.Fatalf("Encode(%d samples) error = %v, want ErrInvalidPCMFrame", samples, err)
		}
	}
	if _, err := encoder.Encode(make([]int16, 320), make([]byte, 1275)); !errors.Is(err, ErrDestinationTooSmall) {
		t.Fatalf("short Encode() destination error = %v, want ErrDestinationTooSmall", err)
	}
}

func TestLibopusCodecsCloseIdempotentlyAndFailClosedAfterClose(t *testing.T) {
	t.Parallel()

	decoder, err := NewLibopusDecoder()
	if err != nil {
		t.Fatalf("NewLibopusDecoder() error = %v", err)
	}
	encoder, err := NewLibopusEncoder()
	if err != nil {
		t.Fatalf("NewLibopusEncoder() error = %v", err)
	}
	if err := decoder.Close(); err != nil {
		t.Fatalf("first decoder Close() error = %v", err)
	}
	if err := decoder.Close(); err != nil {
		t.Fatalf("second decoder Close() error = %v", err)
	}
	if _, err := decoder.Decode([]byte{0xff}, make([]int16, 320)); !errors.Is(err, ErrCodecClosed) {
		t.Fatalf("Decode() after close error = %v, want ErrCodecClosed", err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatalf("first encoder Close() error = %v", err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatalf("second encoder Close() error = %v", err)
	}
	if _, err := encoder.Encode(make([]int16, 320), make([]byte, 1276)); !errors.Is(err, ErrCodecClosed) {
		t.Fatalf("Encode() after close error = %v, want ErrCodecClosed", err)
	}
}

func TestLibopusCodecsCloseRace(t *testing.T) {
	encoder, err := NewLibopusEncoder()
	if err != nil {
		t.Fatalf("NewLibopusEncoder() error = %v", err)
	}
	pcm := deterministicPCMFixture(0)
	var packet [MaxOpusPacketBytes]byte
	encodedBytes, err := encoder.Encode(pcm[:], packet[:])
	if err != nil {
		t.Fatalf("fixture Encode() error = %v", err)
	}
	decoder, err := NewLibopusDecoder()
	if err != nil {
		t.Fatalf("NewLibopusDecoder() error = %v", err)
	}

	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		var destination [MaxOpusPacketBytes]byte
		for iteration := 0; iteration < 100; iteration++ {
			_, _ = encoder.Encode(pcm[:], destination[:])
		}
	}()
	go func() {
		defer workers.Done()
		var destination [SamplesPerFrame]int16
		for iteration := 0; iteration < 100; iteration++ {
			_, _ = decoder.Decode(packet[:encodedBytes], destination[:])
		}
	}()
	_ = encoder.Close()
	_ = decoder.Close()
	workers.Wait()
}

func TestVerifyLibopusExercisesBothCodecDirections(t *testing.T) {
	t.Parallel()

	if err := VerifyLibopus(); err != nil {
		t.Fatalf("VerifyLibopus() error = %v", err)
	}
}

func deterministicPCMFixture(phase int) [SamplesPerFrame]int16 {
	var pcm [SamplesPerFrame]int16
	for index := range pcm {
		if (index+phase*7)%40 < 20 {
			pcm[index] = 8_000
		} else {
			pcm[index] = -8_000
		}
	}
	return pcm
}
