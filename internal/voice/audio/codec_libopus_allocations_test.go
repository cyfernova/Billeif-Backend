//go:build cgo && voice_libopus

package audio

import "testing"

func TestWarmedLibopusOperationsAllocateNoGoMemory(t *testing.T) {
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

	pcm := deterministicPCMFixture(0)
	var packet [MaxOpusPacketBytes]byte
	encodedBytes, err := encoder.Encode(pcm[:], packet[:])
	if err != nil {
		t.Fatalf("fixture Encode() error = %v", err)
	}
	var decoded [SamplesPerFrame]int16

	if allocations := testing.AllocsPerRun(1_000, func() {
		encodedBytes, _ = encoder.Encode(pcm[:], packet[:])
	}); allocations != 0 {
		t.Fatalf("warmed libopus Encode allocations = %.2f, want 0", allocations)
	}
	if allocations := testing.AllocsPerRun(1_000, func() {
		_, _ = decoder.Decode(packet[:encodedBytes], decoded[:])
	}); allocations != 0 {
		t.Fatalf("warmed libopus Decode allocations = %.2f, want 0", allocations)
	}
}

func BenchmarkLibopusEncode(b *testing.B) {
	encoder, err := NewLibopusEncoder()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = encoder.Close() })
	pcm := deterministicPCMFixture(0)
	var packet [MaxOpusPacketBytes]byte
	b.ReportAllocs()
	b.SetBytes(PCMBytesPerFrame)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := encoder.Encode(pcm[:], packet[:]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLibopusDecode(b *testing.B) {
	encoder, err := NewLibopusEncoder()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = encoder.Close() })
	pcm := deterministicPCMFixture(0)
	var packet [MaxOpusPacketBytes]byte
	encodedBytes, err := encoder.Encode(pcm[:], packet[:])
	if err != nil {
		b.Fatal(err)
	}
	decoder, err := NewLibopusDecoder()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = decoder.Close() })
	var destination [SamplesPerFrame]int16
	b.ReportAllocs()
	b.SetBytes(PCMBytesPerFrame)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := decoder.Decode(packet[:encodedBytes], destination[:]); err != nil {
			b.Fatal(err)
		}
	}
}
