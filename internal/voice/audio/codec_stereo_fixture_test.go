//go:build cgo && voice_libopus && voice_libopus_test

package audio

import "testing"

func TestLibopusDecoderDownmixesFortyEightKilohertzStereoOpus(t *testing.T) {
	decoder, err := NewLibopusDecoder()
	if err != nil {
		t.Fatalf("NewLibopusDecoder() error = %v", err)
	}
	t.Cleanup(func() { _ = decoder.Close() })

	// Deterministic 20 ms interleaved stereo fixture: speech-like square wave
	// on the left channel and silence on the right channel.
	var stereo [stereo48kSamplesPerFrame]int16
	for sample := 0; sample < 960; sample++ {
		if sample%120 < 60 {
			stereo[sample*2] = 10_000
		} else {
			stereo[sample*2] = -10_000
		}
	}
	var packet [MaxOpusPacketBytes]byte
	encodedBytes, err := encodeStereo48kFixture(stereo[:], packet[:])
	if err != nil {
		t.Fatalf("encodeStereo48kFixture() error = %v", err)
	}

	var mono [SamplesPerFrame]int16
	samples, err := decoder.Decode(packet[:encodedBytes], mono[:])
	if err != nil {
		t.Fatalf("Decode(48 kHz stereo Opus) error = %v", err)
	}
	if samples != 320 {
		t.Fatalf("Decode(48 kHz stereo Opus) samples = %d, want 320 mono samples", samples)
	}
	nonZero := 0
	for _, sample := range mono {
		if sample != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Fatal("downmixed 16 kHz mono output is entirely silent")
	}
}
